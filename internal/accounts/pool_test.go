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
