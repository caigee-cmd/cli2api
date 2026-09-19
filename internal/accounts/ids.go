package accounts

import "strings"

const (
	RoutingStrategyRoundRobin         = "round-robin"
	RoutingStrategyWeightedRoundRobin = "weighted-round-robin"
	RoutingStrategyFillFirst          = "fill-first"
)

func NormalizeRoutingStrategy(strategy string) string {
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case RoutingStrategyFillFirst:
		return RoutingStrategyFillFirst
	case RoutingStrategyWeightedRoundRobin:
		return RoutingStrategyWeightedRoundRobin
	default:
		return RoutingStrategyRoundRobin
	}
}

// NormalizeProviderFamily maps a stored or requested provider ID onto the
// canonical family name. An empty value is Qoder, matching the historical
// default account family.
func NormalizeProviderFamily(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return "qoder"
	}
	return provider
}

// NormalizeRegion maps a stored or requested region onto the canonical
// routing region. An empty value is global.
func NormalizeRegion(region string) string {
	region = strings.ToLower(strings.TrimSpace(region))
	if region == "" {
		return "global"
	}
	return region
}

func CanonicalModelID(model string) string {
	key := strings.ToLower(strings.TrimSpace(model))
	key = strings.NewReplacer("_", "-", " ", "-").Replace(key)
	if key == "" {
		return "auto"
	}
	return key
}

// NormalizeModelName converts display-name formats sent by external clients
// into the canonical model ID used for routing. It currently strips a leading
// "Provider: " segment (single-word provider, no spaces) before lowercasing
// and folding separators so names like "DeepSeek: DeepSeek V4.1 Flash" become
// "deepseek-v4.1-flash".
func NormalizeModelName(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return model
	}
	parts := strings.SplitN(model, ":", 2)
	if len(parts) == 2 {
		provider := strings.TrimSpace(parts[0])
		displayName := strings.TrimSpace(parts[1])
		if !strings.Contains(provider, " ") {
			model = displayName
		}
	}
	return CanonicalModelID(model)
}

// NormalizeWeight maps a stored priority onto a scheduling weight. The
// console exposes 1..100 with 50 as the default; anything outside the range
// falls back to the default so a bad row cannot distort rotation.
func NormalizeWeight(priority int) int {
	if priority < 1 || priority > 100 {
		return defaultWeight
	}
	return priority
}

const defaultWeight = 50
