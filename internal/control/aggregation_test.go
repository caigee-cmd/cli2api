package control

import (
	"context"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func TestAggregationResolveFiltersByProviderAndOrders(t *testing.T) {
	store := &aggTestStore{agg: accounts.Aggregation{
		ID: "my-model", Name: "My Model", Enabled: true,
		Strategy: accounts.AggregationPriority, AllowFallbacks: true, MaxAttempts: 3,
		Members: []accounts.AggregationMember{
			{Provider: "trae", Model: "m3", Priority: 2},
			{Provider: "qoder", Model: "m1", Priority: 0},
			{Provider: "workbuddy", Model: "m2", Priority: 1},
		},
	}}
	svc := NewAggregations(store)
	// Allow only qoder and workbuddy; trae member must be filtered out.
	got, err := svc.Resolve(context.Background(), "my-model", map[string]bool{"qoder": true, "workbuddy": true})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(got.Members) != 2 || got.Members[0].Provider != "qoder" || got.Members[1].Provider != "workbuddy" {
		t.Fatalf("unexpected members: %+v", got.Members)
	}
}

func TestAggregationResolveRejectsDisabledAndUnknown(t *testing.T) {
	store := &aggTestStore{agg: accounts.Aggregation{
		ID: "my-model", Enabled: false,
		Members: []accounts.AggregationMember{{Provider: "qoder", Model: "m1"}},
	}}
	svc := NewAggregations(store)
	if _, err := svc.Resolve(context.Background(), "my-model", nil); err != ErrAggregationNotFound {
		t.Fatalf("disabled aggregation should be not-found, got %v", err)
	}
	if _, err := svc.Resolve(context.Background(), "nope", nil); err != ErrAggregationNotFound {
		t.Fatalf("unknown aggregation should be not-found, got %v", err)
	}
}

func TestAggregationResolveUsesCacheAndInvalidatesOnWrite(t *testing.T) {
	store := &aggTestStore{agg: accounts.Aggregation{
		ID: "my-model", Enabled: true, Strategy: accounts.AggregationRoundRobin,
		MaxAttempts: 3, AllowFallbacks: true,
		Members: []accounts.AggregationMember{{Provider: "qoder", Model: "m1"}},
	}}
	svc := NewAggregations(store)
	for i := 0; i < 5; i++ {
		if _, err := svc.Resolve(context.Background(), "my-model", nil); err != nil {
			t.Fatalf("resolve %d: %v", i, err)
		}
	}
	if store.listCalls != 1 {
		t.Fatalf("cache missed: ListAggregations called %d times, want 1", store.listCalls)
	}
	name := "renamed"
	if _, err := svc.Update(context.Background(), "my-model", accounts.UpdateAggregation{Name: &name}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := svc.Resolve(context.Background(), "my-model", nil); err != nil {
		t.Fatalf("resolve after write: %v", err)
	}
	if store.listCalls != 2 {
		t.Fatalf("cache not invalidated: ListAggregations called %d times, want 2", store.listCalls)
	}
}

// aggTestStore implements only the narrow AggregationStore surface.
type aggTestStore struct {
	agg       accounts.Aggregation
	listCalls int
}

func (s *aggTestStore) GetAggregation(_ context.Context, id string) (accounts.Aggregation, error) {
	if id != s.agg.ID {
		return accounts.Aggregation{}, accounts.ErrAggregationNotFound
	}
	return s.agg, nil
}
func (s *aggTestStore) InsertAggregation(_ context.Context, agg accounts.Aggregation) (accounts.Aggregation, error) {
	return agg, nil
}
func (s *aggTestStore) SaveAggregation(_ context.Context, agg accounts.Aggregation) (accounts.Aggregation, error) {
	return agg, nil
}
func (s *aggTestStore) ListAggregations(_ context.Context) ([]accounts.Aggregation, error) {
	s.listCalls++
	return []accounts.Aggregation{s.agg}, nil
}
func (s *aggTestStore) DeleteAggregation(_ context.Context, _ string) error { return nil }
