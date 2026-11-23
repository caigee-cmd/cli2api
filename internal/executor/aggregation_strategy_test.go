package executor

import (
	"context"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func aggRouteQuery(plan *AggregatePlan) RouteQuery {
	return RouteQuery{
		PublicModel:      "my-model",
		ModelResolver:    plan.resolver(),
		StrategyOverride: string(plan.Strategy),
		PriorityResolver: plan.priorityResolver(),
		WeightResolver:   plan.weightResolver(),
	}
}

func TestAggregatePriorityAlwaysPicksHighestPriority(t *testing.T) {
	p := aggTestPool()
	plan := NewAggregatePlan(accounts.Aggregation{
		ID: "my-model", Strategy: accounts.AggregationPriority, MaxAttempts: 3,
		Members: []accounts.AggregationMember{
			{Provider: "workbuddy", Model: "model-b", Priority: 0, Weight: 100},
			{Provider: "qoder", Model: "model-a", Priority: 1, Weight: 100},
		},
	})
	q := aggRouteQuery(plan)
	for i := 0; i < 10; i++ {
		item, ok := p.PickRoute(q)
		if !ok || item.ID != "workbuddy" {
			t.Fatalf("priority strategy pick %d = %+v (%v), want workbuddy", i, item, ok)
		}
	}
}

func TestAggregatePriorityFallsToNextOnCooldown(t *testing.T) {
	p := aggTestPool()
	plan := NewAggregatePlan(accounts.Aggregation{
		ID: "my-model", Strategy: accounts.AggregationPriority, MaxAttempts: 3,
		Members: []accounts.AggregationMember{
			{Provider: "workbuddy", Model: "model-b", Priority: 0, Weight: 100},
			{Provider: "qoder", Model: "model-a", Priority: 1, Weight: 100},
		},
	})
	// Cool the top-priority member's real model.
	top, _ := p.ByID("workbuddy")
	p.MarkClassified("workbuddy", Classified{Kind: KindRateLimit, Model: effectiveModel(top, aggRouteQuery(plan)), Cooldown: time.Minute})
	item, ok := p.PickRoute(aggRouteQuery(plan))
	if !ok || item.ID != "qoder" {
		t.Fatalf("after top-priority cooldown want qoder, got %+v ok=%v", item, ok)
	}
}

func TestAggregateFillFirstUsesMemberWeight(t *testing.T) {
	p := aggTestPool()
	plan := NewAggregatePlan(accounts.Aggregation{
		ID: "my-model", Strategy: accounts.AggregationFillFirst, MaxAttempts: 3,
		Members: []accounts.AggregationMember{
			// Deliberately low account weight on workbuddy; member weight is high.
			{Provider: "workbuddy", Model: "model-b", Priority: 0, Weight: 500},
			{Provider: "qoder", Model: "model-a", Priority: 1, Weight: 100},
		},
	})
	p.items[1].Weight = 1   // workbuddy account weight tiny
	p.items[0].Weight = 200 // qoder account weight large
	for i := 0; i < 6; i++ {
		item, ok := p.PickRoute(aggRouteQuery(plan))
		if !ok || item.ID != "workbuddy" {
			t.Fatalf("fill-first should use member weight, pick %d = %+v", i, item)
		}
	}
}

func TestAggregateRoundRobinRotates(t *testing.T) {
	p := aggTestPool()
	p.SetRoutingStrategy(RoutingStrategyRoundRobin)
	plan := NewAggregatePlan(accounts.Aggregation{
		ID: "my-model", Strategy: accounts.AggregationRoundRobin, MaxAttempts: 3,
		Members: []accounts.AggregationMember{
			{Provider: "workbuddy", Model: "model-b", Priority: 0, Weight: 100},
			{Provider: "qoder", Model: "model-a", Priority: 1, Weight: 100},
		},
	})
	q := aggRouteQuery(plan)
	seen := map[string]bool{}
	for i := 0; i < 6; i++ {
		item, ok := p.PickRoute(q)
		if !ok {
			t.Fatal("no route")
		}
		seen[item.ID] = true
	}
	if !seen["workbuddy"] || !seen["qoder"] {
		t.Fatalf("round-robin should rotate across members, seen=%v", seen)
	}
}

// TestAggregateFailoverStaysInFirstRegion guards the cross-region invariant:
// once an aggregated route picks its first member, every failover attempt for
// that request must stay in the same region. Without the lock, a priority
// member in cn falling through could fail over to a global member.
func TestAggregateFailoverStaysInFirstRegion(t *testing.T) {
	p := NewPool(
		[]string{"http://qoder-cn:3020", "http://wb-global:3020"},
		[]string{"qoder", "workbuddy"},
	)
	p.items[0].Provider = "qoder"
	p.items[0].Region = "cn"
	p.items[1].Provider = "workbuddy"
	p.items[1].Region = "global"
	p.MergeModels("qoder", []string{"shared"})
	p.MergeModels("workbuddy", []string{"shared"})

	plan := NewAggregatePlan(accounts.Aggregation{
		ID: "my-model", Strategy: accounts.AggregationPriority,
		AllowFallbacks: true, MaxAttempts: 3,
		Members: []accounts.AggregationMember{
			{Provider: "qoder", Region: "cn", Model: "shared", Priority: 0, Weight: 100},
			{Provider: "workbuddy", Region: "", Model: "shared", Priority: 1, Weight: 100},
		},
	})
	ctx := WithAggregatePlan(context.Background(), plan)
	e := ChatExecutor{Pool: p}
	loop := e.newRouteLoop(ctx, "", "", translate.ChatRequest{Model: "my-model"})

	first, _, err := loop.pickNext(e, "my-model")
	if err != nil {
		t.Fatalf("first pick: %v", err)
	}
	if got := NormalizeRegion(first.Region); got != "cn" {
		t.Fatalf("first pick region=%q, want cn", got)
	}
	if got := NormalizeRegion(loop.regionFilter); got != "cn" {
		t.Fatalf("regionFilter after first pick=%q, want cn (must lock before failover)", got)
	}

	// A second attempt must be confined to cn. There is no second cn member,
	// so excluding the first must yield no candidate instead of a global one.
	loop.excluded[first.ID] = struct{}{}
	_, _, err = loop.pickNext(e, "my-model")
	if err == nil {
		t.Fatal("failover crossed region: second pick succeeded on a different region")
	}
}

func TestAggregateMemberAccountPin(t *testing.T) {
	p := NewPool(nil, nil)
	p.Upsert(Item{ID: "q1", Provider: "qoder", Region: "global", Models: []string{"model-a"}})
	p.Upsert(Item{ID: "q2", Provider: "qoder", Region: "global", Models: []string{"model-a"}})
	plan := NewAggregatePlan(accounts.Aggregation{
		ID: "my-model", Strategy: accounts.AggregationPriority, MaxAttempts: 2,
		Members: []accounts.AggregationMember{
			{Provider: "qoder", Model: "model-a", AccountID: "q2", Priority: 0, Weight: 100},
		},
	})
	q1, _ := p.ByID("q1")
	q2, _ := p.ByID("q2")
	if _, ok := plan.matchMember(q1); ok {
		t.Fatal("q1 must not satisfy a member pinned to q2")
	}
	if member, ok := plan.matchMember(q2); !ok || member.AccountID != "q2" {
		t.Fatalf("q2 member = %+v ok=%v", member, ok)
	}
	for i := 0; i < 5; i++ {
		item, ok := p.PickRoute(aggRouteQuery(plan))
		if !ok || item.ID != "q2" {
			t.Fatalf("pinned member pick %d = %+v ok=%v", i, item, ok)
		}
	}
}

func TestAggregatePinnedMemberWinsOverWildcard(t *testing.T) {
	p := NewPool(nil, nil)
	p.Upsert(Item{ID: "q1", Provider: "qoder", Region: "global", Models: []string{"model-a"}})
	plan := NewAggregatePlan(accounts.Aggregation{
		ID: "my-model", Strategy: accounts.AggregationPriority, MaxAttempts: 2,
		Members: []accounts.AggregationMember{
			{Provider: "qoder", Model: "model-a", Priority: 0, Weight: 100},
			{Provider: "qoder", Model: "model-a", AccountID: "q1", Priority: 1, Weight: 400},
		},
	})
	q1, _ := p.ByID("q1")
	member, ok := plan.matchMember(q1)
	if !ok || member.AccountID != "q1" || member.Priority != 1 || member.Weight != 400 {
		t.Fatalf("pinned member = %+v ok=%v, want account-specific priority/weight", member, ok)
	}
}
