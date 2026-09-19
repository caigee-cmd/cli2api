package qoder

import "github.com/caigee-cmd/cli2api/internal/accounts"

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

func (w *workerQuotaBlock) hasRemaining() bool {
	return w != nil && w.Remaining > 0 && (w.Available == nil || *w.Available)
}

func (w *workerQuota) snapshot() *accounts.QuotaSnapshot {
	if w == nil || w.UserQuota == nil {
		return nil
	}
	snapshot := &accounts.QuotaSnapshot{
		Used:       w.UserQuota.Used,
		Total:      w.UserQuota.Total,
		Remaining:  w.UserQuota.Remaining,
		Percentage: w.UserQuota.Percentage,
		Unit:       w.UserQuota.Unit,
		Exceeded:   w.IsQuotaExceeded || w.UserQuota.Percentage >= 100,
		FetchedAt:  w.FetchedAt,
	}
	if snapshot.Unit == "" {
		snapshot.Unit = "credits"
	}
	if w.AddOnQuota != nil {
		snapshot.HasAddOn = true
		snapshot.AddOnUsed = w.AddOnQuota.Used
		snapshot.AddOnTotal = w.AddOnQuota.Total
		snapshot.AddOnRemaining = w.AddOnQuota.Remaining
		snapshot.AddOnUnit = w.AddOnQuota.Unit
		snapshot.AddOnAvailable = w.AddOnQuota.Available
		if snapshot.AddOnUnit == "" {
			snapshot.AddOnUnit = "credits"
		}
	}
	if w.OrgResourcePackage != nil {
		snapshot.HasResourcePackage = true
		snapshot.ResourcePackageUsed = w.OrgResourcePackage.Used
		snapshot.ResourcePackageTotal = w.OrgResourcePackage.Total
		snapshot.ResourcePackageRemaining = w.OrgResourcePackage.Remaining
		snapshot.ResourcePackageUnit = w.OrgResourcePackage.Unit
		snapshot.ResourcePackageAvailable = w.OrgResourcePackage.Available
		if snapshot.ResourcePackageUnit == "" {
			snapshot.ResourcePackageUnit = "credits"
		}
	}
	if snapshot.Exceeded && (w.AddOnQuota.hasRemaining() || w.OrgResourcePackage.hasRemaining()) {
		snapshot.Exceeded = false
	}
	return snapshot
}
