package gateway

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/executor"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func gzipBody(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(payload); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// A gzip-compressed chat request must decode and route exactly like the plain
// one. This is the whole point of the feature: the same JSON, fewer bytes on the
// wire.
func TestChatCompletionsAcceptsGzipBody(t *testing.T) {
	plain := []byte(`{"model":"qoder/mock","stream":false,"messages":[{"role":"user","content":"hi"}]}`)
	compressed := gzipBody(t, plain)

	var gotBody []byte
	h, closeAll := newBodyTestHandler(t, func(w http.ResponseWriter, r *http.Request) {
		decoded, _ := io.ReadAll(r.Body)
		gotBody = decoded
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "mock",
			"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
		})
	})
	defer closeAll()

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(compressed))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	// The upstream must see the inflated JSON: the encoding is a transport
	// detail between caller and gateway, never forwarded.
	if !bytes.Contains(gotBody, []byte(`"hi"`)) {
		t.Fatalf("upstream body did not contain the message: %s", gotBody)
	}
}

// The default path is untouched: no header means no decoding, byte for byte.
func TestChatCompletionsUncompressedBodyUnchanged(t *testing.T) {
	h, closeAll := newBodyTestHandler(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "mock",
			"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
		})
	})
	defer closeAll()

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"qoder/mock","stream":false,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

// An encoding the gateway cannot decode must name itself instead of surfacing as
// a JSON syntax error.
func TestChatCompletionsRejectsUnknownEncoding(t *testing.T) {
	h, closeAll := newBodyTestHandler(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream must not be called")
	})
	defer closeAll()

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"qoder/mock","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Encoding", "zstd")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415; body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "zstd") {
		t.Fatalf("error does not name the encoding: %s", recorder.Body.String())
	}
}

// Corrupt gzip must be a clear 400, not a panic or a silent empty body.
func TestChatCompletionsRejectsCorruptGzip(t *testing.T) {
	h, closeAll := newBodyTestHandler(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream must not be called")
	})
	defer closeAll()

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader("this is definitely not gzip"))
	req.Header.Set("Content-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", recorder.Code, recorder.Body.String())
	}
}

// The inflated size is bounded independently of the compressed size, so a small
// gzip body cannot expand into an unbounded read.
func TestDecodeRequestBodyBoundsInflatedSize(t *testing.T) {
	// 8 MiB of zeros compresses to a few KB; ask for a 4 KiB inflated limit.
	big := bytes.Repeat([]byte("a"), 8<<20)
	compressed := gzipBody(t, big)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "gzip")
	recorder := httptest.NewRecorder()

	body := decodeRequestBody(recorder, req, int64(len(compressed)), 4096)
	defer body.Close()
	if body.err != nil {
		t.Fatalf("decodeRequestBody: %v", body.err)
	}
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) > 4096 {
		t.Fatalf("inflated read = %d bytes, want <= 4096", len(got))
	}
}

// identity is the explicit "not compressed" spelling and must behave like no
// header at all.
func TestDecodeRequestBodyIdentityIsTransparent(t *testing.T) {
	payload := []byte(`{"a":1}`)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(payload))
	req.Header.Set("Content-Encoding", "identity")
	recorder := httptest.NewRecorder()

	body := decodeRequestBody(recorder, req, 1<<20, 1<<20)
	defer body.Close()
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("body = %q, want %q", got, payload)
	}
}

// x-gzip is the legacy alias; some clients still send it.
func TestDecodeRequestBodyAcceptsXGzip(t *testing.T) {
	payload := []byte(`{"b":2}`)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(gzipBody(t, payload)))
	req.Header.Set("Content-Encoding", "x-gzip")
	recorder := httptest.NewRecorder()

	body := decodeRequestBody(recorder, req, 1<<20, 1<<20)
	defer body.Close()
	if body.err != nil {
		t.Fatalf("decodeRequestBody: %v", body.err)
	}
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("body = %q, want %q", got, payload)
	}
}

// Anthropic callers get the same treatment; the compat endpoint shares the
// decoding path.
func TestAnthropicMessagesAcceptsGzipBody(t *testing.T) {
	plain := []byte(`{"model":"qoder/mock","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	h, closeAll := newBodyTestHandler(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "mock",
			"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
		})
	})
	defer closeAll()

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(gzipBody(t, plain)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

// Native /v1/responses reads the whole body as bytes, so it needs the same
// decoding or a compressed request would fail JSON parsing.
func TestResponsesAcceptsGzipBody(t *testing.T) {
	plain := []byte(`{"model":"qoder/mock","input":"hi"}`)
	h, closeAll := newBodyTestHandler(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "mock",
			"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
		})
	})
	defer closeAll()

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(gzipBody(t, plain)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

var _ = translate.MaxNativeRequestBytes

// newBodyTestHandler builds a gateway Handler whose executor points at a stub
// upstream, mirroring how the streaming tests wire the real code path.
func newBodyTestHandler(t *testing.T, upstream http.HandlerFunc) (http.Handler, func()) {
	t.Helper()
	upstreamSrv := httptest.NewServer(upstream)
	pool := executor.NewPool(nil, nil)
	pool.Upsert(executor.Item{ID: "acct", URL: upstreamSrv.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	exec := executor.NewChatExecutor(pool, "")
	exec.HTTPClient = upstreamSrv.Client()
	handler := &Handler{Executor: exec, Pool: pool}

	var mux http.ServeMux
	mux.HandleFunc("/v1/chat/completions", handler.HandleChatCompletions)
	mux.HandleFunc("/v1/messages", handler.HandleAnthropicMessages)
	mux.HandleFunc("/v1/responses", handler.HandleResponses)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := auth.WithIdentity(r.Context(), auth.Identity{Kind: auth.KindKey, KeyID: "test"})
		mux.ServeHTTP(w, r.WithContext(ctx))
	}), upstreamSrv.Close
}
