package runtime

import (
	"context"
	"log"
	"net/http"
	"time"

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
	}
	m.persistQuota(ctx, accountID, quota)
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
