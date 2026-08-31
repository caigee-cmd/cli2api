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
	p.MarkOK("a", "")
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
	// Both saturated: MaxInFlight is the sole bottleneck, so the pool must
	// decline to dispatch (ok=false) rather than round-trip into a worker
	// 429. The executor maps this to a rate-limit Retry-After.
	p.MergeHealth("b", true, false, 1, 0, "")
	if _, ok := p.Pick("", nil); ok {
		t.Fatal("fully saturated pool must decline to dispatch, not surface a candidate")
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
	p.MarkOK("a", "")
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

// P1#1: a pinned account that is model-cooled (or saturated) must not be
// returned over the pin; the pool falls through to normal scheduling among
// the remaining eligible accounts, so the client's X-Qoder-Account can no
// longer defeat per-model cooldown or MaxInFlight.
func TestPreferAccountEscapesModelCooldown(t *testing.T) {
	p := NewPool([]string{"http://a:3020", "http://b:3020"}, []string{"a", "b"})
	p.MarkClassified("a", Classified{Kind: KindRateLimit, Cooldown: time.Hour, Failover: true, Model: "glm-5.3"})
	// Pin a, which is cooled for glm-5.3; must fall through to b.
	item, ok := p.PickRoute(RouteQuery{PreferAccount: "a", PublicModel: "glm-5.3"})
	if !ok || item.ID != "b" {
		t.Fatalf("pinned+model-cooled a must escape to b, got %+v ok=%v", item, ok)
	}
}

func TestPreferAccountEscapesSaturation(t *testing.T) {
	p := NewPool([]string{"http://a:3020", "http://b:3020"}, []string{"a", "b"})
	p.Upsert(Item{ID: "a", MaxInFlight: 1})
	p.Upsert(Item{ID: "b", MaxInFlight: 1})
	p.MergeHealth("a", true, false, 1, 0, "") // a saturated
	p.MergeHealth("b", true, false, 0, 0, "")
	// Pin a, which is at its concurrency cap; must fall through to b.
	item, ok := p.PickRoute(RouteQuery{PreferAccount: "a"})
	if !ok || item.ID != "b" {
		t.Fatalf("pinned+saturated a must escape to b, got %+v ok=%v", item, ok)
	}
}

// P1#2: a model-scoped success clears only that model's cooldown and
// backoff ladder; other models keep their cooldowns. A 200 on model-B must
// not un-cool a still-rate-limited model-A.
func TestMarkOKScopedToModelLeavesOthers(t *testing.T) {
	p := NewPool([]string{"http://a:3020"}, []string{"a"})
	p.MarkClassified("a", Classified{Kind: KindRateLimit, Cooldown: time.Hour, Failover: true, Model: "glm-5.3"})
	p.MarkClassified("a", Classified{Kind: KindRateLimit, Cooldown: time.Hour, Failover: true, Model: "deepseek-v4-flash"})
	// Success on deepseek-v4-flash only.
	p.MarkOK("a", "deepseek-v4-flash")
	item, _ := p.ByID("a")
	if _, ok := item.ModelDownUntil["deepseek-v4-flash"]; ok {
		t.Fatalf("deepseek-v4-flash cooldown must be cleared, got %+v", item.ModelDownUntil)
	}
	if _, ok := item.ModelDownUntil["glm-5.3"]; !ok {
		t.Fatalf("glm-5.3 cooldown must survive, got %+v", item.ModelDownUntil)
	}
	if _, ok := item.ModelBackoff["deepseek-v4-flash"]; ok {
		t.Fatalf("deepseek-v4-flash backoff must be cleared, got %+v", item.ModelBackoff)
	}
	if _, ok := item.ModelBackoff["glm-5.3"]; !ok {
		t.Fatalf("glm-5.3 backoff must survive, got %+v", item.ModelBackoff)
	}
}

// P1#3: backoff only climbs on a repeat of the same kind. A change in kind
// (rate_limit -> auth) must restart the ladder, not keep climbing.
func TestBackoffResetsOnKindChange(t *testing.T) {
	p := NewPool([]string{"http://a:3020"}, []string{"a"})
	base := 30 * time.Second
	p.MarkClassified("a", Classified{Kind: KindRateLimit, Cooldown: base, Failover: true})
	first, _ := p.ByID("a")
	// Different kind with the same base cooldown: ladder restarts, so the
	// cooldown must not exceed (and should equal) the base.
	p.MarkClassified("a", Classified{Kind: KindAuth, Cooldown: base, Failover: true})
	second, _ := p.ByID("a")
	if !second.DownUntil.After(first.DownUntil) {
		t.Fatalf("expected a fresh cooldown at base duration, first=%v second=%v", first.DownUntil, second.DownUntil)
	}
	// The new-kind cooldown must be ~base, not escalated. Allow slack for
	// the time between the two MarkClassified calls.
	got := time.Until(second.DownUntil)
	if got > base+5*time.Second {
		t.Fatalf("kind change must reset backoff, got cooldown %v > base %v", got, base)
	}
}

// P1#4: when every eligible account is only concurrency-saturated (no
// cooldown), PickRoute must decline to dispatch (ok=false). When any
// account is cooling, it surfaces the earliest-resume candidate with
// ok=true so the caller can report a retry-after.
func TestSaturatedOnlyReturnsFalseButCoolingReturnsCandidate(t *testing.T) {
	p := NewPool([]string{"http://a:3020", "http://b:3020"}, []string{"a", "b"})
	p.Upsert(Item{ID: "a", MaxInFlight: 1})
	p.Upsert(Item{ID: "b", MaxInFlight: 1})
	p.MergeHealth("a", true, false, 1, 0, "")
	p.MergeHealth("b", true, false, 1, 0, "")
	// Pure saturation: no cooldown anywhere -> decline.
	if _, ok := p.Pick("", nil); ok {
		t.Fatal("fully saturated pool must decline to dispatch")
	}
	// Now a is cooling instead of saturated; it must be surfaced.
	p.MergeHealth("a", true, false, 0, 0, "")
	p.MarkDown("a", time.Hour, "cooling")
	item, ok := p.Pick("", nil)
	if !ok || item.ID != "a" {
		t.Fatalf("cooling account must be surfaced, got %+v ok=%v", item, ok)
	}
}

// P1#1: the observer snapshot must deep-copy the mutable reference fields
// (ModelDownUntil, ModelBackoff, ModelLastKind, Models, Quota). A struct
// copy aliases the live pool maps, so the async persistence drainer racing
// a later MarkClassified would read partially-written maps. clone() detaches
// them.
func TestItemCloneIsDeep(t *testing.T) {
	p := NewPool([]string{"http://a:3020"}, []string{"a"})
	p.MarkClassified("a", Classified{Kind: KindRateLimit, Cooldown: time.Hour, Failover: true, Model: "glm-5.3"})
	var first Item
	once := 0
	p.SetObserver(func(item Item) {
		if once == 0 {
			first = item
			once = 1
		}
	})
	// Capture one snapshot with a 30m cooldown, then mutate the live item
	// with a different cooldown. The captured snapshot must keep the 30m
	// value, proving the map was deep-copied rather than aliased.
	p.MarkClassified("a", Classified{Kind: KindRateLimit, Cooldown: 30 * time.Minute, Failover: true, Model: "glm-5.3"})
	if first.ModelDownUntil == nil {
		t.Fatal("snapshot must capture the model cooldown")
	}
	snap := first.ModelDownUntil["glm-5.3"]
	// Mutating the live pool must not change the detached snapshot.
	p.MarkClassified("a", Classified{Kind: KindRateLimit, Cooldown: 15 * time.Minute, Failover: true, Model: "glm-5.3"})
	if !first.ModelDownUntil["glm-5.3"].Equal(snap) {
		t.Fatalf("clone must deep-copy ModelDownUntil: snapshot changed after live mutation to %v (was %v)",
			first.ModelDownUntil["glm-5.3"], snap)
	}
}

// P2#3: per-model last-kind prevents cross-model backoff confusion. The
// sequence rate_limit(model-A) -> auth(model-B) -> auth(model-A) must NOT
// escalate model-A's backoff: model-A's last kind was rate_limit, so the
// third failure (auth) is a kind change, restarting model-A's ladder at 0.
// A second auth(model-A) must THEN escalate, proving the ladder works.
func TestModelBackoffIndependentLastKind(t *testing.T) {
	p := NewPool([]string{"http://a:3020"}, []string{"a"})
	base := 30 * time.Second
	// 1. model-A rate_limit  (ladder level 0)
	p.MarkClassified("a", Classified{Kind: KindRateLimit, Cooldown: base, Failover: true, Model: "glm-5.3"})
	// 2. model-B auth        (must not touch model-A's ladder)
	p.MarkClassified("a", Classified{Kind: KindAuth, Cooldown: base, Failover: true, Model: "deepseek-v4-flash"})
	// 3. model-A auth        (kind CHANGE for model-A -> ladder restarts at 0)
	p.MarkClassified("a", Classified{Kind: KindAuth, Cooldown: base, Failover: true, Model: "glm-5.3"})
	item, _ := p.ByID("a")
	if got := item.ModelBackoff["glm-5.3"]; got != 0 {
		t.Fatalf("model-A backoff must restart to 0 on kind change, got %d", got)
	}
	// 4. model-A auth again  (now a repeat auth -> ladder climbs to 1)
	p.MarkClassified("a", Classified{Kind: KindAuth, Cooldown: base, Failover: true, Model: "glm-5.3"})
	item, _ = p.ByID("a")
	if got := item.ModelBackoff["glm-5.3"]; got != 1 {
		t.Fatalf("model-A backoff must climb to 1 on second auth, got %d", got)
	}
	// model-B's ladder is untouched by model-A's activity.
	if got := item.ModelBackoff["deepseek-v4-flash"]; got != 0 {
		t.Fatalf("model-B backoff must stay 0, got %d", got)
	}
}
