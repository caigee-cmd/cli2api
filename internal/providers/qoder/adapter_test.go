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

	"github.com/caigee-cmd/cli2api/internal/providers"
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

func TestBuildChatPayloadForwardsReasoningAndContextParameters(t *testing.T) {
	enableThinking := true
	payload := BuildChatPayload(translate.ChatRequest{
		Model:                 "minimax-m3",
		Messages:              []translate.ChatMessage{{Role: "user", Content: "hi"}},
		EnableThinking:        &enableThinking,
		ReasoningEffort:       json.RawMessage(`"high"`),
		ReasoningBudgetTokens: json.RawMessage(`16384`),
		ContextLength:         json.RawMessage(`500000`),
		MaxInputTokens:        json.RawMessage(`1000000`),
	}, true)

	if payload["enable_thinking"] != true {
		t.Fatalf("enable_thinking = %#v", payload["enable_thinking"])
	}
	for key, want := range map[string]string{
		"reasoning_effort":        `"high"`,
		"reasoning_budget_tokens": "16384",
		"context_length":          "500000",
		"max_input_tokens":        "1000000",
	} {
		got, ok := payload[key].(json.RawMessage)
		if !ok || string(got) != want {
			t.Fatalf("%s = %#v, want %s", key, payload[key], want)
		}
	}
}

func TestBuildChatPayloadTokenPrecedenceAndOmission(t *testing.T) {
	for _, stream := range []bool{false, true} {
		req := translate.ChatRequest{Model: "m", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}}
		payload := BuildChatPayload(req, stream)
		if len(payload) != 3 || payload["stream"] != stream || payload["model"] != "m" {
			t.Fatalf("minimal payload=%+v", payload)
		}
		req.MaxTokens = json.RawMessage(`12`)
		payload = BuildChatPayload(req, stream)
		if string(payload["max_tokens"].(json.RawMessage)) != "12" {
			t.Fatalf("max_tokens=%v", payload["max_tokens"])
		}
		req.MaxCompletionTokens = json.RawMessage(`34`)
		payload = BuildChatPayload(req, stream)
		if string(payload["max_tokens"].(json.RawMessage)) != "34" {
			t.Fatalf("completion token precedence=%v", payload["max_tokens"])
		}
		no := false
		req.EnableThinking = &no
		req.EnableReasoning = &no
		req.IsReasoning = &no
		req.ParallelToolCalls = &no
		payload = BuildChatPayload(req, stream)
		for _, key := range []string{"enable_thinking", "enable_reasoning", "is_reasoning", "parallel_tool_calls"} {
			if value, ok := payload[key]; !ok || value != false {
				t.Fatalf("explicit false %s=%v present=%v", key, value, ok)
			}
		}
	}
}

func TestModelInfosMapsQoderPriceFields(t *testing.T) {
	models := ModelInfos([]map[string]any{
		{"id": "kimi-k3", "mapped_key": "kmodel_latest", "display_name": "Kimi-K3", "price_factor": 1.4},
		{"id": "qwen3.7-plus", "mapped_key": "qmodel", "display_name": "Qwen3.7-Plus", "price_factor": 0.1},
		{"id": "qwen3.8-flash", "mapped_key": "qfmodel", "display_name": "Qwen3.8-Flash", "price_factor": 0.0, "is_free": true},
		{"id": "qwen3.8-max", "mapped_key": "qmodel_38max", "display_name": "Qwen3.8-Max", "price_factor": 0.5, "is_free": true},
		{"id": "auto", "mapped_key": "auto", "display_name": "Auto", "price_factor": 0.5},
		// No price data at all: neither credits nor free may be invented.
		{"id": "no-price", "mapped_key": "npmodel", "display_name": "NoPrice"},
	})

	byID := map[string]providers.ModelInfo{}
	for _, m := range models {
		byID[m.PublicModel] = m
	}

	if got := byID["kimi-k3"].Credits; got != "x1.4" {
		t.Errorf("kimi-k3 credits = %q, want x1.4", got)
	}
	if got := byID["qwen3.7-plus"].Credits; got != "x0.1" {
		t.Errorf("qwen3.7-plus credits = %q, want x0.1", got)
	}
	if got := byID["auto"].Credits; got != "x0.5" {
		t.Errorf("auto credits = %q, want x0.5", got)
	}

	free := byID["qwen3.8-flash"]
	if !free.Free || free.Credits != "0" {
		t.Errorf("qwen3.8-flash free=%v credits=%q, want free + \"0\"", free.Free, free.Credits)
	}
	// A dual is_free + positive factor model (Qwen3.8-Max) is NOT free: the
	// Qoder client still labels it with its multiplier.
	dual := byID["qwen3.8-max"]
	if dual.Free || dual.Credits != "x0.5" {
		t.Errorf("qwen3.8-max free=%v credits=%q, want not-free + x0.5", dual.Free, dual.Credits)
	}
	for id, m := range byID {
		if id == "qwen3.8-flash" {
			continue
		}
		if m.Free {
			t.Errorf("%s must not be flagged free", id)
		}
	}
	if priced := byID["no-price"]; priced.Credits != "" || priced.Free {
		t.Errorf("no-price must stay unpriced: credits=%q free=%v", priced.Credits, priced.Free)
	}
}

func TestModelInfosPrefersExplicitCreditsText(t *testing.T) {
	models := ModelInfos([]map[string]any{
		{"id": "x", "mapped_key": "xk", "display_name": "X", "credits": "2x credits", "price_factor": 0.5},
	})
	if len(models) != 1 || models[0].Credits != "2x credits" {
		t.Fatalf("explicit credits text must win: %+v", models)
	}
}

func TestApplyModelPricingMapsWorkerFields(t *testing.T) {
	entry := map[string]any{"id": "kimi-k3", "price_factor": 1.4}
	ApplyModelPricing(entry)
	if entry["credits"] != "x1.4" {
		t.Errorf("credits = %v, want x1.4", entry["credits"])
	}
	if _, ok := entry["free"]; ok {
		t.Errorf("paid model must not be flagged free: %v", entry["free"])
	}

	freeEntry := map[string]any{"id": "qwen3.8-flash", "price_factor": 0.0, "is_free": true}
	ApplyModelPricing(freeEntry)
	if freeEntry["credits"] != "0" || freeEntry["free"] != true {
		t.Errorf("free model = %+v, want credits 0 + free", freeEntry)
	}

	// is_free=true with a positive factor (Qwen3.8-Max) must NOT be free and
	// must show the multiplier, matching the Qoder client label.
	dual := map[string]any{"id": "qwen3.8-max", "price_factor": 0.5, "is_free": true}
	ApplyModelPricing(dual)
	if dual["credits"] != "x0.5" {
		t.Errorf("dual credits = %v, want x0.5", dual["credits"])
	}
	if _, ok := dual["free"]; ok {
		t.Errorf("dual model must not be flagged free: %v", dual["free"])
	}

	// limited_time_free tag is the Qoder free signal.
	tagged := map[string]any{"id": "tagged", "price_factor": 0.5, "tags": []any{"limited_time_free"}}
	ApplyModelPricing(tagged)
	if tagged["credits"] != "0" || tagged["free"] != true {
		t.Errorf("limited_time_free = %+v, want credits 0 + free", tagged)
	}

	unpriced := map[string]any{"id": "unknown"}
	ApplyModelPricing(unpriced)
	if _, ok := unpriced["credits"]; ok {
		t.Errorf("unpriced model must not gain credits: %+v", unpriced)
	}
	if _, ok := unpriced["free"]; ok {
		t.Errorf("unpriced model must not be flagged free: %+v", unpriced)
	}
}
