package trae

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func encodeCredential(t *testing.T, credential Credential) []byte {
	t.Helper()
	payload, err := credential.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

// An unknown access-token expiry must not trigger a renewal on every request.
// The 401 path is the fallback when the token really is stale.
func TestCredentialDoesNotRefreshUnknownExpiry(t *testing.T) {
	var exchanges atomic.Int32
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchanges.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
			"Token": "fresh-at", "TokenExpireAt": 4102444800,
		}})
	}))
	store.items = map[string][]byte{"acc1": encodeCredential(t, Credential{
		AccessToken: "at", RefreshToken: "rt", UID: "u1",
	})}

	for i := 0; i < 5; i++ {
		credential, err := client.credential(context.Background(), "acc1")
		if err != nil {
			t.Fatal(err)
		}
		if credential.AccessToken != "at" {
			t.Fatalf("unexpected token %q", credential.AccessToken)
		}
	}
	if got := exchanges.Load(); got != 0 {
		t.Fatalf("unknown expiry caused %d renewals, want 0", got)
	}
}

// Concurrent requests on a nearly expired token must share one renewal: Trae
// rotates the refresh token, so parallel exchanges can invalidate the session.
func TestCredentialSerializesConcurrentRenewal(t *testing.T) {
	var exchanges atomic.Int32
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchanges.Add(1)
		time.Sleep(30 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
			"Token": "fresh-at", "TokenExpireAt": 4102444800,
		}})
	}))
	store.items = map[string][]byte{"acc1": encodeCredential(t, Credential{
		AccessToken: "at", RefreshToken: "rt", UID: "u1",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})}

	var wg sync.WaitGroup
	var mu sync.Mutex
	tokens := map[string]int{}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			credential, err := client.credential(context.Background(), "acc1")
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			tokens[credential.AccessToken]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	if got := exchanges.Load(); got != 1 {
		t.Fatalf("renewals=%d, want 1", got)
	}
	if tokens["fresh-at"] != 8 {
		t.Fatalf("tokens=%v, want 8 fresh", tokens)
	}
}

// A 401 on the already rotated token must not rotate the refresh token again.
func TestRefreshAfterRejectReusesTokenStoredByAnotherRequest(t *testing.T) {
	var exchanges atomic.Int32
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchanges.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
			"Token": "fresh-at", "TokenExpireAt": 4102444800,
		}})
	}))
	store.items = map[string][]byte{"acc1": encodeCredential(t, Credential{
		AccessToken: "fresh-at", RefreshToken: "rt2", UID: "u1", ExpiresAt: 4102444800,
	})}

	credential, err := client.refreshAfterReject(context.Background(), "acc1", Credential{
		AccessToken: "stale-at", RefreshToken: "rt", UID: "u1", ExpiresAt: 4102444800,
	})
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken != "fresh-at" {
		t.Fatalf("token=%q, want reuse", credential.AccessToken)
	}
	if got := exchanges.Load(); got != 0 {
		t.Fatalf("renewals=%d, want 0", got)
	}
}

// Keepalive has to renew a credential whose expiry is unknown exactly once per
// sweep, otherwise an idle session silently dies with the refresh token.
func TestKeepaliveRenewsUnknownExpiryOnce(t *testing.T) {
	var exchanges atomic.Int32
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchanges.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
			"Token": "fresh-at", "TokenExpireAt": 4102444800,
		}})
	}))
	store.items = map[string][]byte{"acc1": encodeCredential(t, Credential{
		AccessToken: "at", RefreshToken: "rt", UID: "u1",
	})}

	if err := client.Keepalive(context.Background(), "acc1"); err != nil {
		t.Fatal(err)
	}
	if got := exchanges.Load(); got != 1 {
		t.Fatalf("renewals=%d, want 1", got)
	}
	if err := client.Keepalive(context.Background(), "acc1"); err != nil {
		t.Fatal(err)
	}
	if got := exchanges.Load(); got != 1 {
		t.Fatalf("a known fresh expiry must not renew again, renewals=%d", got)
	}
}
