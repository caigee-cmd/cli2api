package qoder

import "testing"

// A plan with a monthly quota plus a check-in add-on must sum both buckets;
// upstream reports totalUsagePercentage over the same aggregate, and the CLI
// sums the buckets too. This is issue #229: the add-on reward never moved the
// base-only headline.
func TestWorkerQuotaSnapshotSumsBuckets(t *testing.T) {
	quota := (&workerQuota{
		UserQuota:       &workerQuotaBlock{Total: 300, Used: 139, Remaining: 161, Percentage: 47, Unit: "credits"},
		AddOnQuota:      &workerQuotaBlock{Total: 400, Used: 0, Remaining: 400, Percentage: 0, Unit: "credits"},
		IsQuotaExceeded: false,
		FetchedAt:       "now",
	}).snapshot()

	if quota == nil {
		t.Fatal("snapshot is nil")
	}
	if quota.Total != 700 || quota.Used != 139 || quota.Remaining != 561 {
		t.Fatalf("headline must sum buckets: %+v", quota)
	}
	// 139 / 700 = 19.857% -> matches upstream totalUsagePercentage ~= 20.
	if quota.Percentage < 19.8 || quota.Percentage > 19.9 {
		t.Fatalf("percentage = %v, want ~19.86", quota.Percentage)
	}
	if quota.Exceeded {
		t.Fatalf("account with add-on remaining must not be exceeded: %+v", quota)
	}
	if !quota.HasAddOn || quota.AddOnRemaining != 400 {
		t.Fatalf("add-on detail must be preserved: %+v", quota)
	}
}

// A balance-only account (no subscription) reports userQuota 0/0/0 and keeps
// its credits entirely in the add-on bucket; the headline must show them.
func TestWorkerQuotaSnapshotBalanceOnlyAccount(t *testing.T) {
	quota := (&workerQuota{
		UserQuota:  &workerQuotaBlock{Total: 0, Used: 0, Remaining: 0, Percentage: 0, Unit: "credits"},
		AddOnQuota: &workerQuotaBlock{Total: 400, Used: 0, Remaining: 400, Percentage: 1, Unit: "credits"},
	}).snapshot()

	if quota == nil || quota.Total != 400 || quota.Remaining != 400 || quota.Used != 0 {
		t.Fatalf("balance-only headline should follow the add-on: %+v", quota)
	}
}

// A consumed base quota with an available add-on keeps the account routable and
// shows the combined remainder.
func TestWorkerQuotaSnapshotBaseExhaustedAddOnAvailable(t *testing.T) {
	quota := (&workerQuota{
		UserQuota:       &workerQuotaBlock{Total: 100, Used: 100, Remaining: 0, Percentage: 100, Unit: "credits"},
		AddOnQuota:      &workerQuotaBlock{Total: 50, Used: 10, Remaining: 40, Unit: "credits"},
		IsQuotaExceeded: true,
	}).snapshot()

	if quota == nil || quota.Exceeded {
		t.Fatalf("add-on remaining must keep Exceeded false: %+v", quota)
	}
	if quota.Remaining != 40 || quota.Total != 150 || quota.Used != 110 {
		t.Fatalf("headline should be the combined buckets: %+v", quota)
	}
}

// An org resource package is included in the aggregate and can rescue an
// otherwise exhausted account.
func TestWorkerQuotaSnapshotIncludesResourcePackage(t *testing.T) {
	available := true
	quota := (&workerQuota{
		UserQuota:          &workerQuotaBlock{Total: 100, Used: 100, Remaining: 0, Percentage: 100},
		OrgResourcePackage: &workerQuotaBlock{Total: 50, Used: 10, Remaining: 40, Percentage: 20, Available: &available},
		IsQuotaExceeded:    true,
	}).snapshot()

	if quota == nil || quota.Exceeded || !quota.HasResourcePackage || quota.ResourcePackageRemaining != 40 {
		t.Fatalf("quota = %+v", quota)
	}
	if quota.Total != 150 || quota.Remaining != 40 {
		t.Fatalf("resource package must join the aggregate: %+v", quota)
	}
}

// An unavailable resource package is excluded from the aggregate and cannot
// rescue an exhausted account.
func TestWorkerQuotaSnapshotIgnoresUnavailableResourcePackage(t *testing.T) {
	available := false
	quota := (&workerQuota{
		UserQuota: &workerQuotaBlock{Total: 100, Used: 100, Remaining: 0, Percentage: 100},
		OrgResourcePackage: &workerQuotaBlock{
			Total: 50, Remaining: 40, Available: &available,
		},
		IsQuotaExceeded: true,
	}).snapshot()

	if quota == nil || !quota.Exceeded || quota.ResourcePackageAvailable == nil || *quota.ResourcePackageAvailable {
		t.Fatalf("quota = %+v", quota)
	}
	if quota.Total != 100 || quota.Remaining != 0 {
		t.Fatalf("unavailable bucket must not count: %+v", quota)
	}
}

// No bucket reported at all is "unknown", never a fabricated zero.
func TestWorkerQuotaSnapshotNoBucketsIsNil(t *testing.T) {
	if quota := (&workerQuota{}).snapshot(); quota != nil {
		t.Fatalf("quota = %+v, want nil", quota)
	}
}
