package runtime

import (
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func TestQuotaNeedsRevalidationCoversDevinChatQuotaCooldown(t *testing.T) {
	now := time.Now()
	cooling := Item{ID: "d1", Provider: "devin", LastKind: accounts.KindQuota, DownUntil: now.Add(48 * time.Hour)}
	if !quotaNeedsRevalidation(cooling, now) {
		t.Fatal("devin account cooled for quota must be revalidated even with a healthy snapshot")
	}
	expired := cooling
	expired.DownUntil = now.Add(-time.Minute)
	if quotaNeedsRevalidation(expired, now) {
		t.Fatal("expired cooldown needs no revalidation")
	}
	rate := cooling
	rate.LastKind = accounts.KindRateLimit
	if quotaNeedsRevalidation(rate, now) {
		t.Fatal("non-quota cooldowns are untouched")
	}
	other := cooling
	other.Provider = "workbuddy"
	if quotaNeedsRevalidation(other, now) {
		t.Fatal("only devin uses the chat-quota revalidation path")
	}
	exceeded := Item{ID: "x", Provider: "workbuddy", Quota: &QuotaSnapshot{Exceeded: true}}
	if !quotaNeedsRevalidation(exceeded, now) {
		t.Fatal("exceeded snapshots are still revalidated")
	}
}
