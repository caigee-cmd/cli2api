package gateway

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/executor"
	applogs "github.com/caigee-cmd/cli2api/internal/logs"
)

// When the heartbeat commits before the upstream head arrives, the client must
// still see the account and provider headers on the response.
func TestHeartbeatCommitsAccountHeadersOnLiveStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":null}]}` + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer upstream.Close()

	t.Setenv(heartbeatEnvAfter, "150")
	t.Setenv(heartbeatEnvInterval, "100")

	handler, closeAll := newHeartbeatFollowupHandler(t, upstream.URL, nil)
	defer closeAll()

	gateway := httptest.NewServer(handler)
	defer gateway.Close()

	resp, err := http.Post(gateway.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"qoder/mock","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("X-CLI2API-Account"); got != "acct" {
		t.Fatalf("X-CLI2API-Account = %q, want acct", got)
	}
	if got := resp.Header.Get("X-Qoder-Account"); got != "acct" {
		t.Fatalf("X-Qoder-Account = %q, want acct", got)
	}
	if got := resp.Header.Get("X-CLI2API-Provider"); got != "qoder" {
		t.Fatalf("X-CLI2API-Provider = %q, want qoder", got)
	}

	sawKeepalive, sawDone := false, false
	reader := bufio.NewReader(resp.Body)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if strings.HasPrefix(line, ": keepalive") {
			sawKeepalive = true
		}
		if strings.HasPrefix(line, "data: [DONE]") {
			sawDone = true
			break
		}
		if err != nil {
			break
		}
	}
	if !sawKeepalive {
		t.Fatal("expected a heartbeat frame before the upstream body, otherwise this did not test the early-commit path")
	}
	if !sawDone {
		t.Fatal("stream did not complete")
	}
}

// A client that hangs up while the heartbeat is running must be recorded as
// canceled, not as a successful stream open.
func TestHeartbeatDisconnectIsNotRecordedAsStreamOpen(t *testing.T) {
	started := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(2500 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":null}]}` + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer upstream.Close()

	t.Setenv(heartbeatEnvAfter, "150")
	t.Setenv(heartbeatEnvInterval, "100")

	store := &recordingRequestStore{}
	recorder := applogs.NewRequestRecorder(store)
	defer recorder.Close()

	handler, closeAll := newHeartbeatFollowupHandler(t, upstream.URL, recorder)
	defer closeAll()

	gateway := httptest.NewServer(handler)
	defer gateway.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gateway.URL+"/v1/chat/completions",
		strings.NewReader(`{"model":"qoder/mock","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	reader := bufio.NewReader(resp.Body)
	deadline := time.Now().Add(3 * time.Second)
	sawKeepalive := false
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if strings.HasPrefix(line, ": keepalive") {
			sawKeepalive = true
			break
		}
		if err != nil {
			break
		}
	}
	if !sawKeepalive {
		resp.Body.Close()
		cancel()
		t.Fatal("did not see a heartbeat frame before disconnecting")
	}
	cancel()
	_ = resp.Body.Close()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream was never reached")
	}
	// Give the handler time to Adopt and finish the log after the client is gone
	// and the delayed upstream head arrives.
	time.Sleep(3 * time.Second)
	recorder.Close()

	statuses := store.statuses()
	for _, status := range statuses {
		if status == accounts.RequestStatusStreaming {
			t.Fatalf("heartbeat-phase disconnect was recorded as a normal stream open: %v", statuses)
		}
	}
	foundCanceled := false
	for _, status := range statuses {
		if status == accounts.RequestStatusCanceled {
			foundCanceled = true
		}
	}
	if !foundCanceled {
		t.Fatalf("expected a canceled log entry, got %v", statuses)
	}
}

func newHeartbeatFollowupHandler(t *testing.T, upstreamURL string, recorder *applogs.RequestRecorder) (http.Handler, func()) {
	t.Helper()
	pool := executor.NewPool(nil, nil)
	pool.Upsert(executor.Item{ID: "acct", URL: upstreamURL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	exec := executor.NewChatExecutor(pool, "")
	exec.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	handler := &Handler{Executor: exec, Pool: pool, Recorder: recorder}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", handler.HandleChatCompletions)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := auth.WithIdentity(r.Context(), auth.Identity{Kind: auth.KindKey, KeyID: "test"})
		mux.ServeHTTP(w, r.WithContext(ctx))
	}), func() {}
}

type recordingRequestStore struct {
	mu      sync.Mutex
	updates []accounts.RequestLog
}

func (s *recordingRequestStore) InsertRequestLog(_ context.Context, log accounts.RequestLog) error {
	s.mu.Lock()
	s.updates = append(s.updates, log)
	s.mu.Unlock()
	return nil
}
func (s *recordingRequestStore) UpdateRequestLog(_ context.Context, log accounts.RequestLog) error {
	s.mu.Lock()
	s.updates = append(s.updates, log)
	s.mu.Unlock()
	return nil
}
func (s *recordingRequestStore) InsertRequestAttempt(context.Context, accounts.RequestAttempt) error {
	return nil
}
func (s *recordingRequestStore) InsertRequestStreamDiagnostic(context.Context, accounts.RequestStreamDiagnostic) error {
	return nil
}
func (s *recordingRequestStore) InsertRequestUsageDetail(context.Context, accounts.RequestUsageDetail) error {
	return nil
}
func (s *recordingRequestStore) PurgeRequestLogs(context.Context, time.Duration, int) (int64, error) {
	return 0, nil
}
func (s *recordingRequestStore) ExpireRequestLogDetail(context.Context, time.Duration) (int64, error) {
	return 0, nil
}

func (s *recordingRequestStore) statuses() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.updates))
	for _, log := range s.updates {
		out = append(out, log.Status)
	}
	return out
}
