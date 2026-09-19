package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/providers/qoder"
)

// Health/probe scheduling. Quota fetch failures must not flip Ready.
// Reads pool items and writes runtime/health via existing Merge/Set helpers.

func (m *Manager) RefreshAccount(ctx context.Context, id string, forceQuota bool) error {
	if m == nil || m.pool == nil {
		return fmt.Errorf("account manager not ready")
	}
	item, ok := m.pool.ByID(id)
	if !ok {
		return fmt.Errorf("account %s is not running", id)
	}
	return m.refreshOne(ctx, item, forceQuota)
}

// AccountView returns the same projection the accounts list builds, for one
func (m *Manager) RefreshAll(ctx context.Context, forceQuota bool) error {
	var joined error
	for _, item := range m.pool.Items() {
		if err := m.refreshOne(ctx, item, forceQuota); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}

func (m *Manager) refreshOne(ctx context.Context, item Item, forceQuota bool) error {
	if item.RuntimeState == "dead" {
		return nil
	}
	if item.Runtime == string(providers.RuntimeInProcess) || strings.TrimSpace(item.URL) == "" {
		return m.refreshInProcess(ctx, item)
	}
	client := qoder.WorkerClient{HTTP: m.httpClient}
	health, statusCode, err := client.Health(ctx, item.URL)
	if err != nil {
		if statusCode == 0 {
			ready := false
			hot := false
			m.pool.MergeHealth(item.ID, ready, hot, 0, item.Restarts, err.Error())
			_ = m.store.Observe(ctx, item.ID, "", "error", err.Error(), KindUnavailable)
			return fmt.Errorf("health account %s: %w", item.ID, err)
		}
		return fmt.Errorf("decode account %s health: %w", item.ID, err)
	}
	ready := statusCode < 300 && health.OK && health.Ready
	m.pool.MergeHealth(item.ID, ready, health.Hot, health.InFlight, item.Restarts, health.LastError)
	if ready || health.Hot {
		m.resetRestartBackoff(item.ID)
	}
	status := "login_required"
	if ready || health.Hot {
		status = "ready"
	} else if health.LastError != "" {
		status = "error"
	}
	if err := m.store.Observe(ctx, item.ID, health.UID, status, health.LastError, ""); err != nil {
		return err
	}
	// Fetch quota after the health/observe path so a quota endpoint outage
	// never changes readiness. A successful exhausted snapshot may still
	// keep the account out of request routing.
	if health.Hot || ready {
		m.fetchQuota(ctx, item.ID, item.URL, forceQuota)
		m.fetchAccountModels(ctx, item)
	}
	return nil
}

func (m *Manager) refreshInProcess(ctx context.Context, item Item) error {
	adapter, ok := m.providers.Get(item.Provider)
	if !ok || adapter.Prober == nil {
		// No prober registered: leave pool state alone and never hit ""+/health.
		return nil
	}
	health, err := adapter.Prober.Probe(ctx, item.ID)
	if err != nil {
		m.pool.MergeHealth(item.ID, false, false, 0, item.Restarts, err.Error())
		_ = m.store.Observe(ctx, item.ID, "", "error", err.Error(), KindUnavailable)
		return fmt.Errorf("probe account %s: %w", item.ID, err)
	}
	m.pool.MergeHealth(item.ID, health.Ready, health.Hot, health.InFlight, item.Restarts, health.LastError)
	if health.Ready || health.Hot {
		m.resetRestartBackoff(item.ID)
	}
	status := "login_required"
	if health.Ready || health.Hot {
		status = "ready"
	} else if health.LastError != "" {
		status = "error"
	}
	if err := m.store.Observe(ctx, item.ID, health.UID, status, health.LastError, ""); err != nil {
		return err
	}
	if health.Ready || health.Hot {
		go func(accountID string, prober providers.AccountProber) {
			quotaCtx, cancel := context.WithTimeout(m.runCtx, 5*time.Second)
			defer cancel()
			m.fetchProviderQuota(quotaCtx, accountID, prober)
		}(item.ID, adapter.Prober)
		m.fetchAccountModels(ctx, item)
	}
	return nil
}

func (m *Manager) forceReady(accountID string) bool {
	if m == nil || strings.TrimSpace(accountID) == "" || strings.TrimSpace(m.config.DataDir) == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(m.config.DataDir, "force-ready", accountID))
	return err == nil
}
