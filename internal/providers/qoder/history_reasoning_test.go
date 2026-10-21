package qoder

import (
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

// Regression for #330: client-supplied historical reasoning must survive the
// plaintext body rebuild.
func TestBuildPlainChatBodyKeepsHistoricalReasoning(t *testing.T) {
	req := translate.ChatRequest{Model: "dfmodel", Messages: []translate.ChatMessage{
		{Role: "user", Content: "Use the synthetic snapshot."},
		{Role: "assistant", Content: "Snapshot checked.", ReasoningContent: "Synthetic marker: M1"},
		{Role: "user", Content: "Continue."},
	}}
	body := BuildPlainChatBody(req, time.Unix(1, 0))
	messages := body["messages"].([]map[string]any)
	if got := messages[1]["reasoning_content"]; got != "Synthetic marker: M1" {
		t.Fatalf("historical reasoning lost: got %#v", got)
	}
	if _, ok := messages[0]["reasoning_content"]; ok {
		t.Fatalf("empty reasoning_content should be omitted: %#v", messages[0])
	}
}
