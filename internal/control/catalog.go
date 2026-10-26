package control

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

const CatalogCacheTTL = 5 * time.Minute

// catalogSnapshotSecret is the app_secrets key holding the last successful
// merged catalog. It keeps /v1/models populated across a restart and after a
// failed refresh, so a transient upstream failure never empties the model list.
const catalogSnapshotSecret = "catalog_snapshot_v1"

type CatalogMode int

const (
	CatalogModeMerge CatalogMode = iota
	CatalogModeExpand
)

type CatalogCacheEntry struct {
	Models []map[string]any
	At     time.Time
}

type catalogRefresh struct {
	done   chan struct{}
	models []map[string]any
	err    error
}

// CatalogFetcher loads the raw display catalog. Runtime pool membership and
// SQLite settings stay outside this cache.
type CatalogFetcher func(refresh bool, accountID string, mode CatalogMode) ([]map[string]any, error)

// CatalogStore persists the last successful catalog snapshot. internal/store
// satisfies it; the narrow surface keeps control free of the store package.
type CatalogStore interface {
	GetSecret(ctx context.Context, name string) (string, bool, error)
	SetSecret(ctx context.Context, name, value string) error
}

type catalogSnapshot struct {
	Version  int                     `json:"version"`
	SyncedAt string                  `json:"synced_at"`
	Models   []map[string]any        `json:"models"`
	Health   []CatalogProviderHealth `json:"providers,omitempty"`
}

const catalogSnapshotVersion = 1

// Catalog is the shared /v1/models and /api/models display cache. It does not
// own runtime catalogs or proven-model membership.
type Catalog struct {
	TTL      time.Duration
	Interval time.Duration
	Fetch    CatalogFetcher
	Store    CatalogStore
	Health   *CatalogHealth

	mu       sync.Mutex
	cache    map[string]CatalogCacheEntry
	inflight map[string]*catalogRefresh
	syncedAt time.Time
	lastErr  string
}

func NewCatalog(fetch CatalogFetcher) *Catalog {
	return &Catalog{
		TTL:      CatalogCacheTTL,
		Fetch:    fetch,
		cache:    map[string]CatalogCacheEntry{},
		inflight: map[string]*catalogRefresh{},
	}
}

func CatalogCacheKey(accountID string, mode CatalogMode) string {
	view := "merged"
	if mode == CatalogModeExpand {
		view = "regional"
	}
	if strings.TrimSpace(accountID) == "" {
		return "*@" + view
	}
	return accountID + "@" + view
}

// defaultKey is the merged, account-agnostic view /v1/models serves. Only that
// view is persisted: per-account and regional shapes are console-only.
func defaultCatalogKey() string { return CatalogCacheKey("", CatalogModeMerge) }

func CloneModelList(models []map[string]any) []map[string]any {
	if models == nil {
		return nil
	}
	out := make([]map[string]any, 0, len(models))
	for _, model := range models {
		item := make(map[string]any, len(model))
		for key, value := range model {
			item[key] = value
		}
		out = append(out, item)
	}
	return out
}

func (c *Catalog) CachedCount(accountID string, mode CatalogMode) int {
	if c == nil {
		return 0
	}
	key := CatalogCacheKey(accountID, mode)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.cache[key]
	if !ok {
		return 0
	}
	return len(entry.Models)
}

// ModelContextLength resolves a model's default context window from the cached
// (or freshly fetched) merged catalog. It returns ok=false when the model or its
// window is unknown, so callers fall back to the static default.
func (c *Catalog) ModelContextLength(modelID string) (int, bool) {
	dev, _, ok := c.ModelContextWindows(modelID)
	return dev, ok
}

// ModelContextWindows returns a model's default window and its largest
// selectable window. max is 0 when the model has no larger tier. ok=false when
// the model or its window is unknown, so callers fall back to the static
// default.
func (c *Catalog) ModelContextWindows(modelID string) (dev, max int, ok bool) {
	if c == nil {
		return 0, 0, false
	}
	models, err := c.Get(false, "", CatalogModeMerge)
	if err != nil {
		return 0, 0, false
	}
	key := ModelContextKey(modelID)
	for _, model := range models {
		id, _ := model["id"].(string)
		if ModelContextKey(id) != key {
			continue
		}
		if window, ok := catalogInt(model["catalog_context_length"]); ok && window > 0 {
			dev = window
		}
		if windowMax, ok := catalogInt(model["catalog_context_length_max"]); ok && windowMax > dev {
			max = windowMax
		}
		return dev, max, dev > 0
	}
	return 0, 0, false
}

func (c *Catalog) snapshot(accountID string, mode CatalogMode) []map[string]any {
	if c == nil {
		return nil
	}
	key := CatalogCacheKey(accountID, mode)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.cache[key]
	if !ok {
		return nil
	}
	return CloneModelList(entry.Models)
}

// Get returns the display catalog for one view. A failed fetch never discards a
// catalog that already served: the last successful list is returned and the
// failure is reported through Status instead.
func (c *Catalog) Get(refresh bool, accountID string, mode CatalogMode) ([]map[string]any, error) {
	if c == nil || c.Fetch == nil {
		return nil, nil
	}
	ttl := c.TTL
	if ttl <= 0 {
		ttl = CatalogCacheTTL
	}
	key := CatalogCacheKey(accountID, mode)
	c.mu.Lock()
	entry, hasCache := c.cache[key]
	if !refresh && hasCache && time.Since(entry.At) < ttl {
		c.mu.Unlock()
		return CloneModelList(entry.Models), nil
	}
	if !refresh && hasCache {
		_ = c.startLocked(key, true, accountID, mode)
		c.mu.Unlock()
		return CloneModelList(entry.Models), nil
	}
	refreshing := c.startLocked(key, refresh, accountID, mode)
	c.mu.Unlock()
	<-refreshing.done
	if refreshing.err != nil {
		if hasCache {
			return CloneModelList(entry.Models), nil
		}
		if restored := c.snapshot(accountID, mode); len(restored) > 0 {
			return restored, nil
		}
		return nil, refreshing.err
	}
	return CloneModelList(refreshing.models), nil
}

// recordFailure keeps the last error on the default view only: the console and
// task 2's status surface describe the shared catalog, not a per-account probe.
func (c *Catalog) recordFailure(key string, err error) {
	if c == nil || err == nil || key != defaultCatalogKey() {
		return
	}
	c.mu.Lock()
	c.lastErr = strings.TrimSpace(err.Error())
	c.mu.Unlock()
}

func (c *Catalog) startLocked(key string, force bool, accountID string, mode CatalogMode) *catalogRefresh {
	if refreshing, ok := c.inflight[key]; ok {
		return refreshing
	}
	refreshing := &catalogRefresh{done: make(chan struct{})}
	c.inflight[key] = refreshing
	go func() {
		models, err := c.Fetch(force, accountID, mode)
		refreshing.models = models
		refreshing.err = err
		if err == nil {
			at := time.Now()
			c.mu.Lock()
			c.cache[key] = CatalogCacheEntry{Models: CloneModelList(models), At: at}
			if key == defaultCatalogKey() {
				c.syncedAt = at
				c.lastErr = ""
			}
			c.mu.Unlock()
			if key == defaultCatalogKey() {
				c.persist(context.Background())
			}
		} else if key == defaultCatalogKey() {
			// Record a failed background/TTL refresh too: without this the status
			// only ever reported failures of an explicitly forced refresh.
			c.recordFailure(key, err)
		}
		c.mu.Lock()
		delete(c.inflight, key)
		c.mu.Unlock()
		close(refreshing.done)
	}()
	return refreshing
}

// Status reports the shared catalog's sync state plus each provider's own.
func (c *Catalog) Status() CatalogStatus {
	if c == nil {
		return CatalogStatus{Providers: nil}
	}
	c.mu.Lock()
	syncedAt := c.syncedAt
	if entry, ok := c.cache[defaultCatalogKey()]; ok && entry.At.After(syncedAt) {
		syncedAt = entry.At
	}
	lastErr := c.lastErr
	refreshing := len(c.inflight) > 0
	c.mu.Unlock()
	interval := c.Interval
	if interval < 0 {
		interval = 0
	}
	status := CatalogStatus{
		Refreshing:      refreshing,
		LastError:       lastErr,
		IntervalSeconds: int(interval / time.Second),
		Providers:       c.Health.Snapshot(),
	}
	// A provider behind its last success makes the whole view stale even when the
	// last full refresh succeeded for the others.
	for _, provider := range status.Providers {
		if provider.Stale {
			status.Stale = true
			break
		}
	}
	if !status.Stale {
		status.Stale = lastErr != ""
	}
	if !syncedAt.IsZero() {
		status.SyncedAt = syncedAt.UTC().Format(time.RFC3339)
	}
	return status
}

// Refresh forces one full reload and reports the resulting status. The returned
// error is the fetch error for this attempt; a served catalog is still intact
// when it is non-nil.
func (c *Catalog) Refresh() (CatalogStatus, error) {
	if c == nil || c.Fetch == nil {
		return CatalogStatus{}, nil
	}
	_, err := c.Get(true, "", CatalogModeMerge)
	return c.Status(), err
}

// RunRefresher keeps the merged catalog warm: it refreshes once at start and
// then every Interval, so a newly announced upstream model reaches clients
// without waiting for someone to open the model list. A zero or negative
// interval disables the loop (manual refresh only).
func (c *Catalog) RunRefresher(stop <-chan struct{}) {
	if c == nil || c.Fetch == nil || c.Interval <= 0 {
		return
	}
	c.backgroundRefresh()
	ticker := time.NewTicker(c.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			c.backgroundRefresh()
		}
	}
}

func (c *Catalog) backgroundRefresh() {
	if c == nil || c.Fetch == nil {
		return
	}
	if _, err := c.Get(true, "", CatalogModeMerge); err != nil {
		logFailure("catalog", err)
	}
}

// persist writes the default view and provider health to app_secrets. Called
// only after a successful default-view fetch, so the stored snapshot is always
// a usable catalog.
func (c *Catalog) persist(ctx context.Context) {
	if c == nil || c.Store == nil {
		return
	}
	c.mu.Lock()
	entry, ok := c.cache[defaultCatalogKey()]
	syncedAt := c.syncedAt
	c.mu.Unlock()
	if !ok || len(entry.Models) == 0 {
		return
	}
	if syncedAt.IsZero() {
		syncedAt = entry.At
	}
	payload, err := json.Marshal(catalogSnapshot{
		Version:  catalogSnapshotVersion,
		SyncedAt: syncedAt.UTC().Format(time.RFC3339),
		Models:   entry.Models,
		Health:   c.Health.Snapshot(),
	})
	if err != nil {
		logFailure("catalog snapshot encode", err)
		return
	}
	if err := c.Store.SetSecret(ctx, catalogSnapshotSecret, string(payload)); err != nil {
		logFailure("catalog snapshot write", err)
	}
}

// Restore loads the persisted snapshot into the cache without treating it as
// fresh: the entry keeps its original timestamp, so the next read refreshes it
// in the background while still serving the last known models.
func (c *Catalog) Restore(ctx context.Context) {
	if c == nil || c.Store == nil {
		return
	}
	raw, ok, err := c.Store.GetSecret(ctx, catalogSnapshotSecret)
	if err != nil {
		logFailure("catalog snapshot read", err)
		return
	}
	if !ok || strings.TrimSpace(raw) == "" {
		return
	}
	var snapshot catalogSnapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		logFailure("catalog snapshot decode", err)
		return
	}
	if len(snapshot.Models) == 0 {
		return
	}
	at, parseErr := time.Parse(time.RFC3339, snapshot.SyncedAt)
	if parseErr != nil {
		at = time.Now().Add(-CatalogCacheTTL)
	}
	c.mu.Lock()
	key := defaultCatalogKey()
	if _, exists := c.cache[key]; !exists {
		c.cache[key] = CatalogCacheEntry{Models: CloneModelList(snapshot.Models), At: at}
		c.syncedAt = at
	}
	c.mu.Unlock()
	c.Health.Restore(snapshot.Health)
}
