package gateway

// Upstream usage arrives in OpenAI form, where prompt_tokens includes the
// cached share. Anthropic splits it: input_tokens counts only the uncached
// part, and cache_read_input_tokens / cache_creation_input_tokens carry the
// rest, so a client adding the three gets the whole prompt back.

// anthropicInputTokens is the uncached share of an OpenAI prompt count.
func anthropicInputTokens(promptTokens int, cacheReadTokens, cacheWriteTokens *int) int {
	billed := promptTokens - derefInt(cacheReadTokens) - derefInt(cacheWriteTokens)
	if billed < 0 {
		return 0
	}
	return billed
}

// anthropicUsage is the usage object on a Messages response or the final
// message_delta of a Messages stream.
func anthropicUsage(promptTokens, completionTokens int, cacheReadTokens, cacheWriteTokens *int) map[string]any {
	usage := map[string]any{
		"input_tokens":  anthropicInputTokens(promptTokens, cacheReadTokens, cacheWriteTokens),
		"output_tokens": completionTokens,
	}
	if cacheReadTokens != nil {
		usage["cache_read_input_tokens"] = *cacheReadTokens
	}
	if cacheWriteTokens != nil {
		usage["cache_creation_input_tokens"] = *cacheWriteTokens
	}
	return usage
}
