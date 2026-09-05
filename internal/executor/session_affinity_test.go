package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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

	pool := accounts.NewPool(nil, nil)
	pool.Upsert(accounts.Item{ID: "a", URL: a.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	pool.Upsert(accounts.Item{ID: "b", URL: b.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
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

	pool := accounts.NewPool(nil, nil)
	pool.Upsert(accounts.Item{ID: "a", URL: a.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	pool.Upsert(accounts.Item{ID: "b", URL: b.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	pool.Upsert(accounts.Item{ID: "cn", URL: cn.URL, Provider: "qoder", Region: "cn", Runtime: "child_process"})
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

	pool := accounts.NewPool(nil, nil)
	pool.Upsert(accounts.Item{ID: "a", URL: a.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	pool.Upsert(accounts.Item{ID: "b", URL: b.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
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
	executor.CommitSession(ctx, first.Routing, first.AccountID)
	second, err := executor.ChatStreamProxy(ctx, req, "", "")
	if err != nil || second.AccountID != "a" || second.Routing != routingSticky {
		t.Fatalf("second = %+v, err=%v", second, err)
	}
	second.Response.Body.Close()
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

	pool := accounts.NewPool(nil, nil)
	pool.Upsert(accounts.Item{ID: "a", URL: a.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	pool.Upsert(accounts.Item{ID: "b", URL: b.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	pool.Upsert(accounts.Item{ID: "cn", URL: cn.URL, Provider: "qoder", Region: "cn", Runtime: "child_process"})
	executor := NewChatExecutor(pool, "")
	executor.SessionAffinity.Bind("session-429", "a")

	result, err := executor.ChatNonStream(WithSessionKey(context.Background(), "session-429"), translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err != nil || result.AccountID != "b" || result.Routing != routingStickyEscape || result.AttemptCount != 2 {
		t.Fatalf("result = %+v, err=%v", result, err)
	}
}
