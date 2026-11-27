package command

import (
	"testing"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

// chatRequestWithSystem builds a minimal request carrying one system turn.
func chatRequestWithSystem(system string) translate.ChatRequest {
	return translate.ChatRequest{
		Model:     "deepseek/deepseek-v4.1-flash",
		MaxTokens: rawMessage(1024),
		Messages: []translate.ChatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: "hello"},
		},
	}
}

// TestWireSystemKeepsBlocksAndOmitsEmpty pins the system contract that the
// adapter's cross-provider behaviour depends on:
//   - each folded system/developer turn becomes its own text block, so block
//     boundaries survive the round trip;
//   - an empty caller system is omitted entirely instead of being sent as ""
//     or " ". Upstream answers "" / " " by injecting ~7.5K tokens of the CLI's
//     own default agent prompt, which replaces the caller's instructions.
func TestWireSystemKeepsBlocksAndOmitsEmpty(t *testing.T) {
	if got := wireSystem(nil); got != "" {
		t.Fatalf("empty system must wire as \"\" (null is rejected upstream), got %#v", got)
	}
	if got := wireSystem([]string{"   "}); got != "" {
		t.Fatalf("blank system must wire as \"\", got %#v", got)
	}

	blocks, ok := wireSystem([]string{"be terse", "answer in JSON"}).([]map[string]any)
	if !ok {
		t.Fatalf("system must wire as a text-block array")
	}
	if len(blocks) != 2 {
		t.Fatalf("blocks = %d, want 2", len(blocks))
	}
	for _, block := range blocks {
		if block["type"] != "text" {
			t.Errorf("block type = %v", block["type"])
		}
	}
	// The CLI joins non-final blocks with a newline; the final block is verbatim.
	if blocks[0]["text"] != "be terse\n" {
		t.Errorf("first block = %q", blocks[0]["text"])
	}
	if blocks[1]["text"] != "answer in JSON" {
		t.Errorf("last block = %q", blocks[1]["text"])
	}
}
