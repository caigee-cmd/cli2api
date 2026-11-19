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
