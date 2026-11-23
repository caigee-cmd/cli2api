package executor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func TestSessionAffinityExpiresAndEvictsLeastRecentlyUsed(t *testing.T) {
	affinity := NewSessionAffinity(20*time.Millisecond, 2)
	affinity.Bind("one", "a")
	affinity.Bind("two", "b")
	if got, ok := affinity.Get("one"); !ok || got != "a" {
		t.Fatalf("one = %q, %v", got, ok)
	}
	affinity.Bind("three", "c")
	if _, ok := affinity.Get("two"); ok {
		t.Fatal("least recently used binding should be evicted")
	}
	time.Sleep(25 * time.Millisecond)
	if _, ok := affinity.Get("one"); ok {
		t.Fatal("expired binding should be removed")
	}
}

func TestSessionAffinityStatsTrackRoutingEvents(t *testing.T) {
	affinity := NewSessionAffinity(time.Minute, 4)
	affinity.Bind("session", "a")
	affinity.Bind("session", "b")
	if _, ok := affinity.Get("session"); !ok {
		t.Fatal("expected session binding")
	}
	if _, ok := affinity.Get("missing"); ok {
		t.Fatal("unexpected missing binding")
	}
	affinity.RecordEscape("rate_limit")
	stats := affinity.Stats()
	if stats.Bindings != 1 || stats.Hits != 1 || stats.Misses != 1 || stats.Escapes != 1 || stats.Rebindings != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	if stats.LastEscapeReason != "rate_limit" || stats.LastMissReason != "not_found" || stats.TTLSeconds != 60 {
		t.Fatalf("escape stats = %+v", stats)
	}
}

// Cache affinity is account/session scoped, not a global region latch. A
// bound conversation must stay on its account even when the route spans
// providers and ordinary round-robin would pick another one first.
func TestSessionAffinityKeepsConversationAcrossProviders(t *testing.T) {
	var otherHits atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherHits.Add(1)
		_, _ = io.WriteString(w, `{"model":"shared","choices":[{"message":{"content":"other"},"finish_reason":"stop"}],"usage":{"source":"upstream"}}`)
	}))
	defer other.Close()
	bound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"model":"shared","choices":[{"message":{"content":"bound"},"finish_reason":"stop"}],"usage":{"source":"upstream"}}`)
	}))
	defer bound.Close()

	pool := NewPool(nil, nil)
	// "a-other" sorts first, so unscoped round-robin would hit it.
	pool.Upsert(Item{ID: "a-other", URL: other.URL, Provider: "workbuddy", Region: "cn", Runtime: "child_process", Models: []string{"shared"}})
	pool.Upsert(Item{ID: "z-bound", URL: bound.URL, Provider: "qoder", Region: "global", Runtime: "child_process", Models: []string{"shared"}})
	ex := NewChatExecutor(pool, "")
	ex.SessionAffinity.Bind("cache-session", "z-bound")

	for i := 0; i < 2; i++ {
		got, err := ex.ChatNonStream(WithSessionKey(context.Background(), "cache-session"), translate.ChatRequest{
			Model: "shared", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
		}, "", "")
		if err != nil || got.AccountID != "z-bound" || got.Routing != routingSticky {
			t.Fatalf("turn %d = %+v, err=%v", i, got, err)
		}
	}
	if otherHits.Load() != 0 {
		t.Fatalf("other provider received %d requests despite cache binding", otherHits.Load())
	}
}

func TestChatNonStreamSessionAffinityAndPinPriority(t *testing.T) {
	server := func(id string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"model":"glm-5.2","choices":[{"message":{"content":"`+id+`"},"finish_reason":"stop"}],"usage":{"source":"upstream"}}`)
		}))
	}
	a := server("a")
	defer a.Close()
	b := server("b")
	defer b.Close()

	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "a", URL: a.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	pool.Upsert(Item{ID: "b", URL: b.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	executor := NewChatExecutor(pool, "")
	ctx := WithSessionKey(context.Background(), "session-1")
	req := translate.ChatRequest{Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}}

	first, err := executor.ChatNonStream(ctx, req, "", "")
	if err != nil || first.AccountID != "a" || first.Routing != routingPool {
		t.Fatalf("first = %+v, err=%v", first, err)
	}
	second, err := executor.ChatNonStream(ctx, req, "", "")
	if err != nil || second.AccountID != "a" || second.Routing != routingSticky {
		t.Fatalf("second = %+v, err=%v", second, err)
	}
	pinned, err := executor.ChatNonStream(ctx, req, "b", "")
	if err != nil || pinned.AccountID != "b" || pinned.Routing != routingPin {
		t.Fatalf("pinned = %+v, err=%v", pinned, err)
	}
	afterPin, err := executor.ChatNonStream(ctx, req, "", "")
	if err != nil || afterPin.AccountID != "a" || afterPin.Routing != routingSticky {
		t.Fatalf("after pin = %+v, err=%v", afterPin, err)
	}
}

func TestChatNonStreamSessionAffinityEscapesUnknownCatalogOnLaterModel(t *testing.T) {
	server := func(id string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"model":"`+id+`","choices":[{"message":{"content":"`+id+`"},"finish_reason":"stop"}],"usage":{"source":"upstream"}}`)
		}))
	}
	devin := server("devin")
	defer devin.Close()
	workbuddy := server("workbuddy")
	defer workbuddy.Close()

	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "devin", URL: devin.URL, Provider: "devin", Region: "global", Runtime: "child_process", Models: []string{"gpt-5-6-sol"}, ProvenModels: []string{"gpt-5-6-sol"}})
	pool.Upsert(Item{ID: "workbuddy", URL: workbuddy.URL, Provider: "workbuddy", Region: "global", Runtime: "child_process", Models: []string{"deepseek-v4.1-flash"}})
	executor := NewChatExecutor(pool, "")
	executor.SessionAffinity.Bind("compact-session", "devin")

	result, err := executor.ChatNonStream(WithSessionKey(context.Background(), "compact-session"), translate.ChatRequest{
		Model: "deepseek-v4.1-flash", Messages: []translate.ChatMessage{{Role: "user", Content: "compact"}},
	}, "", "")
	if err != nil || result.AccountID != "workbuddy" || result.Routing != routingPool {
		t.Fatalf("result = %+v, err=%v", result, err)
	}
}

func TestChatNonStreamSessionAffinityEscapesWithinBoundRegion(t *testing.T) {
	server := func(id string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"model":"glm-5.2","choices":[{"message":{"content":"`+id+`"},"finish_reason":"stop"}],"usage":{"source":"upstream"}}`)
		}))
	}
	a := server("a")
	defer a.Close()
	b := server("b")
	defer b.Close()
	cn := server("cn")
	defer cn.Close()

	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "a", URL: a.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	pool.Upsert(Item{ID: "b", URL: b.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	pool.Upsert(Item{ID: "cn", URL: cn.URL, Provider: "qoder", Region: "cn", Runtime: "child_process"})
	pool.MarkDown("a", time.Hour, "cooling")
	executor := NewChatExecutor(pool, "")
	executor.SessionAffinity.Bind("session-1", "a")

	result, err := executor.ChatNonStream(WithSessionKey(context.Background(), "session-1"), translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err != nil || result.AccountID != "b" || result.Routing != routingStickyEscape {
		t.Fatalf("result = %+v, err=%v", result, err)
	}
	if bound, ok := executor.SessionAffinity.Get("session-1"); !ok || bound != "b" {
		t.Fatalf("escaped binding = %q, %v", bound, ok)
	}
	if reason := executor.SessionAffinity.Stats().LastEscapeReason; reason != "account_cooldown" {
		t.Fatalf("escape reason = %q, want account_cooldown", reason)
	}
}

// A logical aggregation model is a virtual public ID. Its bound account must
// still be validated against the plan's members, and the binding must survive
// newRouteLoop; otherwise every request falls back to pool routing and prompt
// cache affinity is lost across turns.
func TestAggregateSessionAffinityKeepsBoundAccount(t *testing.T) {
	var q1Hits, w1Hits atomic.Int32
	q1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Model != "model-a" {
			t.Errorf("q1 upstream model = %q want model-a", body.Model)
		}
		q1Hits.Add(1)
		_, _ = io.WriteString(w, `{"model":"model-a","choices":[{"message":{"content":"q1"},"finish_reason":"stop"}],"usage":{"source":"upstream"}}`)
	}))
	defer q1.Close()
	w1 := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		w1Hits.Add(1)
		t.Error("workbuddy must not be called while the session is bound to qoder")
	}))
	defer w1.Close()

	pool := NewPool(nil, nil)
	pool.Upsert(Item{
		ID: "a-qoder", URL: q1.URL, Provider: "qoder", Region: "global", Runtime: "child_process",
		Models: []string{"model-a"}, ModelAliases: map[string]string{"model-b": "model-a"},
	})
	pool.Upsert(Item{
		ID: "z-workbuddy", URL: w1.URL, Provider: "workbuddy", Region: "cn", Runtime: "child_process",
		Models: []string{"model-b"},
	})
	plan := NewAggregatePlan(accounts.Aggregation{
		ID: "logical-model-b", Strategy: accounts.AggregationRoundRobin,
		AllowFallbacks: true, MaxAttempts: 2,
		Members: []accounts.AggregationMember{
			{Provider: "qoder", Model: "model-b", Priority: 0},
			{Provider: "workbuddy", Model: "model-b", Priority: 1},
		},
	})
	executor := NewChatExecutor(pool, "")
	executor.SessionAffinity.Bind("agg-session", "a-qoder")
	ctx := WithAggregatePlan(WithSessionKey(context.Background(), "agg-session"), plan)
	req := translate.ChatRequest{Model: "logical-model-b", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}}

	for i := 0; i < 2; i++ {
		result, err := executor.ChatNonStream(ctx, req, "", "")
		if err != nil || result.AccountID != "a-qoder" || result.Routing != routingSticky {
			t.Fatalf("turn %d = %+v, err=%v", i, result, err)
		}
	}
	if q1Hits.Load() != 2 || w1Hits.Load() != 0 {
		t.Fatalf("hits q1=%d w1=%d, want q1=2 w1=0", q1Hits.Load(), w1Hits.Load())
	}
}

// A hard failure on the bound aggregate member must cross the provider
// boundary, invalidate the stale binding, and rebind to the account that
// actually served the retry. The next turn then sticks to it for cache reuse.
func TestAggregateSessionAffinityFailoverRebindsAcrossProviders(t *testing.T) {
	var q1Hits, w1Hits atomic.Int32
	q1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Model != "model-a" {
			t.Errorf("q1 upstream model = %q want model-a", body.Model)
		}
		q1Hits.Add(1)
		w.Header().Set("X-Qoder-Error-Kind", accounts.KindRateLimit)
		w.Header().Set("X-Qoder-Failover", "1")
		http.Error(w, `{"error":{"message":"too many requests","kind":"rate_limit"}}`, http.StatusTooManyRequests)
	}))
	defer q1.Close()
	w1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Model != "model-b" {
			t.Errorf("w1 upstream model = %q want model-b", body.Model)
		}
		w1Hits.Add(1)
		_, _ = io.WriteString(w, `{"model":"model-b","choices":[{"message":{"content":"w1"},"finish_reason":"stop"}],"usage":{"source":"upstream"}}`)
	}))
	defer w1.Close()

	pool := NewPool(nil, nil)
	pool.Upsert(Item{
		ID: "a-qoder", URL: q1.URL, Provider: "qoder", Region: "global", Runtime: "child_process",
		Models: []string{"model-a"}, ModelAliases: map[string]string{"model-b": "model-a"},
	})
	pool.Upsert(Item{
		ID: "z-workbuddy", URL: w1.URL, Provider: "workbuddy", Region: "cn", Runtime: "child_process",
		Models: []string{"model-b"},
	})
	plan := NewAggregatePlan(accounts.Aggregation{
		ID: "logical-model-b", Strategy: accounts.AggregationRoundRobin,
		AllowFallbacks: true, MaxAttempts: 2,
		Members: []accounts.AggregationMember{
			{Provider: "qoder", Model: "model-b", Priority: 0},
			{Provider: "workbuddy", Model: "model-b", Priority: 1},
		},
	})
	executor := NewChatExecutor(pool, "")
	executor.SessionAffinity.Bind("agg-failover", "a-qoder")
	ctx := WithAggregatePlan(WithSessionKey(context.Background(), "agg-failover"), plan)
	req := translate.ChatRequest{Model: "logical-model-b", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}}

	first, err := executor.ChatNonStream(ctx, req, "", "")
	if err != nil || first.AccountID != "z-workbuddy" || first.Routing != routingStickyEscape || first.AttemptCount != 2 {
		t.Fatalf("first = %+v, err=%v", first, err)
	}
	if bound, ok := executor.SessionAffinity.Get("agg-failover"); !ok || bound != "z-workbuddy" {
		t.Fatalf("rebound = %q, ok=%v", bound, ok)
	}
	second, err := executor.ChatNonStream(ctx, req, "", "")
	if err != nil || second.AccountID != "z-workbuddy" || second.Routing != routingSticky {
		t.Fatalf("second = %+v, err=%v", second, err)
	}
	if q1Hits.Load() != 1 || w1Hits.Load() != 2 {
		t.Fatalf("hits q1=%d w1=%d, want q1=1 w1=2", q1Hits.Load(), w1Hits.Load())
	}
}

func TestChatStreamProxySessionAffinity(t *testing.T) {
	server := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
		}))
	}
	a := server()
	defer a.Close()
	b := server()
	defer b.Close()

	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "a", URL: a.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	pool.Upsert(Item{ID: "b", URL: b.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	executor := NewChatExecutor(pool, "")
	ctx := WithSessionKey(context.Background(), "stream-session")
	req := translate.ChatRequest{Model: "glm-5.2", Stream: true, Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}}

	first, err := executor.ChatStreamProxy(ctx, req, "", "")
	if err != nil || first.AccountID != "a" || first.Routing != routingPool {
		t.Fatalf("first = %+v, err=%v", first, err)
	}
	if _, err := io.ReadAll(first.Response.Body); err != nil {
		t.Fatal(err)
	}
	first.Response.Body.Close()
	executor.CommitSession(ctx, req, first.Routing, first.AccountID)
	second, err := executor.ChatStreamProxy(ctx, req, "", "")
	if err != nil || second.AccountID != "a" || second.Routing != routingSticky {
		t.Fatalf("second = %+v, err=%v", second, err)
	}
	second.Response.Body.Close()
}

// The worker can echo a per-account label that differs from the pool ID. The
// session binding must still use the pool routing key, or the next request's
// Pool.ByID lookup fails and cache affinity silently disappears.
func TestSessionBindingUsesPoolIDWhenWorkerRewritesAccountLabel(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("X-Qoder-Account", "worker-label")
		_, _ = io.WriteString(w, `{"model":"shared","choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"source":"upstream"}}`)
	}))
	defer server.Close()

	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "pool-a", URL: server.URL, Provider: "qoder", Region: "global", Runtime: "child_process", Models: []string{"shared"}})
	executor := NewChatExecutor(pool, "")
	ctx := WithSessionKey(context.Background(), "label-rewrite")
	req := translate.ChatRequest{Model: "shared", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}}

	first, err := executor.ChatNonStream(ctx, req, "", "")
	if err != nil || first.AccountID != "worker-label" {
		t.Fatalf("first = %+v, err=%v", first, err)
	}
	if bound, ok := executor.SessionAffinity.Get("label-rewrite"); !ok || bound != "pool-a" {
		t.Fatalf("binding = %q ok=%v, want pool-a", bound, ok)
	}
	second, err := executor.ChatNonStream(ctx, req, "", "")
	if err != nil || second.Routing != routingSticky || second.AccountID != "worker-label" {
		t.Fatalf("second = %+v, err=%v", second, err)
	}
	if hits.Load() != 2 {
		t.Fatalf("worker hits = %d, want 2", hits.Load())
	}
}

// A stream must become sticky as soon as the upstream accepts it, before the
// body finishes. Otherwise a second turn started during the stream can select
// another account and lose the prompt cache.
func TestAggregateStreamBindsBeforeBodyEnds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "a-q1", URL: server.URL, Provider: "qoder", Region: "global", Runtime: "child_process", Models: []string{"model-a"}})
	plan := NewAggregatePlan(accounts.Aggregation{
		ID: "logical", Strategy: accounts.AggregationPriority, AllowFallbacks: true, MaxAttempts: 2,
		Members: []accounts.AggregationMember{{Provider: "qoder", Model: "model-a", AccountID: "a-q1", Priority: 0}},
	})
	executor := NewChatExecutor(pool, "")
	ctx := WithAggregatePlan(WithSessionKey(context.Background(), "stream-agg"), plan)
	result, err := executor.ChatStreamProxy(ctx, translate.ChatRequest{
		Model: "logical", Stream: true, Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err != nil || result.AccountID != "a-q1" {
		t.Fatalf("result = %+v, err=%v", result, err)
	}
	if bound, ok := executor.SessionAffinity.Get("stream-agg"); !ok || bound != "a-q1" {
		t.Fatalf("binding before body read = %q ok=%v", bound, ok)
	}
	_, _ = io.Copy(io.Discard, result.Response.Body)
	result.Response.Body.Close()
}

func TestChatNonStreamContentSessionAffinityWithoutExplicitKey(t *testing.T) {
	server := func(id string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"model":"glm-5.2","choices":[{"message":{"content":"`+id+`"},"finish_reason":"stop"}],"usage":{"source":"upstream"}}`)
		}))
	}
	a := server("a")
	defer a.Close()
	b := server("b")
	defer b.Close()

	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "a", URL: a.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	pool.Upsert(Item{ID: "b", URL: b.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	executor := NewChatExecutor(pool, "")
	firstReq := translate.ChatRequest{Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "plan the refactor"}}}
	laterReq := translate.ChatRequest{
		Model: "glm-5.2",
		Messages: []translate.ChatMessage{
			{Role: "user", Content: "plan the refactor"},
			{Role: "assistant", Content: "ok"},
			{Role: "user", Content: "continue"},
		},
	}
	otherReq := translate.ChatRequest{Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "a different conversation"}}}

	first, err := executor.ChatNonStream(context.Background(), firstReq, "", "")
	if err != nil || first.AccountID != "a" || first.Routing != routingPool {
		t.Fatalf("first = %+v, err=%v", first, err)
	}
	later, err := executor.ChatNonStream(context.Background(), laterReq, "", "")
	if err != nil || later.AccountID != "a" || later.Routing != routingSticky {
		t.Fatalf("later = %+v, err=%v", later, err)
	}
	other, err := executor.ChatNonStream(context.Background(), otherReq, "", "")
	if err != nil || other.AccountID != "b" || other.Routing != routingPool {
		t.Fatalf("other = %+v, err=%v", other, err)
	}
}

func TestChatNonStreamImageOnlyContentSessionAffinity(t *testing.T) {
	server := func(id string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"model":"glm-5.2","choices":[{"message":{"content":"`+id+`"},"finish_reason":"stop"}],"usage":{"source":"upstream"}}`)
		}))
	}
	a := server("a")
	defer a.Close()
	b := server("b")
	defer b.Close()

	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "a", URL: a.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	pool.Upsert(Item{ID: "b", URL: b.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	executor := NewChatExecutor(pool, "")
	image := []any{map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/cat.png"}}}
	firstReq := translate.ChatRequest{Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: image}}}
	laterReq := translate.ChatRequest{
		Model: "glm-5.2",
		Messages: []translate.ChatMessage{
			{Role: "user", Content: image},
			{Role: "assistant", Content: "a cat"},
			{Role: "user", Content: "what color?"},
		},
	}
	otherReq := translate.ChatRequest{Model: "glm-5.2", Messages: []translate.ChatMessage{{
		Role:    "user",
		Content: []any{map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/dog.png"}}},
	}}}

	first, err := executor.ChatNonStream(context.Background(), firstReq, "", "")
	if err != nil || first.AccountID != "a" || first.Routing != routingPool {
		t.Fatalf("first = %+v, err=%v", first, err)
	}
	later, err := executor.ChatNonStream(context.Background(), laterReq, "", "")
	if err != nil || later.AccountID != "a" || later.Routing != routingSticky {
		t.Fatalf("later = %+v, err=%v", later, err)
	}
	other, err := executor.ChatNonStream(context.Background(), otherReq, "", "")
	if err != nil || other.AccountID != "b" || other.Routing != routingPool {
		t.Fatalf("other = %+v, err=%v", other, err)
	}
}

func TestChatStreamProxyContentSessionAffinityWithoutExplicitKey(t *testing.T) {
	server := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
		}))
	}
	a := server()
	defer a.Close()
	b := server()
	defer b.Close()

	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "a", URL: a.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	pool.Upsert(Item{ID: "b", URL: b.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	executor := NewChatExecutor(pool, "")
	firstReq := translate.ChatRequest{Model: "glm-5.2", Stream: true, Messages: []translate.ChatMessage{{Role: "user", Content: "plan the refactor"}}}
	laterReq := translate.ChatRequest{
		Model:  "glm-5.2",
		Stream: true,
		Messages: []translate.ChatMessage{
			{Role: "user", Content: "plan the refactor"},
			{Role: "assistant", Content: "ok"},
			{Role: "user", Content: "continue"},
		},
	}

	first, err := executor.ChatStreamProxy(context.Background(), firstReq, "", "")
	if err != nil || first.AccountID != "a" || first.Routing != routingPool {
		t.Fatalf("first = %+v, err=%v", first, err)
	}
	if _, err := io.ReadAll(first.Response.Body); err != nil {
		t.Fatal(err)
	}
	first.Response.Body.Close()
	executor.CommitSession(context.Background(), firstReq, first.Routing, first.AccountID)
	later, err := executor.ChatStreamProxy(context.Background(), laterReq, "", "")
	if err != nil || later.AccountID != "a" || later.Routing != routingSticky {
		t.Fatalf("later = %+v, err=%v", later, err)
	}
	later.Response.Body.Close()
}

func TestChatNonStreamSessionAffinityRateLimitEscapesSameRegion(t *testing.T) {
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Qoder-Error-Kind", accounts.KindRateLimit)
		w.Header().Set("X-Qoder-Failover", "1")
		http.Error(w, `{"error":{"message":"too many requests","kind":"rate_limit"}}`, http.StatusTooManyRequests)
	}))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"model":"glm-5.2","choices":[{"message":{"content":"b"},"finish_reason":"stop"}],"usage":{"source":"upstream"}}`)
	}))
	defer b.Close()
	cn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("sticky escape must stay in the bound region")
	}))
	defer cn.Close()

	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "a", URL: a.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	pool.Upsert(Item{ID: "b", URL: b.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	pool.Upsert(Item{ID: "cn", URL: cn.URL, Provider: "qoder", Region: "cn", Runtime: "child_process"})
	executor := NewChatExecutor(pool, "")
	executor.SessionAffinity.Bind("session-429", "a")

	result, err := executor.ChatNonStream(WithSessionKey(context.Background(), "session-429"), translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err != nil || result.AccountID != "b" || result.Routing != routingStickyEscape || result.AttemptCount != 2 {
		t.Fatalf("result = %+v, err=%v", result, err)
	}
}

func TestChatNonStreamSessionAffinityEscapesSaturatedProvider(t *testing.T) {
	server := func(id string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"model":"glm-5.2","choices":[{"message":{"content":"`+id+`"},"finish_reason":"stop"}],"usage":{"source":"upstream"}}`)
		}))
	}
	trae := server("trae")
	defer trae.Close()
	other := server("qoder")
	defer other.Close()

	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "trae", URL: trae.URL, Provider: "trae", Region: "cn", Runtime: "child_process", MaxInFlight: 1})
	pool.Upsert(Item{ID: "qoder", URL: other.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	if !pool.Acquire("trae") {
		t.Fatal("acquire trae slot")
	}
	executor := NewChatExecutor(pool, "")
	executor.SessionAffinity.Bind("session-full", "trae")

	result, err := executor.ChatNonStream(WithSessionKey(context.Background(), "session-full"), translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err != nil || result.AccountID != "qoder" || result.Routing != routingPool {
		t.Fatalf("result = %+v, err=%v", result, err)
	}
	if reason := executor.SessionAffinity.Stats().LastEscapeReason; reason != "concurrency_saturated" {
		t.Fatalf("escape reason = %q, want concurrency_saturated", reason)
	}
	// A completed request rebinds to the account that actually served it, so
	// the next turn follows the account that had room.
	if bound, ok := executor.SessionAffinity.Get("session-full"); !ok || bound != "qoder" {
		t.Fatalf("binding = %q, %v", bound, ok)
	}
}

func TestChatNonStreamSessionAffinityStaysWhenEveryProviderIsFull(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "trae", URL: "http://127.0.0.1:1", Provider: "trae", Region: "cn", Runtime: "child_process", MaxInFlight: 1})
	pool.Upsert(Item{ID: "qoder", URL: "http://127.0.0.1:2", Provider: "qoder", Region: "global", Runtime: "child_process", MaxInFlight: 1})
	if !pool.Acquire("trae") || !pool.Acquire("qoder") {
		t.Fatal("acquire both slots")
	}
	executor := NewChatExecutor(pool, "")
	executor.SessionAffinity.Bind("session-all-full", "trae")

	_, err := executor.ChatNonStream(WithSessionKey(context.Background(), "session-all-full"), translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	var execution *ExecutionError
	if !errors.As(err, &execution) || execution.Classified.Code != "rate_limit" || execution.Classified.Message != "all accounts at capacity" {
		t.Fatalf("err = %v, want capacity rate_limit", err)
	}
	if reason := executor.SessionAffinity.Stats().LastEscapeReason; reason != "" {
		t.Fatalf("escape reason = %q, want none", reason)
	}
	if bound, ok := executor.SessionAffinity.Get("session-all-full"); !ok || bound != "trae" {
		t.Fatalf("binding = %q, %v", bound, ok)
	}
}

func TestChatNonStreamSessionAffinityKeepsStickyWhileProviderHasCapacity(t *testing.T) {
	server := func(id string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"model":"glm-5.2","choices":[{"message":{"content":"`+id+`"},"finish_reason":"stop"}],"usage":{"source":"upstream"}}`)
		}))
	}
	bound := server("bound")
	defer bound.Close()
	sibling := server("sibling")
	defer sibling.Close()
	other := server("qoder")
	defer other.Close()

	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "bound", URL: bound.URL, Provider: "trae", Region: "cn", Runtime: "child_process", MaxInFlight: 1})
	pool.Upsert(Item{ID: "sibling", URL: sibling.URL, Provider: "trae", Region: "cn", Runtime: "child_process", MaxInFlight: 1})
	pool.Upsert(Item{ID: "qoder", URL: other.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	if !pool.Acquire("bound") {
		t.Fatal("acquire bound slot")
	}
	executor := NewChatExecutor(pool, "")
	executor.SessionAffinity.Bind("session-sibling", "bound")

	result, err := executor.ChatNonStream(WithSessionKey(context.Background(), "session-sibling"), translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err != nil || result.AccountID != "sibling" || result.Routing != routingStickyEscape {
		t.Fatalf("result = %+v, err=%v", result, err)
	}
	if boundID, ok := executor.SessionAffinity.Get("session-sibling"); !ok || boundID != "sibling" {
		t.Fatalf("rebound = %q, %v", boundID, ok)
	}
}

func TestChatNonStreamSessionAffinityHonorsProvenModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"model":"glm-5.2","choices":[{"message":{"content":"a"},"finish_reason":"stop"}],"usage":{"source":"upstream"}}`)
	}))
	defer server.Close()
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("sticky account with a proven model must not escape to the catalog-only account")
	}))
	defer other.Close()

	pool := NewPool(nil, nil)
	pool.Upsert(Item{
		ID: "a", URL: server.URL, Provider: "qoder", Region: "global", Runtime: "child_process",
		Models: []string{"hy3"}, ProvenModels: []string{"glm-5.2"},
	})
	pool.Upsert(Item{
		ID: "b", URL: other.URL, Provider: "qoder", Region: "global", Runtime: "child_process",
		Models: []string{"glm-5.2"},
	})
	executor := NewChatExecutor(pool, "")
	executor.SessionAffinity.Bind("session-proven", "a")

	result, err := executor.ChatNonStream(WithSessionKey(context.Background(), "session-proven"), translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err != nil || result.AccountID != "a" || result.Routing != routingSticky {
		t.Fatalf("result = %+v, err=%v", result, err)
	}
}
