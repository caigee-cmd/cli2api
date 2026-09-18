package api

import (
	"net/http"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/control"
)

func (s *Server) handleModelsAPI(w http.ResponseWriter, r *http.Request) {
	refresh := r.URL.Query().Get("refresh") == "1"
	mode := control.CatalogModeMerge
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("view")), "regional") {
		mode = control.CatalogModeExpand
	}
	models, err := s.fetchDisplayModels(refresh, s.requestedAccount(r), mode)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "catalog_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   s.decorateModelsWithContext(r.Context(), s.filterModelsForIdentity(r, models)),
	})
}

func (s *Server) fetchDisplayModels(refresh bool, accountID string, mode control.CatalogMode) ([]map[string]any, error) {
	if s == nil || s.control == nil || s.control.Catalog == nil {
		return s.fetchWorkerModelsForMode(refresh, accountID, catalogMode(mode))
	}
	return s.control.Catalog.Get(refresh, accountID, mode)
}

func (s *Server) fetchCatalogModels(refresh bool, accountID string, mode control.CatalogMode) ([]map[string]any, error) {
	return s.fetchWorkerModelsForMode(refresh, accountID, catalogMode(mode))
}
