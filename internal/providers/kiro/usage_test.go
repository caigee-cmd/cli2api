package kiro

import (
	"math"
	"strings"
	"testing"
)

// capturedLegacyStream is what generateAssistantResponse sent for one short
// reply on a live account: no token counts, only a context share and credits.
func capturedLegacyStream() []streamEvent {
	return []streamEvent{
		{Type: "assistantResponseEvent", Event: map[string]any{"content": "ok", "modelId": "claude-sonnet-4.6"}},
		{Type: "metadataEvent", Event: map[string]any{"stopReason": "END_TURN"}},
		{Type: "contextUsageEvent", Event: map[string]any{"contextUsagePercentage": 0.4108999967575073}},
		{Type: "meteringEvent", Event: map[string]any{"unit": "credit", "unitPlural": "credits", "usage": 0.017389340696517413}},
	}
}

func finalUsage(t *testing.T, raw string) map[string]any {
	t.Helper()
	var usage map[string]any
	for _, event := range parseSSE(t, strings.ReplaceAll(raw, "data: [DONE]", "")) {
		if value, ok := event["usage"].(map[string]any); ok {
			usage = value
		}
	}
	if usage == nil {
		t.Fatalf("no usage chunk:\n%s", raw)
	}
	return usage
}

func TestLegacyStreamReportsEstimatedTokensAndCredits(t *testing.T) {
	var out strings.Builder
	writeConsumed(&out, "claude-sonnet-4.6", sliceSource(capturedLegacyStream()), nil, nil, nil)
	usage := finalUsage(t, out.String())

	// 0.4109% of the 1M window Claude 4.6 uses.
	if got := usage["prompt_tokens"]; got != float64(4109) {
		t.Fatalf("prompt_tokens = %v", got)
	}
	if got := usage["completion_tokens"]; got != float64(1) {
		t.Fatalf("completion_tokens = %v", got)
	}
	if got, _ := usage["credits"].(float64); math.Abs(got-0.017389340696517413) > 1e-12 {
		t.Fatalf("credits = %v", usage["credits"])
	}
	if usage["source"] != "estimate" {
		t.Fatalf("source = %v", usage["source"])
	}
}

func TestLegacyNonStreamKeepsUsage(t *testing.T) {
	var out strings.Builder
	writeConsumed(&out, "claude-sonnet-4.6", sliceSource(capturedLegacyStream()), nil, nil, nil)
	outcome, err := collectOutcome(strings.NewReader(out.String()), "medium")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.PromptTokens != 4109 || outcome.CompletionTokens != 1 || outcome.UsageSource != "estimate" {
		t.Fatalf("outcome = %+v", outcome)
	}
	if outcome.Credits == nil || math.Abs(*outcome.Credits-0.017389340696517413) > 1e-12 {
		t.Fatalf("credits = %v", outcome.Credits)
	}
}

func TestMeteringEventsAddUp(t *testing.T) {
	frames := []streamEvent{
		{Type: "meteringEvent", Event: map[string]any{"usage": 0.25}},
		{Type: "meteringEvent", Event: map[string]any{"usage": 0.5}},
	}
	var last float64
	if err := consumeEvents(sliceSource(frames), &streamCallbacks{OnCredits: func(v float64) { last = v }}); err != nil {
		t.Fatal(err)
	}
	if last != 0.75 {
		t.Fatalf("credits = %v", last)
	}
}

// CreateResponse states a credit total; a metering event after it must not
// be added on top.
func TestStatedCreditsAreNotDoubleCounted(t *testing.T) {
	frames := []streamEvent{
		{Type: "metadataEvent", Event: map[string]any{"credits": 1.5}},
		{Type: "meteringEvent", Event: map[string]any{"usage": 0.25}},
	}
	var last float64
	if err := consumeEvents(sliceSource(frames), &streamCallbacks{OnCredits: func(v float64) { last = v }}); err != nil {
		t.Fatal(err)
	}
	if last != 1.5 {
		t.Fatalf("credits = %v", last)
	}
}

// Token counts the upstream states are kept and not marked as an estimate.
func TestStatedTokensWin(t *testing.T) {
	frames := []streamEvent{
		{Type: "assistantResponseEvent", Event: map[string]any{"content": "a much longer reply than one token"}},
		{Type: "metadataEvent", Event: map[string]any{
			// float64, as encoding/json decodes upstream numbers.
			"usage": map[string]any{"input_tokens": float64(11), "output_tokens": float64(7)}, "credits": 0.5, "contextUsagePercentage": 3.0,
		}},
	}
	var out strings.Builder
	writeConsumed(&out, "claude-sonnet-4.5", sliceSource(frames), nil, nil, nil)
	usage := finalUsage(t, out.String())
	if usage["prompt_tokens"] != float64(11) || usage["completion_tokens"] != float64(7) || usage["source"] != "provider" {
		t.Fatalf("usage = %v", usage)
	}
}

func TestContextWindow(t *testing.T) {
	cases := map[string]int{
		"claude-haiku-4.5":  200_000,
		"claude-sonnet-4.5": 200_000,
		"claude-sonnet-4":   200_000,
		"claude-sonnet-4.6": 1_000_000,
		"claude-opus-4-7":   1_000_000,
		"claude-opus-5.5":   1_000_000,
		"claude-opus-5":     1_000_000,
		"auto":              200_000,
	}
	for model, want := range cases {
		if got := contextWindow(model); got != want {
			t.Errorf("contextWindow(%q) = %d, want %d", model, got, want)
		}
	}
}

func TestNoUsageWhenNothingToCount(t *testing.T) {
	var out strings.Builder
	writeConsumed(&out, "claude-sonnet-4.6", sliceSource(nil), nil, nil, nil)
	if strings.Contains(out.String(), `"usage"`) {
		t.Fatalf("unexpected usage:\n%s", out.String())
	}
}
