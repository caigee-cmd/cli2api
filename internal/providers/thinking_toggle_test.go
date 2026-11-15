package providers

import (
	"encoding/json"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

func TestRequestsDisabledThinking(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"object disabled", `{"type":"disabled"}`, true},
		{"object case insensitive", `{"type":"Disabled"}`, true},
		{"bare string disabled", `"disabled"`, true},
		{"object enabled is not off", `{"type":"enabled"}`, false},
		{"object enabled with budget", `{"type":"enabled","budget_tokens":1024}`, false},
		{"object without type", `{"budget_tokens":1024}`, false},
		{"empty object", `{}`, false},
		{"null", `null`, false},
		{"string enabled", `"enabled"`, false},
		{"array with disabled", `[{"type":"disabled"}]`, true},
		{"array without disabled", `[{"type":"enabled"}]`, false},
		{"malformed", `{`, false},
		{"effort none is NOT this channel", `"none"`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := translate.ChatRequest{Thinking: json.RawMessage(tc.body)}
			if got := RequestsDisabledThinking(req); got != tc.want {
				t.Fatalf("RequestsDisabledThinking(%s)=%v want %v", tc.body, got, tc.want)
			}
		})
	}
}

func TestRequestsDisabledThinkingIgnoresOtherChannels(t *testing.T) {
	// Negative control: reasoning_effort / enable_thinking are level-path
	// signals and must not be mistaken for the nested thinking off switch.
	for _, req := range []translate.ChatRequest{
		{ReasoningEffort: json.RawMessage(`"none"`)},
		{EnableThinking: boolPtr(false)},
		{IsReasoning: boolPtr(false)},
		{},
	} {
		if RequestsDisabledThinking(req) {
			t.Fatalf("level-path signal must not trigger the off switch: %+v", req)
		}
	}
}

func boolPtr(v bool) *bool { return &v }

func TestDisabledThinkingField(t *testing.T) {
	got := DisabledThinkingField()
	if got["type"] != DisabledThinkingType {
		t.Fatalf("DisabledThinkingField()=%v", got)
	}
}
