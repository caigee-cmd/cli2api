package qoder

import (
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers/qoder/qauth"
)

func quotaBlock(total, used, remaining float64) *qauth.Quota {
	return &qauth.Quota{Total: total, Used: used, Remaining: remaining, Unit: "credits"}
}

func TestBridgeQuotaSnapshotSumsBuckets(t *testing.T) {
	b := NewBridge("pat", qauth.CN, "mid", "mtok", "mac")
	b.account = &qauth.AccountStatus{
		Plan:      "PLAN_TIER_FREE",
		UserQuota: quotaBlock(400, 400, 0),
		AddOnQuota: &qauth.Quota{
			Total: 3000, Used: 1501, Remaining: 1499, Unit: "credits",
		},
	}
	snap := b.QuotaSnapshot()
	if snap == nil {
		t.Fatal("QuotaSnapshot = nil")
	}
	if snap.Remaining != 1499 || snap.Total != 3400 || snap.Used != 1901 {
		t.Fatalf("headline = %v/%v (used %v), want 1499/3400 (used 1901)", snap.Remaining, snap.Total, snap.Used)
	}
	if !snap.HasAddOn || snap.AddOnRemaining != 1499 {
		t.Fatalf("add-on detail = %+v", snap)
	}
	if snap.Exceeded {
		t.Fatal("plan exhausted but add-on available must not report Exceeded")
	}
}

func TestBridgeQuotaSnapshotIncludesResourcePackage(t *testing.T) {
	b := NewBridge("pat", qauth.CN, "mid", "mtok", "mac")
	b.account = &qauth.AccountStatus{
		Plan:       "PLAN_TIER_PRO_TRIAL",
		UserQuota:  quotaBlock(300, 0, 300),
		AddOnQuota: quotaBlock(300, 0, 300),
		OrgResourcePackage: &qauth.OrgResourcePackage{
			Cap: 500, Used: 100, Remaining: 400, Available: true, Unit: "credits",
		},
	}
	snap := b.QuotaSnapshot()
	if snap == nil {
		t.Fatal("QuotaSnapshot = nil")
	}
	if snap.Remaining != 1000 || snap.Total != 1100 {
		t.Fatalf("headline = %v/%v, want 1000/1100", snap.Remaining, snap.Total)
	}
	if !snap.HasResourcePackage || snap.ResourcePackageRemaining != 400 {
		t.Fatalf("resource package detail = %+v", snap)
	}
}

func TestBridgeQuotaSnapshotExhaustedStaysExceeded(t *testing.T) {
	b := NewBridge("pat", qauth.CN, "mid", "mtok", "mac")
	b.account = &qauth.AccountStatus{
		IsQuotaExceeded: true,
		UserQuota:       quotaBlock(400, 400, 0),
	}
	snap := b.QuotaSnapshot()
	if snap == nil {
		t.Fatal("QuotaSnapshot = nil")
	}
	if !snap.Exceeded {
		t.Fatal("no add-on and no resource package must keep Exceeded true")
	}
}

func TestBridgeQuotaSnapshotNilAddOnKeepsDetailFields(t *testing.T) {
	b := NewBridge("pat", qauth.CN, "mid", "mtok", "mac")
	b.account = &qauth.AccountStatus{
		Plan:      "PLAN_TIER_FREE",
		UserQuota: quotaBlock(300, 0, 300),
	}
	snap := b.QuotaSnapshot()
	if snap == nil {
		t.Fatal("QuotaSnapshot = nil")
	}
	if snap.Remaining != 300 || snap.Total != 300 {
		t.Fatalf("headline = %v/%v, want 300/300", snap.Remaining, snap.Total)
	}
	if snap.HasAddOn || snap.HasResourcePackage {
		t.Fatalf("absent buckets must not be fabricated: %+v", snap)
	}
	// No successful quota fetch has happened, so FetchedAt must be empty rather
	// than a fabricated "now" that hides a stale balance.
	if snap.FetchedAt != "" {
		t.Fatalf("FetchedAt = %q, want empty before any upstream fetch", snap.FetchedAt)
	}
}

// A snapshot served from cache must keep the real last-fetch time rather than
// restamping every read as "now", which made a stale balance look fresh.
func TestBridgeQuotaSnapshotFetchedAtIsRealTime(t *testing.T) {
	b := NewBridge("pat", qauth.CN, "mid", "mtok", "mac")
	b.account = &qauth.AccountStatus{UserQuota: quotaBlock(100, 10, 90)}
	// Simulate a successful fetch that happened a while ago.
	past := time.Now().Add(-45 * time.Minute).UTC().Truncate(time.Second)
	b.quotaFetchedAt = past
	snap := b.QuotaSnapshot()
	if snap == nil {
		t.Fatal("QuotaSnapshot = nil")
	}
	if snap.FetchedAt != past.Format(time.RFC3339) {
		t.Fatalf("FetchedAt = %q, want the real fetch time %q", snap.FetchedAt, past.Format(time.RFC3339))
	}
}

// An unavailable org package must not inflate the headline (worker-path parity).
func TestBridgeQuotaSnapshotSkipsUnavailableResourcePackage(t *testing.T) {
	b := NewBridge("pat", qauth.CN, "mid", "mtok", "mac")
	b.account = &qauth.AccountStatus{
		UserQuota: quotaBlock(300, 100, 200),
		OrgResourcePackage: &qauth.OrgResourcePackage{
			Cap: 500, Used: 0, Remaining: 500, Available: false, Unit: "credits",
		},
	}
	snap := b.QuotaSnapshot()
	if snap == nil {
		t.Fatal("QuotaSnapshot = nil")
	}
	if snap.Remaining != 200 || snap.Total != 300 {
		t.Fatalf("headline = %v/%v, want 200/300 (unavailable package excluded)", snap.Remaining, snap.Total)
	}
	if !snap.HasResourcePackage || snap.ResourcePackageAvailable == nil || *snap.ResourcePackageAvailable {
		t.Fatalf("resource package detail must report unavailable: %+v", snap)
	}
}
