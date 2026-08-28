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

func TestPickRouteKeepsQoderFailoverInsideRegion(t *testing.T) {
	p := NewPool(nil, nil)
	p.Upsert(Item{ID: "g1", URL: "http://g1", Provider: "qoder", Region: "global", Runtime: "child_process"})
	p.Upsert(Item{ID: "g2", URL: "http://g2", Provider: "qoder", Region: "global", Runtime: "child_process"})
	p.Upsert(Item{ID: "c1", URL: "http://c1", Provider: "qoder", Region: "cn", Runtime: "child_process"})

	p.MarkClassified("g1", Classified{Kind: KindRateLimit, Cooldown: time.Hour, Message: "429", Failover: true})
	next, ok := p.PickRoute(RouteQuery{ProviderFilter: "qoder", RegionFilter: "global"})
	if !ok || next.ID != "g2" {
		t.Fatalf("global failover = %+v ok=%v", next, ok)
	}

	cn, ok := p.PickRoute(RouteQuery{ProviderFilter: "qoder", RegionFilter: "cn"})
	if !ok || cn.ID != "c1" {
		t.Fatalf("cn pick = %+v ok=%v", cn, ok)
	}

	p.MarkClassified("c1", Classified{Kind: KindRateLimit, Cooldown: time.Hour, Message: "429", Failover: true})
	escaped, ok := p.PickRoute(RouteQuery{ProviderFilter: "qoder", RegionFilter: "cn", PreferAccount: "c1"})
	if !ok || escaped.Region == "global" {
		t.Fatalf("cooling CN pin escaped to %+v ok=%v", escaped, ok)
	}
	if got := p.LenRoute(RouteQuery{ProviderFilter: "qoder", RegionFilter: "global"}); got != 2 {
		t.Fatalf("global candidates = %d", got)
	}
}

func TestPickRouteFiltersByPublicModel(t *testing.T) {
	p := NewPool(nil, nil)
	p.Upsert(Item{ID: "a", URL: "http://a", Provider: "qoder", Region: "global", Runtime: "child_process"})
	p.Upsert(Item{ID: "b", URL: "http://b", Provider: "qoder", Region: "global", Runtime: "child_process"})
	p.MergeModels("a", []string{"glm-5.2"})
	p.MergeModels("b", []string{"hy3", "glm-5.2"})

	got, ok := p.PickRoute(RouteQuery{ProviderFilter: "qoder", PublicModel: "hy3"})
	if !ok || got.ID != "b" {
		t.Fatalf("hy3 pick = %+v ok=%v", got, ok)
	}
	if n := p.LenRoute(RouteQuery{ProviderFilter: "qoder", PublicModel: "hy3"}); n != 1 {
		t.Fatalf("hy3 candidates = %d", n)
	}

	if _, ok := p.PickRoute(RouteQuery{ProviderFilter: "qoder", PublicModel: "hy3", PreferAccount: "a"}); ok {
		t.Fatal("pin to an account that does not serve hy3 must not silently switch")
	}

	unknown, ok := p.PickRoute(RouteQuery{ProviderFilter: "qoder", PublicModel: "hy3", PreferAccount: "missing"})
	if !ok || unknown.ID != "b" {
		t.Fatalf("unknown pin should fall back to hy3 account, got %+v ok=%v", unknown, ok)
	}
}

func TestModelNotAvailableDoesNotCooldown(t *testing.T) {
	p := NewPool(nil, nil)
	p.Upsert(Item{ID: "a", URL: "http://a", Provider: "qoder"})
	p.MarkClassified("a", Classified{Kind: KindModelNotAvailable, Failover: true, Cooldown: 15 * time.Second, Message: "hy3 missing"})
	item, ok := p.ByID("a")
	if !ok || !item.DownUntil.IsZero() || item.LastKind != "" {
		t.Fatalf("model_not_available must not cool the account: %+v", item)
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
