package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

const (
	defaultContextLength  = 180000
	miniMaxM3ContextLimit = 1000000
)

func CanonicalModelID(model string) string {
	key := strings.ToLower(strings.TrimSpace(model))
	key = strings.NewReplacer("_", "-", " ", "-").Replace(key)
	return key
}
func ModelContextKey(model string) string {
	return CanonicalModelID(model)
}

func defaultContextForModel(model string) int {
	if CanonicalModelID(model) == "minimax-m3" {
		return miniMaxM3ContextLimit
	}
	return defaultContextLength
}

func asInt(value any) (int, bool) {
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

func (a *App) decorateProviderSettings(ctx context.Context, item map[string]any, provider, settingsKey string) {
	dev, _ := asInt(item["catalog_context_length"])
	max, _ := asInt(item["catalog_context_length_max"])
	supportsMax, _ := item["supports_max_mode"].(bool)
	if provider == "trae" && !supportsMax && max > 0 && max != dev {
		supportsMax = true
		item["supports_max_mode"] = true
	}
	var setting accounts.ProviderModelSetting
	if a.Control != nil && a.Control.Settings != nil {
		setting, _ = a.Control.Settings.GetProviderModelSetting(ctx, provider, settingsKey)
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

func (a *App) decorateModelsWithContext(ctx context.Context, models []map[string]any) []map[string]any {
	settings := map[string]int{}
	if a.Control != nil && a.Control.Settings != nil {
		listed, err := a.Control.Settings.ListModelContexts(ctx)
		if err == nil {
			settings = listed
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
		if catalogWindow, ok := asInt(item["catalog_context_length"]); ok && catalogWindow > 0 {
			item["catalog_context_length"] = catalogWindow
		}
		switch provider {
		case "trae", "workbuddy":
			a.decorateProviderSettings(ctx, item, provider, settingsKey)
		default:
			defaultValue := defaultContextForModel(settingsKey)
			value, custom := settings[settingsKey]
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
