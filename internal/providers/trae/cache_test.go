package trae

import (
	"testing"
)

func TestTraeOutcomePreservesPromptCache(t *testing.T) {
	aggregate := map[string]any{
		"model": "glm-5.2",
		"usage": map[string]any{
			"prompt_tokens":     100,
			"completion_tokens": 50,
			"prompt_tokens_details": map[string]any{
				"cached_tokens": 80,
			},
		},
	}
	out, err := outcomeFromAggregate(aggregate, "fallback")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.CacheReadTokens == nil || *out.CacheReadTokens != 80 {
		t.Fatalf("expected CacheReadTokens 80, got %v", out.CacheReadTokens)
	}
}
