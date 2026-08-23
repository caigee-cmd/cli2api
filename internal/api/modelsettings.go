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

func (s *Server) applyModelContextDefaults(ctx context.Context, req *translate.ChatRequest) error {
	if req == nil || strings.TrimSpace(req.Model) == "" {
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
		settingsKey := modelContextKey(id)
		defaultValue := defaultContextForModel(settingsKey)
		value, custom := settings[settingsKey]
		if !custom {
			value = defaultValue
		}
		item["settings_key"] = settingsKey
		item["context_length"] = value
		item["default_context_length"] = defaultValue
		item["context_custom"] = custom
		decorated = append(decorated, item)
	}
	return decorated
}

func (s *Server) handleModelSetting(w http.ResponseWriter, r *http.Request) {
	modelKey := modelContextKey(strings.TrimPrefix(r.URL.Path, "/api/models/"))
	if modelKey == "" {
		writeErr(w, http.StatusBadRequest, "invalid_model", "model id required")
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
