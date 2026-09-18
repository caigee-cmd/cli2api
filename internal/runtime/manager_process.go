package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/caigee-cmd/cli2api/internal/providers"
	proxyutil "github.com/caigee-cmd/cli2api/internal/proxy"
)

// Child-process lifecycle and Qoder HOME/CLI materialization. In-process
// providers only Upsert the pool; Qoder paths below understand HOME, CLI, and
// daemon env. processes/nextPort remain on Manager; this file does not copy them.

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
			RuntimeState: "starting",
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
		Ready: &notReady, RuntimeState: "starting",
	})

	home := filepath.Join(m.config.DataDir, "runtime", account.ID)
	if err := materializeHome(ctx, m.store, account, home); err != nil {
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
		Ready: &notReady, RuntimeState: "starting",
	})
	go m.watchAccount(account.ID, process)
	return nil
}

func qoderConfigDirName(region string) string {
	if strings.EqualFold(strings.TrimSpace(region), "cn") {
		return ".qoder-cn"
	}
	return ".qoder"
}

func qoderAuthDir(home, region string) string {
	return filepath.Join(home, qoderConfigDirName(region), ".auth")
}

func materializeHome(ctx context.Context, store AccountStore, account Account, home string) error {
	authDir := qoderAuthDir(home, account.ProviderRegion)
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		return fmt.Errorf("create account home: %w", err)
	}
	credential, err := store.LoadCredential(ctx, account.ID)
	if errors.Is(err, ErrAccountNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(authDir, "user"), credential.UserBlob, 0o600); err != nil {
		return fmt.Errorf("write user credential: %w", err)
	}
	if err := os.WriteFile(filepath.Join(authDir, "machine_id"), []byte(credential.MachineID), 0o600); err != nil {
		return fmt.Errorf("write machine id: %w", err)
	}
	return nil
}

type prefixLogWriter struct {
	prefix string
	next   io.Writer
	buf    []byte
}

func (w *prefixLogWriter) Write(p []byte) (int, error) {
	if w == nil || w.next == nil {
		return len(p), nil
	}
	w.buf = append(w.buf, p...)
	for {
		idx := -1
		for i, b := range w.buf {
			if b == '\n' {
				idx = i
				break
			}
		}
		if idx < 0 {
			break
		}
		line := append([]byte(nil), w.buf[:idx+1]...)
		w.buf = w.buf[idx+1:]
		if _, err := w.next.Write(append([]byte(w.prefix), line...)); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

type ExecStarter struct {
	mu     sync.RWMutex
	Config ManagerConfig
}

// configSnapshot returns a stable copy of the starter config. Start uses one
// snapshot for the whole spawn so a concurrent SetProxyURL cannot race with
// reading fields.
func (s *ExecStarter) ConfigSnapshot() ManagerConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Config
}

// SetProxyURL updates the global proxy used for future spawns.
func (s *ExecStarter) SetProxyURL(value string) {
	s.mu.Lock()
	s.Config.ProxyURL = strings.TrimSpace(value)
	s.mu.Unlock()
}

// SetProxyAPIKey updates the manager proxy API key used for future spawns.
func (s *ExecStarter) SetProxyAPIKey(value string) {
	s.mu.Lock()
	s.Config.ProxyAPIKey = value
	s.mu.Unlock()
}

type execProcess struct {
	cmd  *exec.Cmd
	url  string
	done chan error
}

func (p *execProcess) URL() string        { return p.url }
func (p *execProcess) Done() <-chan error { return p.done }
func (p *execProcess) Stop() error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	if err := p.cmd.Process.Signal(os.Interrupt); err == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

func proxyEnv(env []string, raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return env
	}
	filtered := make([]string, 0, len(env)+2)
	for _, value := range env {
		key := strings.SplitN(value, "=", 2)[0]
		switch strings.ToUpper(key) {
		case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY":
			continue
		}
		filtered = append(filtered, value)
	}
	if setting, err := proxyutil.Parse(raw); err == nil && setting.Mode == proxyutil.ModeProxy {
		filtered = append(filtered, "HTTP_PROXY="+raw, "HTTPS_PROXY="+raw, "http_proxy="+raw, "https_proxy="+raw)
	}
	return filtered
}

func (s *ExecStarter) Start(_ context.Context, account Account, home string, port int) (ManagedProcess, error) {
	config := s.ConfigSnapshot()
	env, err := StarterEnv(config, account, home, port)
	if err != nil {
		return nil, err
	}
	node := config.NodeBinary
	if node == "" {
		node = "node"
	}
	cmd := exec.Command(node, config.DaemonPath)
	cmd.Env = env
	writer := config.MaxLogWriters
	if writer == nil {
		writer = os.Stderr
	}
	writer = &prefixLogWriter{prefix: "[account=" + account.ID + "] ", next: writer}
	cmd.Stdout = writer
	cmd.Stderr = writer
	if err := os.MkdirAll(filepath.Join(home, "work"), 0o700); err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	process := &execProcess{cmd: cmd, url: "http://127.0.0.1:" + strconv.Itoa(port), done: make(chan error, 1)}
	go func() {
		process.done <- cmd.Wait()
		close(process.done)
	}()
	return process, nil
}

// starterEnv builds the worker environment from a stable config snapshot. The
// effective proxy is resolved once (account override wins, else global) and
// applied to both the proxy env vars and QODER_PROXY_URL.
func StarterEnv(config ManagerConfig, account Account, home string, port int) ([]string, error) {
	if config.DaemonPath == "" {
		return nil, fmt.Errorf("worker daemon path required")
	}
	cliPath, site, configDir, configEnv, err := QoderRuntimeSpec(config, account, home)
	if err != nil {
		return nil, err
	}
	effectiveProxy := proxyutil.Effective(account.ProxyURL, config.ProxyURL)
	env := proxyEnv(os.Environ(), effectiveProxy)
	return append(env,
		"HOME="+home,
		"QODER_HOME="+configDir,
		configEnv+"="+configDir,
		"QODER_SITE="+site,
		"QODER_ACCOUNT_ID="+account.ID,
		"QODER_MAX_INFLIGHT="+strconv.Itoa(account.MaxInFlight),
		"WORKER_HOST=127.0.0.1",
		"WORKER_PORT="+strconv.Itoa(port),
		"PROXY_API_KEY="+config.ProxyAPIKey,
		"QODERCLI_JS="+cliPath,
		"PLAIN_TEMPLATE_PATH="+config.TemplatePath,
		"QODER_WARMUP_CWD="+filepath.Join(home, "work"),
		"QODER_PROXY_URL="+effectiveProxy,
	), nil
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
func QoderRuntimeSpec(cfg ManagerConfig, account Account, home string) (cliPath, site, configDir, configEnv string, err error) {
	region := strings.ToLower(strings.TrimSpace(account.ProviderRegion))
	configDir = filepath.Join(home, qoderConfigDirName(region))
	switch region {
	case "", "global":
		cliPath = strings.TrimSpace(cfg.QoderCLIPath)
		if cliPath == "" {
			return "", "", "", "", fmt.Errorf("qoder global CLI path required")
		}
		return cliPath, "global", configDir, "QODER_CONFIG_DIR", nil
	case "cn":
		cliPath = strings.TrimSpace(cfg.QoderCNCLIPath)
		if cliPath == "" {
			return "", "", "", "", fmt.Errorf("qoder CN CLI path required: set QODERCNCLI_JS to @qodercn-ai/qoderclicn bundle/qoderclicn.js")
		}
		return cliPath, "cn", configDir, "QODERCN_CONFIG_DIR", nil
	default:
		return "", "", "", "", fmt.Errorf("unknown qoder region %q", account.ProviderRegion)
	}
}

func (m *Manager) SyncCredential(ctx context.Context, id, authType string) error {
	account, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}
	home := filepath.Join(m.config.DataDir, "runtime", id)
	authDir := qoderAuthDir(home, account.ProviderRegion)
	userBlob, err := os.ReadFile(filepath.Join(authDir, "user"))
	if err != nil {
		return fmt.Errorf("read qoder user credential: %w", err)
	}
	machineID, err := os.ReadFile(filepath.Join(authDir, "machine_id"))
	if err != nil {
		return fmt.Errorf("read qoder machine id: %w", err)
	}
	return m.store.SaveCredential(ctx, id, authType, NativeCredential{
		UserBlob:  userBlob,
		MachineID: string(machineID),
	})
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
