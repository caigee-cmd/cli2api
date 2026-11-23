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

// TestAggregateFailoverCrossesMemberRegions guards the cross-provider
// invariant: aggregated members carry their own provider/region, so the route
// loop must NOT pin the first member's region. A cn member falling through must
// be able to reach a global member, and plan.MaxAttempts must survive the first
// pick instead of being recomputed from the pinned region.
func TestAggregateFailoverCrossesMemberRegions(t *testing.T) {
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
	if loop.regionFilter != "" {
		t.Fatalf("aggregated loop must not pin a region, got %q", loop.regionFilter)
	}
	if loop.attempts != 2 {
		t.Fatalf("attempts after first pick=%d, want 2 (plan.MaxAttempts capped by candidates)", loop.attempts)
	}

	// A second attempt must be allowed to cross the member region. Excluding the
	// cn member leaves the global workbuddy member as the next candidate.
	loop.excluded[first.ID] = struct{}{}
	second, _, err := loop.pickNext(e, "my-model")
	if err != nil {
		t.Fatalf("failover should reach the global member: %v", err)
	}
	if got := NormalizeRegion(second.Region); got != "global" {
		t.Fatalf("failover region=%q, want global", got)
	}
	if loop.attempts != 2 {
		t.Fatalf("attempts after failover=%d, want 2 (plan.MaxAttempts must not be overwritten)", loop.attempts)
	}
}
