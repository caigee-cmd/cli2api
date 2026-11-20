package kiro

import (
	"strings"
	"testing"
	"time"
)

func longText(word string, n int) string {
	return strings.TrimSpace(strings.Repeat(word+" ", n))
}

func cacheConversation(model string, turns ...string) conversation {
	c := conversation{Model: model, System: longText("system", 2000)}
	for i, text := range turns[:len(turns)-1] {
		if i%2 == 0 {
			c.History = append(c.History, historyTurn{User: &userTurn{Text: text}})
		} else {
			c.History = append(c.History, historyTurn{Assistant: &assistantTurn{Text: text}})
		}
	}
	c.Current = userTurn{Text: turns[len(turns)-1]}
	return c
}

func TestPromptCacheFirstRequestWritesOnly(t *testing.T) {
	cache := newPromptCache()
	turn := cache.begin("acct", cacheConversation("claude-sonnet-4.6", "hello"))
	usage := turn.estimate()
	if usage.Read != 0 || usage.Write != usage.Total || usage.Total < minCacheableTokens {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestPromptCacheNextTurnReadsStoredPrefix(t *testing.T) {
	cache := newPromptCache()
	first := cache.begin("acct", cacheConversation("claude-sonnet-4.6", "hello"))
	first.commit()

	// The new turns are long enough that the stored prefix stays under the
	// 85% read cap.
	next := cache.begin("acct", cacheConversation("claude-sonnet-4.6", "hello", longText("reply", 600), longText("again", 600)))
	usage := next.estimate()
	if usage.Read != first.usage.Total {
		t.Fatalf("read = %d, want previous prompt %d", usage.Read, first.usage.Total)
	}
	cacheable := int(float64(usage.Total) * maxCacheReadShare)
	if usage.Read+usage.Write != cacheable {
		t.Fatalf("usage = %+v, cacheable = %d", usage, cacheable)
	}
}

func TestPromptCacheIsPerAccount(t *testing.T) {
	cache := newPromptCache()
	cache.begin("a", cacheConversation("claude-sonnet-4.6", "hello")).commit()
	usage := cache.begin("b", cacheConversation("claude-sonnet-4.6", "hello")).estimate()
	if usage.Read != 0 {
		t.Fatalf("account b read account a's cache: %+v", usage)
	}
}

func TestPromptCacheExpires(t *testing.T) {
	now := time.Now()
	cache := newPromptCache()
	cache.now = func() time.Time { return now }
	cache.begin("acct", cacheConversation("claude-sonnet-4.6", "hello")).commit()
	now = now.Add(promptCacheTTL + time.Second)
	usage := cache.begin("acct", cacheConversation("claude-sonnet-4.6", "hello", "hi", "again")).estimate()
	if usage.Read != 0 || usage.Write != usage.Total {
		t.Fatalf("expired cache still read: %+v", usage)
	}
}

func TestPromptCacheSkipsShortPrompts(t *testing.T) {
	cache := newPromptCache()
	c := conversation{Model: "claude-sonnet-4.6", Current: userTurn{Text: "hi"}}
	usage := cache.begin("acct", c).estimate()
	if usage.Read != 0 || usage.Write != 0 {
		t.Fatalf("short prompt cached: %+v", usage)
	}
}

func TestPromptCacheOpusNeedsLongerPrefix(t *testing.T) {
	c := conversation{Model: "claude-opus-4.6", System: longText("system", 1500), Current: userTurn{Text: "hi"}}
	usage := newPromptCache().begin("acct", c).estimate()
	if usage.Total < minCacheableTokens || usage.Total >= opusMinCacheableTokens || usage.Write != 0 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestPromptCacheFailedStreamIsNotStored(t *testing.T) {
	cache := newPromptCache()
	turn := cache.begin("acct", cacheConversation("claude-sonnet-4.6", "hello"))
	frames := []streamEvent{
		{Type: "assistantResponseEvent", Event: map[string]any{"content": "partial"}},
	}
	failing := func() eventSource {
		source := sliceSource(frames)
		return func() (streamEvent, error) {
			next, err := source()
			if err != nil {
				return streamEvent{}, errUpstreamTest
			}
			return next, nil
		}
	}()
	var out strings.Builder
	writeConsumed(&out, "claude-sonnet-4.6", failing, nil, nil, turn)
	if usage := cache.begin("acct", cacheConversation("claude-sonnet-4.6", "hello")).estimate(); usage.Read != 0 {
		t.Fatalf("failed stream seeded a hit: %+v", usage)
	}
}

func TestPromptCacheNilIsSafe(t *testing.T) {
	var cache *promptCache
	turn := cache.begin("acct", cacheConversation("claude-sonnet-4.6", "hello"))
	if turn != nil || turn.estimate() != nil {
		t.Fatal("nil cache produced an estimate")
	}
	turn.commit()
}

func TestPromptCacheScaledStaysWithinInput(t *testing.T) {
	usage := &promptCacheUsage{Read: 800, Write: 150, Total: 1000}
	read, write := usage.scaled(4000)
	if read != 3200 || write != 600 {
		t.Fatalf("scaled = %d, %d", read, write)
	}
	read, write = (&promptCacheUsage{Read: 900, Write: 900, Total: 1000}).scaled(100)
	if read+write > 100 {
		t.Fatalf("read %d + write %d exceeds input", read, write)
	}
}

// The usage chunk carries the estimate in OpenAI form; prompt_tokens stays the
// whole prompt and collectOutcome passes the cache split on.
func TestStreamUsageCarriesCacheEstimate(t *testing.T) {
	cache := newPromptCache()
	cache.begin("acct", cacheConversation("claude-sonnet-4.6", "hello")).commit()
	turn := cache.begin("acct", cacheConversation("claude-sonnet-4.6", "hello", "hi there", "and now?"))

	var out strings.Builder
	writeConsumed(&out, "claude-sonnet-4.6", sliceSource(capturedLegacyStream()), nil, nil, turn)
	usage := finalUsage(t, out.String())
	read, _ := usage["cache_read_tokens"].(float64)
	write, _ := usage["cache_write_tokens"].(float64)
	if usage["prompt_tokens"] != float64(4109) || read <= 0 || read+write > 4109 {
		t.Fatalf("usage = %v", usage)
	}

	outcome, err := collectOutcome(strings.NewReader(out.String()), "")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.CacheReadTokens == nil || *outcome.CacheReadTokens != int(read) || outcome.CacheWriteTokens == nil || *outcome.CacheWriteTokens != int(write) {
		t.Fatalf("outcome = %+v", outcome)
	}
}
