package trae

import (
	"encoding/json"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func TestNormalizeReasoningLevelAliases(t *testing.T) {
	cases := map[string]string{
		"extra_high": "xhigh",
		"Extra high": "xhigh",
		"light":      "low",
		"off":        "none",
		"HIGH":       "high",
	}
	for input, want := range cases {
		if got := normalizeReasoningLevel(input); got != want {
			t.Fatalf("normalizeReasoningLevel(%q)=%q want %q", input, got, want)
		}
	}
}

func TestClampReasoningLevelDropsUnknownAndFallsBack(t *testing.T) {
	caps := providers.ModelCapabilities{ReasoningOptions: []string{"low", "medium"}, ReasoningDefault: "low"}
	if got := clampReasoningLevel("xhigh", caps); got != "low" {
		t.Fatalf("clamp=%q", got)
	}
	if got := clampReasoningLevel("high", providers.ModelCapabilities{}); got != "" {
		t.Fatalf("no options should drop: %q", got)
	}
}

func TestApplySoloChatFieldsMapsOpenAIAndStripsQoderKeys(t *testing.T) {
	on := true
	req := translate.ChatRequest{
		EnableThinking:  &on,
		ReasoningEffort: json.RawMessage(`"extra_high"`),
	}
	obj := map[string]any{
		"context_length": 500000, "max_input_tokens": 500000, "enable_thinking": true,
	}
	caps := providers.ModelCapabilities{
		MaxMode: true, ReasoningOptions: []string{"low", "medium", "high", "xhigh"}, ReasoningDefault: "medium",
	}
	applySoloChatFields(obj, req, true, caps)
	if obj["is_max_mode"] != 1 {
		t.Fatalf("is_max_mode=%v", obj["is_max_mode"])
	}
	if obj["reasoning_effort_level"] != "xhigh" {
		t.Fatalf("level=%v", obj["reasoning_effort_level"])
	}
	for _, key := range []string{"context_length", "max_input_tokens", "enable_thinking", "reasoning_effort"} {
		if _, ok := obj[key]; ok {
			t.Fatalf("leaked %s", key)
		}
	}
}

func TestApplySoloChatFieldsOmitsMaxWhenUnsupported(t *testing.T) {
	obj := map[string]any{}
	applySoloChatFields(obj, translate.ChatRequest{}, true, providers.ModelCapabilities{})
	if _, ok := obj["is_max_mode"]; ok {
		t.Fatal("unsupported max mode should not be sent")
	}
}
