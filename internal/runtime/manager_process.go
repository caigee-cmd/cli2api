package runtime

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/providers/qoder"
)

// Child-process lifecycle. HOME/CLI/daemon spawn live in providers/qoder;
// this file owns process tables, restart, and pool upsert.

func (m *Manager) ReplaceProxyAPIKey(ctx context.Context, key string) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	m.config.ProxyAPIKey = key
	accounts := make([]Account, 0, len(m.processes))
	for id := range m.processes {
		account, err := m.store.Get(ctx, id)
		if err != nil {
			m.mu.Unlock()
			return err
		}
		if account.Enabled {
			accounts = append(accounts, account)
		}
	}
	m.mu.Unlock()
	// Push to the starter outside the manager lock; the default starter is the
	// pointer *ExecStarter, so assert the behavior interface rather than the
	// concrete (and never-matching) value type.
	if starter, ok := m.starter.(APIKeyConfigurableStarter); ok {
		starter.SetProxyAPIKey(key)
	}
	for _, account := range accounts {
		if err := m.stopAccount(account.ID); err != nil {
			return err
		}
		if err := m.startAccountWithRecovery(ctx, account); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) startAccount(ctx context.Context, account Account) error {
	descriptor, _, err := providers.Resolve(account.Provider, account.ProviderRegion)
	if err != nil {
		return err
	}
	if descriptor.Runtime == providers.RuntimeInProcess {
		if m.runCtx.Err() != nil {
			return errManagerClosed
		}
		m.pool.Upsert(Item{
			ID: account.ID, Provider: descriptor.ID, Region: account.ProviderRegion,
			Runtime: string(descriptor.Runtime), DropSystemPrompt: account.DropSystemPrompt,
			Weight: NormalizeWeight(account.Priority), MaxInFlight: account.MaxInFlight, Quota: account.Quota,
			ModelAliases: account.ModelAliases, EnabledModels: account.EnabledModels, ModelsRestricted: account.ModelsRestricted,
			RuntimeState: "starting",
		})
		return nil
	}
	// Qoder PAT-backed accounts run fully in-process (chat signing, login,
	// catalog, quota, checkin all live in internal/providers/qoder via the
	// qauth Bridge). Skip the per-account Node+WASM worker spawn entirely:
	// it was the unstable, resource-heavy path this account model replaces.
	// Accounts still carrying the legacy qoder-native-v1 CLI blob (no PAT)
	// fall through to the worker spawn so they keep working until the
	// operator re-logs them with a PAT.
	if descriptor.ID == "qoder" && m.accountHasPAT(ctx, account.ID) {
		log.Printf("[qoder-pat] account %s: in-process path (PAT stored), skipping worker spawn", account.ID)
		if m.runCtx.Err() != nil {
			return errManagerClosed
		}
		ready := true
		m.pool.Upsert(Item{
			ID: account.ID, Provider: descriptor.ID, Region: account.ProviderRegion,
			Runtime: string(providers.RuntimeInProcess), DropSystemPrompt: account.DropSystemPrompt,
			Weight: NormalizeWeight(account.Priority), MaxInFlight: account.MaxInFlight, Quota: account.Quota,
			ModelAliases: account.ModelAliases, EnabledModels: account.EnabledModels, ModelsRestricted: account.ModelsRestricted,
			Ready: &ready, RuntimeState: "ready",
		})
		return nil
	}
	m.mu.Lock()
	if m.runCtx.Err() != nil {
		m.mu.Unlock()
		return errManagerClosed
	}
	if _, exists := m.processes[account.ID]; exists {
		m.mu.Unlock()
		return nil
	}
	port := m.nextPort
	m.nextPort++
	m.mu.Unlock()
	notReady := false
	m.pool.Upsert(Item{
		ID: account.ID, Provider: descriptor.ID, Region: account.ProviderRegion,
		Runtime: string(descriptor.Runtime),
		Weight:  NormalizeWeight(account.Priority), MaxInFlight: account.MaxInFlight, Quota: account.Quota,
		ModelAliases: account.ModelAliases, EnabledModels: account.EnabledModels, ModelsRestricted: account.ModelsRestricted,
		Ready: &notReady, RuntimeState: "starting",
	})

	home := filepath.Join(m.config.DataDir, "runtime", account.ID)
	if err := qoder.MaterializeHome(ctx, m.store, account, home); err != nil {
		return err
	}
	process, err := m.starter.Start(ctx, account, home, port)
	if err != nil {
		return fmt.Errorf("start account %s: %w", account.ID, err)
	}
	m.mu.Lock()
	if m.runCtx.Err() != nil {
		m.mu.Unlock()
		_ = process.Stop()
		return errManagerClosed
	}
	if _, exists := m.processes[account.ID]; exists {
		m.mu.Unlock()
		_ = process.Stop()
		return nil
	}
	m.processes[account.ID] = process
	restarts := m.restarts[account.ID]
	m.mu.Unlock()
	m.pool.Upsert(Item{
		ID: account.ID, URL: process.URL(), Provider: descriptor.ID,
		Region: account.ProviderRegion, Runtime: string(descriptor.Runtime), Restarts: restarts,
		Weight: NormalizeWeight(account.Priority), MaxInFlight: account.MaxInFlight, Quota: account.Quota,
		ModelAliases: account.ModelAliases, EnabledModels: account.EnabledModels, ModelsRestricted: account.ModelsRestricted,
		Ready: &notReady, RuntimeState: "starting",
	})
	go m.watchAccount(account.ID, process)
	return nil
}

type ExecStarter struct {
	mu sync.Mutex
	// Config is initial configuration; after first use access it through methods.
	Config ManagerConfig
	inner  *qoder.Starter
}

func NewExecStarter(config ManagerConfig) *ExecStarter {
	return &ExecStarter{Config: config, inner: &qoder.Starter{Config: starterConfig(config)}}
}

// ensureInnerLocked is called with s.mu held.
func (s *ExecStarter) ensureInnerLocked() *qoder.Starter {
	if s.inner == nil {
		s.inner = &qoder.Starter{Config: starterConfig(s.Config)}
	}
	return s.inner
}

func starterConfig(config ManagerConfig) qoder.StarterConfig {
	return qoder.StarterConfig{
		NodeBinary:     config.NodeBinary,
		DaemonPath:     config.DaemonPath,
		QoderCLIPath:   config.QoderCLIPath,
		QoderCNCLIPath: config.QoderCNCLIPath,
		TemplatePath:   config.TemplatePath,
		ProxyAPIKey:    config.ProxyAPIKey,
		ProxyURL:       config.ProxyURL,
		MaxLogWriters:  config.MaxLogWriters,
	}
}

func (s *ExecStarter) ConfigSnapshot() ManagerConfig {
	if s == nil {
		return ManagerConfig{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Config
}

func (s *ExecStarter) SetProxyURL(value string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureInnerLocked().SetProxyURL(value)
	s.Config.ProxyURL = strings.TrimSpace(value)
}

func (s *ExecStarter) SetProxyAPIKey(value string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureInnerLocked().SetProxyAPIKey(value)
	s.Config.ProxyAPIKey = value
}

func (s *ExecStarter) Start(ctx context.Context, account Account, home string, port int) (ManagedProcess, error) {
	if s == nil {
		s = NewExecStarter(ManagerConfig{})
	}
	s.mu.Lock()
	inner := s.ensureInnerLocked()
	s.mu.Unlock()
	return inner.Start(ctx, account, home, port)
}

func StarterEnv(config ManagerConfig, account Account, home string, port int) ([]string, error) {
	return qoder.StarterEnv(starterConfig(config), account, home, port)
}

func QoderRuntimeSpec(cfg ManagerConfig, account Account, home string) (cliPath, site, configDir, configEnv string, err error) {
	spec, err := qoder.RuntimeSpec(cfg.QoderCLIPath, cfg.QoderCNCLIPath, account.ProviderRegion, home)
	if err != nil {
		return "", "", "", "", err
	}
	return spec.CLIPath, spec.Site, spec.ConfigDir, spec.ConfigEnv, nil
}

func (m *Manager) ReloadProxyURL(ctx context.Context, value string) error {
	// Serialize reloads so concurrent PATCHes cannot interleave stop/start.
	m.proxyReloadMu.Lock()
	defer m.proxyReloadMu.Unlock()

	value = strings.TrimSpace(value)

	m.mu.Lock()
	unchanged := strings.TrimSpace(m.config.ProxyURL) == value
	// Skip only when nothing changed AND the running workers already match the
	// desired value. A previous failure leaves proxyReloadPending set, so the
	// same value can be retried.
	if unchanged && !m.proxyReloadPending {
		m.mu.Unlock()
		return nil
	}
	m.config.ProxyURL = value
	m.proxyReloadPending = true
	m.mu.Unlock()

	// Push to the starter outside the manager lock so we never nest the
	// manager lock around the starter lock.
	if starter, ok := m.starter.(ProxyConfigurableStarter); ok {
		starter.SetProxyURL(value)
	}

	accounts, err := m.store.List(ctx)
	if err != nil {
		return err
	}
	var joined error
	for _, account := range accounts {
		if !m.shouldRestartForGlobalProxy(account) {
			continue
		}
		if err := m.stopAccount(account.ID); err != nil {
			joined = errors.Join(joined, fmt.Errorf("stop account %s: %w", account.ID, err))
			continue
		}
		if err := m.startAccountWithRecovery(ctx, account); err != nil {
			joined = errors.Join(joined, fmt.Errorf("restart account %s: %w", account.ID, err))
		}
	}

	// Clear the pending flag only when every worker switched successfully, so a
	// later identical PATCH retries the ones that failed.
	if joined == nil {
		m.mu.Lock()
		m.proxyReloadPending = false
		m.mu.Unlock()
	}
	return joined
}

// shouldRestartForGlobalProxy reports whether a global proxy change must
// restart the account's worker: only enabled child-process (Qoder) accounts
// with no per-account proxy inherit the global setting.
func (m *Manager) shouldRestartForGlobalProxy(account Account) bool {
	descriptor, _, err := providers.Resolve(account.Provider, account.ProviderRegion)
	return err == nil &&
		account.Enabled &&
		strings.TrimSpace(account.ProxyURL) == "" &&
		descriptor.Runtime == providers.RuntimeChildProcess
}

func (m *Manager) SyncCredential(ctx context.Context, id, authType string) error {
	account, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}
	home := filepath.Join(m.config.DataDir, "runtime", id)
	return qoder.SyncCredential(ctx, m.store, account, home, authType)
}

func (m *Manager) stopAccount(id string) error {
	m.mu.Lock()
	process := m.processes[id]
	delete(m.processes, id)
	m.mu.Unlock()
	m.pool.Remove(id)
	if process == nil {
		return nil
	}
	return process.Stop()
}

func (m *Manager) AccountURL(id string) (string, bool) {
	item, ok := m.pool.ByID(id)
	return item.URL, ok
}

// accountHasPAT reports whether a Qoder account has a stored
// qoder-pat-v1 credential payload. PAT-backed accounts skip the worker spawn
// and run fully in-process; accounts without a PAT (legacy CLI blob) keep
// the worker path until the operator re-logs them with a PAT. A transient
// store error is treated as "no PAT" so the worker spawns and serves as a
// fallback, matching the legacy behaviour.
func (m *Manager) accountHasPAT(ctx context.Context, id string) bool {
	if m == nil || m.store == nil {
		return false
	}
	format, payload, err := m.store.LoadCredentialPayload(ctx, id)
	if err != nil {
		return false
	}
	return format == "qoder-pat-v1" && len(payload) > 0
}
