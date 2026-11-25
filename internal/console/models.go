package console

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/control"
)

func splitModelSettingPath(raw, queryProvider string) (provider, modelKey string) {
	raw = strings.TrimPrefix(raw, "/api/models/")
	provider = strings.ToLower(strings.TrimSpace(queryProvider))
	for _, prefix := range []string{"trae/", "workbuddy/", "qoder/", "devin/", "command/", "codex/", "zhipu/", "qwenwork/", "kimi/", "codearts/", "kiro/", "minimaxcode/"} {
		if strings.HasPrefix(strings.ToLower(raw), prefix) {
			provider = strings.TrimSuffix(prefix, "/")
			raw = raw[len(prefix):]
			break
		}
	}
	return provider, control.ModelContextKey(raw)
}

const (
	modelPageDefaultSize = 50
	modelPageMaxSize     = 100
)

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
	data := h.decorateModels(r.Context(), h.filterModels(r, models))
	// Aggregation models are a model-pool concept, not a provider. Do not merge
	// them into the console catalog: provider settings, account directories and
	// the model-pool member picker all group by provider, so a synthetic
	// "aggregation" row there either creates a phantom provider card or hides a
	// real model that happens to share the logical ID. Clients discover logical
	// models through the key-scoped /v1/models view instead.
	if !h.crossProviderPoolOn() {
		// The gateway refuses bare ids with the pool off; serve prefixed ids so
		// every client that picks from this list produces a request it accepts.
		data = control.PrefixModelEntries(data)
	}
	page, pageSize, paged, err := parseModelPagination(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	body := map[string]any{"object": "list", "data": data}
	if paged {
		total := len(data)
		totalPages := (total + pageSize - 1) / pageSize
		if totalPages == 0 {
			totalPages = 1
		}
		if page > totalPages {
			page = totalPages
		}
		start := (page - 1) * pageSize
		if start > total {
			start = total
		}
		end := start + pageSize
		if end > total {
			end = total
		}
		body["data"] = data[start:end]
		body["pagination"] = map[string]any{
			"page": page, "page_size": pageSize, "total": total,
			"total_pages": totalPages, "has_next": page < totalPages,
		}
	}
	writeJSON(w, http.StatusOK, body)
}

func parseModelPagination(r *http.Request) (page, pageSize int, paged bool, err error) {
	pageRaw, sizeRaw := strings.TrimSpace(r.URL.Query().Get("page")), strings.TrimSpace(r.URL.Query().Get("page_size"))
	if pageRaw == "" && sizeRaw == "" {
		return 1, modelPageDefaultSize, false, nil
	}
	page, pageSize, paged = 1, modelPageDefaultSize, true
	if pageRaw != "" {
		page, err = strconv.Atoi(pageRaw)
		if err != nil || page < 1 {
			return 0, 0, false, fmt.Errorf("page must be a positive integer")
		}
	}
	if sizeRaw != "" {
		pageSize, err = strconv.Atoi(sizeRaw)
		if err != nil || pageSize < 1 || pageSize > modelPageMaxSize {
			return 0, 0, false, fmt.Errorf("page_size must be between 1 and %d", modelPageMaxSize)
		}
	}
	return page, pageSize, paged, nil
}

// HandleCatalogStatus reports the model-catalog sync state. GET only reads it;
// POST forces one full reload first and answers with the state that resulted.
// The body is 200 in both cases — a partial or failed refresh is described by
// last_error and the per-provider rows, not by the HTTP status.
func (h *Handler) HandleCatalogStatus(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Control == nil || h.Control.Catalog == nil {
		writeErr(w, http.StatusServiceUnavailable, "catalog_unavailable", "model catalog is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, h.Control.Catalog.Status())
	case http.MethodPost:
		status, _ := h.Control.Catalog.Refresh()
		writeJSON(w, http.StatusOK, status)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST only")
	}
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
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxConsoleBodyBytes)).Decode(&input); err != nil {
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
	case "kimi", "codearts":
		// Kimi Code and CodeArts have no max-mode tier, so only the reasoning
		// default is part of their setting payload.
		return map[string]any{
			"model": setting.Model, "provider": setting.Provider,
			"reasoning_effort": setting.ReasoningEffort,
			"context_custom":   setting.ContextCustom,
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
