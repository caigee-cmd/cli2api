package app

import (
	"github.com/caigee-cmd/cli2api/internal/control"
)

func (a *App) fetchDisplayModels(refresh bool, accountID string, mode control.CatalogMode) ([]map[string]any, error) {
	if a == nil || a.Control == nil || a.Control.Catalog == nil {
		return a.FetchWorkerModelsForMode(refresh, accountID, CatalogMode(mode))
	}
	return a.Control.Catalog.Get(refresh, accountID, mode)
}

func (a *App) fetchCatalogModels(refresh bool, accountID string, mode control.CatalogMode) ([]map[string]any, error) {
	return a.FetchWorkerModelsForMode(refresh, accountID, CatalogMode(mode))
}
