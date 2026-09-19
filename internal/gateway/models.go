package gateway

import "net/http"

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
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   models,
	})
}
