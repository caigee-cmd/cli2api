package accounts

import (
	"testing"
	"time"
)

func TestRoundRobinSkipsDownAccounts(t *testing.T) {
	p := NewPool([]string{"http://a:3020", "http://b:3020", "http://c:3020"}, []string{"a", "b", "c"})
	first, ok := p.Pick("", nil)
	if !ok || first.ID != "a" {
		t.Fatalf("first=%+v ok=%v", first, ok)
	}
	second, ok := p.Pick("", nil)
	if !ok || second.ID != "b" {
		t.Fatalf("second=%+v ok=%v", second, ok)
	}
	p.MarkDown("c", time.Hour, "quota")
	third, ok := p.Pick("", nil)
	if !ok || third.ID != "a" {
		t.Fatalf("third after wrap should skip down c, got %+v", third)
	}
	preferred, ok := p.Pick("b", nil)
	if !ok || preferred.ID != "b" {
		t.Fatalf("prefer b got %+v", preferred)
	}
	excluded := map[string]struct{}{"a": {}, "b": {}}
	fallback, ok := p.Pick("", excluded)
	if !ok || fallback.ID != "c" {
		t.Fatalf("excluded fallback got %+v ok=%v", fallback, ok)
	}
}

func TestUpsertKeepsExistingQuota(t *testing.T) {
	p := NewPool([]string{"http://a:3020"}, []string{"a"})
	p.MergeQuota("a", &QuotaSnapshot{Remaining: 900, Total: 1000, Unit: "credits"})
	p.MergeHealth("a", true, true, 0, 0, "")
	p.Upsert(Item{ID: "a", URL: "http://a:3020", Provider: "trae", Runtime: "in_process"})
	item, _ := p.ByID("a")
	if item.Quota == nil || item.Quota.Remaining != 900 || item.Provider != "trae" {
		t.Fatalf("quota should survive upsert, got %+v", item)
	}
	if item.Ready == nil || !*item.Ready || item.Hot == nil || !*item.Hot {
		t.Fatalf("health should survive upsert, got ready=%v hot=%v", item.Ready, item.Hot)
	}
}

func TestMarkOKClearsCooldown(t *testing.T) {
	p := NewPool([]string{"http://a:3020", "http://b:3020"}, []string{"a", "b"})
	p.MarkClassified("a", Classified{Kind: KindRateLimit, Cooldown: time.Hour, Message: "429", Failover: true})
	if item, _ := p.ByID("a"); item.DownUntil.IsZero() {
		t.Fatal("expected cooldown")
	}
	p.MarkOK("a")
	if item, _ := p.ByID("a"); !item.DownUntil.IsZero() || item.LastError != "" {
		t.Fatalf("expected clear, got %+v", item)
	}
}

func TestPickSkipsQuotaCooldownOnlyWhenMarkedDown(t *testing.T) {
	p := NewPool([]string{"http://a:3020", "http://b:3020"}, []string{"a", "b"})
	p.MarkClassified("a", Classified{Kind: KindQuota, Cooldown: 0, Message: "quota", Failover: false})
	first, ok := p.Pick("", nil)
	if !ok || first.ID != "a" {
		t.Fatalf("quota should not take the account out of rotation, got %+v", first)
	}
	p.MarkClassified("a", Classified{Kind: KindRateLimit, Cooldown: time.Hour, Message: "429", Failover: true})
	next, ok := p.Pick("", nil)
	if !ok || next.ID != "b" {
		t.Fatalf("rate-limited a should be skipped, got %+v", next)
	}
}

// A numeric rotation cursor silently re-seats rotation when the candidate set
// shrinks (a retry excludes an account, or one enters cooldown), starving some
// accounts and hammering others. The cursor must resume from the previous
// pick's identity instead.
func TestRotationSurvivesShrinkingCandidateSet(t *testing.T) {
	p := NewPool([]string{"http://a:3020", "http://b:3020", "http://c:3020", "http://d:3020"},
		[]string{"a", "b", "c", "d"})
	picks := map[string]int{}
	for i := 0; i < 8; i++ {
		item, ok := p.Pick("", nil)
		if !ok {
			t.Fatalf("pick %d failed", i)
		}
		picks[item.ID]++
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		if picks[id] != 2 {
			t.Fatalf("even rotation expected 2 picks each, got %v", picks)
		}
	}

	// Take b out the way a retry exclusion or cooldown would. Rotation must
	// continue from c, not restart at a.
	excluded := map[string]struct{}{"b": {}}
	got := map[string]int{}
	for i := 0; i < 6; i++ {
		item, _ := p.Pick("", excluded)
		got[item.ID]++
	}
	for _, id := range []string{"a", "c", "d"} {
		if got[id] != 2 {
			t.Fatalf("b excluded: expected 2 picks each, got %v", got)
		}
	}
	if got["b"] != 0 {
		t.Fatalf("excluded b was picked: %v", got)
	}
}

func TestMaxInFlightCapsConcurrency(t *testing.T) {
	p := NewPool([]string{"http://a:3020", "http://b:3020"}, []string{"a", "b"})
	p.Upsert(Item{ID: "a", MaxInFlight: 1})
	p.Upsert(Item{ID: "b", MaxInFlight: 1})
	p.MergeHealth("a", true, false, 1, 0, "")
	p.MergeHealth("b", true, false, 0, 0, "")

	// a is saturated at its limit, so traffic must move to b.
	item, ok := p.Pick("", nil)
	if !ok || item.ID != "b" {
		t.Fatalf("saturated account must be skipped, got %+v", item)
	}
	// Both saturated: fall back to reporting the least-bad candidate.
	p.MergeHealth("b", true, false, 1, 0, "")
	if _, ok := p.Pick("", nil); !ok {
		t.Fatal("fully saturated pool must still surface a candidate")
	}
}

func TestMaxInFlightUnsetDoesNotBlock(t *testing.T) {
	p := NewPool([]string{"http://a:3020"}, []string{"a"})
	p.MergeHealth("a", true, false, 999, 0, "")
	if _, ok := p.Pick("", nil); !ok {
		t.Fatal("an unset limit must not block routing")
	}
}

func TestWeightedRotationIsProportional(t *testing.T) {
	p := NewPool([]string{"http://a:3020", "http://b:3020"}, []string{"a", "b"})
	p.Upsert(Item{ID: "a", Weight: 90})
	p.Upsert(Item{ID: "b", Weight: 10})
	counts := map[string]int{}
	for i := 0; i < 20; i++ {
		item, _ := p.Pick("", nil)
		counts[item.ID]++
	}
	if counts["a"] != 18 || counts["b"] != 2 {
		t.Fatalf("weight 90:10 over 20 picks = %v", counts)
	}
}

func TestUniformWeightsUsePlainRoundRobin(t *testing.T) {
	p := NewPool([]string{"http://a:3020", "http://b:3020"}, []string{"a", "b"})
	p.Upsert(Item{ID: "a", Weight: 50})
	p.Upsert(Item{ID: "b", Weight: 50})
	counts := map[string]int{}
	for i := 0; i < 8; i++ {
		item, _ := p.Pick("", nil)
		counts[item.ID]++
	}
	if counts["a"] != 4 || counts["b"] != 4 {
		t.Fatalf("uniform weights must stay even, got %v", counts)
	}
}

func TestModelCooldownLeavesOtherModelsAvailable(t *testing.T) {
	p := NewPool([]string{"http://a:3020"}, []string{"a"})
	p.MarkClassified("a", Classified{Kind: KindRateLimit, Cooldown: time.Hour, Failover: true, Model: "glm-5.3"})

	item, _ := p.PickRoute(RouteQuery{PublicModel: "glm-5.3"})
	if item.ID != "a" {
		t.Fatalf("all cooling: must still surface the account, got %+v", item)
	}
	// No candidate is down for a different model, so the account serves it.
	if item, ok := p.PickRoute(RouteQuery{PublicModel: "deepseek-v4-flash"}); !ok || item.ID != "a" {
		t.Fatalf("model-scoped cooldown must not block other models, got %+v ok=%v", item, ok)
	}
}

func TestAccountCooldownBlocksEveryModel(t *testing.T) {
	p := NewPool([]string{"http://a:3020", "http://b:3020"}, []string{"a", "b"})
	p.MarkClassified("a", Classified{Kind: KindUnavailable, Cooldown: time.Hour, Failover: true})
	counts := map[string]int{}
	for i := 0; i < 4; i++ {
		item, _ := p.PickRoute(RouteQuery{PublicModel: "glm-5.3"})
		counts[item.ID]++
	}
	if counts["a"] != 0 || counts["b"] != 4 {
		t.Fatalf("account-wide cooldown must block all models, got %v", counts)
	}
}

func TestRepeatedFailuresBackOff(t *testing.T) {
	p := NewPool([]string{"http://a:3020"}, []string{"a"})
	base := 30 * time.Second
	p.MarkClassified("a", Classified{Kind: KindRateLimit, Cooldown: base, Failover: true})
	first, _ := p.ByID("a")

	p.MarkClassified("a", Classified{Kind: KindRateLimit, Cooldown: base, Failover: true})
	second, _ := p.ByID("a")
	if !second.DownUntil.After(first.DownUntil) {
		t.Fatalf("repeat failure must extend the cooldown: %v then %v", first.DownUntil, second.DownUntil)
	}
	if second.BackoffLevel < first.BackoffLevel {
		t.Fatalf("backoff level must climb: %d then %d", first.BackoffLevel, second.BackoffLevel)
	}

	// Success clears the ladder.
	p.MarkOK("a")
	reset, _ := p.ByID("a")
	if reset.BackoffLevel != 0 || !reset.DownUntil.IsZero() {
		t.Fatalf("MarkOK must reset backoff: %+v", reset)
	}
}

func TestBackoffIsCapped(t *testing.T) {
	for level := 0; level < 40; level++ {
		d, next := nextBackoffCooldown(time.Minute, level)
		if d > backoffCeiling {
			t.Fatalf("level %d cooldown %v exceeds ceiling %v", level, d, backoffCeiling)
		}
		if next > backoffMaxLevel && next != level {
			t.Fatalf("level %d returned next %d past the cap", level, next)
		}
	}
}

func TestNormalizeWeight(t *testing.T) {
	cases := map[int]int{0: 50, 1: 1, 50: 50, 100: 100, 101: 50, -5: 50}
	for input, want := range cases {
		if got := NormalizeWeight(input); got != want {
			t.Fatalf("NormalizeWeight(%d)=%d want %d", input, got, want)
		}
	}
}
