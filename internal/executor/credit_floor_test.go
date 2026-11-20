package executor

import (
	"encoding/json"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

// Credit floor gates only paid models on an account whose sampled remaining
// balance is at or below the effective floor. A free model is always exempt,
// and a nil quota (never sampled) is never blocked because there is no
// trustworthy balance to compare.
func TestCreditFloorBlocksPaidModelButNotFreeModel(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.SetDefaultCreditFloor(10)
	pool.Upsert(Item{
		ID:         "acc-low",
		Provider:   "qoder",
		Region:     "global",
		Models:     []string{"paid-one", "free-one"},
		FreeModels: []string{"free-one"},
		Quota:      &QuotaSnapshot{Exceeded: false, Remaining: 5, Total: 100, Unit: "credits"},
	})

	if _, ok := pool.PickRoute(RouteQuery{PublicModel: "paid-one"}); ok {
		t.Fatal("paid model: low-balance account was picked, want blocked by floor")
	}
	item, ok := pool.PickRoute(RouteQuery{PublicModel: "free-one"})
	if !ok || item.ID != "acc-low" {
		t.Fatal("free model: account was blocked, want served (free models exempt)")
	}
}

// Per-account override wins over the global floor. A non-nil 0 disables the
// floor for that account even when a global floor is set (option A semantics).
func TestCreditFloorPerAccountOverride(t *testing.T) {
	global := 50.0
	zero := 0.0
	high := 1.0

	pool := NewPool(nil, nil)
	pool.SetDefaultCreditFloor(global)
	pool.Upsert(Item{
		ID: "acc-zero", Provider: "qoder", Region: "global",
		Models: []string{"paid"}, Quota: &QuotaSnapshot{Remaining: 10, Total: 100, Unit: "credits"},
		CreditFloor: &zero,
	})
	pool.Upsert(Item{
		ID: "acc-high", Provider: "qoder", Region: "global",
		Models: []string{"paid"}, Quota: &QuotaSnapshot{Remaining: 10, Total: 100, Unit: "credits"},
		CreditFloor: &high,
	})
	pool.Upsert(Item{
		ID: "acc-inherit", Provider: "qoder", Region: "global",
		Models: []string{"paid"}, Quota: &QuotaSnapshot{Remaining: 10, Total: 100, Unit: "credits"},
	})

	// acc-zero: override 0 disables floor -> stays eligible (remaining 10 <= global 50).
	// acc-high: override 1 -> remaining 10 > 1 -> stays eligible.
	// acc-inherit: no override -> global 50 -> remaining 10 <= 50 -> blocked.
	seen := map[string]bool{}
	for i := 0; i < 6; i++ {
		if item, ok := pool.PickRoute(RouteQuery{PublicModel: "paid"}); ok {
			seen[item.ID] = true
		}
	}
	if !seen["acc-zero"] || !seen["acc-high"] {
		t.Fatalf("per-account overrides should stay eligible, seen=%v", seen)
	}
	if seen["acc-inherit"] {
		t.Fatal("account without override should inherit global floor and be blocked")
	}
}

// A floor of 0 (global default) disables the gate entirely.
func TestCreditFloorZeroDisablesGate(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.SetDefaultCreditFloor(0)
	pool.Upsert(Item{
		ID: "acc", Provider: "qoder", Region: "global",
		Models: []string{"paid"}, Quota: &QuotaSnapshot{Remaining: 0, Total: 100, Unit: "credits"},
	})
	if _, ok := pool.PickRoute(RouteQuery{PublicModel: "paid"}); !ok {
		t.Fatal("global floor 0 must disable the gate")
	}
}

// An account whose quota has never been sampled (nil) is not blocked by the
// floor — there is no balance to compare against.
func TestCreditFloorDoesNotBlockUnsampledQuota(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.SetDefaultCreditFloor(10)
	pool.Upsert(Item{
		ID: "acc", Provider: "qoder", Region: "global",
		Models: []string{"paid"}, Quota: nil,
	})
	if _, ok := pool.PickRoute(RouteQuery{PublicModel: "paid"}); !ok {
		t.Fatal("nil quota (never sampled) must not be blocked by credit floor")
	}
}

// The pre-existing Exceeded flag still gates ahead of the floor; a balance
// above the floor must not rescue an upstream-reported exhausted account.
func TestCreditFloorDoesNotOverrideExceeded(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.SetDefaultCreditFloor(1)
	pool.Upsert(Item{
		ID: "acc", Provider: "qoder", Region: "global",
		Models: []string{"paid"},
		Quota:  &QuotaSnapshot{Exceeded: true, Remaining: 999, Total: 1000, Unit: "credits"},
	})
	if _, ok := pool.PickRoute(RouteQuery{PublicModel: "paid"}); ok {
		t.Fatal("upstream Exceeded must still block regardless of remaining>floor")
	}
}

// SetCreditFloor updates an existing account's override in place.
func TestSetCreditFloorUpdatesInPlace(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "acc", Provider: "qoder", Region: "global", Models: []string{"paid"},
		Quota: &QuotaSnapshot{Remaining: 5, Total: 100, Unit: "credits"}})
	pool.SetDefaultCreditFloor(10)
	// Before override: blocked by global floor.
	if _, ok := pool.PickRoute(RouteQuery{PublicModel: "paid"}); ok {
		t.Fatal("should be blocked by global floor before override")
	}
	zero := 0.0
	pool.SetCreditFloor("acc", &zero)
	// After override 0: eligible.
	if _, ok := pool.PickRoute(RouteQuery{PublicModel: "paid"}); !ok {
		t.Fatal("override 0 should re-enable the account")
	}
}

// A floor is a credit threshold, not a generic remaining-value threshold.
// Providers reporting percent, entitlement packs or rate-limit windows must
// keep their current scheduling behaviour even when the numeric remaining is
// at or below the floor.
func TestCreditFloorOnlyAppliesToCreditsUnit(t *testing.T) {
	for _, unit := range []string{"percent", "entitlement_pack", "rate_limit_window", "", "Credits"} {
		t.Run(unit, func(t *testing.T) {
			pool := NewPool(nil, nil)
			pool.SetDefaultCreditFloor(100)
			pool.Upsert(Item{
				ID: "acc-non-credit", Provider: "qoder", Region: "global",
				Models: []string{"paid"}, Quota: &QuotaSnapshot{Remaining: 5, Total: 100, Unit: unit},
			})
			if _, ok := pool.PickRoute(RouteQuery{PublicModel: "paid"}); !ok {
				t.Fatalf("unit %q must not be credit-floor blocked", unit)
			}
		})
	}
}

// An explicit per-account zero must be visible in API responses so the
// console can distinguish "disable this floor" from "inherit the global one".
func TestAccountCreditFloorJSONKeepsExplicitZero(t *testing.T) {
	zero := 0.0
	payload, err := json.Marshal(accounts.Account{CreditFloor: &zero})
	if err != nil {
		t.Fatalf("marshal account: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal account: %v", err)
	}
	value, present := decoded["credit_floor"]
	if !present {
		t.Fatalf("explicit credit_floor=0 was omitted from JSON: %s", payload)
	}
	if number, ok := value.(float64); !ok || number != 0 {
		t.Fatalf("credit_floor=%v, want numeric 0", value)
	}
}

// Percent-denominated accounts use their own floor. The global credit floor
// must never gate a percent account, even with the same numeric value.
func TestPercentFloorBlocksPaidModelButNotFreeModel(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.SetDefaultCreditFloor(100)
	pool.SetDefaultPercentFloor(20)
	pool.Upsert(Item{
		ID: "acc-percent-low", Provider: "kimi", Region: "global",
		Models: []string{"paid", "free"}, FreeModels: []string{"free"},
		Quota: &QuotaSnapshot{Remaining: 15, Total: 100, Unit: "percent"},
	})
	if _, ok := pool.PickRoute(RouteQuery{PublicModel: "paid"}); ok {
		t.Fatal("percent account at 15% remaining should be blocked by percent floor 20")
	}
	item, ok := pool.PickRoute(RouteQuery{PublicModel: "free"})
	if !ok || item.ID != "acc-percent-low" {
		t.Fatal("free model must stay available for percent account")
	}
}

func TestPercentFloorPerAccountOverride(t *testing.T) {
	globalPct := 50.0
	zero := 0.0
	high := 1.0
	pool := NewPool(nil, nil)
	pool.SetDefaultPercentFloor(globalPct)
	pool.Upsert(Item{ID: "pct-zero", Provider: "kimi", Region: "global", Models: []string{"paid"},
		Quota: &QuotaSnapshot{Remaining: 10, Total: 100, Unit: "percent"}, PercentFloor: &zero})
	pool.Upsert(Item{ID: "pct-high", Provider: "kimi", Region: "global", Models: []string{"paid"},
		Quota: &QuotaSnapshot{Remaining: 10, Total: 100, Unit: "percent"}, PercentFloor: &high})
	pool.Upsert(Item{ID: "pct-inherit", Provider: "kimi", Region: "global", Models: []string{"paid"},
		Quota: &QuotaSnapshot{Remaining: 10, Total: 100, Unit: "percent"}})
	seen := map[string]bool{}
	for i := 0; i < 6; i++ {
		if item, ok := pool.PickRoute(RouteQuery{PublicModel: "paid"}); ok {
			seen[item.ID] = true
		}
	}
	if !seen["pct-zero"] || !seen["pct-high"] {
		t.Fatalf("percent overrides should stay eligible, seen=%v", seen)
	}
	if seen["pct-inherit"] {
		t.Fatal("percent account without override should inherit global percent floor and be blocked")
	}
}

// Global credit floor 100 used to block percent accounts numerically (the
// maintainer's objection). It must now be inert for percent units.
func TestCreditFloorDoesNotBlockPercentAccount(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.SetDefaultCreditFloor(100)
	pool.SetDefaultPercentFloor(0)
	pool.Upsert(Item{ID: "pct", Provider: "devin", Region: "global", Models: []string{"paid"},
		Quota: &QuotaSnapshot{Remaining: 5, Total: 100, Unit: "percent"}})
	if _, ok := pool.PickRoute(RouteQuery{PublicModel: "paid"}); !ok {
		t.Fatal("CLI2API_CREDIT_FLOOR must not gate a percent account")
	}
}

func TestSetPercentFloorUpdatesInPlace(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "pct", Provider: "kimi", Region: "global", Models: []string{"paid"},
		Quota: &QuotaSnapshot{Remaining: 5, Total: 100, Unit: "percent"}})
	pool.SetDefaultPercentFloor(10)
	if _, ok := pool.PickRoute(RouteQuery{PublicModel: "paid"}); ok {
		t.Fatal("should be blocked by global percent floor before override")
	}
	zero := 0.0
	pool.SetPercentFloor("pct", &zero)
	if _, ok := pool.PickRoute(RouteQuery{PublicModel: "paid"}); !ok {
		t.Fatal("percent override 0 should re-enable the account")
	}
}

func TestAccountPercentFloorJSONKeepsExplicitZero(t *testing.T) {
	zero := 0.0
	payload, err := json.Marshal(accounts.Account{PercentFloor: &zero})
	if err != nil {
		t.Fatalf("marshal account: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal account: %v", err)
	}
	value, present := decoded["percent_floor"]
	if !present {
		t.Fatalf("explicit percent_floor=0 was omitted from JSON: %s", payload)
	}
	if number, ok := value.(float64); !ok || number != 0 {
		t.Fatalf("percent_floor=%v, want numeric 0", value)
	}
}
