package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/executor"
)

func TestKeyConcurrencyLimitRejectsTheExtraRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	h, closeAll := newKeyLimitHandler(t, 1, func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		<-release
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "mock",
			"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}, "finish_reason": "stop"}},
		})
	})
	defer closeAll()

	firstDone := make(chan int, 1)
	go func() {
		firstDone <- postChat(h, "key-a")
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not reach upstream")
	}

	if got := postChat(h, "key-a"); got != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want 429", got)
	}

	close(release)
	if got := <-firstDone; got != http.StatusOK {
		t.Fatalf("first request status = %d, want 200", got)
	}
	if got := postChat(h, "key-a"); got != http.StatusOK {
		t.Fatalf("request after release status = %d, want 200", got)
	}
}

func TestKeyConcurrencyUnlimitedDoesNotCount(t *testing.T) {
	limits := &KeyInFlight{}
	identity := auth.Identity{Kind: auth.KindKey, KeyID: "key", MaxInFlight: 0}
	if !limits.acquire(identity) || !limits.acquire(identity) {
		t.Fatal("unlimited keys must not consume a slot")
	}
	console := auth.Identity{Kind: auth.KindConsole, MaxInFlight: 1}
	if !limits.acquire(console) || !limits.acquire(console) {
		t.Fatal("console traffic must not consume a slot")
	}
}

func TestKeyConcurrencySlotsArePerKey(t *testing.T) {
	limits := &KeyInFlight{}
	held := auth.Identity{Kind: auth.KindKey, KeyID: "key-a", MaxInFlight: 1}
	if !limits.acquire(held) {
		t.Fatal("first slot")
	}
	if limits.acquire(held) {
		t.Fatal("same key must stop at its cap")
	}
	other := auth.Identity{Kind: auth.KindKey, KeyID: "key-b", MaxInFlight: 1}
	if !limits.acquire(other) {
		t.Fatal("another key has its own cap")
	}
	unlimited := auth.Identity{Kind: auth.KindKey, KeyID: "key-a", MaxInFlight: 0}
	if !limits.acquire(unlimited) {
		t.Fatal("raising the cap to unlimited must not keep rejecting")
	}
	limits.release(held)
	if !limits.acquire(held) {
		t.Fatal("releasing the slot must allow the next request")
	}
}

func newKeyLimitHandler(t *testing.T, maxInFlight int, upstream http.HandlerFunc) (http.Handler, func()) {
	t.Helper()
	upstreamSrv := httptest.NewServer(upstream)
	pool := executor.NewPool(nil, nil)
	pool.Upsert(executor.Item{ID: "acct", URL: upstreamSrv.URL, Provider: "qoder", Region: "global", Runtime: "child_process", MaxInFlight: 8})
	exec := executor.NewChatExecutor(pool, "")
	exec.HTTPClient = upstreamSrv.Client()
	handler := &Handler{Executor: exec, Pool: pool, KeyLimits: &KeyInFlight{}}

	var mux http.ServeMux
	mux.HandleFunc("/v1/chat/completions", handler.HandleChatCompletions)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := auth.WithIdentity(r.Context(), auth.Identity{
			Kind: auth.KindKey, KeyID: r.Header.Get("X-Test-Key"), MaxInFlight: maxInFlight,
		})
		mux.ServeHTTP(w, r.WithContext(ctx))
	}), upstreamSrv.Close
}

func postChat(handler http.Handler, keyID string) int {
	body := bytes.NewBufferString(`{"model":"qoder/mock","stream":false,"messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-Key", keyID)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder.Code
}
