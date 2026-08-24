package accounts

import (
	"testing"
	"time"
)

func TestPickRouteRespectsProviderFamilyAndCooldown(t *testing.T) {
	p := NewPool(nil, nil)
	p.Upsert(Item{ID: "q1", URL: "http://q1", Provider: "qoder", Runtime: "child_process"})
	p.Upsert(Item{ID: "q2", URL: "http://q2", Provider: "qoder", Runtime: "child_process"})
	p.Upsert(Item{ID: "w1", Provider: "workbuddy", Runtime: "in_process"})
	p.Upsert(Item{ID: "w2", Provider: "workbuddy", Runtime: "in_process"})

	// Baseline failover stays inside one provider family.
	p.MarkClassified("w1", Classified{Kind: KindRateLimit, Cooldown: time.Hour, Message: "429", Failover: true})
	next, ok := p.PickRoute(RouteQuery{ProviderFilter: "workbuddy"})
	if !ok || next.ID != "w2" {
		t.Fatalf("workbuddy failover = %+v ok=%v", next, ok)
	}

	// A cooling Qoder account must never be selected for a workbuddy route.
	qpick, ok := p.PickRoute(RouteQuery{ProviderFilter: "qoder"})
	if !ok || qpick.Provider != "qoder" {
		t.Fatalf("qoder pick = %+v", qpick)
	}

	// Pin wins inside the filtered family.
	pin, ok := p.PickRoute(RouteQuery{ProviderFilter: "workbuddy", PreferAccount: "w2"})
	if !ok || pin.ID != "w2" {
		t.Fatalf("pin = %+v", pin)
	}

	// Excluded accounts shrink the candidate count, not the whole pool.
	if got := p.LenRoute(RouteQuery{ProviderFilter: "workbuddy", Excluded: map[string]struct{}{"w2": {}}}); got != 1 {
		t.Fatalf("workbuddy candidates after exclusion = %d", got)
	}
}

func TestQuotaUsesLongCooldownWithoutRotation(t *testing.T) {
	p := NewPool(nil, nil)
	p.Upsert(Item{ID: "w1", Provider: "workbuddy"})
	p.Upsert(Item{ID: "w2", Provider: "workbuddy"})
	p.MarkClassified("w1", Classified{Kind: KindQuota, Cooldown: 0, Message: "insufficient credit", Failover: false})
	item, ok := p.PickRoute(RouteQuery{ProviderFilter: "workbuddy"})
	if !ok || item.ID != "w1" {
		t.Fatalf("quota should not remove the account from rotation: %+v", item)
	}
}
