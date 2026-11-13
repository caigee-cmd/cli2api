package app

import (
	"github.com/caigee-cmd/cli2api/internal/control"
	"github.com/caigee-cmd/cli2api/internal/endpoint"
	"net/http"
)

func (a *App) RequestedAccount(r *http.Request) string { return endpoint.RequestedAccount(r) }
func (a *App) filterModelsForIdentity(r *http.Request, models []map[string]any) []map[string]any {
	identity := a.requestIdentity(r)
	models = control.FilterModelsByAccountAllow(models, identity, a.accountModelAllows())
	return control.FilterModelsForIdentity(identity, models)
}

// accountModelAllows is the routing allowlist the public model list consults.
// It is read from the live pool, not the catalog cache, so saving "only
// selected models" hides those names on the next /v1/models without waiting
// for a catalog refresh.
func (a *App) accountModelAllows() []control.AccountModelAllow {
	if a == nil || a.Pool == nil {
		return nil
	}
	items := a.Pool.Items()
	if len(items) == 0 {
		return nil
	}
	out := make([]control.AccountModelAllow, 0, len(items))
	for _, item := range items {
		out = append(out, control.AccountModelAllow{
			Provider:         item.Provider,
			Region:           item.Region,
			ModelsRestricted: item.ModelsRestricted,
			EnabledModels:    item.EnabledModels,
			Aliases:          item.ModelAliases,
		})
	}
	return out
}
