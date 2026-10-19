package executor

import "testing"

// QuotaBlockedForModel must surface an account that could serve the requested
// model but is kept out of routing solely because its quota is exhausted. This
// is how the request path returns a quota error instead of a generic "no
// accounts available" when every model-serving account is out of credits.
func TestQuotaBlockedForModel(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{
		ID:       "acc-exhausted",
		Provider: "qoder",
		Region:   "global",
		Models:   []string{"ultimate"},
		Quota:    &QuotaSnapshot{Exceeded: true},
	})

	if _, ok := pool.QuotaBlockedForModel(RouteQuery{PublicModel: "ultimate"}); !ok {
		t.Fatal("exhausted account serving the model was not reported as quota-blocked")
	}
}

// A free model spends no credits, so an exhausted account is never quota-blocked
// for it: the account can still serve the route.
func TestQuotaBlockedForModelFreeModelNotBlocked(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{
		ID:         "acc-exhausted",
		Provider:   "qoder",
		Region:     "global",
		Models:     []string{"qwen3.8-flash", "ultimate"},
		FreeModels: []string{"qwen3.8-flash"},
		Quota:      &QuotaSnapshot{Exceeded: true},
	})

	if _, ok := pool.QuotaBlockedForModel(RouteQuery{PublicModel: "qwen3.8-flash"}); ok {
		t.Fatal("free model was reported as quota-blocked; an exhausted account can still serve it")
	}
}

// An account that does not serve the requested model, is not exhausted, or is
// excluded from the attempt must not be reported as quota-blocked.
func TestQuotaBlockedForModelNotBlocked(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{
		ID:       "acc-ok",
		Provider: "qoder",
		Region:   "global",
		Models:   []string{"ultimate"},
		Quota:    &QuotaSnapshot{Exceeded: false},
	})
	pool.Upsert(Item{
		ID:       "acc-other-model",
		Provider: "qoder",
		Region:   "global",
		Models:   []string{"other"},
		Quota:    &QuotaSnapshot{Exceeded: true},
	})

	if _, ok := pool.QuotaBlockedForModel(RouteQuery{PublicModel: "ultimate"}); ok {
		t.Fatal("no account serving 'ultimate' is quota-blocked, but one was reported")
	}

	excluded := RouteQuery{PublicModel: "ultimate", Excluded: map[string]struct{}{"acc-ok": {}}}
	if _, ok := pool.QuotaBlockedForModel(excluded); ok {
		t.Fatal("excluded accounts must not be reported as quota-blocked")
	}
}
