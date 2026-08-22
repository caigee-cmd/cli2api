package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/config"
)

func TestManagementRoutesRequireAPIKey(t *testing.T) {
	srv := New(config.Config{
		Host:        "127.0.0.1",
		Port:        3010,
		ProxyAPIKey: "secret",
		QoderHome:   t.TempDir(),
	})
	h := srv.Handler()

	for _, path := range []string{
		"/api/overview",
		"/api/models",
		"/api/login/status",
		"/api/login/device",
		"/api/login/pat",
		"/api/rewarm",
		"/api/chat",
		"/api/accounts",
	} {
		method := http.MethodGet
		if strings.HasPrefix(path, "/api/login/") || path == "/api/rewarm" || path == "/api/chat" {
			method = http.MethodPost
		}
		req := httptest.NewRequest(method, path, bytes.NewReader([]byte("{}")))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without key: got %d want 401 body=%s", method, path, rec.Code, rec.Body.String())
		}
	}
}

func TestSPAFallbackServesIndexForClientRoutes(t *testing.T) {
	srv := New(config.Config{
		Host:        "127.0.0.1",
		Port:        3010,
		ProxyAPIKey: "secret",
		QoderHome:   t.TempDir(),
	})
	req := httptest.NewRequest(http.MethodGet, "/auth", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /auth: got %d want 200", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if !bytes.Contains(body, []byte(`id="root"`)) && !bytes.Contains(body, []byte("CLI2API")) {
		t.Fatalf("GET /auth did not serve SPA index: %s", string(body[:min(200, len(body))]))
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("GET /auth content-type=%q", ct)
	}
}

func TestOverviewWithAPIKeyDoesNotLeakWorkerProxyFailureAs200Auth(t *testing.T) {
	srv := New(config.Config{
		Host:        "127.0.0.1",
		Port:        3010,
		ProxyAPIKey: "secret",
		QoderHome:   t.TempDir(),
	})
	req := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/overview with key: got %d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ok"] != true {
		t.Fatalf("overview ok=%v", payload["ok"])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestSPAFallbackServesAccounts(t *testing.T) {
	srv := New(config.Config{
		Host:        "127.0.0.1",
		Port:        3010,
		ProxyAPIKey: "secret",
		QoderHome:   t.TempDir(),
	})
	req := httptest.NewRequest(http.MethodGet, "/accounts", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /accounts: got %d want 200", rec.Code)
	}
}

func TestSPAFallbackServesLogin(t *testing.T) {
	srv := New(config.Config{
		Host:        "127.0.0.1",
		Port:        3010,
		ProxyAPIKey: "secret",
		QoderHome:   t.TempDir(),
	})
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /login: got %d want 200", rec.Code)
	}
}
