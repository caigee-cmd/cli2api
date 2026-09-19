package auth

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

type KeyLookup interface {
	LookupAPIKey(context.Context, string) (accounts.APIKey, bool, error)
}

type Verifier struct {
	consoleKey *atomic.Pointer[string]
	keys       KeyLookup
}

func NewVerifier(consoleKey string, keys KeyLookup) Verifier {
	key := &atomic.Pointer[string]{}
	v := Verifier{consoleKey: key, keys: keys}
	v.SetConsoleKey(consoleKey)
	return v
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
	key, ok, err := v.keys.LookupAPIKey(ctx, secret)
	if err != nil || !ok || !key.Enabled {
		return Identity{}, false
	}
	return KeyIdentity(key), true
}
