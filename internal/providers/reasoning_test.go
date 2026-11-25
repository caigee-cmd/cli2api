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

func TestResolveReasoningLevelDegradesLowerTiersToNearestDeclared(t *testing.T) {
	caps := ModelCapabilities{ReasoningOptions: []string{"low", "high"}, ReasoningDefault: "high"}
	if got := ResolveReasoningLevel("medium", caps); got != "low" {
		t.Fatalf("medium clamp=%q, want low", got)
	}
	if got := ResolveReasoningLevel("bogus", caps); got != "high" {
		t.Fatalf("unknown clamp=%q, want default high", got)
	}
	if got := ResolveReasoningLevel("high", ModelCapabilities{}); got != "" {
		t.Fatalf("empty caps clamp=%q, want empty", got)
	}
}

func TestResolveReasoningLevelBelowFloorUsesLowestDeclared(t *testing.T) {
	// deepseek-v4.1-flash on WorkBuddy advertises low/high/max with high as the
	// default. A client that disables thinking satisfies none of those, and the
	// answer is the cheapest declared tier — never the default high.
	caps := ModelCapabilities{ReasoningOptions: []string{"low", "high", "max"}, ReasoningDefault: "high"}
	if got := ResolveReasoningLevel("none", caps); got != "low" {
		t.Fatalf("none clamp=%q, want low", got)
	}
	if got := ResolveReasoningLevel("off", caps); got != "low" {
		t.Fatalf("off clamp=%q, want low", got)
	}
	if got := ResolveReasoningLevel("max", caps); got != "max" {
		t.Fatalf("max clamp=%q, want max (never downgraded)", got)
	}
	if got := ResolveReasoningLevel("", caps); got != "high" {
		t.Fatalf("empty request=%q, want default high", got)
	}
}

func TestResolveReasoningLevelKeepsDeclaredOff(t *testing.T) {
	caps := ModelCapabilities{ReasoningOptions: []string{"none", "low", "high"}, ReasoningDefault: "high"}
	if got := ResolveReasoningLevel("none", caps); got != "none" {
		t.Fatalf("none on an off-capable model=%q, want none", got)
	}
}

func TestResolveReasoningLevelDegradesDownOnly(t *testing.T) {
	caps := ModelCapabilities{ReasoningOptions: []string{"low", "medium", "high"}, ReasoningDefault: "medium"}
	for requested, want := range map[string]string{
		"max":    "high",
		"xhigh":  "high",
		"high":   "high",
		"medium": "medium",
		"low":    "low",
		"none":   "low",
	} {
		if got := ResolveReasoningLevel(requested, caps); got != want {
			t.Fatalf("%s resolved to %q, want %q", requested, got, want)
		}
	}
}
