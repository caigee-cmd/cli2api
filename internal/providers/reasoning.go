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

// ResolveReasoningLevel clamps a client/console requested level to what the
// model's catalog actually declares. Exact matches always win. When the
// requested level is a high tier the model does not declare, degrade through
// the nearest lower tiers (like LiteLLM's max->xhigh->high chain) instead of
// jumping to an arbitrary first/default value. This avoids both inventing a
// level and silently downgrading "max" to "low" when "high" is available.
// Unknown or lower-tier values keep the older default/first fallback.
func ResolveReasoningLevel(level string, caps ModelCapabilities) string {
	if len(caps.ReasoningOptions) == 0 {
		return ""
	}
	normalized := NormalizeReasoningLevel(level)
	if normalized != "" {
		if containsReasoningOption(caps.ReasoningOptions, normalized) {
			return normalized
		}
		for _, candidate := range reasoningDegradationChain(normalized) {
			if containsReasoningOption(caps.ReasoningOptions, candidate) {
				return candidate
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

// reasoningDegradationChain returns lower tiers ordered from nearest to
// farthest. Only high tiers need explicit degradation; lower/unknown values
// fall through to the model default in ResolveReasoningLevel.
func reasoningDegradationChain(level string) []string {
	switch level {
	case "max":
		return []string{"xhigh", "high", "medium", "low", "none"}
	case "xhigh":
		return []string{"high", "medium", "low", "none"}
	default:
		return nil
	}
}
