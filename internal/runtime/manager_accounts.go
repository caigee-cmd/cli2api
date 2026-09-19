package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
	proxyutil "github.com/caigee-cmd/cli2api/internal/proxy"
)

// Process start/stop/sync helpers used by control.Accounts. Persistence and
// enable/import order live in control; these methods only mutate processes and
// the live pool. They do not own Manager.mu, persist channels, or SQLite writes
// except the console view reads.

func (m *Manager) StartAccount(ctx context.Context, account Account) error {
	return m.startAccountWithRecovery(ctx, account)
}

func (m *Manager) StopAccount(id string) error {
	return m.stopAccount(id)
}

func (m *Manager) RemoveAccount(id string) error {
	m.mu.Lock()
	delete(m.restarts, id)
	delete(m.restartBackoff, id)
	m.mu.Unlock()
	runtimeDir := filepath.Join(m.config.DataDir, "runtime", id)
	if err := os.RemoveAll(runtimeDir); err != nil {
		return fmt.Errorf("remove account runtime: %w", err)
	}
	return nil
}

func (m *Manager) SyncAccount(ctx context.Context, before, after Account) error {
	// Request sanitization applies per request, so sync it into the pool
	// without restarting anything.
	if before.DropSystemPrompt != after.DropSystemPrompt {
		m.pool.SetDropSystemPrompt(after.ID, after.DropSystemPrompt)
	}
	if before.Priority != after.Priority {
		m.pool.SetWeight(after.ID, after.Priority)
	}
	if before.Enabled && !after.Enabled {
		return m.stopAccount(after.ID)
	}
	if !before.Enabled && after.Enabled {
		return m.startAccountWithRecovery(ctx, after)
	}
	if before.Enabled && after.Enabled && before.ProxyURL != after.ProxyURL {
		descriptor, _, resolveErr := providers.Resolve(after.Provider, after.ProviderRegion)
		if resolveErr == nil && descriptor.Runtime == providers.RuntimeChildProcess {
			if err := m.stopAccount(after.ID); err != nil {
				return err
			}
			return m.startAccountWithRecovery(ctx, after)
		}
	}
	if before.Enabled && after.Enabled && before.MaxInFlight != after.MaxInFlight {
		if err := m.stopAccount(after.ID); err != nil {
			return err
		}
		return m.startAccountWithRecovery(ctx, after)
	}
	return nil
}

func (m *Manager) Create(ctx context.Context, input CreateAccount) (Account, error) {
	account, err := m.store.Create(ctx, input)
	if err != nil {
		return Account{}, err
	}
	if account.Enabled {
		if err := m.startAccountWithRecovery(ctx, account); err != nil {
			return account, err
		}
	}
	return account, nil
}

func (m *Manager) Update(ctx context.Context, id string, input UpdateAccount) error {
	before, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := m.store.Update(ctx, id, input); err != nil {
		return err
	}
	after, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}
	return m.SyncAccount(ctx, before, after)
}

func (m *Manager) Delete(ctx context.Context, id string) error {
	if err := m.stopAccount(id); err != nil {
		return err
	}
	if err := m.store.Delete(ctx, id); err != nil {
		return err
	}
	return m.RemoveAccount(id)
}

func (m *Manager) Import(ctx context.Context, input ImportAccount) (Account, error) {
	account, err := m.store.Create(ctx, CreateAccount{
		AutoCheckin: input.AutoCheckin, CheckinTime: input.CheckinTime,
		Name: input.Name, Provider: input.Provider, Region: input.Region, Enabled: false,
		MaxInFlight: input.MaxInFlight, Priority: input.Priority, DropSystemPrompt: input.DropSystemPrompt,
		WorkBuddyAutoCheckin: input.WorkBuddyAutoCheckin,
		WorkBuddyCheckinTime: input.WorkBuddyCheckinTime, ProxyURL: input.ProxyURL,
	})
	if err != nil {
		return Account{}, err
	}
	if err := m.store.SaveCredential(ctx, account.ID, "native", input.Credential); err != nil {
		_ = m.store.Delete(ctx, account.ID)
		return Account{}, err
	}
	if input.Enabled {
		enabled := true
		if err := m.store.Update(ctx, account.ID, UpdateAccount{Enabled: &enabled}); err != nil {
			return Account{}, err
		}
		account, err = m.store.Get(ctx, account.ID)
		if err != nil {
			return Account{}, err
		}
		if err := m.startAccountWithRecovery(ctx, account); err != nil {
			return account, err
		}
	}
	return m.store.Get(ctx, account.ID)
}

// RefreshAccount re-probes one account's health, quota, and model catalog.
// forceQuota bypasses the worker's own quota cache, matching the console's
// "Refresh credits" button. It refreshes only the requested account, so a
func (m *Manager) AccountView(ctx context.Context, id string) (AccountView, error) {
	if m == nil || m.store == nil {
		return AccountView{}, fmt.Errorf("account manager not ready")
	}
	account, err := m.store.Get(ctx, id)
	if err != nil {
		return AccountView{}, err
	}
	view := AccountView{Account: account, Quota: account.Quota, ProxyURL: proxyutil.Redact(account.ProxyURL)}
	if item, ok := m.pool.ByID(account.ID); ok {
		view.Ready = item.Ready == nil || *item.Ready
		view.Hot = item.Hot != nil && *item.Hot
		view.InFlight = item.InFlight
		view.Restarts = item.Restarts
		view.RuntimeState = item.RuntimeState
		view.RestartBackoffLevel = item.RestartBackoffLevel
		if !item.NextRestartAt.IsZero() && time.Now().Before(item.NextRestartAt) {
			view.NextRestartAt = item.NextRestartAt.UTC().Format(time.RFC3339)
		}
		if item.Quota != nil {
			view.Quota = item.Quota
		}
		view.ModelCooldowns = activeModelCooldowns(item.ModelDownUntil)
		if !item.DownUntil.IsZero() && time.Now().Before(item.DownUntil) {
			view.DownUntil = item.DownUntil.UTC().Format(time.RFC3339)
		}
		if view.LastError == "" {
			view.LastError = item.LastError
		}
		if view.LastErrorKind == "" {
			view.LastErrorKind = item.LastKind
		}
	}
	return view, nil
}

func (m *Manager) Accounts(ctx context.Context) ([]AccountView, error) {
	stored, err := m.store.List(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]AccountView, 0, len(stored))
	for _, account := range stored {
		view := AccountView{Account: account, Quota: account.Quota, ProxyURL: proxyutil.Redact(account.ProxyURL)}
		if item, ok := m.pool.ByID(account.ID); ok {
			view.Ready = item.Ready == nil || *item.Ready
			view.Hot = item.Hot != nil && *item.Hot
			view.InFlight = item.InFlight
			view.Restarts = item.Restarts
			view.RuntimeState = item.RuntimeState
			view.RestartBackoffLevel = item.RestartBackoffLevel
			if !item.NextRestartAt.IsZero() && time.Now().Before(item.NextRestartAt) {
				view.NextRestartAt = item.NextRestartAt.UTC().Format(time.RFC3339)
			}
			view.Quota = item.Quota
			view.ModelCooldowns = activeModelCooldowns(item.ModelDownUntil)
			if !item.DownUntil.IsZero() && time.Now().Before(item.DownUntil) {
				view.DownUntil = item.DownUntil.UTC().Format(time.RFC3339)
			}
			if view.LastError == "" {
				view.LastError = item.LastError
			}
			if view.LastErrorKind == "" {
				view.LastErrorKind = item.LastKind
			}
		}
		views = append(views, view)
	}
	return views, nil
}

func activeModelCooldowns(cooldowns map[string]time.Time) map[string]string {
	if len(cooldowns) == 0 {
		return nil
	}
	now := time.Now()
	active := make(map[string]string, len(cooldowns))
	for model, until := range cooldowns {
		if now.Before(until) {
			active[model] = until.UTC().Format(time.RFC3339)
		}
	}
	if len(active) == 0 {
		return nil
	}
	return active
}
