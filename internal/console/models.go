package console

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/control"
)

func splitModelSettingPath(raw, queryProvider string) (provider, modelKey string) {
	raw = strings.TrimPrefix(raw, "/api/models/")
	provider = strings.ToLower(strings.TrimSpace(queryProvider))
	for _, prefix := range []string{"trae/", "workbuddy/", "qoder/", "devin/", "command/", "codex/"} {
		if strings.HasPrefix(strings.ToLower(raw), prefix) {
			provider = strings.TrimSuffix(prefix, "/")
			raw = raw[len(prefix):]
			break
		}
	}
	return provider, control.ModelContextKey(raw)
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
	switch r.Method {
	case http.MethodGet:
		setting, err := h.Control.Settings.ReadModelSetting(r.Context(), provider, modelKey)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "model_setting_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, modelSettingResponse(setting))
	case http.MethodPatch:
		var input struct {
			ContextLength   *int    `json:"context_length"`
			MaxMode         *bool   `json:"max_mode"`
			ReasoningEffort *string `json:"reasoning_effort"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		setting, err := h.Control.Settings.UpdateModelSetting(r.Context(), provider, modelKey, input.ContextLength, control.ProviderModelSettingPatch{
			MaxMode: input.MaxMode, ReasoningEffort: input.ReasoningEffort,
		})
		if err != nil {
			writeOperationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, modelSettingResponse(setting))
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or PATCH only")
	}
}

func modelSettingResponse(setting control.ModelSetting) map[string]any {
	switch setting.Provider {
	case "trae", "workbuddy":
		return map[string]any{
			"model": setting.Model, "provider": setting.Provider,
			"max_mode": setting.MaxMode, "reasoning_effort": setting.ReasoningEffort,
			"context_custom": setting.ContextCustom,
		}
	case "qoder":
		return map[string]any{
			"model": setting.Model, "provider": setting.Provider,
			"max_mode":               setting.MaxMode,
			"context_length":         setting.ContextLength,
			"default_context_length": setting.DefaultContextLength,
			"context_custom":         setting.ContextCustom,
		}
	default:
		return map[string]any{
			"model": setting.Model, "context_length": setting.ContextLength,
			"default_context_length": setting.DefaultContextLength, "context_custom": setting.ContextCustom,
		}
	}
}
