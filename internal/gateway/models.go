package gateway

import (
	"net/http"

	"github.com/caigee-cmd/cli2api/internal/auth"
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
	var identity auth.Identity
	if h != nil {
		identity = h.requestIdentity(r)
	}
	if h != nil && identity.ModelAggregation && h.AggregationKeyModels != nil {
		// Aggregation keys expose only provider-neutral names (logical model-pool
		// entries and account aliases), never real provider rows.
		models = h.AggregationKeyModels(r.Context(), r, models)
	}
	// The shared catalog never carries logical aggregation entries: they belong
	// to aggregation keys only, so a logical ID can never hide or replace a real
	// provider model for an ordinary client.
	if h.crossProviderPoolOn() == false && !identity.ModelAggregation {
		// The gateway refuses bare ids with the pool off; serve prefixed ids so
		// OpenAI-compatible clients see exactly what the gateway accepts.
		// Aggregation keys are the exception: their whole point is a
		// provider-neutral list, and the matching prepare step accepts those
		// bare names through the account pool.
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
