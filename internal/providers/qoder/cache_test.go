package qoder

import (
	"encoding/json"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

func TestQoderDirectPreservesPromptCacheHitTokens(t *testing.T) {
	raw := json.RawMessage(`{"prompt_tokens":100,"completion_tokens":50,"prompt_tokens_details":{"cached_tokens":80}}`)
	var out providers.ChatOutcome
	applyUsage(&out, raw)
	if out.CacheReadTokens == nil || *out.CacheReadTokens != 80 {
		t.Fatalf("expected CacheReadTokens 80, got %v", out.CacheReadTokens)
	}

	rawHit := json.RawMessage(`{"prompt_tokens":100,"completion_tokens":50,"prompt_cache_hit_tokens":75}`)
	var outHit providers.ChatOutcome
	applyUsage(&outHit, rawHit)
	if outHit.CacheReadTokens == nil || *outHit.CacheReadTokens != 75 {
		t.Fatalf("expected CacheReadTokens 75, got %v", outHit.CacheReadTokens)
	}
}
