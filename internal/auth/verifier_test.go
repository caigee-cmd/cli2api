package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func TestVerifierAcceptsConfiguredAPIKeyHeaders(t *testing.T) {
	verifier := NewVerifier("secret", nil)
	for _, header := range []struct {
		name  string
		value string
	}{
		{"Authorization", "Bearer secret"},
		{"x-api-key", "secret"},
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
		req.Header.Set(header.name, header.value)
		identity, ok := verifier.Authenticate(context.Background(), req)
		if !ok || !identity.Console() {
			t.Fatalf("header %s was rejected", header.name)
		}
	}
}

func TestVerifierRejectsInvalidKeyAndAllowsDisabledGate(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	if _, ok := NewVerifier("secret", nil).Authenticate(context.Background(), req); ok {
		t.Fatal("invalid key was accepted")
	}
	if identity, ok := NewVerifier("", nil).Authenticate(context.Background(), req); !ok || identity.Kind != KindNone {
		t.Fatal("disabled gate rejected request")
	}
}

type stubKeys struct {
	key accounts.APIKey
	ok  bool
}

func (s stubKeys) LookupAPIKey(_ context.Context, secret string) (accounts.APIKey, bool, error) {
	if secret != "named-secret" {
		return accounts.APIKey{}, false, nil
	}
	return s.key, s.ok, nil
}

func TestVerifierAcceptsNamedAPIKey(t *testing.T) {
	verifier := NewVerifier("console", stubKeys{
		ok: true,
		key: accounts.APIKey{
			ID: "key_1", Name: "ci", Providers: []string{"qoder"}, Enabled: true,
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer named-secret")
	identity, ok := verifier.Authenticate(context.Background(), req)
	if !ok || identity.Kind != KindKey || identity.KeyID != "key_1" {
		t.Fatalf("named key identity = %+v ok=%v", identity, ok)
	}
	if identity.Console() {
		t.Fatal("named key must not unlock console routes")
	}
	if !identity.AllowsProvider("qoder") || identity.AllowsProvider("trae") {
		t.Fatalf("provider allowlist = %+v", identity.AllowedProviders)
	}
}

func TestVerifierCopiesShareRotatedConsoleKey(t *testing.T) {
	original := NewVerifier("old-key", nil)
	copied := original
	original.SetConsoleKey("  new-key  ")
	for _, v := range []Verifier{original, copied} {
		for _, tc := range []struct {
			key      string
			accepted bool
		}{{"old-key", false}, {"new-key", true}} {
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("Authorization", "Bearer "+tc.key)
			_, ok := v.Authenticate(req.Context(), req)
			if ok != tc.accepted {
				t.Fatalf("key acceptance=%v want %v", ok, tc.accepted)
			}
		}
	}
}

func TestVerifierConcurrentRotationAndAuthentication(t *testing.T) {
	original := NewVerifier("first-key", nil)
	copied := original
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			original.SetConsoleKey(fmt.Sprintf("key-%d", i))
		}
	}()
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				req := httptest.NewRequest("GET", "/", nil)
				req.Header.Set("Authorization", "Bearer "+copied.ConsoleKey())
				// Rotation may happen between these operations; acceptance is not asserted.
				copied.Authenticate(req.Context(), req)
			}
		}()
	}
	wg.Wait()
	if copied.ConsoleKey() != "key-999" {
		t.Fatal("copy did not observe final key")
	}
}
