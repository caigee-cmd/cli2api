package runtime

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/providers/qoder"
)

// Quota snapshots: pool MergeQuota then Store.SaveQuota. Worker JSON is Qoder-only;
// in-process providers use AccountProber.Quota.

func (m *Manager) fetchProviderQuota(ctx context.Context, accountID string, prober providers.AccountProber) {
	if prober == nil {
		return
	}
	info, err := prober.Quota(ctx, accountID)
	if err != nil || info == nil {
		return
	}
	unit := info.Unit
	if unit == "" {
		unit = "credits"
	}
	quota := &QuotaSnapshot{
		Used:       info.Used,
		Total:      info.Total,
		Remaining:  info.Remaining,
		Percentage: info.Percentage,
		Unit:       unit,
		Exceeded:   info.Exceeded,
		FetchedAt:  info.FetchedAt,
		Windows:    quotaWindowsFromInfo(info.Windows),
	}
	// Package-expiry detail is currently only produced by WorkBuddy's
	// get-user-resource packs; keep other providers from leaking a stale or
	// fabricated expiry into the snapshot.
	if info.ProviderID == "workbuddy" {
		quota.ExpiresAt = info.ExpiresAt
		quota.ExpiringRemain = info.ExpiringRemain
		quota.Packages = quotaPackagesFromInfo(info.Packages)
	}
	m.persistQuota(ctx, accountID, quota)
}

func quotaPackagesFromInfo(packages []providers.QuotaPackage) []accounts.QuotaPackage {
	if len(packages) == 0 {
		return nil
	}
	out := make([]accounts.QuotaPackage, 0, len(packages))
	for _, pkg := range packages {
		unit := pkg.Unit
		if unit == "" {
			unit = "credits"
		}
		out = append(out, accounts.QuotaPackage{
			Remain:  pkg.Remain,
			Used:    pkg.Used,
			Size:    pkg.Size,
			Unit:    unit,
			EndsAt:  pkg.EndsAt,
			EndTime: pkg.EndTime,
		})
	}
	return out
}

func quotaWindowsFromInfo(windows []providers.QuotaWindow) []accounts.QuotaWindow {
	if len(windows) == 0 {
		return nil
	}
	out := make([]accounts.QuotaWindow, 0, len(windows))
	for _, window := range windows {
		unit := window.Unit
		if unit == "" {
			unit = "credits"
		}
		out = append(out, accounts.QuotaWindow{
			ID:         window.ID,
			Label:      window.Label,
			Used:       window.Used,
			Total:      window.Total,
			Remaining:  window.Remaining,
			Percentage: window.Percentage,
			Unit:       unit,
			ResetAt:    window.ResetAt,
			Exceeded:   window.Exceeded,
		})
	}
	return out
}

func (m *Manager) persistQuota(ctx context.Context, accountID string, quota *QuotaSnapshot) {
	if quota == nil {
		return
	}
	if m.forceReady(accountID) {
		// Local/dev override: keep the account routable even when upstream
		// still reports a hard zero balance. Marker file:
		//   $QODER_DATA_DIR/force-ready/<accountID>
		quota.Exceeded = false
		if quota.Remaining <= 0 {
			quota.Remaining = 1
		}
		if quota.Percentage >= 100 {
			quota.Percentage = 99
		}
	}
	m.pool.MergeQuota(accountID, quota)
	if err := m.store.SaveQuota(ctx, accountID, quota); err != nil {
		log.Printf("persist quota account=%s: %v", accountID, err)
	}
}

func (m *Manager) fetchQuota(ctx context.Context, accountID, workerURL string, force bool) {
	client := qoder.WorkerClient{
		HTTP:        &http.Client{Timeout: 5 * time.Second},
		ProxyAPIKey: m.ProxyAPIKey(),
	}
	quota, err := client.Quota(ctx, workerURL, force)
	if err != nil || quota == nil {
		return
	}
	m.persistQuota(ctx, accountID, quota)
}
