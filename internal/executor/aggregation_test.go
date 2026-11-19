package executor

import (
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func aggTestPool() *Pool {
	p := NewPool(
		[]string{"http://qoder:3020", "http://wb:3020", "http://trae:3020"},
		[]string{"qoder", "workbuddy", "trae"},
	)
	// NewPool hardcodes provider=qoder; set explicit families for this test.
	for i, provider := range []string{"qoder", "workbuddy", "trae"} {
		p.items[i].Provider = provider
	}
	// Give each account a concrete model catalog.
	p.MergeModels("qoder", []string{"model-a", "shared"})
	p.MergeModels("workbuddy", []string{"model-b", "shared"})
	p.MergeModels("trae", []string{"model-c"})
	return p
}

func aggPlan(members ...accounts.AggregationMember) *AggregatePlan {
	return NewAggregatePlan(accounts.Aggregation{
		ID: "my-model", Strategy: accounts.AggregationPriority,
		AllowFallbacks: true, MaxAttempts: 3, Members: members,
	})
}

func TestAggregateResolverSameNameAcrossProviders(t *testing.T) {
	plan := aggPlan(
		accounts.AggregationMember{Provider: "qoder", Model: "shared", Priority: 0},
		accounts.AggregationMember{Provider: "workbuddy", Model: "shared", Priority: 1},
	)
	resolver := plan.resolver()
	qoder, _ := aggTestPool().ByID("qoder")
	wb, _ := aggTestPool().ByID("workbuddy")
	if got := resolver(qoder); got != "shared" {
		t.Fatalf("qoder resolved=%q want shared", got)
	}
	if got := resolver(wb); got != "shared" {
		t.Fatalf("workbuddy resolved=%q want shared", got)
	}
}

func TestAggregateResolverDifferentNames(t *testing.T) {
	plan := aggPlan(
		accounts.AggregationMember{Provider: "qoder", Model: "model-a", Priority: 0},
		accounts.AggregationMember{Provider: "workbuddy", Model: "model-b", Priority: 1},
		accounts.AggregationMember{Provider: "trae", Model: "model-c", Priority: 2},
	)
	resolver := plan.resolver()
	p := aggTestPool()
	for _, tc := range []struct{ id, want string }{
		{"qoder", "model-a"}, {"workbuddy", "model-b"}, {"trae", "model-c"},
	} {
		item, _ := p.ByID(tc.id)
		if got := resolver(item); got != tc.want {
			t.Fatalf("%s resolved=%q want %q", tc.id, got, tc.want)
		}
	}
}

func TestAggregateRouteOnlyMatchesMembers(t *testing.T) {
	p := aggTestPool()
	plan := aggPlan(
		accounts.AggregationMember{Provider: "qoder", Model: "model-a", Priority: 0},
		accounts.AggregationMember{Provider: "workbuddy", Model: "model-b", Priority: 1},
	)
	resolver := plan.resolver()
	q := RouteQuery{PublicModel: "my-model", ModelResolver: resolver}
	// trae has model-c only; it must not be selected for this aggregation.
	counts := map[string]int{}
	for i := 0; i < 12; i++ {
		item, ok := p.PickRoute(q)
		if !ok {
			t.Fatal("expected an aggregate route")
		}
		counts[item.ID]++
	}
	if counts["trae"] != 0 {
		t.Fatalf("aggregate route must exclude non-member account, counts=%v", counts)
	}
	if counts["qoder"] == 0 || counts["workbuddy"] == 0 {
		t.Fatalf("both member accounts should be reachable, counts=%v", counts)
	}
}

func TestAggregateCooldownIsScopedToResolvedModel(t *testing.T) {
	p := aggTestPool()
	plan := aggPlan(
		accounts.AggregationMember{Provider: "qoder", Model: "model-a", Priority: 0},
		accounts.AggregationMember{Provider: "workbuddy", Model: "model-b", Priority: 1},
	)
	resolver := plan.resolver()
	// Cool only qoder's resolved model.
	qoder, _ := p.ByID("qoder")
	p.MarkClassified("qoder", Classified{Kind: KindRateLimit, Model: effectiveModel(qoder, RouteQuery{PublicModel: "my-model", ModelResolver: resolver}), Cooldown: time.Hour})
	q := RouteQuery{PublicModel: "my-model", ModelResolver: resolver}
	// Failover must move to workbuddy, not stall on qoder.
	for i := 0; i < 6; i++ {
		item, ok := p.PickRoute(q)
		if !ok {
			t.Fatal("expected a live aggregate route after qoder cooldown")
		}
		if item.ID == "qoder" {
			t.Fatalf("qoder must be cooling for its resolved model")
		}
	}
}

func TestAggregateResolverDoesNotMutateItem(t *testing.T) {
	p := aggTestPool()
	plan := aggPlan(accounts.AggregationMember{Provider: "qoder", Model: "model-a", Priority: 0})
	before, _ := p.ByID("qoder")
	resolver := plan.resolver()
	_ = resolver(before)
	after, _ := p.ByID("qoder")
	if len(before.Models) != len(after.Models) || len(before.NativeModels) != len(after.NativeModels) {
		t.Fatal("resolver must not mutate shared item state")
	}
}

func TestAggregateResolverIsBoundedForManyProviders(t *testing.T) {
	members := make([]accounts.AggregationMember, 0, 200)
	for i := 0; i < 200; i++ {
		members = append(members, accounts.AggregationMember{Provider: "qoder", Model: "m", Priority: i})
	}
	plan := aggPlan(members...)
	resolver := plan.resolver()
	item := Item{ID: "x", Provider: "qoder", Models: []string{"m"}}
	if got := resolver(item); got != "m" {
		t.Fatalf("resolved=%q want m", got)
	}
	// The plan caps at MaxAggregationMembers via PrepareCreate; resolver itself
	// stays allocation-free and O(1) through the byProvider index.
	if len(plan.byProvider["qoder"]) != 200 {
		t.Fatalf("index size = %d", len(plan.byProvider["qoder"]))
	}
}
