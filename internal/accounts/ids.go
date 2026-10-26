package accounts

import (
	"fmt"
	"strings"
)

const (
	RoutingStrategyRoundRobin         = "round-robin"
	RoutingStrategyWeightedRoundRobin = "weighted-round-robin"
	RoutingStrategyFillFirst          = "fill-first"
	// RoutingStrategyFEFO is the opt-in earliest-expiry-first strategy. It
	// ranks accounts inside one provider family by the nearest credit-package
	// expiry and leaves cross-provider rotation untouched.
	RoutingStrategyFEFO = "fefo"
)

func NormalizeRoutingStrategy(strategy string) string {
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case RoutingStrategyFillFirst:
		return RoutingStrategyFillFirst
	case RoutingStrategyWeightedRoundRobin:
		return RoutingStrategyWeightedRoundRobin
	case RoutingStrategyFEFO:
		return RoutingStrategyFEFO
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
// routing region. An empty value or the shorthand "gl" is global.
func NormalizeRegion(region string) string {
	region = strings.ToLower(strings.TrimSpace(region))
	if region == "" || region == "gl" {
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

// NormalizeAccountRouting canonicalizes one account's alias map and enabled
// model list. An alias points at exactly one real model on that account.
// The enabled list is the catalog models the account may serve; empty means
// the operator saved a list with nothing selected.
func NormalizeAccountRouting(aliases map[string]string, enabled []string) (map[string]string, []string, error) {
	normalizedAliases := map[string]string{}
	for alias, modelID := range aliases {
		key := CanonicalModelID(alias)
		target := CanonicalModelID(modelID)
		if key == "" || key == "auto" || target == "" || target == "auto" {
			return nil, nil, fmt.Errorf("alias and model are required")
		}
		if key == target {
			return nil, nil, fmt.Errorf("alias must differ from the real model")
		}
		if previous, exists := normalizedAliases[key]; exists && previous != target {
			return nil, nil, fmt.Errorf("%s maps to more than one model", key)
		}
		normalizedAliases[key] = target
	}
	seen := map[string]struct{}{}
	normalizedEnabled := make([]string, 0, len(enabled))
	for _, modelID := range enabled {
		key := CanonicalModelID(modelID)
		if key == "" || key == "auto" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalizedEnabled = append(normalizedEnabled, key)
	}
	if len(normalizedAliases) == 0 {
		normalizedAliases = nil
	}
	return normalizedAliases, normalizedEnabled, nil
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
