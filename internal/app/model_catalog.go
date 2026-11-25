package app

import (
	"github.com/caigee-cmd/cli2api/internal/control"
	"github.com/caigee-cmd/cli2api/internal/endpoint"
	"net/http"
)

func (a *App) RequestedAccount(r *http.Request) string { return endpoint.RequestedAccount(r) }
func (a *App) filterModelsForIdentity(r *http.Request, models []map[string]any) []map[string]any {
	identity := a.requestIdentity(r)
	allow := a.accountModelAllows()
	// Aggregation keys build their list from account aliases directly, so the
	// per-provider alias expansion would only be discarded work.
	if !identity.ModelAggregation {
		models = control.ExpandModelAliases(models, allow)
	}
	models = control.FilterModelsByAccountAllow(models, identity, allow)
	models = control.FilterModelsForIdentity(identity, models)
	if !identity.ModelAggregation {
		models = control.AggregateModelEntries(models, identity, allow)
	}
	// Aggregation keys get their final provider-neutral list from
	// aggregationKeyModels after this filter: configured logical models first,
	// then account aliases. Returning the filtered catalog here keeps every
	// aggregation member available for that merge.
	return models
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
