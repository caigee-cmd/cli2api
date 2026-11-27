package auth

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

type KeyLookup interface {
	LookupAPIKey(context.Context, string) (accounts.APIKey, bool, error)
}

type Verifier struct {
	consoleKey *atomic.Pointer[string]
	keys       KeyLookup
	cache      *keyCache
}

func NewVerifier(consoleKey string, keys KeyLookup) Verifier {
	key := &atomic.Pointer[string]{}
	v := Verifier{consoleKey: key, keys: keys, cache: newKeyCache(resolveKeyCacheTTL())}
	v.SetConsoleKey(consoleKey)
	return v
}

// InvalidateKeyCache drops cached API-key identities after a console key
// change. Copies of Verifier share the cache pointer, so this reaches every
// handler copy.
func (v Verifier) InvalidateKeyCache() {
	v.cache.invalidate()
}

// ConsoleKey reads the shared live key. Copies of Verifier share this state.
func (v Verifier) ConsoleKey() string {
	if v.consoleKey == nil {
		return ""
	}
	key := v.consoleKey.Load()
	if key == nil {
		return ""
	}
	return *key
}

// SetConsoleKey rotates a verifier created by NewVerifier without replacing
// the verifier copies already held by HTTP handlers.
func (v Verifier) SetConsoleKey(secret string) {
	if v.consoleKey == nil {
		return
	}
	key := strings.TrimSpace(secret)
	v.consoleKey.Store(&key)
}

func bearerSecret(r *http.Request) string {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got == "" {
		got = r.Header.Get("x-api-key")
	}
	return strings.TrimSpace(got)
}

func (v Verifier) Authenticate(ctx context.Context, r *http.Request) (Identity, bool) {
	consoleKey := v.ConsoleKey()
	if consoleKey == "" && v.keys == nil {
		return Identity{Kind: KindNone}, true
	}
	secret := bearerSecret(r)
	if secret == "" {
		return Identity{}, false
	}
	if consoleKey != "" && accounts.ConstantTimeEqual(secret, consoleKey) {
		return ConsoleIdentity(), true
	}
	if v.keys == nil {
		return Identity{}, false
	}
	hash := accounts.HashAPIKey(secret)
	now := time.Now()
	if key, ok := v.cache.get(hash, now); ok {
		return KeyIdentity(key), true
	}
	key, ok, err := v.keys.LookupAPIKey(ctx, secret)
	if err != nil || !ok || !key.Enabled {
		return Identity{}, false
	}
	v.cache.put(hash, key, now)
	return KeyIdentity(key), true
}
