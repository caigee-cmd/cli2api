package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/executor"
)

func intPtr(value int) *int { return &value }

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
}
