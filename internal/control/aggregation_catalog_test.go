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

func aggregationTestEntry(models []map[string]any, id string) map[string]any {
	for _, model := range models {
		if model["id"] == id {
			return model
		}
	}
	return nil
}

func TestMergeAggregationModelsResolvesMemberByProvider(t *testing.T) {
	models := []map[string]any{
		{"id": "glm-5.2", "provider": "qoder", "catalog_context_length": 200000, "max_output_tokens": 64000},
		{"id": "glm-5.2", "provider": "workbuddy", "regions": []string{"cn"}, "catalog_context_length": 128000, "max_output_tokens": 32000},
	}
	list := aggregationCatalogLister{{
		ID: "pool", Name: "Pool", Enabled: true,
		Members: []accounts.AggregationMember{{Provider: "workbuddy", Region: "cn", Model: "glm-5.2"}},
	}}
	got := MergeAggregationModels(context.Background(), list, models)
	if len(got) != 1 {
		t.Fatalf("both provider rows should be hidden, got %#v", got)
	}
	entry := got[0]
	if entry["catalog_context_length"] != 128000 || entry["max_output_tokens"] != 32000 {
		t.Fatalf("member pinned to workbuddy picked another provider's capabilities: %#v", entry)
	}
	if entry["context_length"] != 128000 || entry["default_context_length"] != 128000 {
		t.Fatalf("display window = %v/%v", entry["context_length"], entry["default_context_length"])
	}
}

func TestMergeAggregationModelsStaysConservativeAcrossMembers(t *testing.T) {
	models := []map[string]any{
		{
			"id": "model-a", "provider": "qoder",
			"catalog_context_length": 200000, "max_output_tokens": 64000, "prompt_max_tokens": 160000,
			"reasoning_options": []string{"none", "low", "medium", "high", "max"},
			"reasoning_default": "medium", "supports_images": true, "can_disable_thinking": true,
		},
		{
			"id": "model-b", "provider": "workbuddy",
			"catalog_context_length": 128000, "max_output_tokens": 32000, "prompt_max_tokens": 100000,
			"reasoning_options": []string{"high", "max"},
			"reasoning_default": "high", "supports_images": false,
		},
	}
	list := aggregationCatalogLister{{
		ID: "pool", Name: "Pool", Enabled: true,
		Members: []accounts.AggregationMember{
			{Provider: "qoder", Model: "model-a"},
			{Provider: "workbuddy", Model: "model-b"},
		},
	}}
	got := MergeAggregationModels(context.Background(), list, models)
	entry := aggregationTestEntry(got, "pool")
	if entry == nil {
		t.Fatalf("missing logical entry: %#v", got)
	}
	if entry["catalog_context_length"] != 128000 || entry["max_output_tokens"] != 32000 || entry["prompt_max_tokens"] != 100000 {
		t.Fatalf("numeric intersection = %#v", entry)
	}
	options, _ := entry["reasoning_options"].([]string)
	if len(options) != 2 || options[0] != "high" || options[1] != "max" {
		t.Fatalf("reasoning options = %#v", entry["reasoning_options"])
	}
	if entry["reasoning_default"] != nil || entry["reasoning_effort"] != nil {
		t.Fatalf("members disagree on default, got %#v", entry["reasoning_default"])
	}
	if entry["supports_images"] != false || entry["can_disable_thinking"] != nil {
		t.Fatalf("boolean intersection = %#v", entry)
	}
	if entry["supports_max_mode"] != nil || entry["max_mode"] != nil {
		t.Fatalf("aggregation must not advertise a max tier: %#v", entry["supports_max_mode"])
	}
}

func TestMergeAggregationModelsAppliesConfiguredOverrides(t *testing.T) {
	models := []map[string]any{{
		"id": "model-a", "provider": "qoder",
		"catalog_context_length": 200000, "max_output_tokens": 64000,
		"reasoning_options": []string{"none", "low", "medium", "high", "max"},
		"reasoning_default": "medium",
	}}
	list := aggregationCatalogLister{{
		ID: "pool", Name: "Pool", Enabled: true,
		ContextLength: 1_000_000, MaxOutputTokens: 32000,
		ReasoningOptions: []string{"low", "high", "max"}, ReasoningEffort: "high",
		Members: []accounts.AggregationMember{{Provider: "qoder", Model: "model-a"}},
	}}
	got := MergeAggregationModels(context.Background(), list, models)
	entry := aggregationTestEntry(got, "pool")
	if entry == nil {
		t.Fatalf("missing logical entry: %#v", got)
	}
	if entry["catalog_context_length"] != 1_000_000 || entry["context_length"] != 1_000_000 || entry["default_context_length"] != 1_000_000 {
		t.Fatalf("context override = %#v", entry)
	}
	if entry["max_output_tokens"] != 32000 || entry["context_custom"] != true {
		t.Fatalf("max output override = %#v", entry)
	}
	options, _ := entry["reasoning_options"].([]string)
	if len(options) != 3 || options[0] != "low" || options[1] != "high" || options[2] != "max" {
		t.Fatalf("reasoning override = %#v", entry["reasoning_options"])
	}
	if entry["reasoning_default"] != "high" || entry["reasoning_effort"] != "high" {
		t.Fatalf("default effort = %#v", entry["reasoning_default"])
	}
}

func TestMergeAggregationModelsDropsInheritedDecoration(t *testing.T) {
	models := []map[string]any{{
		"id": "model-a", "provider": "qoder", "native_model": "native-a", "alias_of": "target",
		"catalog_context_length": 200000,
		"context_length":         1_000_000, "default_context_length": 1_000_000,
		"reasoning_effort": "max", "max_mode": true, "context_custom": true,
		"settings_key": "k1", "region": "global", "regions": []string{"global"},
		"credits": "x", "free": true, "supports_max_mode": true,
	}}
	list := aggregationCatalogLister{{
		ID: "pool", Name: "Pool", Enabled: true,
		Members: []accounts.AggregationMember{{Provider: "qoder", Model: "model-a"}},
	}}
	got := MergeAggregationModels(context.Background(), list, models)
	entry := aggregationTestEntry(got, "pool")
	if entry == nil {
		t.Fatalf("missing logical entry: %#v", got)
	}
	for _, field := range []string{"native_model", "alias_of", "region", "regions", "credits", "free", "supports_max_mode", "max_mode", "settings_key", "reasoning_effort"} {
		if entry[field] != nil {
			t.Fatalf("%s leaked into the aggregation entry: %#v", field, entry[field])
		}
	}
	if entry["context_length"] != 200000 || entry["default_context_length"] != 200000 {
		t.Fatalf("decorated window leaked: %#v", entry)
	}
	if entry["context_editable"] != false {
		t.Fatalf("context_editable = %v", entry["context_editable"])
	}
}
