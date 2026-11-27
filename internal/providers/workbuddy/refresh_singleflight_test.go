package workbuddy

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

type lockedStore struct {
	mu  sync.Mutex
	raw []byte
}

func (s *lockedStore) Get(context.Context, string) (accounts.Account, error) {
	return accounts.Account{ID: "acc1", Provider: "workbuddy", ProviderRegion: "cn"}, nil
}

func (s *lockedStore) LoadCredentialPayload(context.Context, string) (string, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return CredentialFormat, s.raw, nil
}

func (s *lockedStore) SaveCredentialPayload(_ context.Context, _ string, _ string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.raw = payload
	return nil
}

func (s *lockedStore) Observe(context.Context, string, string, string, string, string) error {
	return nil
}

func mustEncoded(t *testing.T, credential Credential) []byte {
	t.Helper()
	payload, err := credential.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

// An imported token with no expiry must not be renewed on every request.
func TestCredentialDoesNotRefreshUnknownExpiry(t *testing.T) {
	var exchanges atomic.Int32
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchanges.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"accessToken": "fresh", "expiresIn": 7200,
		}})
	}))
	store := &lockedStore{raw: mustEncoded(t, Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1"})}
	client.store = store

	for i := 0; i < 5; i++ {
		credential, err := client.credential(context.Background(), "acc1")
		if err != nil {
			t.Fatal(err)
		}
		if credential.AccessToken != "at" {
			t.Fatalf("token=%q", credential.AccessToken)
		}
	}
	if got := exchanges.Load(); got != 0 {
		t.Fatalf("renewals=%d, want 0", got)
	}
}

// Concurrent requests inside the renewal window must share a single exchange,
// because WorkBuddy rotates the refresh token.
func TestCredentialSerializesConcurrentRenewal(t *testing.T) {
	var exchanges atomic.Int32
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchanges.Add(1)
		time.Sleep(30 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"accessToken": "fresh", "expiresIn": 7200,
		}})
	}))
	client.store = &lockedStore{raw: mustEncoded(t, Credential{
		AccessToken: "at", RefreshToken: "rt", UID: "u1",
		ExpiresAt: time.Now().Add(time.Minute).Unix(),
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
	if tokens["fresh"] != 8 {
		t.Fatalf("tokens=%v, want 8 fresh", tokens)
	}
}

// A 401 retry on a token another request already rotated reuses the stored one.
func TestRefreshReusesTokenStoredByAnotherRequest(t *testing.T) {
	var exchanges atomic.Int32
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchanges.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"accessToken": "third", "expiresIn": 7200,
		}})
	}))
	client.store = &lockedStore{raw: mustEncoded(t, Credential{
		AccessToken: "fresh", RefreshToken: "rt2", UID: "u1",
		ExpiresAt: time.Now().Add(6 * time.Hour).Unix(),
	})}

	credential, err := client.Refresh(context.Background(), "acc1", Credential{
		AccessToken: "stale", RefreshToken: "rt", UID: "u1",
		ExpiresAt: time.Now().Add(6 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken != "fresh" {
		t.Fatalf("token=%q, want reuse", credential.AccessToken)
	}
	if got := exchanges.Load(); got != 0 {
		t.Fatalf("renewals=%d, want 0", got)
	}
}

// Keepalive skips a token that a normal request already renewed today.
func TestKeepaliveSkipsRecentlyRenewedToken(t *testing.T) {
	var exchanges atomic.Int32
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchanges.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"accessToken": "fresh", "expiresIn": 7200,
		}})
	}))
	client.store = &lockedStore{raw: mustEncoded(t, Credential{
		AccessToken: "at", RefreshToken: "rt", UID: "u1",
		ExpiresAt: time.Now().Add(48 * time.Hour).Unix(),
	})}

	if err := client.Keepalive(context.Background(), "acc1"); err != nil {
		t.Fatal(err)
	}
	if got := exchanges.Load(); got != 0 {
		t.Fatalf("renewals=%d, want 0 for a fresh token", got)
	}
}
