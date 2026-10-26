package executor

import (
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

// fefoBillingUTCOffsetSeconds is the credit-package billing calendar. WorkBuddy
// reports CycleEndTime as a UTC+8 wall clock, so two expiries on the same
// billing day share one priority batch even when their timestamps differ.
const fefoBillingUTCOffsetSeconds = 8 * 60 * 60

// effectiveCreditModel resolves an account alias to the model whose pricing
// actually applies. A free alias must stay free, and an empty/auto model must
// not pretend to spend credits.
func effectiveCreditModel(item Item, publicModel string) string {
	model := routeModel(publicModel)
	if target := item.ModelAliases[model]; target != "" {
		model = routeModel(target)
	}
	return model
}

// routeQuotaExceeded is the single quota gate for routing. The historical
// strategies keep the cached Exceeded flag; FEFO additionally treats a
// trustworthy package set with no live positive balance as exhausted, because
// an expired package can leave Exceeded false until the next quota refresh.
func routeQuotaExceeded(item Item, q RouteQuery) bool {
	model := q.PublicModel
	if !q.fefoNow.IsZero() {
		model = effectiveCreditModel(item, model)
	}
	if itemServesFreeModel(item, model) {
		return false
	}
	if item.Quota != nil && item.Quota.Exceeded {
		return true
	}
	if !q.fefoNow.IsZero() {
		_, depleted := fefoExpiry(item, q.PublicModel, q.fefoNow)
		return depleted
	}
	return false
}

// fefoQuotaState inspects one account's packages without reference to a model.
// tracked is true only when the provider explicitly scoped every reported
// package to this account and the package is credit-denominated. Such a set can
// prove "no live positive balance"; mixed, unscoped, or non-credit data cannot.
func fefoQuotaState(item Item, now time.Time) (earliest int64, depleted, tracked bool) {
	quota := item.Quota
	if quota == nil || len(quota.Packages) == 0 || quota.HasAddOn || quota.HasResourcePackage {
		return 0, false, false
	}
	unknownDate := false
	live := false
	for _, pkg := range quota.Packages {
		if pkg.Scope != accounts.QuotaPackageScopeAccountFEFO || (pkg.Unit != "" && pkg.Unit != "credits") {
			return 0, false, false
		}
		// NaN fails this comparison too, so a malformed balance never counts as
		// spendable. A package with no reported expiry stays in the unknown
		// tier instead of borrowing the subscription or token lifetime.
		if !(pkg.Remain > 0) || (pkg.EndsAt > 0 && pkg.EndsAt <= now.Unix()) {
			continue
		}
		live = true
		if pkg.EndsAt <= 0 {
			unknownDate = true
			continue
		}
		if earliest == 0 || pkg.EndsAt < earliest {
			earliest = pkg.EndsAt
		}
	}
	if !live {
		return 0, true, true
	}
	if unknownDate {
		return 0, false, true
	}
	return earliest, false, true
}

// fefoExpiry returns the nearest live package expiry for a paid model. A free
// model never consumes credits, so it never creates FEFO priority or depletion.
func fefoExpiry(item Item, publicModel string, now time.Time) (expiry int64, depleted bool) {
	model := effectiveCreditModel(item, publicModel)
	if model == "" || itemServesFreeModel(item, model) {
		return 0, false
	}
	earliest, depleted, tracked := fefoQuotaState(item, now)
	if !tracked {
		return 0, false
	}
	return earliest, depleted
}

// fefoCreditDay maps an expiry instant onto the UTC+8 billing day. Expired
// packages are filtered by the caller before this comparison so a package that
// lapsed earlier today cannot remain the "nearest" batch.
func fefoCreditDay(expiry int64) int64 {
	return (expiry + fefoBillingUTCOffsetSeconds) / (24 * 60 * 60)
}

// fefoCandidates narrows the already healthy/model/region/concurrency-filtered
// pool to each provider's earliest billing day. Providers are grouped by family
// so a Qoder or Kimi account never has to compete with a WorkBuddy expiry, and
// the existing ID-cursor round-robin still distributes that day's requests.
// A provider with no usable dates keeps all of its accounts; inside a provider
// that has dated accounts, unknown-date accounts are fallback candidates that
// rejoin as soon as no dated account is available.
func fefoCandidates(available []Item, publicModel string, now time.Time) []Item {
	if len(available) < 2 {
		return available
	}
	minDay := make(map[string]int64, 4)
	known := make(map[string]bool, 4)
	for _, item := range available {
		expiry, _ := fefoExpiry(item, publicModel, now)
		if expiry <= 0 {
			continue
		}
		family := accounts.NormalizeProviderFamily(item.Provider)
		day := fefoCreditDay(expiry)
		if !known[family] || day < minDay[family] {
			minDay[family] = day
			known[family] = true
		}
	}
	if len(known) == 0 {
		return available
	}
	out := make([]Item, 0, len(available))
	for _, item := range available {
		family := accounts.NormalizeProviderFamily(item.Provider)
		if !known[family] {
			out = append(out, item)
			continue
		}
		expiry, _ := fefoExpiry(item, publicModel, now)
		if expiry <= 0 || fefoCreditDay(expiry) != minDay[family] {
			continue
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return available
	}
	return out
}

// HasFEFOQuota reports whether a completed request could have consumed a
// trustworthy package balance. Runtime uses this gate to refresh only opt-in,
// supported routes; the historical strategies add no upstream traffic.
func (p *Pool) HasFEFOQuota(accountID, publicModel string) bool {
	if p == nil || accountID == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.routingStrategy != RoutingStrategyFEFO {
		return false
	}
	for _, item := range p.items {
		if item.ID != accountID {
			continue
		}
		model := effectiveCreditModel(item, publicModel)
		if model == "" || itemServesFreeModel(item, model) {
			return false
		}
		_, _, tracked := fefoQuotaState(item, time.Now())
		return tracked
	}
	return false
}

// FEFOQuotaDepleted reports whether an account routed by FEFO has a
// trustworthy package set with no live positive balance. A naturally expired
// cache looks exactly like this while its top-level Exceeded flag stays false.
// The runtime stale-quota sweep uses it to discover newly granted upstream
// packages; unknown scopes or dates never qualify.
func (p *Pool) FEFOQuotaDepleted(accountID string) bool {
	if p == nil || accountID == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.routingStrategy != RoutingStrategyFEFO {
		return false
	}
	for _, item := range p.items {
		if item.ID != accountID {
			continue
		}
		_, depleted, tracked := fefoQuotaState(item, time.Now())
		return tracked && depleted
	}
	return false
}
