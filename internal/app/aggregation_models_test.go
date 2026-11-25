package app

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/auth"
	appsvc "github.com/caigee-cmd/cli2api/internal/control"
	"github.com/caigee-cmd/cli2api/internal/executor"
)

type aggregationStoreStub struct {
	list []accounts.Aggregation
}

func (s *aggregationStoreStub) InsertAggregation(_ context.Context, agg accounts.Aggregation) (accounts.Aggregation, error) {
	s.list = append(s.list, agg)
	return agg, nil
}

func (s *aggregationStoreStub) SaveAggregation(_ context.Context, agg accounts.Aggregation) (accounts.Aggregation, error) {
	for i := range s.list {
		if s.list[i].ID == agg.ID {
			s.list[i] = agg
		}
	}
	return agg, nil
}

func (s *aggregationStoreStub) ListAggregations(context.Context) ([]accounts.Aggregation, error) {
	return s.list, nil
}

func (s *aggregationStoreStub) GetAggregation(_ context.Context, id string) (accounts.Aggregation, error) {
	for _, agg := range s.list {
		if agg.ID == id {
			return agg, nil
		}
	}
	return accounts.Aggregation{}, accounts.ErrAggregationNotFound
}

func (s *aggregationStoreStub) DeleteAggregation(_ context.Context, id string) error {
	for i := range s.list {
		if s.list[i].ID == id {
			s.list = append(s.list[:i], s.list[i+1:]...)
			return nil
		}
	}
	return accounts.ErrAggregationNotFound
}

func TestAggregationKeyModelsListsConfiguredAndAccountAliases(t *testing.T) {
	store := &aggregationStoreStub{list: []accounts.Aggregation{{
		ID: "glm-5.2", Name: "GLM 5.2", Enabled: true, Strategy: accounts.AggregationPriority,
		Members: []accounts.AggregationMember{{Provider: "qoder", Model: "kimi-k2.6"}},
	}}}
	pool := executor.NewPool(nil, nil)
	pool.Upsert(executor.Item{
		ID: "q1", Provider: "qoder", Models: []string{"kimi-k2.6"},
		ModelAliases: map[string]string{"fast": "kimi-k2.6"},
	})
	a := &App{Pool: pool, Control: &appsvc.Services{Aggregations: appsvc.NewAggregations(store)}}
	models := []map[string]any{
		{"id": "kimi-k2.6", "object": "model", "provider": "qoder", "native_model": "kimi-k2.6"},
	}
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), auth.Identity{
		Kind: auth.KindKey, KeyID: "k1", ModelAggregation: true, AllowedProviders: []string{"qoder"},
	}))
	out := a.aggregationKeyModels(context.Background(), req, models)
	byID := map[string]map[string]any{}
	for _, model := range out {
		byID[model["id"].(string)] = model
	}
	if len(out) != 2 {
		t.Fatalf("models=%v", out)
	}
	logical := byID["glm-5.2"]
	if logical == nil || logical["provider"] != "aggregation" || logical["owned_by"] != "aggregation" {
		t.Fatalf("logical=%v out=%v", logical, out)
	}
	if logical["native_model"] != nil {
		t.Fatalf("native model leaked: %v", logical)
	}
	if byID["fast"] == nil || byID["fast"]["provider"] != nil {
		t.Fatalf("alias=%v out=%v", byID["fast"], out)
	}
	if byID["kimi-k2.6"] != nil {
		t.Fatalf("real model leaked: %v", out)
	}
}
