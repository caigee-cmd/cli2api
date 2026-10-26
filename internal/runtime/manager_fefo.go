package runtime

import (
	"context"
	"time"
)

// RefreshFEFOQuotaAfterUse invalidates a trusted credit balance after the whole
// response (including an SSE body) finishes. It never waits on upstream quota in
// the request path. Concurrent completions share one fetch; a completion during
// that fetch requests one trailing fetch so its consumption is not lost.
func (m *Manager) RefreshFEFOQuotaAfterUse(accountID, publicModel string) {
	if m == nil || m.pool == nil || m.runCtx == nil || !m.pool.HasFEFOQuota(accountID, publicModel) {
		return
	}
	m.mu.Lock()
	if m.runCtx.Err() != nil {
		m.mu.Unlock()
		return
	}
	if m.quotaAfterUse == nil {
		m.quotaAfterUse = map[string]bool{}
	}
	if _, running := m.quotaAfterUse[accountID]; running {
		m.quotaAfterUse[accountID] = true
		m.mu.Unlock()
		return
	}
	m.quotaAfterUse[accountID] = false
	m.quotaAfterUseDone.Add(1)
	m.mu.Unlock()

	go func() {
		defer m.quotaAfterUseDone.Done()
		for {
			if item, ok := m.pool.ByID(accountID); ok && m.pool.HasFEFOQuota(accountID, publicModel) {
				ctx, cancel := context.WithTimeout(m.runCtx, 10*time.Second)
				m.refreshQuotaOnly(ctx, item)
				cancel()
			}
			m.mu.Lock()
			if !m.quotaAfterUse[accountID] || m.runCtx.Err() != nil {
				delete(m.quotaAfterUse, accountID)
				m.mu.Unlock()
				return
			}
			m.quotaAfterUse[accountID] = false
			m.mu.Unlock()
		}
	}()
}
