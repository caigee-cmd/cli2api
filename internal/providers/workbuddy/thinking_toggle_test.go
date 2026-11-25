package workbuddy

import (
	"encoding/json"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func TestApplyChatReasoningDisabledThinking(t *testing.T) {
	caps := providers.ModelCapabilities{
		// A realistic deepseek-v4.1-flash catalog: "none" is NOT advertised,
		// CanDisableThinking is false. The none LEVEL would be clamped back to
		// high; the off SWITCH must not be.
		ReasoningOptions:   []string{"low", "high", "max"},
		ReasoningDefault:   "high",
		CanDisableThinking: false,
	}
	disabled := translate.ChatRequest{
		Model:    "deepseek-v4.1-flash",
		Thinking: json.RawMessage(`{"type":"disabled"}`),
	}
	obj := map[string]any{
		"reasoning":         map[string]any{"effort": "stale"},
		"reasoning_effort":  "max",
		"reasoning_summary": "auto",
		"verbosity":         "high",
	}
	if got := applyChatReasoning(obj, disabled, "max", caps); got != "none" {
		t.Fatalf("resolved level=%q want none", got)
	}
	for _, key := range []string{"reasoning", "reasoning_effort", "reasoning_summary", "verbosity"} {
		if _, ok := obj[key]; ok {
			t.Fatalf("disabled thinking kept effort field %s=%v", key, obj[key])
		}
	}
	thinking, ok := obj["thinking"].(map[string]any)
	if !ok || thinking["type"] != providers.DisabledThinkingType {
		t.Fatalf("thinking marker=%v", obj["thinking"])
	}
}

func TestApplyChatReasoningDisabledDoesNotHijackLevels(t *testing.T) {
	caps := providers.ModelCapabilities{
		ReasoningOptions: []string{"low", "high", "max"},
		ReasoningDefault: "high",
	}
	// Negative controls: enabled thinking keeps the catalog default, while
	// reasoning_effort:none degrades to the cheapest declared tier instead of
	// being clamped back up to the default.
	for _, tc := range []struct {
		name       string
		req        translate.ChatRequest
		wantEffort string
	}{
		{"enabled thinking", translate.ChatRequest{
			Model: "deepseek-v4.1-flash", Thinking: json.RawMessage(`{"type":"enabled"}`),
		}, "high"},
		{"effort none degrades to lowest declared", translate.ChatRequest{
			Model: "deepseek-v4.1-flash", ReasoningEffort: json.RawMessage(`"none"`),
		}, "low"},
		{"effort low preserved", translate.ChatRequest{
			Model: "deepseek-v4.1-flash", ReasoningEffort: json.RawMessage(`"low"`),
		}, "low"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := map[string]any{}
			applyChatReasoning(obj, tc.req, "", caps)
			if _, ok := obj["thinking"]; ok {
				t.Fatalf("level path must not set thinking marker: %v", obj)
			}
			if obj["reasoning_effort"] != tc.wantEffort {
				t.Fatalf("reasoning_effort=%v want %s", obj["reasoning_effort"], tc.wantEffort)
			}
		})
	}
}
