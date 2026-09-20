package gateway

import "testing"

func TestResponsesUsagePreservesCachedInputTokens(t *testing.T) {
	zero := 0
	read := 64
	cached := 48

	withoutCache := responsesUsage(100, 20, nil, nil)
	if _, ok := withoutCache["input_tokens_details"]; ok {
		t.Fatalf("missing cache usage must not be fabricated: %#v", withoutCache)
	}

	withZero := responsesUsage(100, 20, &zero, nil)
	zeroDetails := withZero["input_tokens_details"].(map[string]any)
	if zeroDetails["cached_tokens"] != 0 {
		t.Fatalf("explicit zero cache usage was lost: %#v", withZero)
	}

	withFallback := responsesUsage(100, 20, nil, &cached)
	fallbackDetails := withFallback["input_tokens_details"].(map[string]any)
	if fallbackDetails["cached_tokens"] != 48 {
		t.Fatalf("cached token fallback mismatch: %#v", withFallback)
	}

	withTopLevel := responsesUsage(100, 20, &read, &cached)
	topLevelDetails := withTopLevel["input_tokens_details"].(map[string]any)
	if topLevelDetails["cached_tokens"] != 64 {
		t.Fatalf("cache_read_tokens must win: %#v", withTopLevel)
	}
	if withTopLevel["total_tokens"] != 120 {
		t.Fatalf("cached input must not be added twice: %#v", withTopLevel)
	}
}

func TestParseStreamUsageLineCacheReadFallback(t *testing.T) {
	for _, test := range []struct {
		name  string
		usage string
		want  *int
	}{
		{name: "detail only", usage: `"prompt_tokens_details":{"cached_tokens":2176}`, want: ptrInt(2176)},
		{name: "explicit zero", usage: `"prompt_tokens_details":{"cached_tokens":0}`, want: ptrInt(0)},
		{name: "top-level wins", usage: `"cache_read_tokens":12,"prompt_tokens_details":{"cached_tokens":2176}`, want: ptrInt(12)},
		{name: "unknown stays absent", usage: `"prompt_tokens":16`, want: nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			stats, ok := ParseStreamUsageLine(`data: {"usage":{` + test.usage + `}}`)
			if !ok {
				t.Fatal("usage not parsed")
			}
			if test.want == nil {
				if stats.CacheReadTokens != nil {
					t.Fatalf("fabricated cache read: %v", *stats.CacheReadTokens)
				}
				return
			}
			if stats.CacheReadTokens == nil || *stats.CacheReadTokens != *test.want {
				t.Fatalf("cache read = %v, want %d", stats.CacheReadTokens, *test.want)
			}
		})
	}
}
