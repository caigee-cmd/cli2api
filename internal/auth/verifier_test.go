package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerifierAcceptsConfiguredAPIKeyHeaders(t *testing.T) {
	verifier := NewVerifier("secret")
	for _, header := range []struct {
		name  string
		value string
	}{
		{"Authorization", "Bearer secret"},
		{"x-api-key", "secret"},
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
		req.Header.Set(header.name, header.value)
		if !verifier.Authorized(req) {
			t.Fatalf("header %s was rejected", header.name)
		}
	}
}

func TestVerifierRejectsInvalidKeyAndAllowsDisabledGate(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	if NewVerifier("secret").Authorized(req) {
		t.Fatal("invalid key was accepted")
	}
	if !NewVerifier("").Authorized(req) {
		t.Fatal("disabled gate rejected request")
	}
}
