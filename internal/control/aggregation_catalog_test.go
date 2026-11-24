package control

import (
	"context"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

type aggregationCatalogLister []accounts.Aggregation

func (l aggregationCatalogLister) List(context.Context) ([]accounts.Aggregation, error) {
	return l, nil
}

func TestMergeAggregationModelsOmitsAggregationWithNoVisibleMembers(t *testing.T) {
	models := []map[string]any{{"id": "allowed-model", "provider": "qoder"}}
	list := aggregationCatalogLister{{
		ID: "logical", Name: "Logical", Enabled: true,
		Members: []accounts.AggregationMember{{Provider: "qoder", Model: "disabled-model"}},
	}}
	got := MergeAggregationModels(context.Background(), list, models)
	if len(got) != 1 || got[0]["id"] != "allowed-model" {
		t.Fatalf("aggregation with no visible member should be omitted, got %#v", got)
	}
}

func TestMergeAggregationModelsKeepsAggregationWithOneVisibleMember(t *testing.T) {
	models := []map[string]any{{"id": "allowed-model", "provider": "qoder"}}
	list := aggregationCatalogLister{{
		ID: "logical", Name: "Logical", Enabled: true,
		Members: []accounts.AggregationMember{
			{Provider: "qoder", Model: "disabled-model"},
			{Provider: "qoder", Model: "allowed-model"},
		},
	}}
	got := MergeAggregationModels(context.Background(), list, models)
	if len(got) != 1 || got[0]["id"] != "logical" {
		t.Fatalf("expected one logical entry, got %#v", got)
	}
	if got[0]["provider"] != "aggregation" {
		t.Fatalf("expected aggregation provider, got %#v", got[0])
	}
}
