package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestDecodeChatResultFallsBackToPromptCacheHitTokens(t *testing.T) {
	result, err := decodeChatResult(translate.ChatRequest{Model: "deepseek-v4.1"}, []byte(`{
			"model":"deepseek-v4.1",
			"choices":[{"message":{"content":"OK"},"finish_reason":"stop"}],
			"usage":{
				"prompt_tokens":100,
				"completion_tokens":20,
				"prompt_cache_hit_tokens":80,
				"source":"upstream"
			}
		}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.CacheReadTokens == nil || *result.CacheReadTokens != 80 ||
		result.CachedTokens == nil || *result.CachedTokens != 80 {
		t.Fatalf("cache usage = read:%v cached:%v", result.CacheReadTokens, result.CachedTokens)
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

	pool := NewPool([]string{a.URL, b.URL}, []string{"a", "b"})
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

	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "g1", URL: globalA.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	pool.Upsert(Item{ID: "g2", URL: globalB.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	pool.Upsert(Item{ID: "c1", URL: cn.URL, Provider: "qoder", Region: "cn", Runtime: "child_process"})
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
	if n := pool.LenRoute(RouteQuery{ProviderFilter: "qoder", RegionFilter: "global", Excluded: map[string]struct{}{"g1": {}}}); n != 1 {
		t.Fatalf("after first global failure, remaining global candidates = %d", n)
	}
	if n := pool.LenRoute(RouteQuery{ProviderFilter: "qoder", RegionFilter: "cn"}); n != 1 {
		t.Fatalf("cn candidates after global pin = %d", n)
	}
}

// A bare model shared by two providers must fail over to the other provider.
// The first pick may pin a region for cache affinity, but the pin is released
// on failure because this route is not provider-scoped.
func TestChatNonStreamCrossProviderFailoverReleasesRegionPin(t *testing.T) {
	qoder := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Qoder-Account", "q1")
		w.Header().Set("X-Qoder-Error-Kind", "rate_limit")
		w.Header().Set("X-Qoder-Failover", "1")
		http.Error(w, `{"error":{"message":"too many requests","kind":"rate_limit"}}`, http.StatusTooManyRequests)
	}))
	defer qoder.Close()
	workbuddy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"model":"shared","choices":[{"message":{"content":"OK-W"},"finish_reason":"stop"}],"usage":{"source":"upstream"}}`)
	}))
	defer workbuddy.Close()

	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "q1", URL: qoder.URL, Provider: "qoder", Region: "global", Runtime: "child_process", Models: []string{"shared"}})
	pool.Upsert(Item{ID: "w1", URL: workbuddy.URL, Provider: "workbuddy", Region: "cn", Runtime: "child_process", Models: []string{"shared"}})
	ex := NewChatExecutor(pool, "")
	ex.HTTPClient = qoder.Client()

	got, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model: "shared", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccountID != "w1" || got.Provider != "workbuddy" || got.AttemptCount != 2 {
		t.Fatalf("got %+v, want workbuddy w1 on attempt 2", got)
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

	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "c1", URL: cn.URL, Provider: "qoder", Region: "cn", Runtime: "child_process"})
	pool.Upsert(Item{ID: "g1", URL: global.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
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

	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "a", URL: missing.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	pool.Upsert(Item{ID: "b", URL: hasModel.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
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
	pool := NewPool([]string{srv.URL}, []string{"a"})
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

// The first pick pins the region for the rest of the request, but the retry
// budget must stay the one computed before that pick. Recomputing it against
// the pinned region can leave zero failover hops even though another region
// (or provider) is healthy, which is exactly "quota exhausted but no switch".
func TestRouteFailoverKeepsAttemptBudgetAndCrossesRegion(t *testing.T) {
	p := NewPool(nil, nil)
	p.Upsert(Item{ID: "g1", Provider: "workbuddy", Region: "global", Models: []string{"shared"}})
	p.Upsert(Item{ID: "c1", Provider: "workbuddy", Region: "cn", Models: []string{"shared"}})
	e := ChatExecutor{Pool: p}

	loop := e.newRouteLoop(context.Background(), "", "", translate.ChatRequest{Model: "shared"})
	if loop.attempts != 2 {
		t.Fatalf("attempts=%d, want 2", loop.attempts)
	}
	first, _, err := loop.pickNext(e, "shared")
	if err != nil {
		t.Fatalf("first pick: %v", err)
	}
	if loop.attempts != 2 {
		t.Fatalf("attempt budget shrank after region pin: %d", loop.attempts)
	}

	loop.failover(e, first, Classified{Kind: "unavailable"})
	second, _, err := loop.pickNext(e, "shared")
	if err != nil {
		t.Fatalf("failover pick: %v", err)
	}
	if second.Region == first.Region {
		t.Fatalf("failover stayed in failed region %s: %+v", first.Region, second)
	}
}

func TestChatNonStreamPromptLimitDoesNotCoolOrFailover(t *testing.T) {
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Qoder-Account", "a")
		w.Header().Set("X-Qoder-Error-Kind", "quota")
		w.Header().Set("X-Qoder-Failover", "0")
		http.Error(w, `{"error":{"message":"insufficient_quota token-limit","code":"insufficient_quota","kind":"quota"}}`, http.StatusTooManyRequests)
	}))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("prompt limit should not failover")
	}))
	defer b.Close()
	pool := NewPool([]string{a.URL, b.URL}, []string{"a", "b"})
	ex := NewChatExecutor(pool, "")
	ex.HTTPClient = a.Client()
	_, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model:    "qwen3.7-plus",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "a", "")
	if err == nil {
		t.Fatal("expected prompt limit error")
	}
	item, _ := pool.ByID("a")
	if !item.DownUntil.IsZero() {
		t.Fatalf("prompt limit must not cool account: %+v", item)
	}
}

func TestChatNonStreamHardQuotaCoolsAccount(t *testing.T) {
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Qoder-Account", "a")
		w.Header().Set("X-Qoder-Error-Kind", "quota")
		w.Header().Set("X-Qoder-Failover", "0")
		http.Error(w, `{"error":{"message":"account quota exhausted","code":"insufficient_quota","kind":"quota"}}`, http.StatusTooManyRequests)
	}))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("hard quota should not failover current request")
	}))
	defer b.Close()
	pool := NewPool([]string{a.URL, b.URL}, []string{"a", "b"})
	ex := NewChatExecutor(pool, "")
	ex.HTTPClient = a.Client()
	_, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model:    "qwen3.7-plus",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "a", "")
	if err == nil {
		t.Fatal("expected hard quota error")
	}
	item, _ := pool.ByID("a")
	if item.DownUntil.IsZero() {
		t.Fatalf("hard quota must cool account: %+v", item)
	}
	until := item.DownUntil.In(time.Local)
	if until.Hour() != 0 || until.Minute() != 0 || until.Second() != 0 {
		t.Fatalf("hard quota cooldown must end at local midnight: %s", item.DownUntil)
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
	pool := NewPool([]string{srv.URL}, []string{"default"})
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
	ex := NewChatExecutor(NewPool(nil, nil), "")
	_, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err == nil || !strings.Contains(err.Error(), "no worker accounts") {
		t.Fatalf("error = %v", err)
	}
}

func TestNewChatExecutorUsesProxyKeyForInternalDaemon(t *testing.T) {
	ex := NewChatExecutor(NewPool(nil, nil), "shared-secret")
	if ex.WorkerKey != "shared-secret" {
		t.Fatalf("worker key = %q", ex.WorkerKey)
	}
}

// P1#4: when every eligible account is concurrency-saturated, pick must
// return a rate-limit error (429) so the client gets a clean Retry-After
// rather than a round-trip into the worker's own 429.
func TestChatNonStreamAllSaturatedReturnsRateLimit(t *testing.T) {
	pool := NewPool([]string{"http://127.0.0.1:1", "http://127.0.0.1:2"}, []string{"a", "b"})
	pool.Upsert(Item{ID: "a", URL: "http://127.0.0.1:1", MaxInFlight: 1})
	pool.Upsert(Item{ID: "b", URL: "http://127.0.0.1:2", MaxInFlight: 1})
	pool.MergeHealth("a", true, false, 0, 0, "")
	pool.MergeHealth("b", true, false, 0, 0, "")
	if !pool.Acquire("a") || !pool.Acquire("b") {
		t.Fatal("acquire a/b")
	}
	ex := NewChatExecutor(pool, "")
	_, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model: "glm-5.3", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err == nil {
		t.Fatal("expected a rate-limit error when all accounts are saturated")
	}
	var classified *ExecutionError
	if !errors.As(err, &classified) || classified.Classified.Kind != accounts.KindRateLimit || classified.Classified.Status != 429 {
		t.Fatalf("expected *ExecutionError{rate_limit,429}, got %#v", err)
	}
	if classified.Classified.RetryAfter != 5*time.Second {
		t.Fatalf("capacity retry-after=%s", classified.Classified.RetryAfter)
	}
}

// P1#2: a 200 on model-B must not clear a cooldown recorded for model-A.
// markOK now scopes by model, so the success path only resets model-B.
func TestChatNonStreamSuccessScopedByModelLeavesOtherModelCooled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"content":"OK"},"finish_reason":"stop"}],"usage":{"source":"upstream"}}`)
	}))
	defer srv.Close()
	pool := NewPool([]string{srv.URL}, []string{"a"})
	pool.Upsert(Item{ID: "a", URL: srv.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	// model-A is rate-limited for an hour.
	pool.MarkClassified("a", Classified{
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

	pool := NewPool([]string{srv.URL, srv.URL}, []string{"a", "b"})
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

	pool := NewPool([]string{srv.URL}, []string{"a"})
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

// An upstream can answer 200 and close the body before the first SSE event.
// Nothing has reached the client yet at that point, so the executor must cool
// the account down and retry the request on the next one instead of relaying
// an empty stream as a success.
func TestChatStreamProxyFailsOverEmptyFirstStream(t *testing.T) {
	var aHits, bHits atomic.Int32
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	}))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer b.Close()

	pool := NewPool([]string{a.URL, b.URL}, []string{"a", "b"})
	ex := NewChatExecutor(pool, "")
	ex.HTTPClient = a.Client()

	stream, err := ex.ChatStreamProxy(context.Background(), translate.ChatRequest{
		Model:    "minimax-m3",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "a", "")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Response.Body.Close()
	if stream.AccountID != "b" || aHits.Load() != 1 || bHits.Load() != 1 {
		t.Fatalf("account=%q aHits=%d bHits=%d", stream.AccountID, aHits.Load(), bHits.Load())
	}
	body, err := io.ReadAll(stream.Response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"content":"hi"`) || !strings.Contains(string(body), "[DONE]") {
		t.Fatalf("probe must replay every byte of the retried stream, body=%q", body)
	}
	item, _ := pool.ByID("a")
	if item.DownUntil.IsZero() {
		t.Fatalf("empty-stream account must be cooled: %+v", item)
	}
}

// The worker turns an upstream failure before the first token into a structured
// SSE error event. That event arrives before anything reaches the client, so it
// must trigger a retry on another account instead of ending the stream.
func TestChatStreamProxyFailsOverStructuredErrorFirstEvent(t *testing.T) {
	var bHits atomic.Int32
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "event: error\ndata: {\"error\":{\"message\":\"upstream_stream_interrupted: socket closed\",\"type\":\"api_error\",\"code\":\"upstream_stream_interrupted\",\"kind\":\"unavailable\",\"status\":502,\"failover\":true}}\n\n")
	}))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"recovered\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer b.Close()

	pool := NewPool([]string{a.URL, b.URL}, []string{"a", "b"})
	ex := NewChatExecutor(pool, "")
	ex.HTTPClient = a.Client()

	stream, err := ex.ChatStreamProxy(context.Background(), translate.ChatRequest{
		Model:    "minimax-m3",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "a", "")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Response.Body.Close()
	if stream.AccountID != "b" || bHits.Load() != 1 {
		t.Fatalf("account=%q bHits=%d", stream.AccountID, bHits.Load())
	}
	body, err := io.ReadAll(stream.Response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "recovered") {
		t.Fatalf("body=%q", body)
	}
}

// A client that disconnects while the probe is still waiting for the first
// event must be reported as a cancellation. Recording it as a success would
// mark the account healthy and invite the next request onto the same stream.
func TestChatStreamProxyProbeCancelDoesNotMarkAccountHealthy(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-release
		<-r.Context().Done()
	}))
	defer srv.Close()

	pool := NewPool([]string{srv.URL}, []string{"a"})
	ex := NewChatExecutor(pool, "")
	ex.HTTPClient = srv.Client()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		// Headers are out, so the executor is blocked inside the probe.
		time.Sleep(200 * time.Millisecond)
		cancel()
		close(release)
	}()
	stream, err := ex.ChatStreamProxy(ctx, translate.ChatRequest{
		Model:    "minimax-m3",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "a", "")
	if err != nil {
		t.Fatalf("probe must hand a canceled request back to the relay, err=%v", err)
	}
	defer stream.Response.Body.Close()
	// The body ends because the request was canceled. The transport may report
	// that as the context error or as a closed connection; either way it must
	// not come back as a completed stream.
	if _, err := io.ReadAll(stream.Response.Body); err == nil {
		t.Fatal("canceled probe returned a completed stream")
	}
	item, _ := pool.ByID("a")
	if !item.DownUntil.IsZero() || item.LastKind != "" {
		t.Fatalf("cancellation must not cool or relabel the account: %+v", item)
	}
}

// A provider can answer 200 and then fail inside the SSE body. The executor's
// attempt loop already returned, so this is the only hook that can mark the
// account down for the next request.
func TestObserveStreamFailureCoolsDownQuotaAccount(t *testing.T) {
	pool := NewPool([]string{"http://127.0.0.1:1"}, []string{"acc-quota"})
	pool.Upsert(Item{ID: "acc-quota"})
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
	until := item.DownUntil.In(time.Local)
	if until.Hour() != 0 || until.Minute() != 0 || until.Second() != 0 {
		t.Fatalf("quota cooldown must end at local midnight: %s", item.DownUntil)
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

	pool := NewPool([]string{srv.URL}, []string{"a"})
	pool.MarkClassified("a", Classified{
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
	var classified *ExecutionError
	if !errors.As(err, &classified) || classified.Classified.Kind != accounts.KindRateLimit {
		t.Fatalf("expected rate_limit cooling error, got %#v", err)
	}
	if !strings.Contains(classified.Classified.Message, "glm-5.3") {
		t.Fatalf("cooling error should name the model, got %q", classified.Classified.Message)
	}
	if hits.Load() != 0 {
		t.Fatalf("cooling account received %d requests", hits.Load())
	}
}

func TestChatNonStreamUsesProvenAccountOverQuotaCatalog(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"finish_reason": "stop", "message": map[string]any{"content": "ok"}}},
			"usage":   map[string]any{"source": "upstream"},
		})
	}))
	defer srv.Close()

	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "quota", URL: "http://127.0.0.1:1", Provider: "workbuddy", Region: "cn", Runtime: "child_process"})
	pool.Upsert(Item{ID: "ready", URL: srv.URL, Provider: "workbuddy", Region: "cn", Runtime: "child_process"})
	pool.MarkClassified("quota", Classified{Kind: accounts.KindQuota, Cooldown: time.Hour, Message: "额度已用尽"})
	pool.MarkOK("ready", "deepseek-v4-flash")
	pool.MergeModels("ready", []string{"glm-5.2"})

	ex := NewChatExecutor(pool, "")
	ex.HTTPClient = srv.Client()
	got, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model:    "deepseek-v4-flash",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "workbuddy")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccountID != "ready" || hits.Load() != 1 {
		t.Fatalf("got %+v hits=%d", got, hits.Load())
	}
}

func TestChatNonStreamExpiredQuotaDoesNotMaskModelRateLimit(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{
		ID: "a", URL: "http://127.0.0.1:1", Provider: "workbuddy", Region: "cn", Runtime: "child_process",
		DownUntil: time.Now().Add(-time.Minute), LastKind: accounts.KindQuota,
	})
	pool.MarkClassified("a", Classified{
		Kind: accounts.KindRateLimit, Cooldown: time.Hour, Failover: true,
		Model: "deepseek-v4-flash", Message: "too many requests",
	})
	ex := NewChatExecutor(pool, "")
	_, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model:    "deepseek-v4-flash",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "workbuddy")
	if err == nil {
		t.Fatal("expected model cooling error")
	}
	var classified *ExecutionError
	if !errors.As(err, &classified) || classified.Classified.Kind != accounts.KindRateLimit || classified.Classified.Code != "rate_limit" {
		t.Fatalf("expired quota must not mask a live model rate limit, got %#v", err)
	}
	if !classified.Classified.Failover {
		t.Fatal("model rate-limit cooling must remain failoverable")
	}
}

func TestChatNonStreamQuotaCoolingIsQuotaNotRateLimit(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "quota", URL: "http://127.0.0.1:1", Provider: "workbuddy", Region: "cn", Runtime: "child_process"})
	pool.MarkClassified("quota", Classified{Kind: accounts.KindQuota, Cooldown: time.Hour, Message: "额度已用尽"})
	ex := NewChatExecutor(pool, "")
	_, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model:    "deepseek-v4-flash",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "workbuddy")
	if err == nil {
		t.Fatal("expected quota cooling error")
	}
	var classified *ExecutionError
	if !errors.As(err, &classified) || classified.Classified.Kind != accounts.KindQuota || classified.Classified.Code != "insufficient_quota" {
		t.Fatalf("expected quota cooling error, got %#v", err)
	}
	if !strings.Contains(classified.Classified.Message, "quota cooldown") {
		t.Fatalf("quota cooling error should say quota, got %q", classified.Classified.Message)
	}
}

func TestObserveStreamFailureDropsProvenModel(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "ready", Provider: "workbuddy", Region: "cn", Runtime: "child_process"})
	pool.MarkOK("ready", "deepseek-v4-flash")
	pool.MergeModels("ready", []string{"glm-5.2"})
	item, ok := pool.ByID("ready")
	if !ok || len(item.ProvenModels) != 1 {
		t.Fatalf("expected proven model before stream failure, got %+v", item)
	}

	ex := NewChatExecutor(pool, "")
	ex.ObserveStreamFailure("ready", &providers.Error{
		Kind:    accounts.KindModelNotAvailable,
		Status:  400,
		Message: "model not available",
	}, "deepseek-v4-flash")

	item, _ = pool.ByID("ready")
	if len(item.ProvenModels) != 0 {
		t.Fatalf("stream catalog miss must drop proven model, got %v", item.ProvenModels)
	}
	if !item.ModelsAt.IsZero() {
		t.Fatalf("explicit model miss must invalidate catalog freshness, got %s", item.ModelsAt)
	}
	if _, ok := pool.PickRoute(RouteQuery{PublicModel: "deepseek-v4-flash", ProviderFilter: "workbuddy"}); ok {
		t.Fatal("account must leave the omitted-model route after a stream catalog miss")
	}
}

func TestObserveStreamCatalogUnavailablePreservesModel(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "ready", Provider: "workbuddy", Region: "cn", Runtime: "child_process"})
	pool.MergeModels("ready", []string{"deepseek-v4-flash"})

	ex := NewChatExecutor(pool, "")
	ex.ObserveStreamFailure("ready", &providers.Error{
		Kind:    accounts.KindModelNotAvailable,
		Status:  503,
		Code:    "model_catalog_unavailable",
		Message: "model_catalog_unavailable: dynamic model catalog is unavailable",
	}, "deepseek-v4-flash")

	item, _ := pool.ByID("ready")
	if len(item.Models) != 1 || item.Models[0] != "deepseek-v4-flash" {
		t.Fatalf("catalog outage must preserve cached model, got %v", item.Models)
	}
	if item.ModelsAt.IsZero() {
		t.Fatal("catalog outage must preserve the last successful snapshot timestamp")
	}
	if _, ok := pool.PickRoute(RouteQuery{PublicModel: "deepseek-v4-flash", ProviderFilter: "workbuddy"}); !ok {
		t.Fatal("catalog outage must not remove a healthy account from the model route")
	}
}

func TestObserveStreamFailureWithoutModelTakesAccountDown(t *testing.T) {
	pool := NewPool([]string{"http://127.0.0.1:1"}, []string{"acc-quota"})
	pool.Upsert(Item{ID: "acc-quota"})
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
	pool := NewPool([]string{"http://127.0.0.1:1"}, []string{"a"})
	pool.Upsert(Item{ID: "a", Provider: "qoder", Region: "global", Runtime: "child_process"})
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
	picked, okRoute := pool.PickRoute(RouteQuery{
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
	_, okOther := pool.PickRoute(RouteQuery{
		PublicModel:    "deepseek-v4-flash",
		ProviderFilter: "qoder",
	})
	if !okOther {
		t.Fatal("PickRoute must still serve a different model on the same account")
	}
}

func TestObserveStreamFailureLeavesHealthyAccountAlone(t *testing.T) {
	pool := NewPool([]string{"http://127.0.0.1:1"}, []string{"acc-ok"})
	pool.Upsert(Item{ID: "acc-ok"})
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

// Legacy child-process accounts still send the worker payload through HTTP.
// Account aliases and native model spellings must be applied on that path too;
// otherwise the worker receives the public alias verbatim and the alias is
// silently ignored.
func TestChatNonStreamWorkerAppliesAccountAlias(t *testing.T) {
	var seen atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		seen.Store(body.Model)
		_, _ = io.WriteString(w, `{"model":"model-a","choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"source":"upstream"}}`)
	}))
	defer server.Close()

	pool := NewPool(nil, nil)
	pool.Upsert(Item{
		ID: "a", URL: server.URL, Provider: "qoder", Region: "global", Runtime: "child_process",
		Models: []string{"model-a"}, ModelAliases: map[string]string{"model-b": "model-a"},
	})
	executor := NewChatExecutor(pool, "")
	result, err := executor.ChatNonStream(context.Background(), translate.ChatRequest{
		Model: "model-b", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err != nil || result.AccountID != "a" {
		t.Fatalf("result = %+v, err=%v", result, err)
	}
	if got := seen.Load(); got != "model-a" {
		t.Fatalf("worker model = %v want model-a", got)
	}
}

func TestChatStreamProxyWorkerAppliesAccountAlias(t *testing.T) {
	var seen atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		seen.Store(body.Model)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	pool := NewPool(nil, nil)
	pool.Upsert(Item{
		ID: "a", URL: server.URL, Provider: "qoder", Region: "global", Runtime: "child_process",
		Models: []string{"model-a"}, ModelAliases: map[string]string{"model-b": "model-a"},
	})
	executor := NewChatExecutor(pool, "")
	result, err := executor.ChatStreamProxy(context.Background(), translate.ChatRequest{
		Model: "model-b", Stream: true, Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err != nil || result.AccountID != "a" {
		t.Fatalf("result = %+v, err=%v", result, err)
	}
	if _, err := io.ReadAll(result.Response.Body); err != nil {
		t.Fatal(err)
	}
	result.Response.Body.Close()
	if got := seen.Load(); got != "model-a" {
		t.Fatalf("worker model = %v want model-a", got)
	}
}

// Priority aggregation must not spend the whole retry budget on sibling
// accounts of the first provider. Each priority tier gets its own budget, so
// two exhausted qoder accounts cannot prevent the workbuddy member from being
// tried.
func TestAggregatePriorityFallsThroughToNextProvider(t *testing.T) {
	var q1Hits, q2Hits, w1Hits atomic.Int32
	q1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		q1Hits.Add(1)
		w.Header().Set("X-Qoder-Error-Kind", accounts.KindRateLimit)
		w.Header().Set("X-Qoder-Failover", "1")
		http.Error(w, `{"error":{"message":"too many requests","kind":"rate_limit"}}`, http.StatusTooManyRequests)
	}))
	defer q1.Close()
	q2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		q2Hits.Add(1)
		w.Header().Set("X-Qoder-Error-Kind", accounts.KindRateLimit)
		w.Header().Set("X-Qoder-Failover", "1")
		http.Error(w, `{"error":{"message":"too many requests","kind":"rate_limit"}}`, http.StatusTooManyRequests)
	}))
	defer q2.Close()
	w1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w1Hits.Add(1)
		_, _ = io.WriteString(w, `{"model":"model-b","choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"source":"upstream"}}`)
	}))
	defer w1.Close()

	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "a-q1", URL: q1.URL, Provider: "qoder", Region: "global", Runtime: "child_process", Models: []string{"model-a"}})
	pool.Upsert(Item{ID: "b-q2", URL: q2.URL, Provider: "qoder", Region: "global", Runtime: "child_process", Models: []string{"model-a"}})
	pool.Upsert(Item{ID: "z-w1", URL: w1.URL, Provider: "workbuddy", Region: "cn", Runtime: "child_process", Models: []string{"model-b"}})
	plan := NewAggregatePlan(accounts.Aggregation{
		ID: "logical", Strategy: accounts.AggregationPriority,
		AllowFallbacks: true, MaxAttempts: 2,
		Members: []accounts.AggregationMember{
			{Provider: "qoder", Model: "model-a", Priority: 0},
			{Provider: "workbuddy", Model: "model-b", Priority: 1},
		},
	})
	executor := NewChatExecutor(pool, "")
	result, err := executor.ChatNonStream(WithAggregatePlan(context.Background(), plan), translate.ChatRequest{
		Model: "logical", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.AccountID != "z-w1" || result.AttemptCount != 3 {
		t.Fatalf("result = %+v, want z-w1 after 3 attempts", result)
	}
	if q1Hits.Load() != 1 || q2Hits.Load() != 1 || w1Hits.Load() != 1 {
		t.Fatalf("hits q1=%d q2=%d w1=%d", q1Hits.Load(), q2Hits.Load(), w1Hits.Load())
	}
}

// A deployment-pinned member must use only its account. If that account fails,
// failover goes to the next member/provider, never to a sibling account that
// the member did not select.
func TestAggregatePinnedMemberFailsOverToNextProvider(t *testing.T) {
	var q1Hits, q2Hits, w1Hits atomic.Int32
	q1 := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		q1Hits.Add(1)
		t.Error("q1 is not a member of this logical model")
	}))
	defer q1.Close()
	q2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		q2Hits.Add(1)
		w.Header().Set("X-Qoder-Error-Kind", accounts.KindRateLimit)
		w.Header().Set("X-Qoder-Failover", "1")
		http.Error(w, `{"error":{"message":"too many requests","kind":"rate_limit"}}`, http.StatusTooManyRequests)
	}))
	defer q2.Close()
	w1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w1Hits.Add(1)
		_, _ = io.WriteString(w, `{"model":"model-b","choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"source":"upstream"}}`)
	}))
	defer w1.Close()

	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "a-q1", URL: q1.URL, Provider: "qoder", Region: "global", Runtime: "child_process", Models: []string{"model-a"}})
	pool.Upsert(Item{ID: "b-q2", URL: q2.URL, Provider: "qoder", Region: "global", Runtime: "child_process", Models: []string{"model-a"}})
	pool.Upsert(Item{ID: "z-w1", URL: w1.URL, Provider: "workbuddy", Region: "cn", Runtime: "child_process", Models: []string{"model-b"}})
	plan := NewAggregatePlan(accounts.Aggregation{
		ID: "logical", Strategy: accounts.AggregationPriority,
		AllowFallbacks: true, MaxAttempts: 2,
		Members: []accounts.AggregationMember{
			{Provider: "qoder", Model: "model-a", AccountID: "b-q2", Priority: 0},
			{Provider: "workbuddy", Model: "model-b", Priority: 1},
		},
	})
	executor := NewChatExecutor(pool, "")
	result, err := executor.ChatNonStream(WithAggregatePlan(context.Background(), plan), translate.ChatRequest{
		Model: "logical", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.AccountID != "z-w1" || result.AttemptCount != 2 {
		t.Fatalf("result = %+v, want z-w1 after 2 attempts", result)
	}
	if q1Hits.Load() != 0 || q2Hits.Load() != 1 || w1Hits.Load() != 1 {
		t.Fatalf("hits q1=%d q2=%d w1=%d", q1Hits.Load(), q2Hits.Load(), w1Hits.Load())
	}
}

func TestAttemptsForHonorsRetryBudget(t *testing.T) {
	pool := NewPool(nil, nil)
	for i := 0; i < 8; i++ {
		pool.Upsert(Item{ID: fmt.Sprintf("a%d", i), Provider: "qoder", Region: "global", Runtime: "child_process"})
	}
	ex := NewChatExecutor(pool, "")
	ex.MaxAttempts = 3
	if got := ex.attemptsFor("qoder", "global", "", nil, nil); got != 3 {
		t.Fatalf("attempt budget = %d", got)
	}
	ex.MaxAttempts = 0
	if got := ex.attemptsFor("qoder", "global", "", nil, nil); got != 4 {
		t.Fatalf("default attempt budget = %d", got)
	}
}
