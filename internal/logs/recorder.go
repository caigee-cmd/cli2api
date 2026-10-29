package logs

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

// Request-log retention. Defaults preserve the historical behaviour (7 days /
// 20k rows); deployments that want a longer usage history can raise the
// retention window and the row cap together.
const (
	defaultRetentionDays = 7
	defaultMaxLogRows    = 20_000
	retentionDaysEnv     = "CLI2API_LOG_RETENTION_DAYS"
	maxLogRowsEnv        = "CLI2API_LOG_MAX_ROWS"
)

// RetentionDays is how long request logs are kept; 0 or an invalid value falls
// back to the default.
func RetentionDays() int {
	if v := strings.TrimSpace(os.Getenv(retentionDaysEnv)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultRetentionDays
}

// MaxLogRows caps the retained rows independently of the age window, so a burst
// of traffic cannot grow the SQLite file without bound.
func MaxLogRows() int {
	if v := strings.TrimSpace(os.Getenv(maxLogRowsEnv)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultMaxLogRows
}

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
	SummarizeRequestLogsWith(ctx context.Context, opts accounts.RequestStatsOptions) (accounts.RequestStats, error)
	SummarizeRequestUsage(ctx context.Context, from, to time.Time, apiKeyID string) (accounts.RequestUsage, error)
	SummarizeModelCosts(ctx context.Context, from, to time.Time, apiKeyID string) ([]accounts.CostUsageRow, error)
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

type usageCacheEntry struct {
	usage     accounts.RequestUsage
	expiresAt time.Time
}

// statsCacheMaxEntries bounds the per-window stats cache. At ~1 small entry
// per distinct 10s window this is generous; eviction just forces a re-query.
const statsCacheMaxEntries = 256

type StatsQuery struct {
	Hours int
	From  *time.Time
	To    *time.Time
	// Dims limits which grouped lists are computed (model/account/provider);
	// empty means all. Limit caps each list (0 = overview default).
	Dims  []string
	Limit int
	// APIKeyID narrows the whole aggregate to one console API key; empty means
	// every key. It is part of the stats/usage cache key.
	APIKeyID string
}

type RequestRecorder struct {
	store        RequestPersister
	queue        chan func()
	mu           sync.Mutex
	closed       bool
	done         chan struct{}
	statsCacheMu sync.Mutex
	statsCache   map[string]statsCacheEntry
	usageCacheMu sync.Mutex
	usageCache   map[string]usageCacheEntry
	rateOnce     sync.Once
	rate         *RateCounter
}

func NewRequestRecorder(store RequestPersister) *RequestRecorder {
	recorder := &RequestRecorder{
		store: store,
		queue: make(chan func(), 256),
		done:  make(chan struct{}),
	}
	go recorder.loop()
	return recorder
}

func (r *RequestRecorder) loop() {
	defer close(r.done)
	for fn := range r.queue {
		fn()
	}
}

func (r *RequestRecorder) enqueue(fn func()) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	select {
	case r.queue <- fn:
	default:
		log.Printf("[logs] request recorder queue full, dropping write")
	}
}

func (r *RequestRecorder) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		close(r.queue)
	}
	r.mu.Unlock()
	<-r.done
}

func (r *RequestRecorder) Start(log accounts.RequestLog) {
	r.rateCounter().Add()
	r.enqueue(func() {
		if err := r.store.InsertRequestLog(context.Background(), log); err != nil {
			logf("insert request log: %v", err)
		}
	})
}

func (r *RequestRecorder) rateCounter() *RateCounter {
	if r == nil {
		return nil
	}
	r.rateOnce.Do(func() { r.rate = NewRateCounter() })
	return r.rate
}

// RecentRequests is the number of requests started in the last 60 seconds.
func (r *RequestRecorder) RecentRequests() int { return r.rateCounter().Count() }

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
		keep := time.Duration(RetentionDays()) * 24 * time.Hour
		if _, err := r.store.PurgeRequestLogs(context.Background(), keep, MaxLogRows()); err != nil {
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
	opts := accounts.RequestStatsOptions{From: *from, To: *to, Dims: query.Dims, Limit: query.Limit, APIKeyID: strings.TrimSpace(query.APIKeyID)}
	cacheKey := fmt.Sprintf("%d:%d:%s:%d:%s", from.Unix(), to.Unix(), strings.Join(query.Dims, ","), opts.EffectiveLimit(), opts.APIKeyID)
	r.statsCacheMu.Lock()
	if cached, ok := r.statsCache[cacheKey]; ok && time.Now().Before(cached.expiresAt) {
		r.statsCacheMu.Unlock()
		return cached.stats, nil
	}
	r.statsCacheMu.Unlock()
	stats, err := store.SummarizeRequestLogsWith(ctx, opts)
	if err != nil {
		return accounts.RequestStats{}, err
	}
	r.statsCacheMu.Lock()
	if r.statsCache == nil {
		r.statsCache = make(map[string]statsCacheEntry)
	}
	now = time.Now()
	// Evict expired entries and bound the map so it cannot grow forever — a
	// stats miss is cheap, an unbounded map over arbitrary time windows is not.
	for k, v := range r.statsCache {
		if !now.Before(v.expiresAt) {
			delete(r.statsCache, k)
		}
	}
	for len(r.statsCache) >= statsCacheMaxEntries {
		for k := range r.statsCache {
			delete(r.statsCache, k)
			break
		}
	}
	r.statsCache[cacheKey] = statsCacheEntry{stats: stats, expiresAt: now.Add(10 * time.Second)}
	r.statsCacheMu.Unlock()
	return stats, nil
}

func (r *RequestRecorder) Usage(ctx context.Context, query StatsQuery) (accounts.RequestUsage, error) {
	store := r.Store()
	if store == nil {
		return accounts.RequestUsage{}, fmt.Errorf("request logs unavailable")
	}
	apiKeyID := strings.TrimSpace(query.APIKeyID)
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
	cacheKey := fmt.Sprintf("%d:%d:%s", from.Unix(), to.Unix(), apiKeyID)
	r.usageCacheMu.Lock()
	if cached, ok := r.usageCache[cacheKey]; ok && time.Now().Before(cached.expiresAt) {
		r.usageCacheMu.Unlock()
		return cached.usage, nil
	}
	r.usageCacheMu.Unlock()
	usage, err := store.SummarizeRequestUsage(ctx, *from, *to, apiKeyID)
	if err != nil {
		return accounts.RequestUsage{}, err
	}
	r.usageCacheMu.Lock()
	if r.usageCache == nil {
		r.usageCache = make(map[string]usageCacheEntry)
	}
	now = time.Now()
	for k, v := range r.usageCache {
		if !now.Before(v.expiresAt) {
			delete(r.usageCache, k)
		}
	}
	for len(r.usageCache) >= statsCacheMaxEntries {
		for k := range r.usageCache {
			delete(r.usageCache, k)
			break
		}
	}
	r.usageCache[cacheKey] = usageCacheEntry{usage: usage, expiresAt: now.Add(10 * time.Second)}
	r.usageCacheMu.Unlock()
	return usage, nil
}

func (r *RequestRecorder) StatsCacheSize() int {
	if r == nil {
		return 0
	}
	r.statsCacheMu.Lock()
	defer r.statsCacheMu.Unlock()
	return len(r.statsCache)
}

// CostBreakdown prices usage in the window against official list prices where
// available, and always reports consumed quota. Live (uncached): the panel is
// opened on demand and the hourly aggregate is cheap. apiKeyID narrows the
// window to one console key ("" = every key).
func (r *RequestRecorder) CostBreakdown(ctx context.Context, from, to time.Time, apiKeyID string) (accounts.CostBreakdown, error) {
	store := r.Store()
	if store == nil {
		return accounts.CostBreakdown{}, fmt.Errorf("request logs unavailable")
	}
	rows, err := store.SummarizeModelCosts(ctx, from, to, strings.TrimSpace(apiKeyID))
	if err != nil {
		return accounts.CostBreakdown{}, err
	}
	span := to.Sub(from)
	return accounts.BuildCostBreakdown(rows, from, to, span > 48*time.Hour), nil
}

func logf(format string, args ...any) {
	log.Printf("[logs] "+format, args...)
}
