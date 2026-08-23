package api

import (
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
