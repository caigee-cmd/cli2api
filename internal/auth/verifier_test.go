package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
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
