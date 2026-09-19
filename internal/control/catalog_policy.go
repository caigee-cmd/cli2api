package control

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

var modelsNumericCapFields = []string{
	"catalog_context_length", "catalog_context_length_max",
	"max_output_tokens", "prompt_max_tokens",
}

var modelsBoolCapFields = []string{"supports_max_mode", "can_disable_thinking"}

func AddModelRegion(entry map[string]any, region string) {
	region = strings.TrimSpace(region)
	if region == "" {
		return
	}
	for _, existing := range EntryModelRegions(entry) {
		if existing == region {
			return
		}
	}
	entry["regions"] = append(EntryModelRegions(entry), region)
}

func EntryModelRegions(entry map[string]any) []string {
	if raw, ok := entry["regions"].([]string); ok {
		return raw
	}
	if region, ok := entry["region"].(string); ok {
		region = strings.TrimSpace(region)
		if region != "" {
			return []string{region}
		}
	}
	return nil
}

// MergeModelEntryCapabilities folds a later source entry into the merged one
// conservatively: numeric fields take the minimum and boolean fields are ANDed.
func MergeModelEntryCapabilities(merged, incoming map[string]any) {
	for _, field := range modelsNumericCapFields {
		mergedValue, ok1 := numericFieldValue(merged[field])
		incomingValue, ok2 := numericFieldValue(incoming[field])
		if ok1 && ok2 && incomingValue < mergedValue {
			merged[field] = incomingValue
		}
	}
	for _, field := range modelsBoolCapFields {
		if value, ok := incoming[field].(bool); ok && !value {
			merged[field] = false
		}
	}
}

func modelEntryCredits(entry map[string]any) string {
	credits, _ := entry["credits"].(string)
	return strings.TrimSpace(credits)
}

func modelEntryFree(entry map[string]any) (bool, bool) {
	free, ok := entry["free"].(bool)
	return free, ok
}

// MergeModelEntryPricing drops credits/free when source regions disagree.
func MergeModelEntryPricing(merged, incoming map[string]any) {
	if modelEntryCredits(merged) != modelEntryCredits(incoming) {
		delete(merged, "credits")
		delete(merged, "free")
		return
	}
	mergedFree, hasMergedFree := modelEntryFree(merged)
	incomingFree, hasIncomingFree := modelEntryFree(incoming)
	if hasMergedFree != hasIncomingFree || mergedFree != incomingFree {
		delete(merged, "free")
	}
}

func numericFieldValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case float64:
		return typed, true
	default:
		return 0, false
	}
}

func ModelCapabilitiesEntry(model providers.ModelInfo) map[string]any {
	entry := map[string]any{}
	if model.Capabilities.ContextWindow > 0 {
		entry["catalog_context_length"] = model.Capabilities.ContextWindow
	}
	if model.Capabilities.ContextWindowMax > 0 {
		entry["catalog_context_length_max"] = model.Capabilities.ContextWindowMax
	}
	if model.Capabilities.MaxOutput > 0 {
		entry["max_output_tokens"] = model.Capabilities.MaxOutput
	}
	if model.Capabilities.PromptMaxTokens > 0 {
		entry["prompt_max_tokens"] = model.Capabilities.PromptMaxTokens
	}
	if model.Capabilities.MaxMode {
		entry["supports_max_mode"] = true
	}
	if model.Capabilities.CanDisableThinking {
		entry["can_disable_thinking"] = true
	}
	return entry
}

func ProviderModelEntry(model providers.ModelInfo, provider string) map[string]any {
	entry := map[string]any{
		"id": model.PublicModel, "object": "model", "owned_by": provider,
		"provider": provider, "native_model": model.NativeModel,
	}
	if strings.TrimSpace(model.DisplayName) != "" {
		entry["display_name"] = model.DisplayName
	}
	if credits := strings.TrimSpace(model.Credits); credits != "" {
		entry["credits"] = credits
	}
	if model.Free {
		entry["free"] = true
	}
	if model.Capabilities.ContextWindow > 0 {
		entry["catalog_context_length"] = model.Capabilities.ContextWindow
	}
	if model.Capabilities.ContextWindowMax > 0 {
		entry["catalog_context_length_max"] = model.Capabilities.ContextWindowMax
	}
	if model.Capabilities.MaxOutput > 0 {
		entry["max_output_tokens"] = model.Capabilities.MaxOutput
	}
	if model.Capabilities.PromptMaxTokens > 0 {
		entry["prompt_max_tokens"] = model.Capabilities.PromptMaxTokens
	}
	if model.Capabilities.MaxMode {
		entry["supports_max_mode"] = true
	}
	if len(model.Capabilities.ReasoningOptions) > 0 {
		entry["reasoning_options"] = model.Capabilities.ReasoningOptions
	}
	if model.Capabilities.ReasoningDefault != "" {
		entry["reasoning_default"] = model.Capabilities.ReasoningDefault
	}
	if model.Capabilities.ReasoningType != "" {
		entry["reasoning_type"] = model.Capabilities.ReasoningType
	}
	if model.Capabilities.CanDisableThinking {
		entry["can_disable_thinking"] = true
	}
	return entry
}

// FilterModelsForIdentity applies grants after loading the shared catalog.
func FilterModelsForIdentity(identity auth.Identity, models []map[string]any) []map[string]any {
	if len(identity.AllowedProviders) == 0 {
		return models
	}
	filtered := make([]map[string]any, 0, len(models))
	for _, model := range models {
		provider, _ := model["provider"].(string)
		if provider == "" {
			provider, _ = model["owned_by"].(string)
		}
		if !identity.AllowsProvider(provider) || !identityAllowsAnyModelRegion(identity, model) {
			continue
		}
		filtered = append(filtered, model)
	}
	return filtered
}

func identityAllowsAnyModelRegion(identity auth.Identity, model map[string]any) bool {
	regions := EntryModelRegions(model)
	if len(regions) == 0 {
		return true
	}
	provider, _ := model["provider"].(string)
	if provider == "" {
		provider, _ = model["owned_by"].(string)
	}
	for _, region := range regions {
		if identity.AllowsProviderRegion(provider, region) {
			return true
		}
	}
	return false
}

func catalogInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case json.Number:
		n, err := typed.Int64()
		return int(n), err == nil
	default:
		return 0, false
	}
}

func decorateProviderSettings(ctx context.Context, settings *Settings, item map[string]any, provider, settingsKey string) {
	dev, _ := catalogInt(item["catalog_context_length"])
	max, _ := catalogInt(item["catalog_context_length_max"])
	supportsMax, _ := item["supports_max_mode"].(bool)
	if provider == "trae" && !supportsMax && max > 0 && max != dev {
		supportsMax = true
		item["supports_max_mode"] = true
	}
	var setting accounts.ProviderModelSetting
	if settings != nil {
		setting, _ = settings.GetProviderModelSetting(ctx, provider, settingsKey)
	}
	maxMode := setting.MaxMode && supportsMax && provider == "trae"
	item["max_mode"] = maxMode
	window := dev
	if maxMode && max > 0 {
		window = max
	}
	if window > 0 {
		item["context_length"] = window
		item["default_context_length"] = dev
	}
	defaultLevel, _ := item["reasoning_default"].(string)
	selected := defaultLevel
	if setting.ReasoningEffort != "" {
		selected = setting.ReasoningEffort
	}
	if selected != "" {
		item["reasoning_effort"] = selected
	}
	item["context_custom"] = maxMode || (setting.ReasoningEffort != "" && setting.ReasoningEffort != defaultLevel)
}

// DecorateModelsWithContext applies console settings without mutating the
// shared catalog snapshot.
func DecorateModelsWithContext(ctx context.Context, settings *Settings, models []map[string]any) []map[string]any {
	configured := map[string]int{}
	if settings != nil {
		listed, err := settings.ListModelContexts(ctx)
		if err == nil {
			configured = listed
		}
	}
	decorated := make([]map[string]any, 0, len(models))
	for _, model := range models {
		item := make(map[string]any, len(model)+4)
		for key, value := range model {
			item[key] = value
		}
		id, _ := item["id"].(string)
		provider, _ := item["provider"].(string)
		if provider == "" {
			provider, _ = item["owned_by"].(string)
		}
		provider = strings.ToLower(strings.TrimSpace(provider))
		settingsKey := ModelContextKey(id)
		item["settings_key"] = settingsKey
		item["context_editable"] = provider == "" || provider == "qoder"
		if catalogWindow, ok := catalogInt(item["catalog_context_length"]); ok && catalogWindow > 0 {
			item["catalog_context_length"] = catalogWindow
		}
		switch provider {
		case "trae", "workbuddy":
			decorateProviderSettings(ctx, settings, item, provider, settingsKey)
		default:
			defaultValue := DefaultContextForModel(settingsKey)
			value, custom := configured[settingsKey]
			if !custom {
				value = defaultValue
			}
			item["context_length"] = value
			item["default_context_length"] = defaultValue
			item["context_custom"] = custom
		}
		decorated = append(decorated, item)
	}
	return decorated
}
