package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/executor"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func intPtr(value int) *int { return &value }

func closedStreamPipe(err error) *io.PipeReader {
	reader, writer := io.Pipe()
	_ = writer.CloseWithError(err)
	return reader
}

func TestWriteClassifiedErrKeepsTraeQuotaKind(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeClassifiedErr(recorder, &providers.Error{
		Kind:    accounts.KindQuota,
		Status:  429,
		Message: `{"code":1005,"message":""}`,
	})
	if recorder.Code != 429 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("X-Qoder-Error-Kind") != accounts.KindQuota {
		t.Fatalf("kind=%s", recorder.Header().Get("X-Qoder-Error-Kind"))
	}
	if classifyAPIError(errors.New(`{"code":1005,"message":""}`)).Kind != accounts.KindQuota {
		t.Fatal("numeric 1005 body should classify as quota")
	}
}

func TestBuildChatUsagePreservesPromptCacheTokens(t *testing.T) {
	usage := buildChatUsage(executor.ChatResult{
		PromptTokens:     100,
		CompletionTokens: 20,
		UsageSource:      "upstream",
		CacheReadTokens:  intPtr(64),
		CacheWriteTokens: intPtr(12),
		CachedTokens:     intPtr(64),
	})
	if usage["cache_read_tokens"] != 64 || usage["cache_write_tokens"] != 12 {
		t.Fatalf("cache usage = %#v", usage)
	}
	details, ok := usage["prompt_tokens_details"].(map[string]any)
	if !ok || details["cached_tokens"] != 64 {
		t.Fatalf("prompt token details = %#v", usage["prompt_tokens_details"])
	}
}

func TestRequestSessionKeyRequiresHeaderAndScopesToIdentity(t *testing.T) {
	withHeader := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	withHeader.Header.Set("X-CLI2API-Session", "session-a")
	firstKey := auth.Identity{Kind: auth.KindKey, KeyID: "key-1"}
	emptyReq := translate.ChatRequest{}
	if got := requestSessionKey(withHeader, firstKey, emptyReq); got == "" || got == "session-a" {
		t.Fatalf("header key = %q", got)
	}
	withSameHeader := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	withSameHeader.Header.Set("X-CLI2API-Session", "session-a")
	if requestSessionKey(withHeader, firstKey, emptyReq) != requestSessionKey(withSameHeader, firstKey, emptyReq) {
		t.Fatal("same header should derive the same opaque key")
	}
	withoutHeader := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	if got := requestSessionKey(withoutHeader, firstKey, emptyReq); got != "" {
		t.Fatalf("key without header = %q", got)
	}
	secondKey := auth.Identity{Kind: auth.KindKey, KeyID: "key-2"}
	if requestSessionKey(withHeader, firstKey, emptyReq) == requestSessionKey(withHeader, secondKey, emptyReq) {
		t.Fatal("same session header must be isolated by API key")
	}
}

func TestRequestSessionKeyFallsBackToContentSeed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	identity := auth.Identity{Kind: auth.KindKey, KeyID: "key-1"}
	first := translate.ChatRequest{
		Model:    "glm-5.2",
		Messages: []translate.ChatMessage{{Role: "user", Content: "plan the refactor"}},
	}
	later := translate.ChatRequest{
		Model: "glm-5.2",
		Messages: []translate.ChatMessage{
			{Role: "user", Content: "plan the refactor"},
			{Role: "assistant", Content: "ok"},
			{Role: "user", Content: "continue"},
		},
	}
	got := requestSessionKey(req, identity, first)
	if got == "" {
		t.Fatal("expected content-derived session key")
	}
	if requestSessionKey(req, identity, later) != got {
		t.Fatal("later turn must keep the same content-derived session key")
	}
	other := translate.ChatRequest{
		Model:    "glm-5.2",
		Messages: []translate.ChatMessage{{Role: "user", Content: "a different conversation"}},
	}
	if requestSessionKey(req, identity, other) == got {
		t.Fatal("different first user message must not share a session key")
	}
	headerReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	headerReq.Header.Set("X-CLI2API-Session", "explicit")
	if requestSessionKey(headerReq, identity, first) == got {
		t.Fatal("explicit header must outrank the content seed")
	}
	if requestSessionKey(req, auth.Identity{Kind: auth.KindKey, KeyID: "key-2"}, first) == got {
		t.Fatal("content-derived session keys must stay isolated by API key")
	}
}

func TestBuildChatUsagePreservesZeroPromptCacheTokens(t *testing.T) {
	usage := buildChatUsage(executor.ChatResult{
		CacheReadTokens:  intPtr(0),
		CacheWriteTokens: intPtr(0),
		CachedTokens:     intPtr(0),
	})
	if value, ok := usage["cache_read_tokens"]; !ok || value != 0 {
		t.Fatalf("cache_read_tokens = %#v, present=%v", value, ok)
	}
	if value, ok := usage["cache_write_tokens"]; !ok || value != 0 {
		t.Fatalf("cache_write_tokens = %#v, present=%v", value, ok)
	}
}

func TestStreamFlushWriterFlushesEachWrite(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := streamFlushWriter{W: recorder, F: recorder}
	if _, err := writer.Write([]byte("data: test\n\n")); err != nil {
		t.Fatal(err)
	}
	if !recorder.Flushed {
		t.Fatal("stream chunk was not flushed")
	}
}

func TestRelayOpenAIStreamRequiresDone(t *testing.T) {
	recorder := httptest.NewRecorder()
	_, err := relayOpenAIStream(recorder, strings.NewReader("data: partial\n\n"))
	if err == nil || !strings.Contains(err.Error(), "before [DONE]") {
		t.Fatalf("err = %v", err)
	}
}

func TestRelayOpenAIStreamCapturesUsageChunk(t *testing.T) {
	recorder := httptest.NewRecorder()
	body := strings.Join([]string{
		`data: {"id":"1","choices":[{"delta":{"content":"hi"}}]}`,
		`data: {"id":"1","model":"glm-5.3","usage":{"prompt_tokens":3,"completion_tokens":2,"source":"upstream"}}`,
		`data: [DONE]`,
		"",
	}, "\n")
	stats, err := relayOpenAIStream(recorder, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if stats.Model != "glm-5.3" || stats.PromptTokens == nil || *stats.PromptTokens != 3 ||
		stats.CompletionTokens == nil || *stats.CompletionTokens != 2 || stats.UsageSource != "upstream" {
		t.Fatalf("stats = %+v", stats)
	}
	if stats.FirstTokenAt == nil {
		t.Fatal("first token timestamp missing")
	}
	if stats.SSEEventCount != 1 || stats.BytesRead != int64(len(body)) || stats.LastEvent != "message" || !stats.SawDone {
		t.Fatalf("stream diagnostics = %+v", stats)
	}
}

func TestSSEDeltaHasTokenIgnoresEmptyRoleChunks(t *testing.T) {
	if sseDeltaHasToken(`data: {"choices":[{"delta":{"role":"assistant"}}]}`) {
		t.Fatal("role-only chunk is not a token")
	}
	if sseDeltaHasToken(`data: {"choices":[{"delta":{"content":""}}]}`) {
		t.Fatal("empty content is not a token")
	}
	if !sseDeltaHasToken(`data: {"choices":[{"delta":{"content":"OK"}}]}`) {
		t.Fatal("content delta should count as first token")
	}
	if !sseDeltaHasToken(`data: {"choices":[{"delta":{"reasoning_content":"think"}}]}`) {
		t.Fatal("reasoning delta should count as first token")
	}
	if !sseDeltaHasToken(`data: {"choices":[{"delta":{"tool_calls":[{"index":0}]}}]}`) {
		t.Fatal("tool call delta should count as first token")
	}
}

func TestClassifyAPIErrorPreservesProviderFields(t *testing.T) {
	got := classifyAPIError(&providers.Error{
		Kind:       accounts.KindRateLimit,
		Status:     429,
		Code:       "RESOURCE_EXHAUSTED",
		Type:       "rate_limit_error",
		Message:    "provider busy",
		RetryAfter: time.Second,
	})
	if got.Kind != accounts.KindRateLimit || got.Code != "RESOURCE_EXHAUSTED" || got.Type != "rate_limit_error" || got.Cooldown != 30*time.Second {
		t.Fatalf("classified=%+v", got)
	}
}

func TestRelayOpenAIStreamClassifiesAndSuppressesStructuredError(t *testing.T) {
	recorder := httptest.NewRecorder()
	body := strings.Join([]string{
		"event: error",
		`data: {"error":{"code":"RESOURCE_EXHAUSTED","type":"rate_limit_error","message":"provider busy","retry_after":"1s"}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	_, err := relayOpenAIStream(recorder, strings.NewReader(body))
	if err == nil {
		t.Fatal("expected structured stream error")
	}
	var execErr *executor.ExecutionError
	if !errors.As(err, &execErr) || execErr == nil {
		t.Fatalf("error=%T %v", err, err)
	}
	if execErr.Classified.Kind != accounts.KindRateLimit || execErr.Classified.Code != "RESOURCE_EXHAUSTED" || execErr.Classified.RetryAfter != 30*time.Second {
		t.Fatalf("execution error=%+v", execErr.Classified)
	}
	output := recorder.Body.String()
	if strings.Contains(output, "event: error") {
		t.Fatalf("internal error event leaked to client: %s", output)
	}
	if !strings.Contains(output, `"message":"provider busy"`) || !strings.Contains(output, `"code":"RESOURCE_EXHAUSTED"`) {
		t.Fatalf("structured error was not emitted: %s", output)
	}
}

func TestRelayOpenAIStreamReportsIncompleteStreamStructurally(t *testing.T) {
	recorder := httptest.NewRecorder()
	_, err := relayOpenAIStream(recorder, strings.NewReader("data: partial\n\n"))
	var execErr *executor.ExecutionError
	if !errors.As(err, &execErr) || execErr.Classified.Code != "upstream_stream_incomplete" || execErr.Classified.Status != http.StatusBadGateway {
		t.Fatalf("error=%T %+v", err, err)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"upstream_stream_incomplete"`) {
		t.Fatalf("structured incomplete-stream error was not emitted: %s", recorder.Body.String())
	}
}

func TestRelayOpenAIStreamPreservesTypedReadError(t *testing.T) {
	recorder := httptest.NewRecorder()
	want := &providers.Error{
		Kind: accounts.KindInvalidRequest, Status: http.StatusBadRequest,
		Code: "invalid_argument", Type: "invalid_request_error", Message: "upstream rejected request",
		RetryAfter: 45 * time.Second,
	}
	_, err := relayOpenAIStream(recorder, closedStreamPipe(fmt.Errorf("Connect trailer: %w", want)))
	var executionErr *executor.ExecutionError
	if !errors.As(err, &executionErr) || executionErr.Classified.Kind != want.Kind {
		t.Fatalf("stream error was not classified: %T %v", err, err)
	}
	var got *providers.Error
	if !errors.As(err, &got) || got != want {
		t.Fatalf("error=%T %+v want pointer=%p", err, err, want)
	}
	if got.Kind != accounts.KindInvalidRequest || got.Status != http.StatusBadRequest || got.Code != "invalid_argument" ||
		got.Type != "invalid_request_error" || got.RetryAfter != 45*time.Second {
		t.Fatalf("provider error=%+v", got)
	}
	output := recorder.Body.String()
	if !strings.Contains(output, `"code":"invalid_argument"`) || strings.Contains(output, "upstream_stream_interrupted") {
		t.Fatalf("structured error=%s", output)
	}
	pool := executor.NewPool(nil, nil)
	pool.Upsert(executor.Item{ID: "devin-account"})
	executor.NewChatExecutor(pool, "").ObserveStreamFailure("devin-account", err, "swe-2")
	item, _ := pool.ByID("devin-account")
	if item.LastKind != "" || !item.DownUntil.IsZero() {
		t.Fatalf("invalid request cooled account: kind=%q down=%v", item.LastKind, item.DownUntil)
	}
}

func TestRelayOpenAIStreamWrapsUnknownReadError(t *testing.T) {
	recorder := httptest.NewRecorder()
	cause := errors.New("socket closed")
	_, err := relayOpenAIStream(recorder, closedStreamPipe(cause))
	if !errors.Is(err, cause) {
		t.Fatalf("stream read cause was lost: %v", err)
	}
	var got *executor.ExecutionError
	if !errors.As(err, &got) || got.Classified.Kind != accounts.KindUnavailable || got.Classified.Code != "upstream_stream_interrupted" || got.Classified.Status != http.StatusBadGateway {
		t.Fatalf("error=%T %+v", err, err)
	}
	if !strings.Contains(got.Classified.Message, "stream read error: socket closed") {
		t.Fatalf("message=%q", got.Classified.Message)
	}
	pool := executor.NewPool(nil, nil)
	pool.Upsert(executor.Item{ID: "devin-account"})
	executor.NewChatExecutor(pool, "").ObserveStreamFailure("devin-account", got, "swe-2")
	item, _ := pool.ByID("devin-account")
	if item.LastKind != accounts.KindUnavailable || item.DownUntil.IsZero() {
		t.Fatalf("transport interruption was not unavailable: kind=%q down=%v", item.LastKind, item.DownUntil)
	}
}

func TestParseStreamUsageLineReadsWorkBuddyCredit(t *testing.T) {
	stats, ok := parseStreamUsageLine(`data: {"model":"hy3","usage":{"prompt_tokens":16,"completion_tokens":2,"credit":0.75}}`)
	if !ok || stats.Credits == nil || *stats.Credits != 0.75 {
		t.Fatalf("stats = %+v ok=%v", stats, ok)
	}
}

func TestParseStreamUsageLinePrefersPluralCredits(t *testing.T) {
	stats, ok := parseStreamUsageLine(`data: {"usage":{"prompt_tokens":1,"completion_tokens":1,"credits":3.5,"credit":0.5}}`)
	if !ok || stats.Credits == nil || *stats.Credits != 3.5 {
		t.Fatalf("stats = %+v ok=%v", stats, ok)
	}
}

func TestParseStreamUsageLineMissingCreditStaysNil(t *testing.T) {
	stats, ok := parseStreamUsageLine(`data: {"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	if !ok || stats.Credits != nil {
		t.Fatalf("stats = %+v ok=%v", stats, ok)
	}
}
