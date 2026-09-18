package qoder

import "testing"

func TestWorkerQuotaSnapshotUsesResourcePackage(t *testing.T) {
	quota := (&workerQuota{
		UserQuota:          &workerQuotaBlock{Total: 100, Used: 100, Percentage: 100},
		OrgResourcePackage: &workerQuotaBlock{Total: 50, Used: 10, Remaining: 40, Unit: "credits"},
		IsQuotaExceeded:    true,
	}).snapshot()

	if quota == nil || quota.Exceeded || !quota.HasResourcePackage || quota.ResourcePackageRemaining != 40 {
		t.Fatalf("quota = %+v", quota)
	}
}

func TestWorkerQuotaSnapshotIgnoresUnavailableResourcePackage(t *testing.T) {
	available := false
	quota := (&workerQuota{
		UserQuota: &workerQuotaBlock{Total: 100, Used: 100, Percentage: 100},
		OrgResourcePackage: &workerQuotaBlock{
			Total: 50, Remaining: 40, Available: &available,
		},
		IsQuotaExceeded: true,
	}).snapshot()

	if quota == nil || !quota.Exceeded || quota.ResourcePackageAvailable == nil || *quota.ResourcePackageAvailable {
		t.Fatalf("quota = %+v", quota)
	}
}
