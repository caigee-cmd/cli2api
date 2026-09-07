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

func TestNormalizeProviderRegionAndAllowlist(t *testing.T) {
	if got := NormalizeProviderFamily(""); got != "qoder" {
		t.Fatalf("empty provider = %q", got)
	}
	if got := NormalizeRegion(""); got != "global" {
		t.Fatalf("empty region = %q", got)
	}
	if !ProviderAllowed("", []string{"qoder"}) {
		t.Fatal("empty account provider must match a qoder allowlist")
	}
	if ProviderAllowed("trae", []string{"qoder"}) {
		t.Fatal("trae must not match a qoder allowlist")
	}
	if !ProviderAllowed("workbuddy", nil) {
		t.Fatal("empty allowlist must admit every family")
	}
	if !ProviderAllowed("Qoder", []string{"qoder"}) || !ProviderAllowed("qoder", []string{"QODER"}) {
		t.Fatal("provider allowlists must match case-insensitively")
	}

	ready := Item{ID: "a", Models: []string{"hy3"}, ProvenModels: []string{"glm-5.2"}}
	if !ItemHasModel(ready, "glm-5.2") {
		t.Fatal("proven models must satisfy ItemHasModel")
	}
	if ItemHasModel(Item{ID: "b", Models: []string{"hy3"}}, "glm-5.2") {
		t.Fatal("catalog without the model must fail ItemHasModel")
	}
	coolingUnknown := Item{ID: "c", DownUntil: time.Now().Add(time.Hour)}
	if ItemHasModel(coolingUnknown, "glm-5.2") {
		t.Fatal("cooling empty catalog must fail ItemHasModel")
	}
	if !ItemCouldServeModel(coolingUnknown, "glm-5.2") {
		t.Fatal("cooling empty catalog must still belong on the model route")
	}
}

func TestPickRouteNormalizesProviderAndRegion(t *testing.T) {
	p := NewPool(nil, nil)
	p.Upsert(Item{ID: "q1", URL: "http://q1", Runtime: "child_process"})
	p.Upsert(Item{ID: "q2", URL: "http://q2", Provider: "Qoder", Region: "Global", Runtime: "child_process"})
	p.Upsert(Item{ID: "w1", Provider: "WorkBuddy", Region: "CN", Runtime: "in_process"})

	qoder, ok := p.PickRoute(RouteQuery{ProviderFilter: "QODER", PreferAccount: "q1"})
	if !ok || qoder.ID != "q1" {
		t.Fatalf("empty provider must match qoder filter, got %+v ok=%v", qoder, ok)
	}
	global, ok := p.PickRoute(RouteQuery{ProviderFilter: "qoder", RegionFilter: "GLOBAL", PreferAccount: "q1"})
	if !ok || global.ID != "q1" {
		t.Fatalf("empty region must match global filter, got %+v ok=%v", global, ok)
	}
	workbuddy, ok := p.PickRoute(RouteQuery{ProviderFilter: "workbuddy", RegionFilter: "cn"})
	if !ok || workbuddy.ID != "w1" {
		t.Fatalf("mixed-case provider/region = %+v ok=%v", workbuddy, ok)
	}
	stored, ok := p.ByID("w1")
	if !ok || stored.Provider != "workbuddy" || stored.Region != "cn" {
		t.Fatalf("upsert must store canonical provider/region, got %+v", stored)
	}
	empty, ok := p.ByID("q1")
	if !ok || empty.Provider != "qoder" || empty.Region != "global" {
		t.Fatalf("empty provider/region must store qoder/global, got %+v", empty)
	}
}

func TestPickRouteCandidateCountShrinksAfterRegionPin(t *testing.T) {
	p := NewPool(nil, nil)
	p.Upsert(Item{ID: "g1", URL: "http://g1", Provider: "qoder", Region: "global", Runtime: "child_process"})
	p.Upsert(Item{ID: "g2", URL: "http://g2", Provider: "qoder", Region: "global", Runtime: "child_process"})
	p.Upsert(Item{ID: "c1", URL: "http://c1", Provider: "qoder", Region: "cn", Runtime: "child_process"})

	open := RouteQuery{ProviderFilter: "qoder"}
	if n := p.LenRoute(open); n != 3 {
		t.Fatalf("unpinned qoder candidates = %d", n)
	}
	first, ok := p.PickRoute(open)
	if !ok || first.ID != "g1" {
		t.Fatalf("first pick = %+v ok=%v", first, ok)
	}

	pinned := RouteQuery{ProviderFilter: "qoder", RegionFilter: first.Region, Excluded: map[string]struct{}{first.ID: {}}}
	if n := p.LenRoute(pinned); n != 1 {
		t.Fatalf("after first failure, same-region candidates = %d, want 1", n)
	}
	if n := p.LenRoute(RouteQuery{ProviderFilter: "qoder", RegionFilter: "cn"}); n != 1 {
		t.Fatalf("cn candidates must stay out of the pinned retry set, got %d", n)
	}
	retry, ok := p.PickRoute(pinned)
	if !ok || retry.ID != "g2" {
		t.Fatalf("retry pick = %+v ok=%v", retry, ok)
	}
}

func TestPickRouteHonorsAPIKeyAllowlist(t *testing.T) {
	p := NewPool(nil, nil)
	p.Upsert(Item{ID: "q1", URL: "http://q1", Provider: "qoder", Runtime: "child_process"})
	p.Upsert(Item{ID: "w1", Provider: "workbuddy", Runtime: "in_process"})
	p.Upsert(Item{ID: "t1", Provider: "trae", Runtime: "in_process"})

	got, ok := p.PickRoute(RouteQuery{AllowedProviders: []string{"trae"}})
	if !ok || got.ID != "t1" {
		t.Fatalf("allowlist pick = %+v ok=%v", got, ok)
	}
	if n := p.LenRoute(RouteQuery{AllowedProviders: []string{"qoder", "workbuddy"}}); n != 2 {
		t.Fatalf("two-family allowlist count = %d", n)
	}
	if _, ok := p.PickRoute(RouteQuery{ProviderFilter: "trae", AllowedProviders: []string{"qoder"}}); ok {
		t.Fatal("key limited to qoder must not pick trae")
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

func TestMergeModelsKeepsNativeSpelling(t *testing.T) {
	p := NewPool(nil, nil)
	p.Upsert(Item{ID: "t1", Provider: "trae", Runtime: "in_process"})
	p.MergeModels("t1", []string{"DeepSeek-V4-Flash", "deepseek-v4-flash", "glm-5.2"})
	item, ok := p.ByID("t1")
	if !ok || len(item.Models) != 2 || item.Models[0] != "DeepSeek-V4-Flash" {
		t.Fatalf("models=%v", item.Models)
	}
	if NativeModelID(item, "deepseek-v4-flash") != "DeepSeek-V4-Flash" {
		t.Fatalf("native=%q", NativeModelID(item, "deepseek-v4-flash"))
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
