package providers

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

// TestParseReasoningIntentMatchesLegacyProviderInputs locks the behavior that
// each adapter had before ParseReasoningIntent was introduced. The refactor
// replaced eleven provider-local parsers, so every legacy input shape must
// still resolve to the same neutral level (or the documented provider-side
// "drop the field" result).
func TestParseReasoningIntentMatchesLegacyProviderInputs(t *testing.T) {
	type testCase struct {
		provider string
		name     string
		req      translate.ChatRequest
		want     string
	}
	cases := []testCase{
		// qoder: effort, enable flags, explicit disabled switch.
		{"qoder", "effort string", translate.ChatRequest{ReasoningEffort: json.RawMessage(`"high"`)}, "high"},
		{"qoder", "effort object", translate.ChatRequest{ReasoningEffort: json.RawMessage(`{"effort":"xhigh"}`)}, "xhigh"},
		{"qoder", "enable thinking on", translate.ChatRequest{EnableThinking: boolPtr(true)}, "medium"},
		{"qoder", "enable thinking off", translate.ChatRequest{EnableThinking: boolPtr(false)}, "none"},
		{"qoder", "enable reasoning on", translate.ChatRequest{EnableReasoning: boolPtr(true)}, "medium"},
		{"qoder", "is reasoning on", translate.ChatRequest{IsReasoning: boolPtr(true)}, "medium"},
		{"qoder", "disabled thinking", translate.ChatRequest{Thinking: json.RawMessage(`{"type":"disabled"}`)}, "none"},

		// trae: effort first, then enable flags.
		{"trae", "effort max", translate.ChatRequest{ReasoningEffort: json.RawMessage(`"max"`)}, "max"},
		{"trae", "enable reasoning off", translate.ChatRequest{EnableReasoning: boolPtr(false)}, "none"},
		{"trae", "no signal", translate.ChatRequest{}, ""},

		// workbuddy: effort first, then enable flags.
		{"workbuddy", "effort low", translate.ChatRequest{ReasoningEffort: json.RawMessage(`"low"`)}, "low"},
		{"workbuddy", "enable thinking on", translate.ChatRequest{EnableThinking: boolPtr(true)}, "medium"},
		{"workbuddy", "no signal", translate.ChatRequest{}, ""},

		// kimi: effort only.
		{"kimi", "effort medium", translate.ChatRequest{ReasoningEffort: json.RawMessage(`"medium"`)}, "medium"},
		{"kimi", "effort object", translate.ChatRequest{ReasoningEffort: json.RawMessage(`{"type":"high"}`)}, "high"},
		{"kimi", "no signal", translate.ChatRequest{}, ""},

		// zhipu: effort only.
		{"zhipu", "effort xhigh", translate.ChatRequest{ReasoningEffort: json.RawMessage(`"xhigh"`)}, "xhigh"},
		{"zhipu", "effort object level", translate.ChatRequest{ReasoningEffort: json.RawMessage(`{"level":"low"}`)}, "low"},
		{"zhipu", "no signal", translate.ChatRequest{}, ""},

		// codearts: effort only.
		{"codearts", "effort minimal", translate.ChatRequest{ReasoningEffort: json.RawMessage(`"minimal"`)}, "low"},
		{"codearts", "no signal", translate.ChatRequest{}, ""},

		// codex: legacy parser read a plain effort string only.
		{"codex", "effort high", translate.ChatRequest{ReasoningEffort: json.RawMessage(`"high"`)}, "high"},
		{"codex", "no signal", translate.ChatRequest{}, ""},

		// command: effort string, then thinking type/effort; "none" drops the field.
		{"command", "effort none drops", translate.ChatRequest{ReasoningEffort: json.RawMessage(`"none"`)}, ""},
		{"command", "thinking type high", translate.ChatRequest{Thinking: json.RawMessage(`{"type":"high"}`)}, "high"},
		{"command", "no signal", translate.ChatRequest{}, ""},

		// devin: effort string, then thinking type/effort.
		{"devin", "effort medium", translate.ChatRequest{ReasoningEffort: json.RawMessage(`"medium"`)}, "medium"},
		{"devin", "thinking type low", translate.ChatRequest{Thinking: json.RawMessage(`{"type":"low"}`)}, "low"},
		{"devin", "no signal", translate.ChatRequest{}, ""},

		// kiro: effort non-none, true flags; false and "none" drop the field.
		{"kiro", "effort high", translate.ChatRequest{ReasoningEffort: json.RawMessage(`"high"`)}, "high"},
		{"kiro", "effort none drops", translate.ChatRequest{ReasoningEffort: json.RawMessage(`"none"`)}, ""},
		{"kiro", "is reasoning on", translate.ChatRequest{IsReasoning: boolPtr(true)}, "medium"},
		{"kiro", "is reasoning off drops", translate.ChatRequest{IsReasoning: boolPtr(false)}, ""},

		// meta: plain effort string through the shared clamp.
		{"meta", "effort xhigh", translate.ChatRequest{ReasoningEffort: json.RawMessage(`"xhigh"`)}, "xhigh"},
		{"meta", "no signal", translate.ChatRequest{}, ""},
	}

	dropNoneForProvider := map[string]bool{"command": true, "kiro": true}
	for _, tc := range cases {
		t.Run(tc.provider+"/"+tc.name, func(t *testing.T) {
			intent := ParseReasoningIntent(tc.req)
			got := intent.Level
			if dropNoneForProvider[tc.provider] && (intent.Disabled || got == "none") {
				got = ""
			}
			if got != tc.want {
				t.Fatalf("%s: level=%q disabled=%v, want %q", tc.provider, got, intent.Disabled, tc.want)
			}
		})
	}
}

// TestThinkingBudgetTiersMatchAnthropicConversion pins the budget boundaries
// used by CC Switch's Anthropic converter so round-trips stay stable.
func TestThinkingBudgetTiersMatchAnthropicConversion(t *testing.T) {
	for _, tc := range []struct {
		budget int
		want   string
	}{
		{2048, "low"},
		{8192, "medium"},
		{16384, "high"},
		{24576, "max"},
		{32768, "max"},
	} {
		req := translate.ChatRequest{Thinking: json.RawMessage(`{"type":"enabled","budget_tokens":` + strconv.Itoa(tc.budget) + `}`)}
		if got := ParseReasoningIntent(req).Level; got != tc.want {
			t.Fatalf("budget %d => %q, want %q", tc.budget, got, tc.want)
		}
	}
}
