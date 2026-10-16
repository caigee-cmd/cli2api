package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseStreamUsageLineWithPromptCacheHitTokens(t *testing.T) {
	line := `data: {"model":"deepseek-v4.1","usage":{"prompt_tokens":100,"completion_tokens":50,"prompt_cache_hit_tokens":80}}`
	stats, ok := ParseStreamUsageLine(line)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if stats.CacheReadTokens == nil || *stats.CacheReadTokens != 80 {
		t.Fatalf("expected CacheReadTokens=80, got %v", stats.CacheReadTokens)
	}
	if stats.CachedTokens == nil || *stats.CachedTokens != 80 {
		t.Fatalf("expected CachedTokens=80, got %v", stats.CachedTokens)
	}
}

func TestRelayOpenAIStreamSalvagesFinishReasonWithoutDone(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"
	rec := httptest.NewRecorder()
	stats, err := RelayOpenAIStream(rec, strings.NewReader(stream))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !stats.SawDone {
		t.Fatalf("expected SawDone=true")
	}
	if !strings.Contains(rec.Body.String(), "data: [DONE]") {
		t.Fatalf("expected data: [DONE] in output: %s", rec.Body.String())
	}
}
