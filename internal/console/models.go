package console

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/control"
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

func (h *Handler) HandleModelsAPI(w http.ResponseWriter, r *http.Request) {
	refresh := r.URL.Query().Get("refresh") == "1"
	mode := control.CatalogModeMerge
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("view")), "regional") {
		mode = control.CatalogModeExpand
	}
	models, err := h.fetchDisplayModels(refresh, h.requestedAccount(r), mode)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "catalog_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   h.decorateModels(r.Context(), h.filterModels(r, models)),
	})
}

func (h *Handler) HandleModelSetting(w http.ResponseWriter, r *http.Request) {
	provider, modelKey := splitModelSettingPath(r.URL.Path, r.URL.Query().Get("provider"))
	if modelKey == "" {
		writeErr(w, http.StatusBadRequest, "invalid_model", "model id required")
		return
	}
	if provider == "trae" || provider == "workbuddy" {
		h.handleProviderModelSetting(w, r, provider, modelKey)
		return
	}
	switch r.Method {
	case http.MethodGet:
		value, custom, err := h.Control.Settings.GetModelContext(r.Context(), modelKey)
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
		if err := h.Control.Settings.SetModelContext(r.Context(), modelKey, input.ContextLength); err != nil {
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

func (h *Handler) handleProviderModelSetting(w http.ResponseWriter, r *http.Request, provider, modelKey string) {
	switch r.Method {
	case http.MethodGet:
		setting, err := h.Control.Settings.GetProviderModelSetting(r.Context(), provider, modelKey)
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
		setting, err := h.Control.Settings.GetProviderModelSetting(r.Context(), provider, modelKey)
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
		if err := h.Control.Settings.SetProviderModelSetting(r.Context(), provider, modelKey, setting); err != nil {
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
