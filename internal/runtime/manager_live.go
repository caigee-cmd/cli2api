package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

// liveReadCoalesce bounds how often a console page load may force a full
// upstream read of every account. A reload inside the window reuses the values
// read moments ago instead of probing upstream again — the console would
// otherwise fan out one quota call per account on every tab switch.
const liveReadCoalesce = 15 * time.Second

// LiveAccounts performs one synchronous upstream read of every routable
// account (health, catalog, quota) and returns the projection built from that
// read. Any account whose read failed loses its quota number: the view carries
// QuotaError instead, so the console shows the upstream failure rather than a
// stale snapshot. A read that started inside liveReadCoalesce is reused as-is.
func (m *Manager) LiveAccounts(ctx context.Context, includeDisabled bool) ([]AccountView, error) {
	if m == nil || m.pool == nil {
		return nil, fmt.Errorf("account manager not ready")
	}
	if !m.reserveLiveRead() {
		return m.Accounts(ctx)
	}
	failures := make(map[string]string, m.pool.Len())
	results := m.RefreshAllResults(ctx, true, includeDisabled)
	for _, result := range results {
		if result.OK || strings.TrimSpace(result.AccountID) == "" {
			continue
		}
		message := strings.TrimSpace(result.Message)
		if message == "" {
			message = liveReadFailureMessage(result)
		}
		failures[result.AccountID] = message
	}
	views, err := m.Accounts(ctx)
	if err != nil {
		return nil, err
	}
	return applyLiveReadOutcome(views, failures), nil
}

// applyLiveReadOutcome drops the stored quota of any account whose live read
// failed. A number that was not read this round must not be presented as the
// current balance; the upstream failure takes its place.
func applyLiveReadOutcome(views []AccountView, failures map[string]string) []AccountView {
	for i := range views {
		message, failed := failures[views[i].ID]
		if !failed {
			continue
		}
		views[i].Quota = nil
		views[i].QuotaError = message
	}
	return views
}

// reserveLiveRead reports whether this caller owns the next live read. A read
// outside the coalescing window starts immediately and stamps the clock; a
// concurrent or too-recent caller does not probe upstream again.
func (m *Manager) reserveLiveRead() bool {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.lastLiveReadAt.IsZero() && now.Sub(m.lastLiveReadAt) < liveReadCoalesce {
		return false
	}
	// Stamp before probing so a burst of concurrent page loads coalesces onto
	// the first read instead of each starting its own fan-out.
	m.lastLiveReadAt = now
	return true
}

// liveReadFailureMessage renders the failure when a provider returned no
// verbatim text (for example a bare readiness miss).
func liveReadFailureMessage(result accounts.AccountRefreshResult) string {
	if code := strings.TrimSpace(result.Code); code != "" {
		return code
	}
	if kind := strings.TrimSpace(result.Kind); kind != "" {
		return kind
	}
	return "上游读取失败"
}
