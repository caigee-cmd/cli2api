package qoder

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

type memoryCredentials struct {
	credential accounts.NativeCredential
	region     string
}

func (m memoryCredentials) LoadCredential(context.Context, string) (accounts.NativeCredential, error) {
	return m.credential, nil
}

func (m memoryCredentials) SaveCredential(context.Context, string, string, accounts.NativeCredential) error {
	return nil
}

func TestDirectChatPostsEncodedBodyAndReadsNestedSSE(t *testing.T) {
	var gotPath, gotAuth, gotModel string
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		gotAuth = r.Header.Get("Authorization")
		gotModel = r.Header.Get("X-Model-Key")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"body\":{\"choices\":[{\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1,\"credits\":0.2}}}\n\n")
	}))
	defer upstream.Close()

	direct := NewDirect(memoryCredentials{
		credential: accounts.NativeCredential{
			UserBlob:  []byte(`{"uid":"u-1","name":"Ada","access_token":"dt-token"}`),
			MachineID: "0123456789abcdef",
		},
		region: "cn",
	}, func(context.Context, string) (string, error) { return "cn", nil })
	direct.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	direct.SetHTTP(upstream.Client())

	original := ChatEndpointHook
	ChatEndpointHook = func(string) string {
		return upstream.URL + "/algo/api/v2/service/pro/sse/agent_chat_generation?Encode=1"
	}
	t.Cleanup(func() { ChatEndpointHook = original })

	outcome, err := direct.ChatNonStream(context.Background(), "acc", translate.ChatRequest{
		Model:    "auto",
		Messages: []translate.ChatMessage{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Content != "OK" || outcome.PromptTokens != 3 || outcome.Credits == nil || *outcome.Credits != 0.2 {
		t.Fatalf("outcome = %+v", outcome)
	}
	if !strings.HasPrefix(gotAuth, "Bearer COSY.") {
		t.Fatalf("auth = %q", gotAuth)
	}
	if gotModel != "auto" || !strings.Contains(gotPath, "Encode=1") {
		t.Fatalf("path=%s model=%s", gotPath, gotModel)
	}
	plain, err := DecodeBody(gotBody)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plain), `"content":"ping"`) || !strings.Contains(string(plain), `"agent_id":"agent_common"`) {
		t.Fatalf("plain = %s", plain)
	}
}

func TestDirectChatClassifiesUpstreamStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer upstream.Close()
	direct := NewDirect(memoryCredentials{
		credential: accounts.NativeCredential{UserBlob: []byte(`{"uid":"u","access_token":"dt"}`), MachineID: "0123456789abcdef"},
	}, nil)
	direct.SetHTTP(upstream.Client())
	original := ChatEndpointHook
	ChatEndpointHook = func(string) string { return upstream.URL }
	t.Cleanup(func() { ChatEndpointHook = original })

	_, err := direct.ChatNonStream(context.Background(), "acc", translate.ChatRequest{Model: "auto"})
	classified, ok := err.(*providers.Error)
	if !ok || classified.Kind != accounts.KindAuth || classified.Status != http.StatusForbidden {
		t.Fatalf("err = %#v", err)
	}
}

func newDirectFor(t *testing.T, upstream *httptest.Server) *Direct {
	t.Helper()
	direct := NewDirect(memoryCredentials{
		credential: accounts.NativeCredential{UserBlob: []byte(`{"uid":"u","access_token":"dt"}`), MachineID: "0123456789abcdef"},
	}, nil)
	direct.SetHTTP(upstream.Client())
	original := ChatEndpointHook
	ChatEndpointHook = func(string) string { return upstream.URL }
	t.Cleanup(func() { ChatEndpointHook = original })
	return direct
}

func TestDirectChatStreamRejectsEmptyUpstreamBeforeRelay(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Real-world failure: heartbeats only, then a clean close without
		// content, finish_reason, usage, or an error frame.
		_, _ = io.WriteString(w, "data: {\"body\":{\"choices\":[{\"delta\":{},\"finish_reason\":null}]}}\n\n")
		_, _ = io.WriteString(w, "data: {\"body\":{\"choices\":[{\"delta\":{},\"finish_reason\":null}]}}\n\n")
	}))
	defer upstream.Close()
	direct := newDirectFor(t, upstream)

	resp, _, err := direct.ChatStream(context.Background(), "acc", translate.ChatRequest{Model: "auto"})
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected error for empty upstream stream")
	}
	var providerErr *providers.Error
	if !errors.As(err, &providerErr) || providerErr.Kind != accounts.KindUnavailable || providerErr.Code != "upstream_empty" {
		t.Fatalf("err = %#v", err)
	}
	if resp != nil {
		t.Fatalf("resp = %v, want nil", resp)
	}
}

func TestDirectChatStreamReplaysPeekedFrames(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"body\":{\"choices\":[{\"delta\":{\"content\":\"he\"}}]}}\n\n")
		_, _ = io.WriteString(w, "data: {\"body\":{\"choices\":[{\"delta\":{\"content\":\"llo\"},\"finish_reason\":\"stop\"}]}}\n\n")
	}))
	defer upstream.Close()
	direct := newDirectFor(t, upstream)

	resp, _, err := direct.ChatStream(context.Background(), "acc", translate.ChatRequest{Model: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if !strings.Contains(text, `"content":"he"`) || !strings.Contains(text, `"content":"llo"`) {
		t.Fatalf("peeked frames lost: %s", text)
	}
	if !strings.Contains(text, `"finish_reason":"stop"`) {
		t.Fatalf("upstream finish_reason lost: %s", text)
	}
	if !strings.Contains(text, "data: [DONE]") {
		t.Fatalf("missing [DONE]: %s", text)
	}
}

func TestDirectChatStreamSalvagesTruncatedStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Content streamed but the connection dropped before the terminal
		// finish_reason: the relay must synthesize one so strict OpenAI
		// clients do not fail with "Stream ended without finish_reason".
		_, _ = io.WriteString(w, "data: {\"body\":{\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}}\n\n")
	}))
	defer upstream.Close()
	direct := newDirectFor(t, upstream)

	resp, _, err := direct.ChatStream(context.Background(), "acc", translate.ChatRequest{Model: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"finish_reason":"stop"`) {
		t.Fatalf("missing synthesized finish_reason: %s", body)
	}
}

func TestDirectChatStreamSalvagesTruncatedToolCalls(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"body\":{\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"get_time\",\"arguments\":\"{}\"}}]}}]}}\n\n")
	}))
	defer upstream.Close()
	direct := newDirectFor(t, upstream)

	resp, _, err := direct.ChatStream(context.Background(), "acc", translate.ChatRequest{Model: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"finish_reason":"tool_calls"`) {
		t.Fatalf("missing tool_calls finish_reason: %s", body)
	}
}

func TestDirectChatNonStreamRejectsEmptyUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"body\":{\"choices\":[{\"delta\":{},\"finish_reason\":null}]}}\n\n")
	}))
	defer upstream.Close()
	direct := newDirectFor(t, upstream)

	_, err := direct.ChatNonStream(context.Background(), "acc", translate.ChatRequest{Model: "auto"})
	var providerErr *providers.Error
	if !errors.As(err, &providerErr) || providerErr.Code != "upstream_empty" {
		t.Fatalf("err = %#v", err)
	}
}

func TestDirectChatStreamSalvagesTruncatedStreamWithCachedUsage(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"body\":{\"choices\":[{\"delta\":{\"content\":\"partial\"}}],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":10,\"prompt_tokens_details\":{\"cached_tokens\":80}}}}\n\n")
	}))
	defer upstream.Close()
	direct := newDirectFor(t, upstream)

	resp, _, err := direct.ChatStream(context.Background(), "acc", translate.ChatRequest{Model: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if !strings.Contains(text, `"finish_reason":"stop"`) {
		t.Fatalf("missing synthesized finish_reason: %s", text)
	}
	if !strings.Contains(text, `"cached_tokens":80`) {
		t.Fatalf("missing cached_tokens in terminal chunk: %s", text)
	}
}

// Heartbeat-only upstream that never becomes meaningful must time out with
// upstream_empty so the executor can fail over. Without the bound,
// ensureMeaningfulUpstream would hang forever after a 200 (issue #344).
func TestDirectChatStreamTimesOutHeartbeatOnlyUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		heartbeat := "data: {\"body\":{\"choices\":[{\"delta\":{},\"finish_reason\":null}]}}\n\n"
		for {
			select {
			case <-r.Context().Done():
				return
			default:
				_, _ = io.WriteString(w, heartbeat)
				if flusher != nil {
					flusher.Flush()
				}
				time.Sleep(20 * time.Millisecond)
			}
		}
	}))
	defer upstream.Close()
	direct := newDirectFor(t, upstream)
	direct.ttfb = 80 * time.Millisecond

	started := time.Now()
	resp, _, err := direct.ChatStream(context.Background(), "acc", translate.ChatRequest{Model: "auto"})
	elapsed := time.Since(started)
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected timeout error for heartbeat-only upstream")
	}
	var providerErr *providers.Error
	if !errors.As(err, &providerErr) || providerErr.Code != "upstream_empty" {
		t.Fatalf("err = %#v", err)
	}
	if resp != nil {
		t.Fatalf("resp = %v, want nil", resp)
	}
	// Must fail within the bound (plus small slack), not hang for minutes.
	if elapsed > 2*time.Second {
		t.Fatalf("took %s, want bound near ttfb", elapsed)
	}
}

// A meaningful data line that arrives within the deadline must succeed, and
// the peeked heartbeat + content bytes must still be replayed to the client.
func TestDirectChatStreamPreservesBytesAfterHeartbeatThenContent(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"body\":{\"choices\":[{\"delta\":{},\"finish_reason\":null}]}}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		time.Sleep(30 * time.Millisecond)
		_, _ = io.WriteString(w, "data: {\"body\":{\"choices\":[{\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer upstream.Close()
	direct := newDirectFor(t, upstream)
	direct.ttfb = 500 * time.Millisecond

	resp, _, err := direct.ChatStream(context.Background(), "acc", translate.ChatRequest{Model: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if !strings.Contains(text, `"content":"OK"`) {
		t.Fatalf("meaningful content lost: %s", text)
	}
	if !strings.Contains(text, `"finish_reason":"stop"`) {
		t.Fatalf("finish_reason lost: %s", text)
	}
}

func TestEnsureMeaningfulUpstreamTimesOutHeartbeatOnly(t *testing.T) {
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		heartbeat := "data: {\"body\":{\"choices\":[{\"delta\":{},\"finish_reason\":null}]}}\n\n"
		for i := 0; i < 100; i++ {
			if _, err := io.WriteString(pw, heartbeat); err != nil {
				return
			}
			time.Sleep(15 * time.Millisecond)
		}
	}()
	resp := &http.Response{StatusCode: http.StatusOK, Body: pr}
	started := time.Now()
	out, err := ensureMeaningfulUpstream(resp, 60*time.Millisecond)
	elapsed := time.Since(started)
	if err == nil {
		out.Body.Close()
		t.Fatal("expected timeout")
	}
	var providerErr *providers.Error
	if !errors.As(err, &providerErr) || providerErr.Code != "upstream_empty" {
		t.Fatalf("err = %#v", err)
	}
	if out != nil {
		t.Fatalf("out = %v, want nil", out)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("took %s", elapsed)
	}
}

func TestEnsureMeaningfulUpstreamPreservesBufferedBytes(t *testing.T) {
	body := "data: {\"body\":{\"choices\":[{\"delta\":{},\"finish_reason\":null}]}}\n\n" +
		"data: {\"body\":{\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}}\n\n" +
		"data: {\"body\":{\"choices\":[{\"delta\":{\"content\":\" more\"}}]}}\n\n"
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
	out, err := ensureMeaningfulUpstream(resp, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Body.Close()
	got, _ := io.ReadAll(out.Body)
	if string(got) != body {
		t.Fatalf("peeked replay mismatch:\n got %q\nwant %q", got, body)
	}
}
