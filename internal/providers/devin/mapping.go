package devin

import (
	"strings"
)

var knownEffortSuffixes = []string{
	"-none",
	"-low",
	"-medium",
	"-high",
	"-xhigh",
	"-max",
	"-fast",
	"-priority",
	"-low-priority",
	"-medium-priority",
	"-high-priority",
	"-xhigh-priority",
	"-max-priority",
}

var specialAliases = map[string]string{
	"claude-haiku-4-5": "MODEL_PRIVATE_11",
	"gpt-4-1":          "MODEL_CHAT_GPT_4_1_2025_04_14",
}

var standardLevelOrder = []string{"minimal", "low", "medium", "high", "xhigh", "max"}

// PublicModelID returns the provider-prefixed public model ID when needed.
func PublicModelID(native string) string {
	native = strings.TrimSpace(native)
	if native == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(native), "devin/") {
		return native
	}
	return "devin/" + native
}

func HasEffortSuffix(model string) bool {
	lower := strings.ToLower(strings.TrimSpace(model))
	for _, s := range knownEffortSuffixes {
		if strings.HasSuffix(lower, s) {
			return true
		}
	}
	return false
}

func NormalizeThinkingLevel(level string, budgetTokens int) string {
	normalized := strings.ToLower(strings.TrimSpace(level))
	switch normalized {
	case "minimal", "low", "medium", "high", "xhigh", "max", "fast":
		return normalized
	case "none", "off", "disabled":
		return "none"
	case "auto", "adaptive":
		return "high"
	}
	if budgetTokens > 0 {
		switch {
		case budgetTokens <= 4096:
			return "low"
		case budgetTokens <= 16384:
			return "medium"
		case budgetTokens <= 32768:
			return "high"
		default:
			return "max"
		}
	}
	return ""
}

// ResolveChatModelUID resolves a model identifier into an upstream chat_model_uid.
// catalogLevels is an optional map of base model → allowed thinking levels from
// a fetched/fixture catalog. When nil or missing, bare models stay bare.
func ResolveChatModelUID(rawModel, thinkingLevel string, budgetTokens int, catalogLevels map[string][]string) string {
	model := strings.TrimSpace(rawModel)
	if model == "" {
		return DefaultModelUID
	}
	cleanModel := model
	if strings.HasPrefix(strings.ToLower(cleanModel), "devin/") {
		cleanModel = cleanModel[6:]
	}
	if HasEffortSuffix(cleanModel) {
		return cleanModel
	}

	baseModel := cleanModel
	if colonIdx := strings.LastIndex(cleanModel, ":"); colonIdx != -1 {
		baseModel = strings.TrimSpace(cleanModel[:colonIdx])
		thinkingLevel = strings.TrimSpace(cleanModel[colonIdx+1:])
	}

	effort := NormalizeThinkingLevel(thinkingLevel, budgetTokens)
	lowerBase := strings.ToLower(baseModel)
	canonicalBase := strings.ReplaceAll(lowerBase, ".", "-")

	if alias, exists := specialAliases[canonicalBase]; exists {
		return alias
	}
	if canonicalBase == "claude-sonnet-4-5" || strings.Contains(canonicalBase, "sonnet-4-5") {
		if effort != "" && effort != "none" {
			return "MODEL_PRIVATE_3"
		}
		return "MODEL_PRIVATE_2"
	}
	if canonicalBase == "gemini-3-flash" {
		canonicalBase = "gemini-3-8-flash"
	}

	allowedLevels := catalogLevels[canonicalBase]
	if len(allowedLevels) == 0 && canonicalBase != lowerBase {
		allowedLevels = catalogLevels[lowerBase]
	}

	switch canonicalBase {
	case "swe-1-7":
		if effort == "medium" {
			return "swe-1-7-medium"
		}
		return "swe-1-7"
	case "swe-1-6":
		if effort == "fast" {
			return "swe-1-6-fast"
		}
		return "swe-1-6"
	case "glm-5-2":
		if effort == "none" {
			return "glm-5-2-none"
		}
		if effort == "max" {
			return "glm-5-2-max"
		}
		return "glm-5-2"
	}

	if len(allowedLevels) == 0 {
		return canonicalBase
	}
	defaultEffort := selectDefaultEffort(canonicalBase, allowedLevels)
	clamped := clampEffort(effort, allowedLevels, defaultEffort)
	return canonicalBase + "-" + clamped
}

func selectDefaultEffort(baseModel string, levels []string) string {
	if strings.Contains(baseModel, "swe-2") {
		return "high"
	}
	hasNone, hasLow, hasMedium, hasHigh := false, false, false, false
	for _, l := range levels {
		switch l {
		case "none":
			hasNone = true
		case "low":
			hasLow = true
		case "medium":
			hasMedium = true
		case "high":
			hasHigh = true
		}
	}
	if hasNone && hasLow && strings.HasPrefix(baseModel, "gpt-5") {
		return "low"
	}
	if hasHigh && (strings.Contains(baseModel, "gemini") ||
		strings.Contains(baseModel, "grok") ||
		strings.Contains(baseModel, "glm") ||
		strings.Contains(baseModel, "deepseek") ||
		strings.Contains(baseModel, "kimi") ||
		strings.Contains(baseModel, "nemotron")) {
		return "high"
	}
	if hasMedium {
		return "medium"
	}
	if hasHigh {
		return "high"
	}
	if hasLow {
		return "low"
	}
	return levels[0]
}

func levelIndex(level string) int {
	lower := strings.ToLower(strings.TrimSpace(level))
	for i, l := range standardLevelOrder {
		if l == lower {
			return i
		}
	}
	return -1
}

func clampEffort(requested string, allowed []string, defaultEffort string) string {
	if requested == "" {
		return defaultEffort
	}
	reqLower := strings.ToLower(strings.TrimSpace(requested))
	for _, a := range allowed {
		if reqLower == strings.ToLower(strings.TrimSpace(a)) {
			return a
		}
	}
	if reqLower == "none" {
		return defaultEffort
	}
	reqIdx := levelIndex(reqLower)
	if reqIdx == -1 {
		return defaultEffort
	}
	bestMatch := defaultEffort
	bestDist := 999
	bestIdx := -1
	for _, a := range allowed {
		aIdx := levelIndex(a)
		if aIdx == -1 {
			continue
		}
		dist := reqIdx - aIdx
		if dist < 0 {
			dist = -dist
		}
		if dist < bestDist || (dist == bestDist && aIdx > bestIdx) {
			bestDist = dist
			bestMatch = a
			bestIdx = aIdx
		}
	}
	return bestMatch
}
