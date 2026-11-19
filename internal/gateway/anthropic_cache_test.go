package gateway

import (
	"bytes"
	"strings"
	"testing"
)

func TestAnthropicMessageResponseIncludesCacheUsage(t *testing.T) {
	resp := anthropicMessageResponse("req", "model", "ok", "", nil, "stop", 100, 20, ptrInt(64), ptrInt(12))
	usage, ok := resp["usage"].(map[string]any)
	if !ok {
		t.Fatalf("usage = %#v", resp["usage"])
	}
	if usage["cache_read_input_tokens"] != 64 {
		t.Fatalf("cache_read_input_tokens = %v", usage["cache_read_input_tokens"])
	}
	if usage["cache_creation_input_tokens"] != 12 {
		t.Fatalf("cache_creation_input_tokens = %v", usage["cache_creation_input_tokens"])
	}
	if usage["input_tokens"] != 24 {
		t.Fatalf("input_tokens = %v, want fresh input 24", usage["input_tokens"])
	}
}

func TestRelayAnthropicStreamIncludesCacheUsageInMessageDelta(t *testing.T) {
	body := strings.NewReader("data: {\"model\":\"m\",\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20,\"cache_read_input_tokens\":64,\"cache_creation_input_tokens\":12}}\n\ndata: [DONE]\n\n")
	var out bytes.Buffer
	stats, err := RelayAnthropicStream(&out, body, "req", "model")
	if err != nil {
		t.Fatal(err)
	}
	if stats.CacheReadTokens == nil || *stats.CacheReadTokens != 64 {
		t.Fatalf("cache read stats = %v", stats.CacheReadTokens)
	}
	if !strings.Contains(out.String(), `"cache_read_input_tokens":64`) {
		t.Fatalf("missing cache_read_input_tokens in stream: %s", out.String())
	}
	if !strings.Contains(out.String(), `"cache_creation_input_tokens":12`) {
		t.Fatalf("missing cache_creation_input_tokens in stream: %s", out.String())
	}
	if !strings.Contains(out.String(), `"input_tokens":24`) {
		t.Fatalf("missing fresh input_tokens in stream: %s", out.String())
	}
}

func TestShouldRectifyAnthropicRequest(t *testing.T) {
	for _, model := range []string{"codex/gpt-5", "kiro/claude", "meta/llama", "muse/llama"} {
		raw := []byte(`{"model":"` + model + `"}`)
		if shouldRectifyAnthropicRequest(raw) {
			t.Fatalf("%s must preserve encrypted reasoning envelopes", model)
		}
	}
	for _, model := range []string{"glm-5.2", "qoder/glm-5.2", ""} {
		raw := []byte(`{"model":"` + model + `"}`)
		if !shouldRectifyAnthropicRequest(raw) {
			t.Fatalf("%s should use the legacy rectifier", model)
		}
	}
}
