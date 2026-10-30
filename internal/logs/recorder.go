package logs

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
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

// RecentCompletion is a terminal request finish noticed since the last
// DrainRecentCompletions call. /api/live drains these so Overview can route
// busy→idle fly-outs to Done vs Failed without inventing a parallel stream.
type RecentCompletion struct {
	RequestID string
	AccountID string
	Status    string
	At        time.Time
}

// OpenRequest is an in-memory handle for /api/live waiting-queue projection.
// Same telemetry plane as RecentCompletion (RequestRecorder), not a second
// control plane. Status reuses accounts.RequestStatus* only — typically
// RequestStatusStarted from Prepare/Logs.Start until a terminal Finish.
// AccountID empty ⇒ still selecting (pre-Acquire); set by BindAccount on
// Pool.Acquire; cleared by UnbindAccount on Release; entry removed on
// terminal Finish. SelectingRequests may still project a bound request as
// selecting until selectingMinDwell after StartedAt (display-only), keeping
// AccountID so the UI can hand off queue→slot.
type OpenRequest struct {
	ID             string
	RequestedModel string
	AccountID      string
	Status         string
	StartedAt      time.Time
	// BoundAt is when BindAccount attached the pool slot (elapsed-in-cell).
	// Zero while unbound; cleared on UnbindAccount.
	BoundAt time.Time
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
	recentMu     sync.Mutex
	recent       []RecentCompletion
	openMu       sync.Mutex
	open         map[string]*OpenRequest
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
	r.noteOpen(log)
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
	r.noteCompletion(log)
	if terminalRequestStatus(log.Status) {
		r.clearOpen(log.ID)
	}
	r.enqueue(func() {
		if err := r.store.UpdateRequestLog(context.Background(), log); err != nil {
			logf("update request log: %v", err)
		}
	})
}

func terminalRequestStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case accounts.RequestStatusOK, accounts.RequestStatusError,
		accounts.RequestStatusIncomplete, accounts.RequestStatusCanceled:
		return true
	default:
		return false
	}
}

func (r *RequestRecorder) noteCompletion(log accounts.RequestLog) {
	if r == nil || !terminalRequestStatus(log.Status) {
		return
	}
	acc := strings.TrimSpace(log.AccountID)
	if acc == "" {
		return
	}
	r.recentMu.Lock()
	defer r.recentMu.Unlock()
	r.recent = append(r.recent, RecentCompletion{
		RequestID: strings.TrimSpace(log.ID),
		AccountID: acc,
		Status:    strings.TrimSpace(log.Status),
		At:        time.Now(),
	})
	const capN = 64
	if len(r.recent) > capN {
		r.recent = append([]RecentCompletion(nil), r.recent[len(r.recent)-capN:]...)
	}
}

// DrainRecentCompletions returns and clears finishes recorded since the last drain.
func (r *RequestRecorder) DrainRecentCompletions() []RecentCompletion {
	if r == nil {
		return nil
	}
	r.recentMu.Lock()
	defer r.recentMu.Unlock()
	out := r.recent
	r.recent = nil
	return out
}

const openRequestCap = 512

// selectingMinDwell keeps a request in the /api/live selecting (queue)
// projection for at least this long after Start, even after BindAccount.
// Display-only: BindAccount / Acquire / pickNext are not delayed.
// ~1.6s so Overview's expanded ~400ms /api/live poll can catch several frames
// (collapsed poll is 2.5s and would miss a 500ms window).
const selectingMinDwell = 1600 * time.Millisecond

func (r *RequestRecorder) noteOpen(log accounts.RequestLog) {
	if r == nil {
		return
	}
	id := strings.TrimSpace(log.ID)
	if id == "" {
		return
	}
	started := log.CreatedAt
	if started.IsZero() {
		started = time.Now().UTC()
	} else {
		started = started.UTC()
	}
	status := strings.TrimSpace(log.Status)
	if status == "" {
		status = accounts.RequestStatusStarted
	}
	r.openMu.Lock()
	defer r.openMu.Unlock()
	if r.open == nil {
		r.open = make(map[string]*OpenRequest)
	}
	r.open[id] = &OpenRequest{
		ID:             id,
		RequestedModel: strings.TrimSpace(log.RequestedModel),
		AccountID:      strings.TrimSpace(log.AccountID),
		Status:         status,
		StartedAt:      started,
	}
	r.trimOpenLocked()
}

// BindAccount records Pool.Acquire onto an open request (选号 → running slot).
// No-op when the request was never Start-registered.
func (r *RequestRecorder) BindAccount(requestID, accountID string) {
	if r == nil {
		return
	}
	requestID = strings.TrimSpace(requestID)
	accountID = strings.TrimSpace(accountID)
	if requestID == "" || accountID == "" {
		return
	}
	r.openMu.Lock()
	defer r.openMu.Unlock()
	if o, ok := r.open[requestID]; ok {
		o.AccountID = accountID
		if o.BoundAt.IsZero() {
			o.BoundAt = time.Now().UTC()
		}
	}
}

// UnbindAccount clears the pool bind on Release (failover returns to 选号;
// terminal Finish still deletes via clearOpen).
func (r *RequestRecorder) UnbindAccount(requestID, accountID string) {
	if r == nil {
		return
	}
	requestID = strings.TrimSpace(requestID)
	accountID = strings.TrimSpace(accountID)
	if requestID == "" {
		return
	}
	r.openMu.Lock()
	defer r.openMu.Unlock()
	o, ok := r.open[requestID]
	if !ok {
		return
	}
	if accountID == "" || o.AccountID == "" || o.AccountID == accountID {
		o.AccountID = ""
		o.BoundAt = time.Time{}
	}
}

func (r *RequestRecorder) clearOpen(requestID string) {
	if r == nil {
		return
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return
	}
	r.openMu.Lock()
	defer r.openMu.Unlock()
	delete(r.open, requestID)
}

// SelectingRequests returns open requests still in 选号 for the InflightLiveCard
// waiting queue (FIFO by StartedAt). Real binds still set AccountID on the
// open map immediately; this projection also keeps recently-bound requests
// until selectingMinDwell after StartedAt so the UI can see the wait rail.
// Bound-within-dwell entries keep AccountID so the card can fly queue→slot.
// Does not block Pool.Acquire / pickNext.
func (r *RequestRecorder) SelectingRequests() []OpenRequest {
	if r == nil {
		return nil
	}
	r.openMu.Lock()
	defer r.openMu.Unlock()
	now := time.Now().UTC()
	out := make([]OpenRequest, 0, len(r.open))
	for _, o := range r.open {
		if o == nil {
			continue
		}
		cp := *o
		if strings.TrimSpace(cp.AccountID) != "" {
			if !now.Before(cp.StartedAt.Add(selectingMinDwell)) {
				continue
			}
			// Still selecting for display; keep AccountID for queue→slot handoff.
		}
		out = append(out, cp)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].StartedAt.Before(out[j].StartedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// BoundRequests returns open requests currently attached to a pool account
// (post-Acquire), FIFO by BoundAt then StartedAt. Used by /api/live occupancy
// so InflightLiveCard can show per-concurrency-cell model + elapsed — not one
// timer for the whole account. Includes dwell-bound entries (still on the
// selecting rail) so the target cell exists for queue→slot fly-in.
func (r *RequestRecorder) BoundRequests() []OpenRequest {
	if r == nil {
		return nil
	}
	r.openMu.Lock()
	defer r.openMu.Unlock()
	out := make([]OpenRequest, 0, len(r.open))
	for _, o := range r.open {
		if o == nil || strings.TrimSpace(o.AccountID) == "" {
			continue
		}
		out = append(out, *o)
	}
	sort.SliceStable(out, func(i, j int) bool {
		bi, bj := out[i].BoundAt, out[j].BoundAt
		if bi.IsZero() {
			bi = out[i].StartedAt
		}
		if bj.IsZero() {
			bj = out[j].StartedAt
		}
		if !bi.Equal(bj) {
			return bi.Before(bj)
		}
		if !out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].StartedAt.Before(out[j].StartedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (r *RequestRecorder) trimOpenLocked() {
	if len(r.open) <= openRequestCap {
		return
	}
	// Prefer dropping unbound selecting entries first, then oldest overall.
	type ranked struct {
		id      string
		started time.Time
		bound   bool
	}
	all := make([]ranked, 0, len(r.open))
	for id, o := range r.open {
		all = append(all, ranked{id: id, started: o.StartedAt, bound: strings.TrimSpace(o.AccountID) != ""})
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].bound != all[j].bound {
			return !all[i].bound && all[j].bound // unbound first
		}
		if !all[i].started.Equal(all[j].started) {
			return all[i].started.Before(all[j].started)
		}
		return all[i].id < all[j].id
	})
	for len(r.open) > openRequestCap && len(all) > 0 {
		delete(r.open, all[0].id)
		all = all[1:]
	}
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
