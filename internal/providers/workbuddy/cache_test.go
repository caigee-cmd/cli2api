package workbuddy

import (
	"testing"
)

func TestWorkBuddyOutcomePreservesPromptCacheHitTokens(t *testing.T) {
	aggregate := map[string]any{
		"model": "deepseek-v4.1",
		"usage": map[string]any{
			"prompt_tokens":           100,
			"completion_tokens":       50,
			"prompt_cache_hit_tokens": 80,
		},
	}
	out, err := outcomeFromAggregate(aggregate)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.CacheReadTokens == nil || *out.CacheReadTokens != 80 {
		t.Fatalf("expected CacheReadTokens 80, got %v", out.CacheReadTokens)
	}
}
