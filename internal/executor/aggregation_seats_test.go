package executor

import (
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func TestSeatOrderDoesNotFlattenToModelName(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "a", Provider: "qoder", Region: "global", Models: []string{"model-a"}})
	pool.Upsert(Item{ID: "z", Provider: "qoder", Region: "global", Models: []string{"model-z"}})
	plan := NewAggregatePlan(accounts.Aggregation{
		ID: "outer", Strategy: accounts.AggregationPriority, AllowFallbacks: true, MaxAttempts: 2,
		Members: []accounts.AggregationMember{
			{Provider: "qoder", Model: "model-z", Priority: 0, Weight: 100},
			{Provider: "qoder", Model: "model-a", Priority: 1, Weight: 100},
		},
	})
	plan.Seats = []accounts.RouteSeat{
		{ID: "first", Kind: "member", Priority: 0, Weight: 100, Member: plan.Members[0]},
		{ID: "second", Kind: "member", Priority: 1, Weight: 100, Member: plan.Members[1]},
	}
	exec := ChatExecutor{Pool: pool}
	item, err := exec.pick("req", "", "", "", "outer", nil, map[string]struct{}{}, nil, plan)
	if err != nil || item.ID != "z" {
		t.Fatalf("first pick = %+v %v", item, err)
	}
	item, err = exec.pick("req", "", "", "", "outer", nil, map[string]struct{}{"z": {}}, nil, plan)
	if err != nil || item.ID != "a" {
		t.Fatalf("second pick = %+v %v", item, err)
	}
}
