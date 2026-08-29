package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

type KeyLookup interface {
	LookupAPIKey(context.Context, string) (accounts.APIKey, bool, error)
}

type Verifier struct {
	consoleKey string
	keys       KeyLookup
}

func NewVerifier(consoleKey string, keys KeyLookup) Verifier {
	return Verifier{consoleKey: strings.TrimSpace(consoleKey), keys: keys}
}

func bearerSecret(r *http.Request) string {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got == "" {
		got = r.Header.Get("x-api-key")
	}
	return strings.TrimSpace(got)
}

func (v Verifier) Authenticate(ctx context.Context, r *http.Request) (Identity, bool) {
	if v.consoleKey == "" && v.keys == nil {
		return Identity{Kind: KindNone}, true
	}
	secret := bearerSecret(r)
	if secret == "" {
		return Identity{}, false
	}
	if v.consoleKey != "" && accounts.ConstantTimeEqual(secret, v.consoleKey) {
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

func (v Verifier) Authorized(r *http.Request) bool {
	_, ok := v.Authenticate(r.Context(), r)
	return ok
}
