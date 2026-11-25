package providers

import "strings"

var reasoningLevels = map[string]string{
	"none":       "none",
	"off":        "none",
	"disabled":   "none",
	"low":        "low",
	"light":      "low",
	"minimal":    "low",
	"medium":     "medium",
	"default":    "medium",
	"high":       "high",
	"xhigh":      "xhigh",
	"x-high":     "xhigh",
	"extra_high": "xhigh",
	"extra-high": "xhigh",
	"extrahigh":  "xhigh",
	"max":        "max",
	"ultra":      "max",
}

func NormalizeReasoningLevel(raw string) string {
	key := strings.ToLower(strings.TrimSpace(raw))
	key = strings.NewReplacer(" ", "", "_", "-").Replace(key)
	if mapped, ok := reasoningLevels[key]; ok {
		return mapped
	}
	key = strings.ReplaceAll(key, "-", "_")
	if mapped, ok := reasoningLevels[key]; ok {
		return mapped
	}
	return ""
}

// UniqueReasoningOptionValues keeps the upstream spelling of each reasoning
// level. Request/routing code uses NormalizeReasoningLevel instead; the console
// uses this list so it can display values such as off, minimal, or ultra
// exactly as the provider declared them.
// ReasoningLevelFromThinkingBudget maps an Anthropic-style thinking budget to
// the gateway's neutral reasoning vocabulary. CC Switch uses the same tier
// boundaries when translating Codex effort into Anthropic thinking budgets:
// 2048 / 8192 / 16384 / 24576 for low / medium / high / max(ultra).
// We use the lower bound of each tier as the cut-off so a budget sent by that
// converter round-trips back to the same neutral level.
func ReasoningLevelFromThinkingBudget(budget int) string {
	switch {
	case budget <= 0:
		return ""
	case budget <= 2048:
		return "low"
	case budget <= 8192:
		return "medium"
	case budget <= 16384:
		return "high"
	default:
		return "max"
	}
}

func UniqueReasoningOptionValues(values []string) []string {
	seen := map[string]struct{}{}
	var options []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		options = append(options, value)
	}
	return options
}

// GatewayReasoningOptions returns the complete reasoning level vocabulary this
// gateway accepts from clients. Whether a level is actually forwarded is still
// decided by each provider's per-model catalog capabilities; models without a
// declared reasoning list keep their upstream default behavior.
func GatewayReasoningOptions() []string {
	return []string{"none", "low", "medium", "high", "xhigh", "max"}
}

func UniqueReasoningOptions(values []string) []string {
	seen := map[string]struct{}{}
	var options []string
	for _, value := range values {
		option := NormalizeReasoningLevel(value)
		if option == "" {
			continue
		}
		if _, dup := seen[option]; dup {
			continue
		}
		seen[option] = struct{}{}
		options = append(options, option)
	}
	return options
}

func ResolveReasoningLevel(level string, caps ModelCapabilities) string {
	if len(caps.ReasoningOptions) == 0 {
		return ""
	}
	normalized := NormalizeReasoningLevel(level)
	if normalized != "" {
		if containsReasoningOption(caps.ReasoningOptions, normalized) {
			return normalized
		}
		// Declared ladder: a level the model does not advertise degrades to
		// the nearest lower level it does advertise (CC Switch behavior), and
		// never upgrades. A request below the model's floor (for example "none"
		// against low/high/max) lands on the lowest declared level instead of
		// the model default, so "do not think hard" never becomes "think hard".
		if rank := reasoningRank(normalized); rank >= 0 {
			for _, candidate := range reasoningDegradationChain(normalized) {
				if containsReasoningOption(caps.ReasoningOptions, candidate) {
					return candidate
				}
			}
			if lowest := lowestReasoningOption(caps); lowest != "" {
				return lowest
			}
		}
	}
	if fallback := NormalizeReasoningLevel(caps.ReasoningDefault); fallback != "" {
		if containsReasoningOption(caps.ReasoningOptions, fallback) {
			return fallback
		}
	}
	return caps.ReasoningOptions[0]
}

// lowestReasoningOption returns the cheapest declared level in the neutral
// vocabulary, so a client asking for less thinking than a model supports still
// gets the minimum instead of the model's default tier.
func lowestReasoningOption(caps ModelCapabilities) string {
	lowest := ""
	lowestRank := len(reasoningLadder)
	for _, option := range caps.ReasoningOptions {
		normalized := NormalizeReasoningLevel(option)
		if normalized == "" {
			continue
		}
		if rank := reasoningRank(normalized); rank >= 0 && rank < lowestRank {
			lowest, lowestRank = normalized, rank
		}
	}
	return lowest
}

func containsReasoningOption(options []string, level string) bool {
	for _, option := range options {
		if NormalizeReasoningLevel(option) == level {
			return true
		}
	}
	return false
}

// ReasoningOptionIncludes reports whether a normalized level is declared in
// the capability set. Adapters use it for explicit off switches: when a model
// does not declare "none", an off request must be omitted rather than clamped
// back to a thinking level.
func ReasoningOptionIncludes(options []string, level string) bool {
	return containsReasoningOption(options, NormalizeReasoningLevel(level))
}

// reasoningLadder is the neutral effort ladder from cheapest to most expensive.
// CC Switch uses the same tiers when converting effort into thinking budgets
// (2048 / 8192 / 16384 / 24576), so ranking them here keeps one vocabulary.
var reasoningLadder = []string{"none", "low", "medium", "high", "xhigh", "max"}

func reasoningRank(level string) int {
	for i, candidate := range reasoningLadder {
		if candidate == level {
			return i
		}
	}
	return -1
}

// reasoningDegradationChain returns every lower tier ordered from nearest to
// farthest, and nil for an unknown level. Callers pick the first one the model
// actually declares.
func reasoningDegradationChain(level string) []string {
	rank := reasoningRank(level)
	if rank <= 0 {
		return nil
	}
	chain := make([]string, 0, rank)
	for i := rank - 1; i >= 0; i-- {
		chain = append(chain, reasoningLadder[i])
	}
	return chain
}
