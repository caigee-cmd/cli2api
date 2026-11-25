package app

import (
	"context"
	"net/http"
	"sort"

	"github.com/caigee-cmd/cli2api/internal/control"
)

// aggregationKeyModels is the final /v1/models view for an aggregation key:
// enabled logical model-pool entries plus the provider-neutral account aliases
// the key can reach. Real catalog rows never appear, so the client cannot see
// which provider serves a name. Routing is unchanged and still resolves both
// kinds through the account pool / aggregation plan.
func (a *App) aggregationKeyModels(ctx context.Context, r *http.Request, models []map[string]any) []map[string]any {
	if a == nil {
		return models
	}
	identity := a.requestIdentity(r)
	if !identity.ModelAggregation {
		return models
	}
	aliases := control.AggregateModelEntries(models, identity, a.accountModelAllows())
	var lister control.AggregationLister
	if a.Control != nil {
		lister = a.Control.Aggregations
	}
	merged := control.MergeAggregationModels(ctx, lister, models)
	out := make([]map[string]any, 0, len(merged)+len(aliases))
	seen := make(map[string]bool, len(merged)+len(aliases))
	for _, model := range merged {
		if provider, _ := model["provider"].(string); provider != "aggregation" {
			continue
		}
		id, _ := model["id"].(string)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, model)
	}
	for _, model := range aliases {
		id, _ := model["id"].(string)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, model)
	}
	sort.SliceStable(out, func(i, j int) bool {
		left, _ := out[i]["id"].(string)
		right, _ := out[j]["id"].(string)
		return left < right
	})
	return out
}
