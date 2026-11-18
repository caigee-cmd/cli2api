package trae

import "testing"

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

func TestNormalizeTraeUsageCacheAliases(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  string
		want int
	}{
		{name: "cache read input tokens", raw: `{"prompt_tokens":100,"cache_read_input_tokens":64}`, want: 64},
		{name: "cached tokens", raw: `{"prompt_tokens":100,"cached_tokens":32}`, want: 32},
		{name: "cached content token count", raw: `{"prompt_tokens":100,"cached_content_token_count":16}`, want: 16},
		{name: "input tokens details", raw: `{"prompt_tokens":100,"input_tokens_details":{"cached_tokens":8}}`, want: 8},
	} {
		t.Run(test.name, func(t *testing.T) {
			normalized, ok := normalizeTraeUsage([]byte(test.raw)).(map[string]any)
			if !ok {
				t.Fatalf("normalized type = %T", normalizeTraeUsage([]byte(test.raw)))
			}
			value, ok := normalized["cache_read_tokens"].(float64)
			if !ok || int(value) != test.want {
				t.Fatalf("cache_read_tokens = %v, want %d", normalized["cache_read_tokens"], test.want)
			}
		})
	}
}
