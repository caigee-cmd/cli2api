package qoder

import (
	"encoding/json"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

func TestResolvedReasoningLevelDisabledThinking(t *testing.T) {
	// Qoder already honors reasoning_effort:none / enable_thinking:false; the
	// nested thinking off switch must map to the same "none" level.
	if got := resolvedReasoningLevel(translate.ChatRequest{
		Thinking: json.RawMessage(`{"type":"disabled"}`),
	}); got != "none" {
		t.Fatalf("resolved level=%q want none", got)
	}
	// Negative controls: enabled / level signals keep their own behaviour.
	if got := resolvedReasoningLevel(translate.ChatRequest{
		Thinking: json.RawMessage(`{"type":"enabled"}`),
	}); got != "" {
		t.Fatalf("enabled thinking resolved=%q want empty", got)
	}
	if got := resolvedReasoningLevel(translate.ChatRequest{
		ReasoningEffort: json.RawMessage(`"low"`),
	}); got != "low" {
		t.Fatalf("effort low resolved=%q want low", got)
	}
}

func TestBuildChatPayloadDisabledThinking(t *testing.T) {
	payload := BuildChatPayload(translate.ChatRequest{
		Thinking: json.RawMessage(`{"type":"disabled"}`),
	}, false)
	if payload["enable_thinking"] != false {
		t.Fatalf("enable_thinking=%v want false", payload["enable_thinking"])
	}
	// Negative control: an enabled thinking object must not force the flag.
	payload = BuildChatPayload(translate.ChatRequest{
		Thinking: json.RawMessage(`{"type":"enabled"}`),
	}, false)
	if _, ok := payload["enable_thinking"]; ok {
		t.Fatalf("enabled thinking must not set enable_thinking: %v", payload["enable_thinking"])
	}
}
