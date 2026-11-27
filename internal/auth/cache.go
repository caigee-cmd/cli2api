package auth

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

// Positive API-key lookups are cached briefly. Every public request otherwise
// costs one indexed SQLite read on a single-connection store, and a busy
// gateway fans out thousands of requests that all hit the same handful of keys.
// Console key changes invalidate the cache through control.Keys, so a disabled
// or deleted key stops working immediately; the TTL only bounds staleness for
// changes made directly in the database.
const (
	defaultKeyCacheTTL = 30 * time.Second
	maxKeyCacheTTL     = 5 * time.Minute
	maxKeyCacheEntries = 1024
	keyCacheTTLEnv     = "CLI2API_AUTH_CACHE_MS"
)

func resolveKeyCacheTTL() time.Duration {
	raw := strings.TrimSpace(os.Getenv(keyCacheTTLEnv))
	if raw == "" {
		return defaultKeyCacheTTL
	}
	ms, err := strconv.Atoi(raw)
	if err != nil {
		return defaultKeyCacheTTL
	}
	if ms <= 0 {
		return 0
	}
	ttl := time.Duration(ms) * time.Millisecond
	if ttl > maxKeyCacheTTL {
		return maxKeyCacheTTL
	}
	return ttl
}

type cachedKey struct {
	key       accounts.APIKey
	expiresAt time.Time
}

type keyCache struct {
	mu      sync.Mutex
	entries map[string]cachedKey
	ttl     time.Duration
}

func newKeyCache(ttl time.Duration) *keyCache {
	if ttl <= 0 {
		return nil
	}
	return &keyCache{entries: make(map[string]cachedKey), ttl: ttl}
}

func (c *keyCache) get(hash string, now time.Time) (accounts.APIKey, bool) {
	if c == nil {
		return accounts.APIKey{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[hash]
	if !ok {
		return accounts.APIKey{}, false
	}
	if now.After(entry.expiresAt) {
		delete(c.entries, hash)
		return accounts.APIKey{}, false
	}
	return entry.key, true
}

func (c *keyCache) put(hash string, key accounts.APIKey, now time.Time) {
	if c == nil || hash == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= maxKeyCacheEntries {
		for candidate, entry := range c.entries {
			if now.After(entry.expiresAt) {
				delete(c.entries, candidate)
			}
		}
		// A personal gateway has a handful of keys; if every slot is still live
		// the cache is being abused, so reset rather than grow without bound.
		if len(c.entries) >= maxKeyCacheEntries {
			clear(c.entries)
		}
	}
	c.entries[hash] = cachedKey{key: key, expiresAt: now.Add(c.ttl)}
}

// invalidate drops every cached key. It is called after a console key change
// so revocation never waits for a TTL.
func (c *keyCache) invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	clear(c.entries)
	c.mu.Unlock()
}
