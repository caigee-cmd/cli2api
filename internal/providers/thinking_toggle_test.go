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

func TestParseReasoningIntent(t *testing.T) {
	cases := []struct {
		name     string
		req      translate.ChatRequest
		level    string
		disabled bool
		budget   int
	}{
		{name: "effort string", req: translate.ChatRequest{ReasoningEffort: json.RawMessage(`"high"`)}, level: "high"},
		{name: "effort object", req: translate.ChatRequest{ReasoningEffort: json.RawMessage(`{"effort":"xhigh"}`)}, level: "xhigh"},
		{name: "thinking budget", req: translate.ChatRequest{Thinking: json.RawMessage(`{"type":"enabled","budget_tokens":16384}`)}, level: "high", budget: 16384},
		{name: "disabled thinking", req: translate.ChatRequest{Thinking: json.RawMessage(`{"type":"disabled"}`)}, level: "none", disabled: true},
		{name: "boolean on", req: translate.ChatRequest{EnableThinking: boolPtr(true)}, level: "medium"},
		{name: "boolean off", req: translate.ChatRequest{EnableThinking: boolPtr(false)}, level: "none"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseReasoningIntent(tc.req)
			if got.Level != tc.level || got.Disabled != tc.disabled || got.Budget != tc.budget {
				t.Fatalf("intent=%+v want level=%q disabled=%v budget=%d", got, tc.level, tc.disabled, tc.budget)
			}
		})
	}
}

func TestReasoningOptionIncludes(t *testing.T) {
	if !ReasoningOptionIncludes([]string{"low", "high"}, "HIGH") {
		t.Fatal("high must be found")
	}
	if ReasoningOptionIncludes([]string{"low", "high"}, "none") {
		t.Fatal("none must stay absent")
	}
}
