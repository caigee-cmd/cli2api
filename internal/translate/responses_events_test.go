package translate

import "testing"

func TestParseResponsesEventCanonicalUsage(t *testing.T) {
	payload := `{"type":"response.completed","response":{"usage":{"input_tokens":100,"output_tokens":20,"input_tokens_details":{"cached_tokens":64,"cache_write_tokens":12},"output_tokens_details":{"reasoning_tokens":7}}}}`
	event, ok := ParseResponsesEvent(payload)
	if !ok || !event.Terminal {
		t.Fatalf("event=%+v ok=%v", event, ok)
	}
	if event.CachedTokens == nil || *event.CachedTokens != 64 {
		t.Fatalf("cached tokens=%v", event.CachedTokens)
	}
	if event.CacheWriteTokens == nil || *event.CacheWriteTokens != 12 {
		t.Fatalf("cache write tokens=%v", event.CacheWriteTokens)
	}
	if event.ReasoningTokens == nil || *event.ReasoningTokens != 7 {
		t.Fatalf("reasoning tokens=%v", event.ReasoningTokens)
	}
}
