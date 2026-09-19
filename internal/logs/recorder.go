package logs

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

// RequestPersister is the write surface the recorder actually uses.
// Query methods stay off this interface so a later store package can
// satisfy logs without logs depending on the concrete SQLite type.
type RequestPersister interface {
	InsertRequestLog(ctx context.Context, log accounts.RequestLog) error
	UpdateRequestLog(ctx context.Context, log accounts.RequestLog) error
	InsertRequestAttempt(ctx context.Context, attempt accounts.RequestAttempt) error
	InsertRequestStreamDiagnostic(ctx context.Context, diagnostic accounts.RequestStreamDiagnostic) error
	InsertRequestUsageDetail(ctx context.Context, detail accounts.RequestUsageDetail) error
	PurgeRequestLogs(ctx context.Context, olderThan time.Duration, maxRows int) (int64, error)
}

// RequestQuery is the console/HTTP read surface. The recorder never
// calls these methods; console handlers reach them through Store() when the
// injected value also implements RequestStore.
type RequestQuery interface {
	ClearRequestLogs(ctx context.Context) (int64, error)
	ListRequestLogs(ctx context.Context, filter accounts.RequestLogFilter) (accounts.RequestLogList, error)
	GetRequestLog(ctx context.Context, id string) (accounts.RequestLog, error)
	SummarizeRequestLogs(ctx context.Context, from, to time.Time) (accounts.RequestStats, error)
}

// RequestStore is the union the SQLite store already implements. It keeps
// query handlers on the same injected dependency as the recorder.
type RequestStore interface {
	RequestPersister
	RequestQuery
}

type statsCacheEntry struct {
	stats     accounts.RequestStats
	expiresAt time.Time
}

type StatsQuery struct {
	Hours int
	From  *time.Time
	To    *time.Time
}

type RequestRecorder struct {
	store        RequestPersister
	queue        chan func()
	statsCacheMu sync.Mutex
	statsCache   map[string]statsCacheEntry
}

func NewRequestRecorder(store RequestPersister) *RequestRecorder {
	recorder := &RequestRecorder{
		store: store,
		queue: make(chan func(), 256),
	}
	go recorder.loop()
	return recorder
}

func (r *RequestRecorder) loop() {
	for fn := range r.queue {
		fn()
	}
}

func (r *RequestRecorder) enqueue(fn func()) {
	if r == nil {
		return
	}
	select {
	case r.queue <- fn:
	default:
		log.Printf("[logs] request recorder queue full, dropping write")
	}
}

func (r *RequestRecorder) Start(log accounts.RequestLog) {
	r.enqueue(func() {
		if err := r.store.InsertRequestLog(context.Background(), log); err != nil {
			logf("insert request log: %v", err)
		}
	})
}

func (r *RequestRecorder) Finish(log accounts.RequestLog) {
	r.enqueue(func() {
		if err := r.store.UpdateRequestLog(context.Background(), log); err != nil {
			logf("update request log: %v", err)
		}
	})
}

func (r *RequestRecorder) Attempt(attempt accounts.RequestAttempt) {
	r.enqueue(func() {
		if err := r.store.InsertRequestAttempt(context.Background(), attempt); err != nil {
			logf("insert request attempt: %v", err)
		}
	})
}

func (r *RequestRecorder) StreamDiagnostic(diagnostic accounts.RequestStreamDiagnostic) {
	r.enqueue(func() {
		if err := r.store.InsertRequestStreamDiagnostic(context.Background(), diagnostic); err != nil {
			logf("insert request stream diagnostic: %v", err)
		}
	})
}

func (r *RequestRecorder) UsageDetail(detail accounts.RequestUsageDetail) {
	r.enqueue(func() {
		if err := r.store.InsertRequestUsageDetail(context.Background(), detail); err != nil {
			logf("insert request usage detail: %v", err)
		}
	})
}

func (r *RequestRecorder) PurgeLoop(stop <-chan struct{}, every time.Duration) {
	if r == nil {
		return
	}
	if every <= 0 {
		every = time.Hour
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	r.purgeOnce()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			r.purgeOnce()
		}
	}
}

func (r *RequestRecorder) purgeOnce() {
	r.enqueue(func() {
		if _, err := r.store.PurgeRequestLogs(context.Background(), 7*24*time.Hour, 20_000); err != nil {
			logf("purge request logs: %v", err)
		}
	})
}

func (r *RequestRecorder) Store() RequestStore {
	if r == nil || r.store == nil {
		return nil
	}
	store, _ := r.store.(RequestStore)
	return store
}

func NormalizeStatsHours(hours int) int {
	if hours != 1 && hours != 24 && hours != 168 {
		return 24
	}
	return hours
}

func (r *RequestRecorder) Stats(ctx context.Context, query StatsQuery) (accounts.RequestStats, error) {
	store := r.Store()
	if store == nil {
		return accounts.RequestStats{}, fmt.Errorf("request logs unavailable")
	}
	now := time.Now().UTC().Truncate(10 * time.Second)
	hours := NormalizeStatsHours(query.Hours)
	to := query.To
	if to == nil {
		value := now
		to = &value
	}
	from := query.From
	if from == nil {
		value := to.Add(-time.Duration(hours) * time.Hour)
		from = &value
	}
	cacheKey := fmt.Sprintf("%d:%d", from.Unix(), to.Unix())
	r.statsCacheMu.Lock()
	if cached, ok := r.statsCache[cacheKey]; ok && time.Now().Before(cached.expiresAt) {
		r.statsCacheMu.Unlock()
		return cached.stats, nil
	}
	r.statsCacheMu.Unlock()
	stats, err := store.SummarizeRequestLogs(ctx, *from, *to)
	if err != nil {
		return accounts.RequestStats{}, err
	}
	r.statsCacheMu.Lock()
	if r.statsCache == nil {
		r.statsCache = make(map[string]statsCacheEntry)
	}
	r.statsCache[cacheKey] = statsCacheEntry{stats: stats, expiresAt: time.Now().Add(10 * time.Second)}
	r.statsCacheMu.Unlock()
	return stats, nil
}

func (r *RequestRecorder) StatsCacheSize() int {
	if r == nil {
		return 0
	}
	r.statsCacheMu.Lock()
	defer r.statsCacheMu.Unlock()
	return len(r.statsCache)
}

func logf(format string, args ...any) {
	log.Printf("[logs] "+format, args...)
}
