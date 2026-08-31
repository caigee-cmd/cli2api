package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/config"
)

func TestWaitForWorkerAuthManagerRetriesUntilReady(t *testing.T) {
	var hits atomic.Int32
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		n := hits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "hasAuthManager": n >= 3,
		})
	}))
	defer worker.Close()

	got, err := waitForWorkerAuthManager(context.Background(), func() (string, bool) {
		return worker.URL, true
	}, time.Second, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if got != worker.URL {
		t.Fatalf("url = %s", got)
	}
	if hits.Load() < 3 {
		t.Fatalf("hits = %d", hits.Load())
	}
}

func TestWaitForWorkerAuthManagerTimesOutWhileConnecting(t *testing.T) {
	_, err := waitForWorkerAuthManager(context.Background(), func() (string, bool) {
		return "http://127.0.0.1:1", true
	}, 40*time.Millisecond, 10*time.Millisecond)
	if !errors.Is(err, errWorkerNotWarm) {
		t.Fatalf("err = %v", err)
	}
}

func TestDeviceLoginMissingAccountIsConflict(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(), RuntimeDir: t.TempDir(),
		WorkerDaemonPath: "/dev/null",
	})
	t.Cleanup(func() { _ = srv.Close() })

	req := httptest.NewRequest(http.MethodPost, "/api/accounts/missing/login/device", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDeviceLoginWaitsForAuthManagerThenOpens(t *testing.T) {
	var healthHits atomic.Int32
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			n := healthHits.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "hasAuthManager": n >= 2,
			})
		case "/admin/login/device":
			if healthHits.Load() < 2 {
				t.Fatal("login reached worker before AuthManager was ready")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "status": "pending", "authUrl": "https://qoder.com.cn/device",
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer worker.Close()

	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(), RuntimeDir: t.TempDir(),
		WorkerDaemonPath: "/dev/null",
	})
	t.Cleanup(func() { _ = srv.Close() })
	srv.pool.Upsert(accounts.Item{ID: "acc-cn", URL: worker.URL, Provider: "qoder", Region: "cn"})

	req := httptest.NewRequest(http.MethodPost, "/api/accounts/acc-cn/login/device", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		AuthURL string `json:"authUrl"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.AuthURL != "https://qoder.com.cn/device" {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

// An explicit account that is not in the pool must not fall back to the
// first running account. Without this guard, GET /v1/models?account=missing
// returns another account's catalog, misleading the client and routing
// subsequent requests to the wrong account.
func TestFetchWorkerModelsForNotFoundAccount(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(), RuntimeDir: t.TempDir(),
		WorkerDaemonPath: "/dev/null",
	})
	t.Cleanup(func() { _ = srv.Close() })
	// A running account that must never receive the query.
	srv.pool.Upsert(accounts.Item{ID: "real-acc", URL: "http://127.0.0.1:1", Provider: "qoder", Region: "global", Runtime: "child_process"})

	_, err := srv.fetchWorkerModelsFor(false, "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent account")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
}
