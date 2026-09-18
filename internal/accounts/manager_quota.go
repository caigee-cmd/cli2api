package accounts

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
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
	path := strings.TrimRight(workerURL, "/") + "/admin/quota"
	if force {
		path += "?refresh=1"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return
	}
	if m.config.ProxyAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+m.config.ProxyAPIKey)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return
	}
	var payload struct {
		Quota *workerQuota `json:"quota"`
	}
	if json.NewDecoder(resp.Body).Decode(&payload) != nil || payload.Quota == nil {
		return
	}
	quota := payload.Quota.snapshot()
	m.persistQuota(ctx, accountID, quota)
}

// workerQuota mirrors the daemon /admin/quota response shape.
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

func (w *workerQuota) snapshot() *QuotaSnapshot {
	if w == nil || w.UserQuota == nil {
		return nil
	}
	snapshot := &QuotaSnapshot{
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
