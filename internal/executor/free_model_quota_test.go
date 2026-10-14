package executor

import "testing"

// An account whose paid quota is exhausted can still serve a model the provider
// prices at zero: a free model spends no credits, so the upstream does not
// reject it. Filtering such an account out on quota alone would leave a
// zero-credit account off a route that in fact works.
func TestPickRouteKeepsExhaustedAccountForFreeModel(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{
		ID:         "acc-exhausted",
		Provider:   "qoder",
		Region:     "global",
		Models:     []string{"qwen3.8-flash", "ultimate"},
		FreeModels: []string{"qwen3.8-flash"},
		Quota:      &QuotaSnapshot{Exceeded: true, Total: 0},
	})

	free := RouteQuery{PublicModel: "qwen3.8-flash"}
	item, ok := pool.PickRoute(free)
	if !ok {
		t.Fatal("free model: PickRoute returned no account, want the exhausted one")
	}
	if item.ID != "acc-exhausted" {
		t.Fatalf("free model: picked %q, want acc-exhausted", item.ID)
	}

	paid := RouteQuery{PublicModel: "ultimate"}
	if _, ok := pool.PickRoute(paid); ok {
		t.Fatal("paid model: PickRoute returned the exhausted account, want none")
	}
}

// A model the catalog has not priced is not assumed free. Unknown pricing must
// keep the account filtered out rather than gamble a request on it.
func TestPickRouteKeepsExhaustedAccountFilteredWhenPricingUnknown(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{
		ID:       "acc-exhausted",
		Provider: "qoder",
		Region:   "global",
		Models:   []string{"qwen3.8-flash"},
		Quota:    &QuotaSnapshot{Exceeded: true, Total: 0},
	})

	if _, ok := pool.PickRoute(RouteQuery{PublicModel: "qwen3.8-flash"}); ok {
		t.Fatal("unknown pricing: PickRoute returned the exhausted account, want none")
	}
}

// The exemption is scoped to the free model. Once the same account is asked for
// a paid model it must be filtered out again, so one free request cannot keep
// it on a paid route.
func TestPickRouteExemptionDoesNotLeakToPaidModels(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{
		ID:         "acc-exhausted",
		Provider:   "qoder",
		Region:     "global",
		Models:     []string{"free-one", "paid-one"},
		FreeModels: []string{"free-one"},
		Quota:      &QuotaSnapshot{Exceeded: true},
	})

	for i := 0; i < 3; i++ {
		if _, ok := pool.PickRoute(RouteQuery{PublicModel: "free-one"}); !ok {
			t.Fatalf("free-one attempt %d: no account", i)
		}
		if _, ok := pool.PickRoute(RouteQuery{PublicModel: "paid-one"}); ok {
			t.Fatalf("paid-one attempt %d: exhausted account leaked onto the paid route", i)
		}
	}
}

// RemoveModel must drop the id from the free list too, or a model the account
// stopped serving would keep exempting it from quota filtering.
func TestRemoveModelDropsFreeModelEntry(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{
		ID:         "acc",
		Provider:   "qoder",
		Region:     "global",
		Models:     []string{"free-one", "other"},
		FreeModels: []string{"free-one", "other"},
	})
	pool.RemoveModel("acc", "free-one")
	item, ok := pool.ByID("acc")
	if !ok {
		t.Fatal("account missing from pool")
	}
	for _, model := range item.FreeModels {
		if CanonicalModelID(model) == CanonicalModelID("free-one") {
			t.Fatalf("removed model still in FreeModels: %v", item.FreeModels)
		}
	}
	if len(item.FreeModels) != 1 {
		t.Fatalf("FreeModels = %v, want only the surviving entry", item.FreeModels)
	}
}
