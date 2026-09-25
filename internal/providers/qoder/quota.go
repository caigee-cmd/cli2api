package qoder

import (
	"math"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

type workerQuota struct {
	UserQuota          *workerQuotaBlock `json:"userQuota"`
	AddOnQuota         *workerQuotaBlock `json:"addOnQuota"`
	OrgResourcePackage *workerQuotaBlock `json:"orgResourcePackage"`
	IsQuotaExceeded    bool              `json:"isQuotaExceeded"`
	FetchedAt          string            `json:"fetchedAt"`
}

type workerQuotaBlock struct {
	Total      float64 `json:"total"`
	Used       float64 `json:"used"`
	Remaining  float64 `json:"remaining"`
	Percentage float64 `json:"percentage"`
	Unit       string  `json:"unit"`
	Available  *bool   `json:"available"`
}

// available reports whether the bucket may be spent. A nil Available means the
// upstream did not flag it unavailable.
func (w *workerQuotaBlock) available() bool {
	return w != nil && (w.Available == nil || *w.Available)
}

func (w *workerQuotaBlock) hasRemaining() bool {
	return w.available() && w.Remaining > 0
}

// snapshot projects the upstream quota onto the console/routing model.
//
// Qoder reports credits in up to three buckets: the plan's monthly user quota,
// the add-on pack, and an organization resource package. The upstream CLI sums
// them (its remaining = userQuota + addOnQuota + orgResourcePackage) and its
// totalUsagePercentage is the same aggregate ratio, so the headline must be the
// sum of the available buckets. Daily check-in rewards land in the add-on
// bucket, which is exactly why a base-only headline never moved after a
// check-in (issue #229). Each bucket's detail fields are still reported.
func (w *workerQuota) snapshot() *accounts.QuotaSnapshot {
	if w == nil {
		return nil
	}
	if w.UserQuota == nil && w.AddOnQuota == nil && w.OrgResourcePackage == nil {
		// No bucket reported at all: unknown, not a fabricated zero.
		return nil
	}

	var used, total, remaining float64
	for _, bucket := range []*workerQuotaBlock{w.UserQuota, w.AddOnQuota, w.OrgResourcePackage} {
		if !bucket.available() {
			continue
		}
		used += bucket.Used
		total += bucket.Total
		remaining += bucket.Remaining
	}
	percentage := 0.0
	if total > 0 {
		percentage = math.Min(used/total*100, 100)
	}

	snapshot := &accounts.QuotaSnapshot{
		Used:       used,
		Total:      total,
		Remaining:  remaining,
		Percentage: percentage,
		Unit:       headlineUnit(w),
		FetchedAt:  w.FetchedAt,
	}
	// Exceeded keeps its previous meaning: the account is out of credits unless
	// an available add-on or organization package still holds some.
	exceeded := w.IsQuotaExceeded || (w.UserQuota.available() && w.UserQuota.Percentage >= 100)
	if exceeded && (w.AddOnQuota.hasRemaining() || w.OrgResourcePackage.hasRemaining()) {
		exceeded = false
	}
	snapshot.Exceeded = exceeded

	if w.AddOnQuota != nil {
		snapshot.HasAddOn = true
		snapshot.AddOnUsed = w.AddOnQuota.Used
		snapshot.AddOnTotal = w.AddOnQuota.Total
		snapshot.AddOnRemaining = w.AddOnQuota.Remaining
		snapshot.AddOnUnit = blockUnit(w.AddOnQuota)
		snapshot.AddOnAvailable = w.AddOnQuota.Available
	}
	if w.OrgResourcePackage != nil {
		snapshot.HasResourcePackage = true
		snapshot.ResourcePackageUsed = w.OrgResourcePackage.Used
		snapshot.ResourcePackageTotal = w.OrgResourcePackage.Total
		snapshot.ResourcePackageRemaining = w.OrgResourcePackage.Remaining
		snapshot.ResourcePackageUnit = blockUnit(w.OrgResourcePackage)
		snapshot.ResourcePackageAvailable = w.OrgResourcePackage.Available
	}
	return snapshot
}

func blockUnit(block *workerQuotaBlock) string {
	if block == nil || block.Unit == "" {
		return "credits"
	}
	return block.Unit
}

// headlineUnit picks the unit for the aggregate from the first available
// bucket; every Qoder bucket is credit-denominated, so this only guards against
// an empty upstream value.
func headlineUnit(w *workerQuota) string {
	for _, bucket := range []*workerQuotaBlock{w.UserQuota, w.AddOnQuota, w.OrgResourcePackage} {
		if bucket.available() && bucket.Unit != "" {
			return bucket.Unit
		}
	}
	return "credits"
}
