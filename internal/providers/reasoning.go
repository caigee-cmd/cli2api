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
		for _, option := range caps.ReasoningOptions {
			if option == normalized {
				return normalized
			}
		}
	}
	if fallback := NormalizeReasoningLevel(caps.ReasoningDefault); fallback != "" {
		for _, option := range caps.ReasoningOptions {
			if option == fallback {
				return fallback
			}
		}
	}
	return caps.ReasoningOptions[0]
}
