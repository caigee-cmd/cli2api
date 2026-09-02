package executor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func TestBuildWorkerPayloadForwardsReasoningAndContextParameters(t *testing.T) {
	enableThinking := true
	payload := buildWorkerPayload(translate.ChatRequest{
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

func TestDecodeChatResultPreservesPromptCacheUsage(t *testing.T) {
	result, err := decodeChatResult(translate.ChatRequest{Model: "minimax-m3"}, []byte(`{
		"model":"minimax-m3",
		"choices":[{"message":{"content":"OK"},"finish_reason":"stop"}],
		"usage":{
			"prompt_tokens":100,
			"completion_tokens":20,
			"cache_read_tokens":64,
			"cache_write_tokens":12,
			"prompt_tokens_details":{"cached_tokens":64},
			"source":"upstream"
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.CacheReadTokens == nil || *result.CacheReadTokens != 64 ||
		result.CacheWriteTokens == nil || *result.CacheWriteTokens != 12 ||
		result.CachedTokens == nil || *result.CachedTokens != 64 {
		t.Fatalf("cache usage = read:%v write:%v cached:%v", result.CacheReadTokens, result.CacheWriteTokens, result.CachedTokens)
	}
}

func TestChatNonStreamFailoversRateLimit(t *testing.T) {
	var hitsA atomic.Int32
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsA.Add(1)
		w.Header().Set("X-Qoder-Account", "a")
		w.Header().Set("X-Qoder-Error-Kind", "rate_limit")
		w.Header().Set("X-Qoder-Failover", "1")
		w.Header().Set("Retry-After", "30")
		http.Error(w, `{"error":{"message":"too many requests","code":"rate_limit","kind":"rate_limit"}}`, http.StatusTooManyRequests)
	}))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Qoder-Account") == "a" {
			t.Fatalf("escaped request should not keep pinned account a")
		}
		w.Header().Set("X-Qoder-Account", "b")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "qwen3.7-plus",
			"choices": []map[string]any{{
				"finish_reason": "stop",
				"message":       map[string]any{"content": "OK"},
			}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "source": "upstream"},
		})
	}))
	defer b.Close()

	pool := accounts.NewPool([]string{a.URL, b.URL}, []string{"a", "b"})
	ex := NewChatExecutor(pool, "")
	ex.HTTPClient = a.Client()
	got, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model:    "qwen3.7-plus",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "OK" || got.AccountID != "b" {
		t.Fatalf("got %+v", got)
	}
	if hitsA.Load() != 1 {
		t.Fatalf("hitsA=%d", hitsA.Load())
	}
}

func TestChatNonStreamDoesNotFailoverAcrossQoderRegions(t *testing.T) {
	var hitsCN atomic.Int32
	globalA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Qoder-Account", "g1")
		w.Header().Set("X-Qoder-Error-Kind", "rate_limit")
		w.Header().Set("X-Qoder-Failover", "1")
		http.Error(w, `{"error":{"message":"too many requests","kind":"rate_limit"}}`, http.StatusTooManyRequests)
	}))
	defer globalA.Close()
	globalB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Qoder-Account", "g2")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"finish_reason": "stop", "message": map[string]any{"content": "OK-G"}}},
			"usage":   map[string]any{"source": "upstream"},
		})
	}))
	defer globalB.Close()
	cn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsCN.Add(1)
		t.Fatal("qoder CN must not receive global failover")
	}))
	defer cn.Close()

	pool := accounts.NewPool(nil, nil)
	pool.Upsert(accounts.Item{ID: "g1", URL: globalA.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	pool.Upsert(accounts.Item{ID: "g2", URL: globalB.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	pool.Upsert(accounts.Item{ID: "c1", URL: cn.URL, Provider: "qoder", Region: "cn", Runtime: "child_process"})
	ex := NewChatExecutor(pool, "")
	ex.HTTPClient = globalA.Client()
	got, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "qoder")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccountID != "g2" || got.Content != "OK-G" {
		t.Fatalf("got %+v", got)
	}
	if hitsCN.Load() != 0 {
		t.Fatalf("cn hits=%d", hitsCN.Load())
	}
}

func TestChatNonStreamPinnedCNDoesNotEscapeToGlobal(t *testing.T) {
	cn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Qoder-Account", "c1")
		w.Header().Set("X-Qoder-Error-Kind", "rate_limit")
		w.Header().Set("X-Qoder-Failover", "1")
		http.Error(w, `{"error":{"message":"too many requests","kind":"rate_limit"}}`, http.StatusTooManyRequests)
	}))
	defer cn.Close()
	global := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("pinned CN must not escape to global")
	}))
	defer global.Close()

	pool := accounts.NewPool(nil, nil)
	pool.Upsert(accounts.Item{ID: "c1", URL: cn.URL, Provider: "qoder", Region: "cn", Runtime: "child_process"})
	pool.Upsert(accounts.Item{ID: "g1", URL: global.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	ex := NewChatExecutor(pool, "")
	ex.HTTPClient = cn.Client()
	_, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "c1", "qoder")
	if err == nil {
		t.Fatal("expected CN rate-limit error")
	}
}

func TestChatNonStreamRoutesByAccountCatalog(t *testing.T) {
	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("account without hy3 must not be picked")
	}))
	defer missing.Close()
	hasModel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Qoder-Account", "b")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"finish_reason": "stop", "message": map[string]any{"content": "OK-HY3"}}},
			"usage":   map[string]any{"source": "upstream"},
		})
	}))
	defer hasModel.Close()

	pool := accounts.NewPool(nil, nil)
	pool.Upsert(accounts.Item{ID: "a", URL: missing.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	pool.Upsert(accounts.Item{ID: "b", URL: hasModel.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	pool.MergeModels("a", []string{"glm-5.2"})
	pool.MergeModels("b", []string{"hy3"})
	ex := NewChatExecutor(pool, "")
	ex.HTTPClient = hasModel.Client()
	got, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model: "hy3", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "qoder")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccountID != "b" || got.Content != "OK-HY3" {
		t.Fatalf("got %+v", got)
	}
}

func TestChatNonStreamUnknownModelDoesNotHitWorkers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no worker should be called when no catalog serves the model")
	}))
	defer srv.Close()
	pool := accounts.NewPool([]string{srv.URL}, []string{"a"})
	pool.MergeModels("a", []string{"glm-5.2"})
	ex := NewChatExecutor(pool, "")
	ex.HTTPClient = srv.Client()
	_, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model: "hy3", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "qoder")
	if err == nil || !strings.Contains(err.Error(), "model_not_available") {
		t.Fatalf("err=%v", err)
	}
	item, _ := pool.ByID("a")
	if !item.DownUntil.IsZero() {
		t.Fatalf("unknown model must not cool the account: %+v", item)
	}
}

func TestChatNonStreamDoesNotFailoverQuota(t *testing.T) {
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Qoder-Account", "a")
		w.Header().Set("X-Qoder-Error-Kind", "quota")
		w.Header().Set("X-Qoder-Failover", "0")
		http.Error(w, `{"error":{"message":"insufficient_quota token-limit","code":"insufficient_quota","kind":"quota"}}`, http.StatusTooManyRequests)
	}))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("quota should not failover")
	}))
	defer b.Close()
	pool := accounts.NewPool([]string{a.URL, b.URL}, []string{"a", "b"})
	ex := NewChatExecutor(pool, "")
	ex.HTTPClient = a.Client()
	_, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model:    "qwen3.7-plus",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "a", "")
	if err == nil {
		t.Fatal("expected quota error")
	}
}

func TestChatNonStreamForwardsPinnedAccount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Qoder-Account") != "acc2" {
			t.Fatalf("header=%q", r.Header.Get("X-Qoder-Account"))
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"OK"},"finish_reason":"stop"}],"usage":{"source":"upstream"}}`)
	}))
	defer srv.Close()
	pool := accounts.NewPool([]string{srv.URL}, []string{"default"})
	ex := NewChatExecutor(pool, "")
	ex.HTTPClient = srv.Client()
	got, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "acc2", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "OK" {
		t.Fatalf("got %+v", got)
	}
}

func TestChatNonStreamFailsWhenSQLitePoolIsEmpty(t *testing.T) {
	ex := NewChatExecutor(accounts.NewPool(nil, nil), "")
	_, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err == nil || !strings.Contains(err.Error(), "no worker accounts") {
		t.Fatalf("error = %v", err)
	}
}

func TestNewChatExecutorUsesProxyKeyForInternalDaemon(t *testing.T) {
	ex := NewChatExecutor(accounts.NewPool(nil, nil), "shared-secret")
	if ex.WorkerKey != "shared-secret" {
		t.Fatalf("worker key = %q", ex.WorkerKey)
	}
}

// P1#4: when every eligible account is concurrency-saturated, pick must
// return a rate-limit error (429) so the client gets a clean Retry-After
// rather than a round-trip into the worker's own 429.
func TestChatNonStreamAllSaturatedReturnsRateLimit(t *testing.T) {
	pool := accounts.NewPool([]string{"http://127.0.0.1:1", "http://127.0.0.1:2"}, []string{"a", "b"})
	pool.Upsert(accounts.Item{ID: "a", URL: "http://127.0.0.1:1", MaxInFlight: 1})
	pool.Upsert(accounts.Item{ID: "b", URL: "http://127.0.0.1:2", MaxInFlight: 1})
	pool.MergeHealth("a", true, false, 1, 0, "")
	pool.MergeHealth("b", true, false, 1, 0, "")
	ex := NewChatExecutor(pool, "")
	_, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model: "glm-5.3", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err == nil {
		t.Fatal("expected a rate-limit error when all accounts are saturated")
	}
	var classified *providers.Error
	if !errors.As(err, &classified) || classified.Kind != accounts.KindRateLimit || classified.Status != 429 {
		t.Fatalf("expected *providers.Error{rate_limit,429}, got %#v", err)
	}
}

// P1#2: a 200 on model-B must not clear a cooldown recorded for model-A.
// markOK now scopes by model, so the success path only resets model-B.
func TestChatNonStreamSuccessScopedByModelLeavesOtherModelCooled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"content":"OK"},"finish_reason":"stop"}],"usage":{"source":"upstream"}}`)
	}))
	defer srv.Close()
	pool := accounts.NewPool([]string{srv.URL}, []string{"a"})
	pool.Upsert(accounts.Item{ID: "a", URL: srv.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	// model-A is rate-limited for an hour.
	pool.MarkClassified("a", accounts.Classified{
		Kind: accounts.KindRateLimit, Cooldown: time.Hour,
		Failover: true, Model: "glm-5.3", Message: "429",
	})
	ex := NewChatExecutor(pool, "")
	ex.HTTPClient = srv.Client()
	if _, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model: "deepseek-v4-flash", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "a", ""); err != nil {
		t.Fatal(err)
	}
	item, _ := pool.ByID("a")
	if _, ok := item.ModelDownUntil["glm-5.3"]; !ok {
		t.Fatalf("glm-5.3 cooldown must survive a success on deepseek-v4-flash, got %+v", item.ModelDownUntil)
	}
	if _, ok := item.ModelDownUntil["deepseek-v4-flash"]; ok {
		t.Fatalf("deepseek-v4-flash must not carry a cooldown, got %+v", item.ModelDownUntil)
	}
}

func TestChatNonStreamCancellationDoesNotFailoverOrCooldown(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "unexpected request", http.StatusBadGateway)
	}))
	defer srv.Close()

	pool := accounts.NewPool([]string{srv.URL, srv.URL}, []string{"a", "b"})
	ex := NewChatExecutor(pool, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ex.ChatNonStream(ctx, translate.ChatRequest{
		Model:    "glm-5.3-flash",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("hits = %d, want no upstream requests", hits.Load())
	}
	for _, id := range []string{"a", "b"} {
		item, _ := pool.ByID(id)
		if !item.DownUntil.IsZero() || len(item.ModelDownUntil) != 0 {
			t.Fatalf("account %s was cooled after cancellation: %+v", id, item)
		}
	}
}

func TestChatStreamProxyDoesNotUseClientTotalTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(50 * time.Millisecond)
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	pool := accounts.NewPool([]string{srv.URL}, []string{"a"})
	ex := NewChatExecutor(pool, "")
	ex.HTTPClient = srv.Client()
	ex.HTTPClient.Timeout = 10 * time.Millisecond

	stream, err := ex.ChatStreamProxy(context.Background(), translate.ChatRequest{
		Model:    "minimax-m3",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Response.Body.Close()
	body, err := io.ReadAll(stream.Response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "data: [DONE]\n\n" {
		t.Fatalf("body = %q", body)
	}
}

// A provider can answer 200 and then fail inside the SSE body. The executor's
// attempt loop already returned, so this is the only hook that can mark the
// account down for the next request.
func TestObserveStreamFailureCoolsDownQuotaAccount(t *testing.T) {
	pool := accounts.NewPool([]string{"http://127.0.0.1:1"}, []string{"acc-quota"})
	pool.Upsert(accounts.Item{ID: "acc-quota"})
	ex := NewChatExecutor(pool, "")

	ex.ObserveStreamFailure("acc-quota", &providers.Error{
		Kind:    accounts.KindQuota,
		Status:  429,
		Message: "Your requests have exceeded the quota.",
	}, "deepseek-v4-flash")

	item, ok := pool.ByID("acc-quota")
	if !ok {
		t.Fatal("account missing")
	}
	if item.LastKind != accounts.KindQuota {
		t.Fatalf("account last kind=%q want %q", item.LastKind, accounts.KindQuota)
	}
	if item.DownUntil.IsZero() {
		t.Fatalf("quota failure must cool the whole account")
	}
	if len(item.ModelDownUntil) != 0 {
		t.Fatalf("account-scoped quota must not create model cooldowns: %v", item.ModelDownUntil)
	}
}

func TestChatNonStreamDoesNotDispatchWhileAccountCooling(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		io.WriteString(w, `{"choices":[{"message":{"content":"unexpected"}}]}`)
	}))
	defer srv.Close()

	pool := accounts.NewPool([]string{srv.URL}, []string{"a"})
	pool.MarkClassified("a", accounts.Classified{
		Kind: accounts.KindRateLimit, Cooldown: time.Hour, Failover: true,
		Model: "glm-5.3", Message: "model rate limited",
	})
	ex := NewChatExecutor(pool, "")
	ex.HTTPClient = srv.Client()
	_, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model:    "glm-5.3",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err == nil {
		t.Fatal("expected cooling error")
	}
	var classified *providers.Error
	if !errors.As(err, &classified) || classified.Kind != accounts.KindRateLimit {
		t.Fatalf("expected rate_limit cooling error, got %#v", err)
	}
	if !strings.Contains(classified.Message, "glm-5.3") {
		t.Fatalf("cooling error should name the model, got %q", classified.Message)
	}
	if hits.Load() != 0 {
		t.Fatalf("cooling account received %d requests", hits.Load())
	}
}

func TestObserveStreamFailureWithoutModelTakesAccountDown(t *testing.T) {
	pool := accounts.NewPool([]string{"http://127.0.0.1:1"}, []string{"acc-quota"})
	pool.Upsert(accounts.Item{ID: "acc-quota"})
	ex := NewChatExecutor(pool, "")

	ex.ObserveStreamFailure("acc-quota", &providers.Error{
		Kind:    accounts.KindQuota,
		Status:  429,
		Message: "Your requests have exceeded the quota.",
	}, "")

	item, _ := pool.ByID("acc-quota")
	if item.DownUntil.IsZero() {
		t.Fatal("a failure with no model must cool the whole account")
	}
}

// ObserveStreamFailure must use the same model key that PickRoute uses for
// routing. resolveProviderFilter strips "qoder/" from req.Model before the
// executor sees it, so cooldowns are stored under the bare ID. If the caller
// passes the still-prefixed publicModel, the cooldown key would be
// "qoder/glm-5.2" and routing (which queries with "glm-5.2") would never hit
// it — the account/model would be retried immediately after a stream failure.
func TestObserveStreamFailureUsesStrippedModel(t *testing.T) {
	pool := accounts.NewPool([]string{"http://127.0.0.1:1"}, []string{"a"})
	pool.Upsert(accounts.Item{ID: "a", Provider: "qoder", Region: "global", Runtime: "child_process"})
	ex := NewChatExecutor(pool, "")

	// Simulate the chat handler passing the bare model (req.Model after
	// resolveProviderFilter), not the prefixed publicModel.
	ex.ObserveStreamFailure("a", &providers.Error{
		Kind:    accounts.KindRateLimit,
		Status:  429,
		Message: "too many requests",
	}, "glm-5.2")

	item, _ := pool.ByID("a")
	// Cooldown must be stored under the bare canonical key "glm-5.2".
	until, ok := item.ModelDownUntil["glm-5.2"]
	if !ok || until.IsZero() {
		t.Fatalf("cooldown missing under glm-5.2, got %v", item.ModelDownUntil)
	}

	// Routing with the bare model must be blocked by the cooldown.
	// PickRoute returns ok=true with the cooling item as a retry-after
	// hint; the item itself must not be schedulable right now.
	picked, okRoute := pool.PickRoute(accounts.RouteQuery{
		PublicModel:    "glm-5.2",
		ProviderFilter: "qoder",
	})
	if !okRoute {
		t.Fatal("PickRoute should return the cooling account as a retry hint")
	}
	if picked.ID != "a" {
		t.Fatalf("PickRoute returned wrong account: %s", picked.ID)
	}
	// The cooling item must still be down for this model.
	if _, ok := picked.ModelDownUntil["glm-5.2"]; !ok {
		t.Fatal("PickRoute must surface the model cooldown on the returned item")
	}

	// A different model on the same account must still be available.
	_, okOther := pool.PickRoute(accounts.RouteQuery{
		PublicModel:    "deepseek-v4-flash",
		ProviderFilter: "qoder",
	})
	if !okOther {
		t.Fatal("PickRoute must still serve a different model on the same account")
	}
}

func TestObserveStreamFailureLeavesHealthyAccountAlone(t *testing.T) {
	pool := accounts.NewPool([]string{"http://127.0.0.1:1"}, []string{"acc-ok"})
	pool.Upsert(accounts.Item{ID: "acc-ok"})
	ex := NewChatExecutor(pool, "")

	// The request body was rejected; retrying elsewhere cannot help and the
	// account is not at fault, so it must stay schedulable.
	ex.ObserveStreamFailure("acc-ok", &providers.Error{
		Kind:    accounts.KindInvalidRequest,
		Status:  400,
		Message: "a message has empty content",
	}, "glm-5.3")
	// No error at all also means no state change.
	ex.ObserveStreamFailure("acc-ok", nil, "glm-5.3")
	ex.ObserveStreamFailure("", &providers.Error{Kind: accounts.KindQuota}, "glm-5.3")

	item, _ := pool.ByID("acc-ok")
	if item.LastKind != "" || !item.DownUntil.IsZero() {
		t.Fatalf("account polluted: kind=%q down=%v", item.LastKind, item.DownUntil)
	}
}
