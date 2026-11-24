package control

import (
	"context"
	"sort"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

type AggregationLister interface {
	List(ctx context.Context) ([]accounts.Aggregation, error)
}

// MergeAggregationModels folds enabled logical models into the display catalog.
// Each aggregated entry is built by merging the capability fields of all its
// member models (conservative intersection), then member models are hidden so
// clients see one stable ID instead of one row per provider. Models that are
// not part of any enabled aggregation are left untouched.
//
// The catalog slices passed in/out are plain value maps, so this never mutates
// the shared catalog cache.
func MergeAggregationModels(ctx context.Context, lister AggregationLister, models []map[string]any) []map[string]any {
	if lister == nil || len(models) == 0 {
		return models
	}
	aggs, err := lister.List(ctx)
	if err != nil || len(aggs) == 0 {
		return models
	}

	byID := make(map[string]map[string]any, len(models))
	for _, model := range models {
		id, _ := model["id"].(string)
		if id != "" {
			byID[id] = model
		}
	}

	hidden := map[string]bool{}
	entries := make([]map[string]any, 0, len(aggs))
	for _, agg := range aggs {
		if !agg.Enabled || len(agg.Members) == 0 {
			continue
		}
		var merged map[string]any
		for _, member := range agg.Members {
			model := byID[member.Model]
			if model == nil {
				continue
			}
			hidden[member.Model] = true
			if merged == nil {
				merged = CloneModelEntry(model)
				continue
			}
			MergeModelEntryCapabilities(merged, model)
		}
		if merged == nil {
			// Every member is filtered out by the caller's provider policy. Do
			// not advertise a logical ID that cannot route to any real model.
			continue
		}
		merged["id"] = agg.ID
		merged["display_name"] = agg.Name
		merged["provider"] = "aggregation"
		merged["owned_by"] = "aggregation"
		merged["aggregation"] = true
		merged["aggregation_strategy"] = string(agg.Strategy)
		merged["aggregation_members"] = len(agg.Members)
		delete(merged, "native_model")
		delete(merged, "mapped_key")
		entries = append(entries, merged)
	}
	if len(entries) == 0 && len(hidden) == 0 {
		return models
	}

	out := make([]map[string]any, 0, len(models)-len(hidden)+len(entries))
	for _, model := range models {
		id, _ := model["id"].(string)
		if hidden[id] {
			continue
		}
		out = append(out, model)
	}
	out = append(out, entries...)
	sort.SliceStable(out, func(i, j int) bool {
		left, _ := out[i]["id"].(string)
		right, _ := out[j]["id"].(string)
		return left < right
	})
	return out
}
