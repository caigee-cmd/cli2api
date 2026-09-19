package app

import (
	"context"
	"github.com/caigee-cmd/cli2api/internal/control"
	"github.com/caigee-cmd/cli2api/internal/providers/qoder"
	"time"
)

var ErrWorkerNotWarm = qoder.ErrWorkerNotWarm

func WaitForWorkerAuthManager(ctx context.Context, lookup func() (string, bool), timeout, interval time.Duration) (string, error) {
	return qoder.WaitForAuthManager(ctx, lookup, timeout, interval)
}
func EntryModelRegions(entry map[string]any) []string { return control.EntryModelRegions(entry) }
func (a *App) catalogSource() *control.CatalogSource {
	worker := qoder.DisplayCatalog{Key: a.Auth.ConsoleKey, Lookup: func(id string) (string, bool) {
		if id != "" {
			item, ok := a.Pool.ByID(id)
			return item.URL, ok
		}
		item, ok := a.Pool.First()
		return item.URL, ok
	}}
	return &control.CatalogSource{Providers: a.Providers, WorkerModels: worker.Models, Accounts: func() []control.CatalogAccount {
		var out []control.CatalogAccount
		for _, item := range a.Pool.Items() {
			out = append(out, control.CatalogAccount{ID: item.ID, Provider: item.Provider, Region: item.Region, Worker: item.URL != ""})
		}
		return out
	}}
}
func (a *App) FetchWorkerModelsFor(refresh bool, accountID string) ([]map[string]any, error) {
	return a.FetchWorkerModelsForMode(refresh, accountID, control.CatalogModeMerge)
}
func (a *App) FetchWorkerModelsForMode(refresh bool, accountID string, mode control.CatalogMode) ([]map[string]any, error) {
	return a.catalogSource().Fetch(refresh, accountID, mode)
}
func (a *App) fetchWorkerModels(refresh bool) []map[string]any {
	models, _ := a.FetchWorkerModelsFor(refresh, "")
	return models
}
