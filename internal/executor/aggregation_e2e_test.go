package executor

import (
	"context"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

type aggResolverStub struct {
	agg accounts.Aggregation
	hit int
}

func (s *aggResolverStub) Resolve(_ context.Context, id string, _ map[string]bool) (accounts.Aggregation, error) {
	s.hit++
	if id != s.agg.ID {
		return accounts.Aggregation{}, accounts.ErrAggregationNotFound
	}
	return s.agg, nil
}

func TestAggregateFailoverSettingsDriveAttempts(t *testing.T) {
	pool := aggTestPool()
	resolver := &aggResolverStub{agg: accounts.Aggregation{
		ID: "my-model", Enabled: true, Strategy: accounts.AggregationPriority,
		AllowFallbacks: false, MaxAttempts: 7,
		Members: []accounts.AggregationMember{
			{Provider: "qoder", Model: "model-a", Priority: 0},
			{Provider: "workbuddy", Model: "model-b", Priority: 1},
		},
	}}
	e := ChatExecutor{Pool: pool, MaxAttempts: 6}
	prepared, err := e.Prepare(PrepareInput{
		Context:           context.Background(),
		Request:           translate.ChatRequest{Model: "my-model", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}},
		Identity:          auth.ConsoleIdentity(),
		CrossProviderPool: true,
		ModelAggregation:  true,
		Aggregations:      resolver,
		Now:               time.Now(),
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	loop := e.newRouteLoop(prepared.Context, "", "", prepared.Request)
	if loop.attempts != 1 {
		t.Fatalf("allow_fallbacks=false must cap attempts to 1, got %d", loop.attempts)
	}

	resolver.agg.AllowFallbacks = true
	resolver.agg.MaxAttempts = 2
	prepared, err = e.Prepare(PrepareInput{
		Context:           context.Background(),
		Request:           translate.ChatRequest{Model: "my-model", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}},
		Identity:          auth.ConsoleIdentity(),
		CrossProviderPool: true,
		ModelAggregation:  true,
		Aggregations:      resolver,
		Now:               time.Now(),
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	loop = e.newRouteLoop(prepared.Context, "", "", prepared.Request)
	if loop.attempts != 2 {
		t.Fatalf("max_attempts=2 must cap attempts to 2, got %d", loop.attempts)
	}
}

func TestAggregatePrepareToRoute(t *testing.T) {
	pool := aggTestPool()
	resolver := &aggResolverStub{agg: accounts.Aggregation{
		ID: "my-model", Enabled: true, Strategy: accounts.AggregationPriority,
		AllowFallbacks: true, MaxAttempts: 3,
		Members: []accounts.AggregationMember{
			{Provider: "qoder", Model: "model-a", Priority: 0},
			{Provider: "workbuddy", Model: "model-b", Priority: 1},
		},
	}}
	e := ChatExecutor{Pool: pool}

	prepared, err := e.Prepare(PrepareInput{
		Context:           context.Background(),
		Request:           translate.ChatRequest{Model: "my-model", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}},
		Identity:          auth.ConsoleIdentity(),
		CrossProviderPool: true,
		ModelAggregation:  true,
		Aggregations:      resolver,
		Now:               time.Now(),
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if resolver.hit != 1 {
		t.Fatalf("resolver called %d times, want 1 (snapshot once)", resolver.hit)
	}
	plan := aggregatePlanFrom(prepared.Context)
	if plan == nil || plan.ID != "my-model" {
		t.Fatalf("plan not attached to context: %+v", plan)
	}
	// The loop must not pin a provider for an aggregated request and must route
	// only to members.
	loop := e.newRouteLoop(prepared.Context, "", "", prepared.Request)
	if loop.resolver == nil {
		t.Fatal("route loop missing aggregate resolver")
	}
	counts := map[string]int{}
	for i := 0; i < 10; i++ {
		item, _, err := loop.pickNext(e, "my-model")
		if err != nil {
			t.Fatalf("pick: %v", err)
		}
		counts[item.ID]++
	}
	if counts["trae"] != 0 || counts["qoder"]+counts["workbuddy"] != 10 {
		t.Fatalf("unexpected aggregate routing: %v", counts)
	}
}
