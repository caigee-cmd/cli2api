package executor

import (
	"context"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

func TestSessionAffinityHonorsOnlyFairRotationStrategies(t *testing.T) {
	for _, strategy := range []string{RoutingStrategyRoundRobin, RoutingStrategyWeightedRoundRobin} {
		if !sessionAffinityHonorsStrategy(strategy) {
			t.Fatalf("%s must honor sticky/region hints", strategy)
		}
	}
	for _, strategy := range []string{
		RoutingStrategyFillFirst,
		RoutingStrategyFEFO,
		RoutingStrategyLeastQuotaFirst,
		RoutingStrategyMostQuotaFirst,
		AggregationStrategyPriority,
	} {
		if sessionAffinityHonorsStrategy(strategy) {
			t.Fatalf("%s must rank every eligible account", strategy)
		}
	}
}

func TestFillFirstIgnoresRegionLatch(t *testing.T) {
	p := NewPool(nil, nil)
	p.SetRoutingStrategy(RoutingStrategyFillFirst)
	p.Upsert(Item{ID: "low", Region: "cn", Weight: 1})
	p.Upsert(Item{ID: "high", Region: "global", Weight: 10})
	q := RouteQuery{}
	p.lastRegion[rotationKey(q)] = "cn"
	item, ok := p.PickRoute(q)
	if !ok || item.ID != "high" {
		t.Fatalf("fill-first ignored by region latch: %+v ok=%v", item, ok)
	}
}

func TestDeterministicStrategyDropsImplicitStickyPrefer(t *testing.T) {
	p := NewPool(nil, nil)
	p.SetRoutingStrategy(RoutingStrategyFillFirst)
	p.Upsert(Item{ID: "low", Region: "cn", Weight: 1})
	p.Upsert(Item{ID: "high", Region: "global", Weight: 10})
	ex := NewChatExecutor(p, "")
	ex.SessionAffinity.Bind("session-1", "low")
	ctx := WithSessionKey(context.Background(), "session-1")
	loop := ex.newRouteLoop(ctx, "", "", translate.ChatRequest{Model: "demo"})
	if loop.prefer != "" {
		t.Fatalf("sticky prefer must be dropped for deterministic strategy, got %q", loop.prefer)
	}
	if loop.routing.Source != routingPool {
		t.Fatalf("routing source=%q, want pool", loop.routing.Source)
	}
}
