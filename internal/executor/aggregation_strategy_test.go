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

// A member with AccountID is a deployment pinned to that account rather than
// the whole provider pool. Sibling accounts keep serving legacy members but
// cannot satisfy this member.
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

// An account with an empty catalog must not be admitted to an aggregate route
// by the generic unknown-catalog fail-open when its region does not match the
// plan member. The resolver is authoritative for virtual model IDs.
func TestAggregateExcludesUnknownCatalogOutsideMemberRegion(t *testing.T) {
	p := NewPool(nil, nil)
	p.Upsert(Item{ID: "q-global", Provider: "qoder", Region: "global", Models: []string{"model-a"}})
	p.Upsert(Item{ID: "q-cn", Provider: "qoder", Region: "cn"}) // catalog not fetched
	plan := NewAggregatePlan(accounts.Aggregation{
		ID: "my-model", Strategy: accounts.AggregationRoundRobin, MaxAttempts: 2,
		Members: []accounts.AggregationMember{
			{Provider: "qoder", Region: "global", Model: "model-a", Priority: 0, Weight: 100},
		},
	})
	for i := 0; i < 10; i++ {
		item, ok := p.PickRoute(aggRouteQuery(plan))
		if !ok || item.ID != "q-global" {
			t.Fatalf("aggregate pick %d = %+v ok=%v, want q-global", i, item, ok)
		}
	}
}

// An account-specific deployment is more specific than a provider-wide
// wildcard member for the same model, even when the wildcard sorts first.
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

// A cross-provider aggregate route must not inherit the pool's region latch:
// its members are already provider/region-qualified, and latching one region
// would route every request to that region's provider only.
func TestAggregateRoundRobinDoesNotLatchRegion(t *testing.T) {
	p := NewPool(nil, nil)
	p.Upsert(Item{ID: "q1", Provider: "qoder", Region: "global", Models: []string{"shared"}})
	p.Upsert(Item{ID: "q2", Provider: "qoder", Region: "global", Models: []string{"shared"}})
	p.Upsert(Item{ID: "w1", Provider: "workbuddy", Region: "cn", Models: []string{"shared"}})
	p.Upsert(Item{ID: "w2", Provider: "workbuddy", Region: "cn", Models: []string{"shared"}})

	plan := NewAggregatePlan(accounts.Aggregation{
		ID: "my-model", Strategy: accounts.AggregationRoundRobin, MaxAttempts: 4,
		Members: []accounts.AggregationMember{
			{Provider: "qoder", Model: "shared", Priority: 0, Weight: 100},
			{Provider: "workbuddy", Model: "shared", Priority: 0, Weight: 100},
		},
	})
	q := aggRouteQuery(plan)
	counts := map[string]int{}
	for i := 0; i < 20; i++ {
		item, ok := p.PickRoute(q)
		if !ok {
			t.Fatal("no aggregate route")
		}
		counts[item.Provider]++
	}
	if counts["qoder"] == 0 || counts["workbuddy"] == 0 {
		t.Fatalf("aggregate route latched one provider: %v", counts)
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
