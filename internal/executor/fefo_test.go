package executor

import (
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func fefoTime(days, hour int) int64 {
	now := time.Now().In(time.FixedZone("billing", 8*60*60))
	return time.Date(now.Year(), now.Month(), now.Day()+days, hour, 0, 0, 0, now.Location()).Unix()
}

func fefoPack(remain float64, expiry int64) accounts.QuotaPackage {
	return accounts.QuotaPackage{
		Remain: remain,
		Size:   remain,
		Unit:   "credits",
		Scope:  accounts.QuotaPackageScopeAccountFEFO,
		EndsAt: expiry,
	}
}

func fefoItem(id string, packages ...accounts.QuotaPackage) Item {
	return Item{
		ID:       id,
		Provider: "workbuddy",
		Region:   "cn",
		Models:   []string{"paid", "free", "unknown"},
		FreeModels: []string{
			"free",
		},
		Quota: &accounts.QuotaSnapshot{Total: 100, Remaining: 100, Unit: "credits", Packages: packages},
	}
}

func fefoPool(items ...Item) *Pool {
	p := NewPool(nil, nil)
	p.SetRoutingStrategy(RoutingStrategyFEFO)
	for _, item := range items {
		p.Upsert(item)
	}
	return p
}

func assertFEFOPick(t *testing.T, p *Pool, q RouteQuery, want string) {
	t.Helper()
	item, ok := p.PickRoute(q)
	if !ok || item.ID != want {
		t.Fatalf("pick = %q, %v; want %q", item.ID, ok, want)
	}
}

func TestFEFOSelectsEarliestLivePackageAndFollowsRefresh(t *testing.T) {
	later := fefoItem("a", fefoPack(20, fefoTime(3, 12)))
	earlier := fefoItem("z",
		fefoPack(0, fefoTime(1, 12)),
		fefoPack(100, time.Now().Add(-time.Hour).Unix()),
		fefoPack(10, fefoTime(2, 12)),
	)
	// A stale aggregate field must never outrank the live package list.
	later.Quota.ExpiresAt = fefoTime(1, 12)
	p := fefoPool(later, earlier)
	for i := 0; i < 4; i++ {
		assertFEFOPick(t, p, RouteQuery{PublicModel: "paid"}, "z")
	}
	// A quota refresh changes the winner without replacing the pool.
	p.MergeQuota("z", &QuotaSnapshot{Packages: []accounts.QuotaPackage{
		fefoPack(0, fefoTime(2, 12)),
		fefoPack(20, fefoTime(4, 12)),
	}})
	assertFEFOPick(t, p, RouteQuery{PublicModel: "paid"}, "a")
}

func TestFEFOSameBillingDayRotatesAndIgnoresWeights(t *testing.T) {
	a := fefoItem("a", fefoPack(5, fefoTime(2, 1)))
	b := fefoItem("b", fefoPack(20, fefoTime(2, 23)))
	a.Weight, b.Weight = 99, 1
	p := fefoPool(a, b, fefoItem("later", fefoPack(100, fefoTime(3, 1))))
	for i := 0; i < 10; i++ {
		want := "a"
		if i%2 == 1 {
			want = "b"
		}
		assertFEFOPick(t, p, RouteQuery{PublicModel: "paid"}, want)
	}
}

func TestFEFOUnknownExpiryFallsBackWithoutInventingDates(t *testing.T) {
	a := fefoItem("a", fefoPack(10, 0))
	b := fefoItem("b")
	b.Quota.ExpiresAt = fefoTime(1, 12)
	p := fefoPool(a, b)
	for i := 0; i < 6; i++ {
		want := "a"
		if i%2 == 1 {
			want = "b"
		}
		assertFEFOPick(t, p, RouteQuery{PublicModel: "paid"}, want)
	}

	// Inside one provider, a dated cohort outranks unknown dates, but unknown
	// accounts rejoin once no dated account is available.
	known := fefoItem("known", fefoPack(10, fefoTime(1, 12)))
	unknown := fefoItem("unknown", fefoPack(10, 0))
	later := fefoItem("later", fefoPack(10, fefoTime(2, 12)))
	p = fefoPool(known, unknown, later)
	assertFEFOPick(t, p, RouteQuery{PublicModel: "paid"}, "known")
	assertFEFOPick(t, p, RouteQuery{PublicModel: "paid", Excluded: map[string]struct{}{"known": {}}}, "later")
	assertFEFOPick(t, p, RouteQuery{PublicModel: "paid", Excluded: map[string]struct{}{"known": {}, "later": {}}}, "unknown")
}

func TestFEFOCrossProviderIsolation(t *testing.T) {
	q1 := fefoItem("q1")
	q2 := fefoItem("q2")
	q1.Provider, q2.Provider = "qoder", "qoder"
	q1.Models, q2.Models = []string{"paid"}, []string{"paid"}
	w1 := fefoItem("w1", fefoPack(10, fefoTime(1, 12)))
	w2 := fefoItem("w2", fefoPack(10, fefoTime(2, 12)))
	p := fefoPool(q1, q2, w1, w2)
	// Qoder has no package dates, so it keeps rotating both accounts. WorkBuddy
	// contributes only its nearest-expiry account; w2 must not inherit w1's day.
	want := []string{"q1", "q2", "w1", "q1", "q2", "w1"}
	for i, id := range want {
		item, ok := p.PickRoute(RouteQuery{PublicModel: "paid"})
		if !ok || item.ID != id {
			t.Fatalf("pick %d = %q, %v; want %q", i, item.ID, ok, id)
		}
	}
	// A provider-scoped route is unaffected by another provider's dates.
	assertFEFOPick(t, p, RouteQuery{PublicModel: "paid", ProviderFilter: "qoder"}, "q1")
	assertFEFOPick(t, p, RouteQuery{PublicModel: "paid", ProviderFilter: "workbuddy"}, "w1")

	// Even when every provider has dates, the earliest WorkBuddy package must
	// not suppress the earliest Qoder account: each family picks its own batch.
	q1 = fefoItem("q1", fefoPack(10, fefoTime(10, 12)))
	q2 = fefoItem("q2", fefoPack(10, fefoTime(20, 12)))
	q1.Provider, q2.Provider = "qoder", "qoder"
	q1.Models, q2.Models = []string{"paid"}, []string{"paid"}
	w1 = fefoItem("w1", fefoPack(10, fefoTime(1, 12)))
	w2 = fefoItem("w2", fefoPack(10, fefoTime(2, 12)))
	p = fefoPool(q1, q2, w1, w2)
	want = []string{"q1", "w1", "q1", "w1"}
	for i, id := range want {
		item, ok := p.PickRoute(RouteQuery{PublicModel: "paid"})
		if !ok || item.ID != id {
			t.Fatalf("dated pick %d = %q, %v; want %q", i, item.ID, ok, id)
		}
	}
}

func TestFEFODepletedExpiredAndFreeModels(t *testing.T) {
	dead := fefoItem("a", fefoPack(0, fefoTime(1, 12)), fefoPack(50, time.Now().Add(-time.Minute).Unix()))
	live := fefoItem("z", fefoPack(10, fefoTime(2, 12)))
	p := fefoPool(dead, live)
	q := RouteQuery{PublicModel: "paid"}
	assertFEFOPick(t, p, q, "z")
	if got := p.LenRoute(q); got != 1 {
		t.Fatalf("LenRoute = %d, want 1", got)
	}
	if _, ok := p.QuotaBlockedForModel(q); !ok {
		t.Fatal("expired trusted packages must report quota exhaustion, not model mismatch")
	}
	// A known-free model spends no credits and may still use the exhausted account.
	assertFEFOPick(t, p, RouteQuery{PublicModel: "free"}, "a")
	dead.Quota.Exceeded = true
	p = fefoPool(dead, live)
	assertFEFOPick(t, p, RouteQuery{PublicModel: "free"}, "a")
	assertFEFOPick(t, p, RouteQuery{PublicModel: "paid"}, "z")
}

func TestFEFOUnknownScopeMixedBucketsAndAliasesStayUnknown(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*Item)
	}{
		{"unknown scope", func(i *Item) { i.Quota.Packages[0].Scope = "" }},
		{"other scope", func(i *Item) { i.Quota.Packages[0].Scope = "work-only" }},
		{"non-credit unit", func(i *Item) { i.Quota.Packages[0].Unit = "tokens" }},
		{"mixed unknown date", func(i *Item) { i.Quota.Packages = append(i.Quota.Packages, fefoPack(10, 0)) }},
		{"additional add-on bucket", func(i *Item) { i.Quota.HasAddOn = true }},
		{"organization resource package", func(i *Item) { i.Quota.HasResourcePackage = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := fefoItem("a", fefoPack(10, fefoTime(1, 12)))
			test.edit(&a)
			p := fefoPool(a, fefoItem("z", fefoPack(20, fefoTime(2, 12))))
			// Unknown metadata never outranks a dated package; the account is a
			// fallback candidate once the dated account is unavailable.
			assertFEFOPick(t, p, RouteQuery{PublicModel: "paid"}, "z")
			assertFEFOPick(t, p, RouteQuery{PublicModel: "paid", Excluded: map[string]struct{}{"z": {}}}, "a")
		})
	}

	a := fefoItem("a", fefoPack(10, fefoTime(3, 12)))
	z := fefoItem("z", fefoPack(10, fefoTime(1, 12)))
	a.ModelAliases = map[string]string{"free-alias": "free", "paid-alias": "paid"}
	z.ModelAliases = map[string]string{"free-alias": "free", "paid-alias": "paid"}
	p := fefoPool(a, z)
	assertFEFOPick(t, p, RouteQuery{PublicModel: "free-alias"}, "a")
	assertFEFOPick(t, p, RouteQuery{PublicModel: "free-alias"}, "z")
	assertFEFOPick(t, p, RouteQuery{PublicModel: "paid-alias"}, "z")
}

func TestFEFORespectsPinCooldownSaturationAndExclusion(t *testing.T) {
	early := fefoItem("a", fefoPack(10, fefoTime(1, 12)))
	late := fefoItem("z", fefoPack(10, fefoTime(2, 12)))
	p := fefoPool(early, late)
	assertFEFOPick(t, p, RouteQuery{PublicModel: "paid", PreferAccount: "z"}, "z")

	early.DownUntil = time.Now().Add(time.Hour)
	p = fefoPool(early, late)
	assertFEFOPick(t, p, RouteQuery{PublicModel: "paid"}, "z")

	// Once the cooldown expires, the earlier account returns to the front of
	// its provider's ranking.
	early.DownUntil = time.Now().Add(-time.Second)
	p = fefoPool(early, late)
	assertFEFOPick(t, p, RouteQuery{PublicModel: "paid"}, "a")

	early.DownUntil = time.Time{}
	early.MaxInFlight = 1
	p = fefoPool(early, late)
	if !p.Acquire("a") {
		t.Fatal("acquire")
	}
	assertFEFOPick(t, p, RouteQuery{PublicModel: "paid"}, "z")
	p.Release("a")

	assertFEFOPick(t, p, RouteQuery{PublicModel: "paid", Excluded: map[string]struct{}{"a": {}}}, "z")

	early.Region = "global"
	p = fefoPool(early, late)
	assertFEFOPick(t, p, RouteQuery{PublicModel: "paid", RegionFilter: "cn"}, "z")
}

func TestFEFOLeavesHistoricalStrategiesUnchanged(t *testing.T) {
	for _, strategy := range []string{RoutingStrategyRoundRobin, RoutingStrategyWeightedRoundRobin, RoutingStrategyFillFirst} {
		t.Run(strategy, func(t *testing.T) {
			a := fefoItem("a", fefoPack(0, fefoTime(-1, 12)))
			z := fefoItem("z", fefoPack(20, fefoTime(2, 12)))
			a.Weight, z.Weight = 90, 10
			p := fefoPool(a, z)
			p.SetRoutingStrategy(strategy)
			counts := map[string]int{}
			for i := 0; i < 10; i++ {
				item, ok := p.PickRoute(RouteQuery{PublicModel: "paid"})
				if !ok {
					t.Fatal("historical strategy gate changed")
				}
				counts[item.ID]++
			}
			want := 5
			if strategy == RoutingStrategyWeightedRoundRobin {
				want = 9
			}
			if strategy == RoutingStrategyFillFirst {
				want = 10
			}
			if counts["a"] != want {
				t.Fatalf("counts=%v; want %d picks for a", counts, want)
			}
		})
	}
}

func TestFEFOIgnoresSubscriptionAndTokenExpiry(t *testing.T) {
	// The top-level quota timestamp is a subscription/reset boundary on
	// providers such as Qoder, never a credit-package expiry.
	q1 := fefoItem("q1")
	q2 := fefoItem("q2")
	q1.Provider, q2.Provider = "qoder", "qoder"
	q1.Quota.ExpiresAt = fefoTime(1, 12)
	q2.Quota.ExpiresAt = fefoTime(2, 12)
	p := fefoPool(q1, q2)
	for i := 0; i < 4; i++ {
		want := "q1"
		if i%2 == 1 {
			want = "q2"
		}
		assertFEFOPick(t, p, RouteQuery{PublicModel: "paid"}, want)
	}

	// A dated package still outranks an account whose only timestamp is the
	// aggregate field.
	aggregateOnly := fefoItem("aggregate")
	aggregateOnly.Quota.ExpiresAt = fefoTime(1, 12)
	p = fefoPool(aggregateOnly, fefoItem("dated", fefoPack(10, fefoTime(2, 12))))
	assertFEFOPick(t, p, RouteQuery{PublicModel: "paid"}, "dated")
}

func TestFEFOQuotaTrackingAndDepletion(t *testing.T) {
	unknownDate := fefoItem("unknown-date", fefoPack(10, 0))
	if !fefoPool(unknownDate).HasFEFOQuota("unknown-date", "paid") {
		t.Fatal("scoped packages with unknown dates still need post-use refresh")
	}
	unknownScope := fefoItem("unknown-scope", fefoPack(10, fefoTime(1, 12)))
	unknownScope.Quota.Packages[0].Scope = ""
	if fefoPool(unknownScope).HasFEFOQuota("unknown-scope", "paid") {
		t.Fatal("unknown scope must not create extra quota traffic")
	}
	free := fefoItem("free", fefoPack(10, fefoTime(1, 12)))
	if fefoPool(free).HasFEFOQuota("free", "free") {
		t.Fatal("free models must not create FEFO quota traffic")
	}
	check := fefoPool(free)
	check.SetRoutingStrategy(RoutingStrategyFillFirst)
	if check.HasFEFOQuota("free", "paid") {
		t.Fatal("other strategies must not create FEFO quota traffic")
	}
	expired := fefoItem("expired", fefoPack(100, fefoTime(-1, 12)))
	if !fefoPool(expired).FEFOQuotaDepleted("expired") {
		t.Fatal("expired trusted packages should be revalidated under FEFO")
	}
	if fefoPool(unknownDate).FEFOQuotaDepleted("unknown-date") {
		t.Fatal("unknown dates must not be treated as depleted")
	}
}
