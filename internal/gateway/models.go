package gateway

import (
	"net/http"

	"github.com/caigee-cmd/cli2api/internal/control"
)

func (h *Handler) HandleModels(w http.ResponseWriter, r *http.Request) {
	var models []map[string]any
	if h != nil && h.Models != nil {
		fetched, err := h.Models(false, "")
		if err == nil {
			models = fetched
		}
	}
	if h != nil && h.FilterModels != nil {
		models = h.FilterModels(r, models)
	}
	if h != nil && h.DecorateModels != nil {
		models = h.DecorateModels(r, models)
	}
	if h.crossProviderPoolOn() == false {
		// The gateway refuses bare ids with the pool off; serve prefixed ids so
		// OpenAI-compatible clients see exactly what the gateway accepts.
		models = control.PrefixModelEntries(models)
	}
	body := map[string]any{
		"object": "list",
		"data":   models,
	}
	// Catalog sync metadata rides beside `data`; OpenAI clients ignore unknown
	// top-level members, and a model-sync client can tell a fresh list from the
	// last known one after an upstream failure.
	if h != nil && h.CatalogStatus != nil {
		if status := h.CatalogStatus(); len(status) > 0 {
			body["catalog"] = status
		}
	}
	writeJSON(w, http.StatusOK, body)
}
