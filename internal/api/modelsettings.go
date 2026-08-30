package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

const (
	defaultContextLength  = 180000
	miniMaxM3ContextLimit = 1000000
)

func canonicalModelID(model string) string {
	key := strings.ToLower(strings.TrimSpace(model))
	key = strings.NewReplacer("_", "-", " ", "-").Replace(key)
	return key
}
func modelContextKey(model string) string {
	return canonicalModelID(model)
}

func defaultContextForModel(model string) int {
	if canonicalModelID(model) == "minimax-m3" {
		return miniMaxM3ContextLimit
	}
	return defaultContextLength
}

func (s *Server) applyModelContextDefaults(ctx context.Context, req *translate.ChatRequest, providerFilter string) error {
	if req == nil || strings.TrimSpace(req.Model) == "" {
		return nil
	}
	if providerFilter != "" && providerFilter != "qoder" {
		return nil
	}
	contextLength, ok, err := s.manager.Store().GetModelContext(ctx, modelContextKey(req.Model))
	if err != nil || !ok {
		return err
	}
	value := json.RawMessage(strconv.Itoa(contextLength))
	if len(req.ContextLength) == 0 {
		req.ContextLength = append(json.RawMessage(nil), value...)
	}
	if len(req.MaxInputTokens) == 0 {
		req.MaxInputTokens = append(json.RawMessage(nil), value...)
	}
	return nil
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

func (s *Server) decorateProviderSettings(ctx context.Context, item map[string]any, provider, settingsKey string) {
	dev, _ := asInt(item["catalog_context_length"])
	max, _ := asInt(item["catalog_context_length_max"])
	supportsMax, _ := item["supports_max_mode"].(bool)
	if !supportsMax && max > 0 && max != dev {
		supportsMax = true
		item["supports_max_mode"] = true
	}
	setting, _ := s.manager.Store().GetProviderModelSetting(ctx, provider, settingsKey)
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

func (s *Server) decorateModelsWithContext(ctx context.Context, models []map[string]any) []map[string]any {
	settings, err := s.manager.Store().ListModelContexts(ctx)
	if err != nil {
		settings = map[string]int{}
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
		settingsKey := modelContextKey(id)
		item["settings_key"] = settingsKey
		item["context_editable"] = provider == "" || provider == "qoder"
		if catalogWindow, ok := asInt(item["catalog_context_length"]); ok && catalogWindow > 0 {
			item["catalog_context_length"] = catalogWindow
		}
		switch provider {
		case "trae", "workbuddy":
			s.decorateProviderSettings(ctx, item, provider, settingsKey)
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

func splitModelSettingPath(raw, queryProvider string) (provider, modelKey string) {
	raw = strings.TrimPrefix(raw, "/api/models/")
	provider = strings.ToLower(strings.TrimSpace(queryProvider))
	for _, prefix := range []string{"trae/", "workbuddy/", "qoder/"} {
		if strings.HasPrefix(strings.ToLower(raw), prefix) {
			provider = strings.TrimSuffix(prefix, "/")
			raw = raw[len(prefix):]
			break
		}
	}
	return provider, modelContextKey(raw)
}

func (s *Server) handleModelSetting(w http.ResponseWriter, r *http.Request) {
	provider, modelKey := splitModelSettingPath(r.URL.Path, r.URL.Query().Get("provider"))
	if modelKey == "" {
		writeErr(w, http.StatusBadRequest, "invalid_model", "model id required")
		return
	}
	if provider == "trae" || provider == "workbuddy" {
		s.handleProviderModelSetting(w, r, provider, modelKey)
		return
	}
	switch r.Method {
	case http.MethodGet:
		value, custom, err := s.manager.Store().GetModelContext(r.Context(), modelKey)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "model_setting_failed", err.Error())
			return
		}
		defaultValue := defaultContextForModel(modelKey)
		if !custom {
			value = defaultValue
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"model": modelKey, "context_length": value,
			"default_context_length": defaultValue, "context_custom": custom,
		})
	case http.MethodPatch:
		var input struct {
			ContextLength int `json:"context_length"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if err := s.manager.Store().SetModelContext(r.Context(), modelKey, input.ContextLength); err != nil {
			writeErr(w, http.StatusBadRequest, "model_setting_failed", err.Error())
			return
		}
		value := input.ContextLength
		custom := value > 0
		if !custom {
			value = defaultContextForModel(modelKey)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"model": modelKey, "context_length": value,
			"default_context_length": defaultContextForModel(modelKey), "context_custom": custom,
		})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or PATCH only")
	}
}

func (s *Server) handleProviderModelSetting(w http.ResponseWriter, r *http.Request, provider, modelKey string) {
	switch r.Method {
	case http.MethodGet:
		setting, err := s.manager.Store().GetProviderModelSetting(r.Context(), provider, modelKey)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "model_setting_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"model": modelKey, "provider": provider,
			"max_mode": setting.MaxMode, "reasoning_effort": setting.ReasoningEffort,
			"context_custom": setting.MaxMode || setting.ReasoningEffort != "",
		})
	case http.MethodPatch:
		var input struct {
			MaxMode         *bool   `json:"max_mode"`
			ReasoningEffort *string `json:"reasoning_effort"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if input.MaxMode == nil && input.ReasoningEffort == nil {
			writeErr(w, http.StatusBadRequest, "invalid_request", "max_mode or reasoning_effort required")
			return
		}
		if provider == "workbuddy" && input.MaxMode != nil {
			writeErr(w, http.StatusBadRequest, "invalid_request", "workbuddy has no max-mode switch")
			return
		}
		setting, err := s.manager.Store().GetProviderModelSetting(r.Context(), provider, modelKey)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "model_setting_failed", err.Error())
			return
		}
		if input.MaxMode != nil {
			setting.MaxMode = *input.MaxMode
		}
		if input.ReasoningEffort != nil {
			setting.ReasoningEffort = strings.TrimSpace(*input.ReasoningEffort)
		}
		if err := s.manager.Store().SetProviderModelSetting(r.Context(), provider, modelKey, setting); err != nil {
			writeErr(w, http.StatusBadRequest, "model_setting_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"model": modelKey, "provider": provider,
			"max_mode": setting.MaxMode, "reasoning_effort": setting.ReasoningEffort,
			"context_custom": setting.MaxMode || setting.ReasoningEffort != "",
		})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or PATCH only")
	}
}
