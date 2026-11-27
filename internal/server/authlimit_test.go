package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/auth"
)

func TestAuthFailureLimiterBlocksAndExpires(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	l := newAuthFailureLimiter(func() time.Time { return now })
	for i := 1; i < authFailureLimit; i++ {
		if blocked, _ := l.failure("1.2.3.4"); blocked {
			t.Fatalf("blocked after %d failures, want %d", i, authFailureLimit)
		}
	}
	blocked, retry := l.failure("1.2.3.4")
	if !blocked || retry <= 0 {
		t.Fatalf("limit failure = (%v, %v), want blocked with retry", blocked, retry)
	}
	if blocked, _ := l.blocked("1.2.3.4"); !blocked {
		t.Fatal("client should be blocked")
	}
	if blocked, _ := l.blocked("5.6.7.8"); blocked {
		t.Fatal("another client must not be blocked")
	}
	now = now.Add(authFailureBlock + time.Second)
	if blocked, _ := l.blocked("1.2.3.4"); blocked {
		t.Fatal("client should be released after cooldown")
	}
	if blocked, _ := l.failure("1.2.3.4"); blocked {
		t.Fatal("first failure after cooldown must not block again")
	}
	l.success("1.2.3.4")
	if blocked, _ := l.blocked("1.2.3.4"); blocked {
		t.Fatal("success must clear the failure state")
	}
}

func TestAPIKeyMissingCredentialsDoNotConsumeLimit(t *testing.T) {
	s := New(Server{Auth: auth.NewVerifier("right-key", nil)})
	h := s.withAPIKey(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	for i := 0; i < authFailureLimit*2; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/overview", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("missing-key attempt %d status = %d, want 401", i, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("first wrong-key status = %d, want 401", rec.Code)
	}
}

func TestAPIKeyFailureLimitReturns429(t *testing.T) {
	s := New(Server{Auth: auth.NewVerifier("right-key", nil)})
	h := s.withAPIKey(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	for i := 1; i <= authFailureLimit; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
		req.Header.Set("Authorization", "Bearer wrong")
		h.ServeHTTP(rec, req)
		want := http.StatusUnauthorized
		if i == authFailureLimit {
			want = http.StatusTooManyRequests
		}
		if rec.Code != want {
			t.Fatalf("attempt %d status = %d, want %d", i, rec.Code, want)
		}
		if i == authFailureLimit && rec.Header().Get("Retry-After") == "" {
			t.Fatal("429 must include Retry-After")
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
	req.Header.Set("Authorization", "Bearer right-key")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("correct key during cooldown status = %d, want 429", rec.Code)
	}
}

func TestAPIKeySuccessResetsFailureLimit(t *testing.T) {
	s := New(Server{Auth: auth.NewVerifier("right-key", nil)})
	h := s.withAPIKey(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	for i := 1; i < authFailureLimit; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
		req.Header.Set("Authorization", "Bearer wrong")
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", i, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
	req.Header.Set("Authorization", "Bearer right-key")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("valid key status = %d, want 204", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/overview", nil)
	req.Header.Set("Authorization", "Bearer wrong-again")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("failure after success status = %d, want 401", rec.Code)
	}
}
