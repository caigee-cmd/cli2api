package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

type countingKeys struct {
	calls atomic.Int32
	key   accounts.APIKey
	ok    bool
}

func (c *countingKeys) LookupAPIKey(_ context.Context, secret string) (accounts.APIKey, bool, error) {
	c.calls.Add(1)
	if secret != "named-secret" {
		return accounts.APIKey{}, false, nil
	}
	return c.key, c.ok, nil
}

func keyRequest() *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer named-secret")
	return req
}

func TestVerifierCachesPositiveLookupUntilInvalidated(t *testing.T) {
	t.Setenv(keyCacheTTLEnv, "30000")
	lookup := &countingKeys{
		ok:  true,
		key: accounts.APIKey{ID: "key_1", Name: "ci", Enabled: true, Providers: []string{"qoder"}},
	}
	verifier := NewVerifier("console", lookup)

	for i := 0; i < 3; i++ {
		if _, ok := verifier.Authenticate(context.Background(), keyRequest()); !ok {
			t.Fatal("cached key was rejected")
		}
	}
	if got := lookup.calls.Load(); got != 1 {
		t.Fatalf("lookup calls = %d, want 1", got)
	}

	verifier.InvalidateKeyCache()
	if _, ok := verifier.Authenticate(context.Background(), keyRequest()); !ok {
		t.Fatal("key was rejected after invalidation")
	}
	if got := lookup.calls.Load(); got != 2 {
		t.Fatalf("lookup calls after invalidation = %d, want 2", got)
	}
}

func TestVerifierDoesNotCacheDisabledKey(t *testing.T) {
	t.Setenv(keyCacheTTLEnv, "30000")
	lookup := &countingKeys{
		ok:  true,
		key: accounts.APIKey{ID: "key_1", Name: "ci", Enabled: false},
	}
	verifier := NewVerifier("console", lookup)
	for i := 0; i < 2; i++ {
		if _, ok := verifier.Authenticate(context.Background(), keyRequest()); ok {
			t.Fatal("disabled key was accepted")
		}
	}
	if got := lookup.calls.Load(); got != 2 {
		t.Fatalf("lookup calls = %d, want 2 (disabled keys must not be cached)", got)
	}
}

func TestVerifierCacheExpires(t *testing.T) {
	lookup := &countingKeys{
		ok:  true,
		key: accounts.APIKey{ID: "key_1", Name: "ci", Enabled: true},
	}
	verifier := NewVerifier("console", lookup)
	verifier.cache = newKeyCache(2 * time.Millisecond)

	if _, ok := verifier.Authenticate(context.Background(), keyRequest()); !ok {
		t.Fatal("key was rejected")
	}
	time.Sleep(5 * time.Millisecond)
	if _, ok := verifier.Authenticate(context.Background(), keyRequest()); !ok {
		t.Fatal("key was rejected after expiry")
	}
	if got := lookup.calls.Load(); got != 2 {
		t.Fatalf("lookup calls = %d, want 2 after expiry", got)
	}
}

func TestVerifierCacheCanBeDisabled(t *testing.T) {
	t.Setenv(keyCacheTTLEnv, "0")
	lookup := &countingKeys{
		ok:  true,
		key: accounts.APIKey{ID: "key_1", Name: "ci", Enabled: true},
	}
	verifier := NewVerifier("console", lookup)
	for i := 0; i < 2; i++ {
		verifier.Authenticate(context.Background(), keyRequest())
	}
	if got := lookup.calls.Load(); got != 2 {
		t.Fatalf("lookup calls = %d, want 2 when cache is disabled", got)
	}
}
