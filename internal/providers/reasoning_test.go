package providers

import "testing"

func TestResolveReasoningLevelDegradesHighTiersToNearestDeclared(t *testing.T) {
	caps := ModelCapabilities{ReasoningOptions: []string{"low", "high"}, ReasoningDefault: "low"}
	if got := ResolveReasoningLevel("max", caps); got != "high" {
		t.Fatalf("max clamp=%q, want high", got)
	}
	if got := ResolveReasoningLevel("xhigh", caps); got != "high" {
		t.Fatalf("xhigh clamp=%q, want high", got)
	}
	if got := ResolveReasoningLevel("extra_high", caps); got != "high" {
		t.Fatalf("extra_high clamp=%q, want high", got)
	}
}

func TestResolveReasoningLevelKeepsDeclaredMax(t *testing.T) {
	caps := ModelCapabilities{ReasoningOptions: []string{"low", "high", "xhigh", "max"}, ReasoningDefault: "high"}
	if got := ResolveReasoningLevel("max", caps); got != "max" {
		t.Fatalf("max clamp=%q, want max", got)
	}
	if got := ResolveReasoningLevel("ultra", caps); got != "max" {
		t.Fatalf("ultra clamp=%q, want max", got)
	}
}

func TestReasoningLevelFromThinkingBudget(t *testing.T) {
	for budget, want := range map[int]string{
		0:     "",
		512:   "low",
		2048:  "low",
		2049:  "medium",
		8192:  "medium",
		8193:  "high",
		16384: "high",
		16385: "max",
		24576: "max",
	} {
		if got := ReasoningLevelFromThinkingBudget(budget); got != want {
			t.Fatalf("budget %d => %q, want %q", budget, got, want)
		}
	}
}

func TestResolveReasoningLevelKeepsOlderFallbackForUnknownAndLowerTiers(t *testing.T) {
	caps := ModelCapabilities{ReasoningOptions: []string{"low", "high"}, ReasoningDefault: "high"}
	if got := ResolveReasoningLevel("medium", caps); got != "high" {
		t.Fatalf("medium clamp=%q, want default high", got)
	}
	if got := ResolveReasoningLevel("bogus", caps); got != "high" {
		t.Fatalf("unknown clamp=%q, want default high", got)
	}
	if got := ResolveReasoningLevel("high", ModelCapabilities{}); got != "" {
		t.Fatalf("empty caps clamp=%q, want empty", got)
	}
}
