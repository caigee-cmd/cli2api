package control

import (
	"strings"
	"sync"
	"time"
)

const CatalogCacheTTL = 5 * time.Minute

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

// Catalog is the shared /v1/models and /api/models display cache. It does not
// own runtime catalogs or proven-model membership.
type Catalog struct {
	TTL      time.Duration
	Fetch    CatalogFetcher
	mu       sync.Mutex
	cache    map[string]CatalogCacheEntry
	inflight map[string]*catalogRefresh
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
		return nil, refreshing.err
	}
	return CloneModelList(refreshing.models), nil
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
			c.mu.Lock()
			c.cache[key] = CatalogCacheEntry{Models: CloneModelList(models), At: time.Now()}
			c.mu.Unlock()
		}
		c.mu.Lock()
		delete(c.inflight, key)
		c.mu.Unlock()
		close(refreshing.done)
	}()
	return refreshing
}
