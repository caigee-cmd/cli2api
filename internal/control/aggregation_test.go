package control

import (
	"context"
	"fmt"
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

func TestAggregationCreateRejectsProviderDisabledModel(t *testing.T) {
	store := &aggTestStore{}
	validator := aggregationMemberRejector{model: "disabled-model"}
	svc := NewAggregations(store, validator)
	_, err := svc.Create(context.Background(), accounts.CreateAggregation{
		ID: "logical", Name: "Logical", Strategy: accounts.AggregationPriority,
		Members: []accounts.AggregationMember{{Provider: "qoder", Region: "cn", Model: "disabled-model"}},
	})
	if err == nil {
		t.Fatal("expected provider-disabled model to be rejected")
	}
	if store.inserts != 0 {
		t.Fatalf("rejected aggregation still inserted %d times", store.inserts)
	}
}

func TestAggregationUpdateRejectsProviderDisabledMember(t *testing.T) {
	store := &aggTestStore{agg: accounts.Aggregation{
		ID: "logical", Name: "Logical", Enabled: true, Strategy: accounts.AggregationPriority,
		AllowFallbacks: true, MaxAttempts: 3,
		Members: []accounts.AggregationMember{{Provider: "qoder", Region: "cn", Model: "enabled-model"}},
	}}
	svc := NewAggregations(store, aggregationMemberRejector{model: "disabled-model"})
	members := []accounts.AggregationMember{{Provider: "qoder", Region: "cn", Model: "disabled-model"}}
	_, err := svc.Update(context.Background(), "logical", accounts.UpdateAggregation{Members: &members})
	if err == nil {
		t.Fatal("expected provider-disabled member update to be rejected")
	}
	if store.saves != 0 {
		t.Fatalf("rejected aggregation still saved %d times", store.saves)
	}
}

type aggregationMemberRejector struct {
	model string
}

func (r aggregationMemberRejector) ValidateAggregationMembers(_ context.Context, members []accounts.AggregationMember) error {
	for _, member := range members {
		if member.Model == r.model {
			return fmt.Errorf("model %q is not enabled in provider settings", member.Model)
		}
	}
	return nil
}

// aggTestStore implements only the narrow AggregationStore surface.
type aggTestStore struct {
	agg       accounts.Aggregation
	listCalls int
	inserts   int
	saves     int
}

func (s *aggTestStore) GetAggregation(_ context.Context, id string) (accounts.Aggregation, error) {
	if id != s.agg.ID {
		return accounts.Aggregation{}, accounts.ErrAggregationNotFound
	}
	return s.agg, nil
}
func (s *aggTestStore) InsertAggregation(_ context.Context, agg accounts.Aggregation) (accounts.Aggregation, error) {
	s.inserts++
	return agg, nil
}
func (s *aggTestStore) SaveAggregation(_ context.Context, agg accounts.Aggregation) (accounts.Aggregation, error) {
	s.saves++
	s.agg = agg
	return agg, nil
}
func (s *aggTestStore) ListAggregations(_ context.Context) ([]accounts.Aggregation, error) {
	s.listCalls++
	return []accounts.Aggregation{s.agg}, nil
}
func (s *aggTestStore) DeleteAggregation(_ context.Context, _ string) error { return nil }
