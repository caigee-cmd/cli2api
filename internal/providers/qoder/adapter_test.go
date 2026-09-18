package qoder

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

func TestAdapterDoesNotRegisterProber(t *testing.T) {
	adapter := NewClient().Adapter()
	if adapter.ID != "qoder" {
		t.Fatalf("id = %q", adapter.ID)
	}
	if adapter.Prober != nil {
		t.Fatal("S09 Adapter must omit Prober so empty-URL Qoder stays off refreshInProcess")
	}
	if adapter.Models == nil || adapter.Login == nil || adapter.Chat == nil {
		t.Fatal("expected Models, Login, and Chat wrappers")
	}
}

func TestAdapterModelsMatchesWorkerClient(t *testing.T) {
	var hits atomic.Int32
	var sawRefresh atomic.Bool
	var auth string
	var accountHeader string
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/models" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		hits.Add(1)
		if r.URL.Query().Get("refresh") == "1" {
			sawRefresh.Store(true)
		}
		auth = r.Header.Get("Authorization")
		accountHeader = r.Header.Get("X-Qoder-Account")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"id": "hy3", "mapped_key": "hy3", "display_name": "HY3"},
			{"id": "glm-5.2", "mapped_key": "gmodel", "display_name": "GLM-5.2"},
		}})
	}))
	defer worker.Close()

	direct := WorkerClient{HTTP: worker.Client(), ProxyAPIKey: "proxy-key"}
	entries, status, _, err := direct.Models(context.Background(), worker.URL, false)
	if err != nil || status != 200 {
		t.Fatalf("direct models status=%d err=%v", status, err)
	}
	wantIDs := CatalogIDs(entries, nil)

	client := NewClient()
	client.SetHTTP(worker.Client())
	client.Bind(func(string) (string, bool) { return worker.URL, true }, func() string { return "proxy-key" })
	models, err := client.Models(context.Background(), "acc-1")
	if err != nil {
		t.Fatal(err)
	}
	gotIDs := CatalogIDsFromInfos(models)
	if strings.Join(gotIDs, ",") != strings.Join(wantIDs, ",") {
		t.Fatalf("adapter ids = %v want %v", gotIDs, wantIDs)
	}
	if auth != "Bearer proxy-key" {
		t.Fatalf("auth = %q", auth)
	}
	if accountHeader != "" {
		t.Fatalf("runtime catalog must not send X-Qoder-Account, got %q", accountHeader)
	}
	if sawRefresh.Load() {
		t.Fatal("runtime catalog must call Models without refresh=1")
	}
	if hits.Load() != 2 {
		t.Fatalf("hits = %d want 2 (direct + adapter, not a dual production request)", hits.Load())
	}
}

func TestAdapterQuotaSnapshotPreservesAddOn(t *testing.T) {
	var refresh string
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/quota" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		refresh = r.URL.Query().Get("refresh")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"quota": map[string]any{
				"isQuotaExceeded": true,
				"fetchedAt":       "now",
				"userQuota":       map[string]any{"total": 100.0, "used": 100.0, "remaining": 0.0, "percentage": 100.0, "unit": "credits"},
				"addOnQuota":      map[string]any{"total": 50.0, "used": 10.0, "remaining": 40.0, "unit": "credits"},
			},
		})
	}))
	defer worker.Close()

	direct := WorkerClient{HTTP: worker.Client(), ProxyAPIKey: "k"}
	want, err := direct.Quota(context.Background(), worker.URL, true)
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient()
	client.SetHTTP(worker.Client())
	client.Bind(func(string) (string, bool) { return worker.URL, true }, func() string { return "k" })
	got, err := client.QuotaSnapshot(context.Background(), "acc-1", true)
	if err != nil {
		t.Fatal(err)
	}
	if refresh != "1" {
		t.Fatalf("refresh = %q", refresh)
	}
	if got == nil || want == nil || got.Exceeded != want.Exceeded || got.HasAddOn != want.HasAddOn || got.AddOnRemaining != want.AddOnRemaining {
		t.Fatalf("adapter quota = %+v want %+v", got, want)
	}
	if got.Exceeded {
		t.Fatal("add-on remaining must keep Exceeded false")
	}
}

func TestAdapterStartLoginWaitsForAuthManager(t *testing.T) {
	var healthHits atomic.Int32
	var deviceHits atomic.Int32
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			n := healthHits.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "hasAuthManager": n >= 2})
		case "/admin/login/device":
			if healthHits.Load() < 2 {
				t.Fatal("device login reached worker before AuthManager was ready")
			}
			deviceHits.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "status": "pending", "authUrl": "https://qoder.com.cn/device"})
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
	}))
	defer worker.Close()

	client := NewClient()
	client.SetHTTP(worker.Client())
	client.SetLoginWait(time.Second, 10*time.Millisecond)
	client.Bind(func(string) (string, bool) { return worker.URL, true }, func() string { return "k" })
	session, err := client.StartLogin(context.Background(), "acc-cn")
	if err != nil {
		t.Fatal(err)
	}
	if session.AuthURL != "https://qoder.com.cn/device" {
		t.Fatalf("auth url = %q", session.AuthURL)
	}
	if deviceHits.Load() != 1 {
		t.Fatalf("device hits = %d", deviceHits.Load())
	}
}

func TestAdapterChatRequestMatchesNewChatRequest(t *testing.T) {
	var gotPath, gotAuth, gotAccount, gotContentType string
	var body []byte
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAccount = r.Header.Get("X-Qoder-Account")
		gotContentType = r.Header.Get("Content-Type")
		body, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "glm-5.2",
			"choices": []map[string]any{{
				"finish_reason": "stop",
				"message":       map[string]any{"content": "hi", "reasoning_content": "think"},
			}},
			"usage": map[string]any{"prompt_tokens": 3, "completion_tokens": 1, "source": "provider", "credits": 0.5},
		})
	}))
	defer worker.Close()

	req := translate.ChatRequest{Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}}
	wantPayload, err := json.Marshal(BuildChatPayload(req, false))
	if err != nil {
		t.Fatal(err)
	}
	direct, err := NewChatRequest(context.Background(), worker.URL, "acc-1", "", "worker-key", wantPayload)
	if err != nil {
		t.Fatal(err)
	}

	client := NewClient()
	client.SetHTTP(worker.Client())
	client.Bind(func(string) (string, bool) { return worker.URL, true }, func() string { return "worker-key" })
	outcome, err := client.ChatNonStream(context.Background(), "acc-1", req)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != direct.URL.Path {
		t.Fatalf("path = %s want %s", gotPath, direct.URL.Path)
	}
	if gotAuth != direct.Header.Get("Authorization") || gotAccount != direct.Header.Get("X-Qoder-Account") || gotContentType != "application/json" {
		t.Fatalf("headers auth=%q account=%q type=%q", gotAuth, gotAccount, gotContentType)
	}
	if string(body) != string(wantPayload) {
		t.Fatalf("body = %s want %s", body, wantPayload)
	}
	if outcome.Content != "hi" || outcome.Reasoning != "think" || outcome.PromptTokens != 3 || outcome.UsageSource != "provider" {
		t.Fatalf("outcome = %+v", outcome)
	}
}
