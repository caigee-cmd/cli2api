package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/config"
	"github.com/caigee-cmd/cli2api/internal/endpoint"
	"github.com/caigee-cmd/cli2api/internal/executor"
	applogs "github.com/caigee-cmd/cli2api/internal/logs"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

func waitForRequestLog(t *testing.T, store applogs.RequestStore, id, status string) accounts.RequestLog {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last accounts.RequestLog
	for time.Now().Before(deadline) {
		item, err := store.GetRequestLog(context.Background(), id)
		if err == nil && item.Status == status {
			return item
		}
		if err == nil {
			last = item
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("request log %s never reached %s: %+v", id, status, last)
	return last
}

func openaiNonStreamBody() string {
	return `{
		"model":"glm-5.2",
		"choices":[{"message":{"content":"","reasoning_content":"plan","tool_calls":[{"id":"call_1","type":"function","function":{"name":"weather","arguments":"{\"city\":\"Shanghai\"}"}}]},"finish_reason":"tool_calls"}],
		"usage":{"prompt_tokens":11,"completion_tokens":4,"source":"upstream"}
	}`
}

func openaiStreamChunks() string {
	return strings.Join([]string{
		`data: {"id":"1","choices":[{"delta":{"reasoning_content":"plan"}}]}`,
		`data: {"id":"1","choices":[{"delta":{"content":"hello"}}]}`,
		`data: {"id":"1","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"weather","arguments":"{\"city\":\"Shanghai\"}"}}]}}]}`,
		`data: {"id":"1","model":"glm-5.2","choices":[{"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":9,"completion_tokens":3,"source":"upstream"}}`,
		`data: [DONE]`,
		"",
	}, "\n\n")
}

func TestOpenAINonStreamContractThroughHandler(t *testing.T) {
	server, closeServer := newCompatibilityServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != endpoint.ChatCompletionsPath {
			t.Fatalf("worker %s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, openaiNonStreamBody())
	})
	defer closeServer()
	store, err := accounts.OpenStore(t.TempDir() + "/qoder.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server.recorder = applogs.NewRequestRecorder(store)
	server.auth = auth.NewVerifier("secret", store)
	server.mux = http.NewServeMux()
	server.routes()

	req := httptest.NewRequest(http.MethodPost, endpoint.ChatCompletionsPath, strings.NewReader(`{"model":"qoder/glm-5.2","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Qoder-Account") != "account-a" || rec.Header().Get("X-CLI2API-Provider") != "qoder" {
		t.Fatalf("headers=%v", rec.Header())
	}
	requestID := rec.Header().Get("X-Request-Id")
	if requestID == "" {
		t.Fatal("missing X-Request-Id")
	}
	var payload struct {
		Object  string `json:"object"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content          any    `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				ToolCalls        []struct {
					ID string `json:"id"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int    `json:"prompt_tokens"`
			CompletionTokens int    `json:"completion_tokens"`
			Source           string `json:"source"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Object != "chat.completion" || len(payload.Choices) != 1 || payload.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("payload=%+v", payload)
	}
	if payload.Choices[0].Message.ReasoningContent != "plan" || len(payload.Choices[0].Message.ToolCalls) != 1 || payload.Choices[0].Message.ToolCalls[0].ID != "call_1" {
		t.Fatalf("message=%+v", payload.Choices[0].Message)
	}
	if payload.Usage.PromptTokens != 11 || payload.Usage.CompletionTokens != 4 || payload.Usage.Source != "upstream" {
		t.Fatalf("usage=%+v", payload.Usage)
	}
	logEntry := waitForRequestLog(t, store, requestID, accounts.RequestStatusOK)
	if logEntry.AttemptCount < 1 || logEntry.AccountID != "account-a" || logEntry.Routing == "" {
		t.Fatalf("log=%+v", logEntry)
	}
}

func TestOpenAIStreamAndConsoleChatShareHandler(t *testing.T) {
	server, closeServer := newCompatibilityServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, openaiStreamChunks())
	})
	defer closeServer()
	store, err := accounts.OpenStore(t.TempDir() + "/qoder.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server.recorder = applogs.NewRequestRecorder(store)
	server.auth = auth.NewVerifier("secret", store)
	server.mux = http.NewServeMux()
	server.routes()

	for _, path := range []string{endpoint.ChatCompletionsPath, "/api/chat"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"qoder/glm-5.2","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Header().Get("Content-Type"), "text/event-stream") {
			t.Fatalf("%s content-type=%q", path, rec.Header().Get("Content-Type"))
		}
		body := rec.Body.String()
		for _, fragment := range []string{`"reasoning_content":"plan"`, `"content":"hello"`, `"name":"weather"`, `"source":"upstream"`, "data: [DONE]"} {
			if !strings.Contains(body, fragment) {
				t.Fatalf("%s missing %s in %s", path, fragment, body)
			}
		}
		requestID := rec.Header().Get("X-Request-Id")
		logEntry := waitForRequestLog(t, store, requestID, accounts.RequestStatusOK)
		if logEntry.Stream != true || logEntry.PromptTokens == nil || *logEntry.PromptTokens != 9 {
			t.Fatalf("%s log=%+v", path, logEntry)
		}
	}
}

func TestOpenAIStreamHTTPErrorBeforeSSEDoesNotOpenStream(t *testing.T) {
	var calls atomic.Int32
	server, closeServer := newCompatibilityServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, `{"error":{"message":"too many requests","type":"rate_limit_error"}}`, http.StatusTooManyRequests)
	})
	defer closeServer()
	req := httptest.NewRequest(http.MethodPost, endpoint.ChatCompletionsPath, strings.NewReader(`{"model":"qoder/glm-5.2","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()
	server.handleChatCompletions(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Qoder-Error-Kind") == "" {
		t.Fatalf("missing error kind: %v", rec.Header())
	}
	if strings.Contains(rec.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("pre-start error used SSE: %v", rec.Header())
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

func TestOpenAIStreamIncompleteDoesNotReplay(t *testing.T) {
	var calls atomic.Int32
	server, closeServer := newCompatibilityServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
	})
	defer closeServer()
	req := httptest.NewRequest(http.MethodPost, endpoint.ChatCompletionsPath, strings.NewReader(`{"model":"qoder/glm-5.2","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()
	func() {
		defer func() { _ = recover() }()
		server.handleChatCompletions(rec, req)
	}()
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"content":"hi"`) || !strings.Contains(rec.Body.String(), `"code":"upstream_stream_incomplete"`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
	if calls.Load() != 1 {
		t.Fatalf("replayed stream: calls=%d", calls.Load())
	}
}

func TestOpenAIStreamWriteFailureDoesNotObserveDisconnect(t *testing.T) {
	server, closeServer := newCompatibilityServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, openaiStreamChunks())
	})
	defer closeServer()
	failing := failingFlushWriter{}
	_, err := relayOpenAIStream(failing, strings.NewReader(openaiStreamChunks()))
	var writeErr *streamRelayWriteError
	if !errors.As(err, &writeErr) {
		t.Fatalf("err=%T %v", err, err)
	}
	if !isStreamClientDisconnect(err) {
		t.Fatal("write failure should count as client disconnect")
	}
	item, _ := server.pool.ByID("account-a")
	if item.LastKind != "" || !item.DownUntil.IsZero() {
		t.Fatalf("disconnect cooled account: %+v", item)
	}
}

func TestPinnedMissingAccountFallsBackToPool(t *testing.T) {
	server, closeServer := newCompatibilityServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, openaiNonStreamBody())
	})
	defer closeServer()
	req := httptest.NewRequest(http.MethodPost, endpoint.ChatCompletionsPath, strings.NewReader(`{"model":"qoder/glm-5.2","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("X-Qoder-Account", "missing")
	rec := httptest.NewRecorder()
	server.handleChatCompletions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("missing pin fallback: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Qoder-Account") != "account-a" {
		t.Fatalf("fallback account=%q", rec.Header().Get("X-Qoder-Account"))
	}
}

func TestEmptyPoolChatReturnsClassifiedHTTPError(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(), RuntimeDir: t.TempDir(),
	})
	t.Cleanup(func() { _ = srv.Close() })
	rec := serveS01(t, srv, http.MethodPost, endpoint.ChatCompletionsPath, `{"model":"qoder/glm-5.2","messages":[{"role":"user","content":"hi"}]}`, "secret")
	if rec.Code == http.StatusOK {
		t.Fatalf("empty pool succeeded: %s", rec.Body.String())
	}
	if rec.Header().Get("X-Qoder-Error-Kind") == "" {
		t.Fatalf("missing kind: %d %s", rec.Code, rec.Body.String())
	}
}

func TestRegionGrantDoesNotEscapeOnChat(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(), RuntimeDir: t.TempDir(),
	})
	t.Cleanup(func() { _ = srv.Close() })
	created, err := srv.manager.Store().CreateAPIKey(context.Background(), accounts.CreateAPIKey{
		Name: "cn-only", Providers: []string{"workbuddy:cn"}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ready := true
	srv.pool.Upsert(accounts.Item{ID: "wb-global", Provider: "workbuddy", Region: "global", Runtime: string(providers.RuntimeInProcess), Ready: &ready})
	rec := serveS01(t, srv, http.MethodPost, endpoint.ChatCompletionsPath, `{"model":"workbuddy/glm-5.2","messages":[{"role":"user","content":"hi"}]}`, created.Secret)
	if rec.Code == http.StatusOK {
		t.Fatalf("region grant escaped: %d %s", rec.Code, rec.Body.String())
	}
}

type failingFlushWriter struct{}

func (failingFlushWriter) Header() http.Header        { return make(http.Header) }
func (failingFlushWriter) WriteHeader(int)            {}
func (failingFlushWriter) Flush()                     {}
func (failingFlushWriter) Write([]byte) (int, error) {
	return 0, errors.New("client closed")
}

func TestOpenAIStreamCancelClosesUpstreamBody(t *testing.T) {
	released := make(chan struct{})
	var once sync.Once
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		flusher.Flush()
		<-r.Context().Done()
		once.Do(func() { close(released) })
	}))
	t.Cleanup(upstream.Close)
	pool := accounts.NewPool(nil, nil)
	pool.Upsert(accounts.Item{ID: "account-a", URL: upstream.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	chatExecutor := executor.NewChatExecutor(pool, "")
	chatExecutor.HTTPClient = upstream.Client()
	server := &Server{executor: chatExecutor, pool: pool}

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, endpoint.ChatCompletionsPath, strings.NewReader(`{"model":"qoder/glm-5.2","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	req = req.WithContext(ctx)
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	rec := httptest.NewRecorder()
	func() {
		defer func() { _ = recover() }()
		server.handleChatCompletions(rec, req)
	}()
	select {
	case <-released:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream body was not closed after cancel")
	}
}
