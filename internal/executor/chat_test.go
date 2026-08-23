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

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/endpoint"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

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
	ex := NewChatExecutor(endpoint.Endpoints{}, pool)
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
	ex := NewChatExecutor(endpoint.Endpoints{}, pool)
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
	ex := NewChatExecutor(endpoint.Endpoints{}, pool)
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
	ex := NewChatExecutor(endpoint.Endpoints{}, accounts.NewPool(nil, nil))
	_, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "")
	if err == nil || !strings.Contains(err.Error(), "no worker accounts") {
		t.Fatalf("error = %v", err)
	}
}

func TestNewChatExecutorUsesProxyKeyForInternalDaemon(t *testing.T) {
	t.Setenv("QODER_WORKER_API_KEY", "")
	t.Setenv("PROXY_API_KEY", "shared-secret")
	ex := NewChatExecutor(endpoint.Endpoints{}, accounts.NewPool(nil, nil))
	if ex.WorkerKey != "shared-secret" {
		t.Fatalf("worker key = %q", ex.WorkerKey)
	}
}
