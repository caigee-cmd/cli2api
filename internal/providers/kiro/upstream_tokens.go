package kiro

import "strings"

// Kiro rarely states token counts, and when it does the shape differs by path
// and version. updateTokens reads every shape Kiro-Go accepts
// (proxy/kiro.go updateTokensFromEvent): the event itself and any nested
// usage / tokenUsage / token_usage object, with camelCase or snake_case keys.
// Input comes from a direct count first, then from uncached + cache read +
// cache write, then from total minus output.

var (
	inputTokenKeys = []string{
		"inputTokens", "promptTokens", "totalInputTokens",
		"input_tokens", "prompt_tokens", "total_input_tokens",
	}
	outputTokenKeys = []string{
		"outputTokens", "completionTokens", "totalOutputTokens",
		"output_tokens", "completion_tokens", "total_output_tokens",
	}
)

// maxUsageDepth bounds the walk over one event; usage objects sit near the top.
const maxUsageDepth = 6

func updateTokens(event map[string]any, input, output int) (int, int) {
	candidates := []map[string]any{event}
	collectUsageMaps(event, &candidates, 0)
	for _, usage := range candidates {
		if n, ok := numberField(usage, outputTokenKeys...); ok {
			output = n
		}
		if n, ok := numberField(usage, inputTokenKeys...); ok {
			input = n
			continue
		}
		uncached, _ := numberField(usage, "uncachedInputTokens", "uncached_input_tokens")
		read, _ := numberField(usage, "cacheReadInputTokens", "cache_read_input_tokens")
		write, _ := numberField(usage, "cacheWriteInputTokens", "cache_write_input_tokens", "cacheCreationInputTokens", "cache_creation_input_tokens")
		if sum := uncached + read + write; sum > 0 {
			input = sum
			continue
		}
		if total, ok := numberField(usage, "totalTokens", "total_tokens"); ok && total > output {
			input = total - output
		}
	}
	return input, output
}

func collectUsageMaps(value any, out *[]map[string]any, depth int) {
	if depth >= maxUsageDepth {
		return
	}
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			switch strings.ToLower(key) {
			case "usage", "tokenusage", "token_usage":
				if m, ok := child.(map[string]any); ok {
					*out = append(*out, m)
				}
			}
			collectUsageMaps(child, out, depth+1)
		}
	case []any:
		for _, child := range v {
			collectUsageMaps(child, out, depth+1)
		}
	}
}
