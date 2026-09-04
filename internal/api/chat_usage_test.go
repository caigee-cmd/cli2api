package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/executor"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

func intPtr(value int) *int { return &value }

func TestWriteClassifiedErrKeepsTraeQuotaKind(t *testing.T) {
	recorder := httptest.NewRecorder()
	failover := true
	writeClassifiedErr(recorder, &providers.Error{
		Kind:     accounts.KindQuota,
		Status:   429,
		Message:  `{"code":1005,"message":""}`,
		Failover: &failover,
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
	if got := requestSessionKey(withHeader, firstKey); got == "" || got == "session-a" {
		t.Fatalf("header key = %q", got)
	}
	withSameHeader := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	withSameHeader.Header.Set("X-CLI2API-Session", "session-a")
	if requestSessionKey(withHeader, firstKey) != requestSessionKey(withSameHeader, firstKey) {
		t.Fatal("same header should derive the same opaque key")
	}
	withoutHeader := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	if got := requestSessionKey(withoutHeader, firstKey); got != "" {
		t.Fatalf("key without header = %q", got)
	}
	secondKey := auth.Identity{Kind: auth.KindKey, KeyID: "key-2"}
	if requestSessionKey(withHeader, firstKey) == requestSessionKey(withHeader, secondKey) {
		t.Fatal("same session header must be isolated by API key")
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
	writer := streamFlushWriter{w: recorder, f: recorder}
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
	var providerErr *providers.Error
	if !errors.As(err, &providerErr) || providerErr == nil {
		t.Fatalf("error=%T %v", err, err)
	}
	if providerErr.Kind != accounts.KindRateLimit || providerErr.Code != "RESOURCE_EXHAUSTED" || providerErr.RetryAfter != 30*time.Second {
		t.Fatalf("provider error=%+v", providerErr)
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
	var providerErr *providers.Error
	if !errors.As(err, &providerErr) || providerErr.Code != "upstream_stream_incomplete" || providerErr.Status != http.StatusBadGateway {
		t.Fatalf("error=%T %+v", err, err)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"upstream_stream_incomplete"`) {
		t.Fatalf("structured incomplete-stream error was not emitted: %s", recorder.Body.String())
	}
}
