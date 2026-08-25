package executor

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

	"github.com/caigee-cmd/cli2api/internal/accounts"
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
	}, "")
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
	}, "a")
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
	}, "acc2")
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
	}, "")
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
		}, "")
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
