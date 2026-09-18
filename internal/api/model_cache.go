package api

import (
	"github.com/caigee-cmd/cli2api/internal/control"
)

func (s *Server) fetchDisplayModels(refresh bool, accountID string, mode control.CatalogMode) ([]map[string]any, error) {
	if s == nil || s.control == nil || s.control.Catalog == nil {
		return s.fetchWorkerModelsForMode(refresh, accountID, catalogMode(mode))
	}
	return s.control.Catalog.Get(refresh, accountID, mode)
}

func (s *Server) fetchCatalogModels(refresh bool, accountID string, mode control.CatalogMode) ([]map[string]any, error) {
	return s.fetchWorkerModelsForMode(refresh, accountID, catalogMode(mode))
}
