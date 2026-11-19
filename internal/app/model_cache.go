package app

import (
	"github.com/caigee-cmd/cli2api/internal/control"
)

func (a *App) fetchDisplayModels(refresh bool, accountID string, mode control.CatalogMode) ([]map[string]any, error) {
	if a == nil || a.Control == nil || a.Control.Catalog == nil {
		return a.FetchWorkerModelsForMode(refresh, accountID, mode)
	}
	return a.Control.Catalog.Get(refresh, accountID, mode)
}

// peekDisplayModels is cache-only. It reuses the catalog snapshot accessor so
// diagnostic endpoints never start a background/synchronous catalog refresh.
// Merge is persisted/restored on cold boot; Expand falls back to Merge.
func (a *App) peekDisplayModels(accountID string, mode control.CatalogMode) []map[string]any {
	if a == nil || a.Control == nil || a.Control.Catalog == nil {
		return nil
	}
	if models := a.Control.Catalog.Peek(accountID, mode); len(models) > 0 {
		return models
	}
	if mode == control.CatalogModeExpand {
		return a.Control.Catalog.Peek("", control.CatalogModeMerge)
	}
	return nil
}
