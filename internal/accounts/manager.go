package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
	proxyutil "github.com/caigee-cmd/cli2api/internal/proxy"
)

var errManagerClosed = errors.New("account manager closed")

type ManagerConfig struct {
	DataDir         string
	BasePort        int
	NodeBinary      string
	DaemonPath      string
	QoderCLIPath    string
	QoderCNCLIPath  string
	TemplatePath    string
	ProxyAPIKey     string
	ProxyURL        string
	MaxLogWriters   io.Writer
	RestartDelay    time.Duration
	RestartMaxDelay time.Duration
}

type ManagedProcess interface {
	URL() string
	Done() <-chan error
	Stop() error
}

type ProcessStarter interface {
	Start(context.Context, Account, string, int) (ManagedProcess, error)
}

// ProxyConfigurableStarter lets the manager push the global outbound proxy to a
// starter that can apply it to newly spawned workers. Kept optional so test
// starters need not implement it.
type ProxyConfigurableStarter interface {
	SetProxyURL(string)
}

// APIKeyConfigurableStarter is the sibling of ProxyConfigurableStarter for the
// manager's proxy API key.
type APIKeyConfigurableStarter interface {
	SetProxyAPIKey(string)
}

// AccountMaintainer is the daily-ops surface for providers that expose a
// credit check-in and a token keepalive. Implemented by workbuddy.Client and
// trae.Client; kept narrow so accounts does not grow a generic check-in
// capability on AccountProber.
type AccountMaintainer interface {
	DailyCheckin(ctx context.Context, accountID string) (string, error)
	Keepalive(ctx context.Context, accountID string) error
}

type Manager struct {
	config         ManagerConfig
	store          *Store
	starter        ProcessStarter
	pool           *Pool
	providers      *providers.Registry
	maintainers    map[string]AccountMaintainer
	mu             sync.Mutex
	processes      map[string]ManagedProcess
	restarts       map[string]int
	restartBackoff map[string]int
	recovering     map[string]bool
	recoverDone    sync.WaitGroup
	nextPort       int
	httpClient     *http.Client
	runCtx         context.Context
	cancel         context.CancelFunc
	// The persistence path is one mutex-guarded goroutine. The dirty set
	// is keyed by account ID and merged on enqueue: a stale snapshot (older
	// StateVersion) that arrives after a newer one is discarded, so the final
	// persisted state is the newest pool state regardless of observer-arrival
	// order (the observer runs after p.mu is released, so two concurrent
	// mutations can enqueue out of production order). Keying by account ID
	// also bounds the set's size to the number of accounts — it cannot grow
	// without limit under DB pressure the way an unbounded FIFO would. Close
	// sets a closed flag under the same lock (no channel to close, no
	// send-on-closed panic); Flush enqueues a marker that fires only once the
	// dirty set has drained to empty, so everything enqueued before the flush
	// is persisted before Flush returns.
	persistMu         sync.Mutex
	persistCond       *sync.Cond
	persistDirty      map[string]Item   // accountID -> latest snapshot (merge on enqueue)
	persistFlushes    []chan struct{}   // ordered flush markers, fire when dirty set drains
	persistedVersions map[string]uint64 // accountID -> last version written to SQLite
	persistClosed     bool
	persistCloseCh    chan struct{} // closed by Close(); drainer's retry backoff watches it
	persistDone       sync.WaitGroup

	// Serializes ReloadProxyURL so two settings PATCHes cannot interleave
	// stop/start cycles. proxyReloadPending stays true while the applied global
	// proxy differs from what the running workers use (a reload attempt failed,
	// or one was never made), so resubmitting the same value can still retry
	// instead of being treated as a no-op. Guarded by mu.
	proxyReloadMu      sync.Mutex
	proxyReloadPending bool
}

func NewManager(config ManagerConfig, store *Store, starter ProcessStarter) *Manager {
	if config.BasePort <= 0 {
		config.BasePort = 32100
	}
	if config.RestartDelay <= 0 {
		config.RestartDelay = time.Second
	}
	if config.RestartMaxDelay <= 0 {
		config.RestartMaxDelay = time.Minute
	}
	if config.RestartMaxDelay < config.RestartDelay {
		config.RestartMaxDelay = config.RestartDelay
	}
	if starter == nil {
		starter = &ExecStarter{Config: config}
	}
	runCtx, cancel := context.WithCancel(context.Background())
	manager := &Manager{
		config:            config,
		store:             store,
		starter:           starter,
		pool:              NewPool(nil, nil),
		maintainers:       map[string]AccountMaintainer{},
		processes:         map[string]ManagedProcess{},
		restarts:          map[string]int{},
		restartBackoff:    map[string]int{},
		recovering:        map[string]bool{},
		nextPort:          config.BasePort,
		httpClient:        &http.Client{Timeout: 2 * time.Second},
		runCtx:            runCtx,
		cancel:            cancel,
		persistDirty:      map[string]Item{},
		persistedVersions: map[string]uint64{},
		persistCloseCh:    make(chan struct{}),
	}
	manager.persistCond = sync.NewCond(&manager.persistMu)
	manager.persistDone.Add(1)
	go manager.drainCooldowns()
	manager.pool.SetObserver(func(item Item) {
		// Merge into the per-account dirty set. The snapshot was cloned
		// under p.mu and stamped with StateVersion there; the observer runs
		// after p.mu is released, so two concurrent mutations can enqueue out
		// of production order. The version check discards a stale snapshot
		// (older version already in the set), so the dirty set always holds
		// the newest-known state per account. Keying by account ID bounds the
		// set's size to the number of accounts.
		manager.persistMu.Lock()
		if !manager.persistClosed {
			if existing, ok := manager.persistDirty[item.ID]; !ok || item.StateVersion >= existing.StateVersion {
				manager.persistDirty[item.ID] = item
				manager.persistCond.Signal()
			}
		}
		manager.persistMu.Unlock()
	})
	return manager
}

// drainCooldowns persists pool state changes for one account at a time,
// draining the per-account dirty set to empty before signaling any flush.
// Because each entry in the dirty set is the newest-known snapshot for its
// account (stale versions are discarded on enqueue), draining the whole set
// to empty guarantees the final persisted state is the newest pool state. A
// flush marker fires only once the dirty set is empty, so everything enqueued
// before the flush call is persisted before Flush returns.
func (m *Manager) drainCooldowns() {
	defer m.persistDone.Done()
	ctx := context.Background()
	for {
		m.persistMu.Lock()
		for len(m.persistDirty) == 0 && len(m.persistFlushes) == 0 && !m.persistClosed {
			m.persistCond.Wait()
		}
		// Drain the whole dirty set before firing any flush so a flush
		// caller sees every account's newest state on disk.
		if len(m.persistDirty) == 0 {
			if len(m.persistFlushes) > 0 {
				flushes := m.persistFlushes
				m.persistFlushes = nil
				m.persistMu.Unlock()
				for _, done := range flushes {
					close(done)
				}
				continue
			}
			if m.persistClosed {
				m.persistMu.Unlock()
				return
			}
			m.persistMu.Unlock()
			continue
		}
		// Pick a stable account (lowest ID) for deterministic drain order.
		id := ""
		for k := range m.persistDirty {
			if id == "" || k < id {
				id = k
			}
		}
		item := m.persistDirty[id]
		delete(m.persistDirty, id)
		// Discard a snapshot that is older than what is already on disk:
		// a newer mutation may have already been persisted while this older
		// snapshot was sitting in the dirty set. Without this guard, a
		// late-arriving stale snapshot would overwrite the newer SQLite state.
		if persisted, ok := m.persistedVersions[id]; ok && item.StateVersion <= persisted {
			m.persistMu.Unlock()
			continue
		}
		m.persistMu.Unlock()

		err := m.store.RecordPoolState(ctx, item)
		if err == nil {
			err = m.store.SaveCooldowns(ctx, item.ID, cooldownRows(item))
		}
		m.persistMu.Lock()
		if err != nil {
			// The write failed (SQLite locked, disk error, connection). Put
			// the snapshot back into the dirty set so it is retried; if a
			// newer snapshot arrived in the meantime the merge's version
			// check keeps the newer one. Only advance persistedVersions on
			// success, otherwise a later stale snapshot could be discarded
			// even though the newer state never reached SQLite. Log the
			// error and back off before the next attempt to avoid a hot
			// spin against a stuck DB.
			log.Printf("persist cooldown account=%s version=%d: %v", id, item.StateVersion, err)
			if existing, ok := m.persistDirty[id]; !ok || item.StateVersion >= existing.StateVersion {
				m.persistDirty[id] = item
			}
			m.persistMu.Unlock()
			// Back off, but stay responsive to shutdown. Close() sets
			// persistClosed and closes persistCloseCh, then waits for the
			// drainer; if the backoff only watched runCtx (which Close
			// cancels only AFTER the wait), a stuck DB would block Close
			// forever. Watching persistCloseCh lets the drainer exit on
			// shutdown, abandoning the unsaved dirty state — on a
			// persistent DB outage there is nothing to persist.
			select {
			case <-time.After(persistRetryBackoff):
			case <-m.runCtx.Done():
				return
			case <-m.persistCloseCh:
				return
			}
			continue
		}
		// Record the persisted version under the lock so a stale snapshot
		// enqueued later is discarded before it can overwrite this state.
		if m.persistedVersions[id] < item.StateVersion {
			m.persistedVersions[id] = item.StateVersion
		}
		m.persistMu.Unlock()
	}
}

// Flush waits for all cooldown writes queued so far to be persisted. The
// marker fires only once the dirty set has drained to empty, so every
// account's newest state enqueued before this call is persisted before
// Flush returns.
func (m *Manager) Flush() {
	if m == nil || m.persistCond == nil {
		return
	}
	done := make(chan struct{})
	m.persistMu.Lock()
	if m.persistClosed {
		m.persistMu.Unlock()
		return
	}
	m.persistFlushes = append(m.persistFlushes, done)
	m.persistCond.Signal()
	m.persistMu.Unlock()
	select {
	case <-done:
	case <-m.runCtx.Done():
	}
}

// cooldownRows flattens one pool item into persisted cooldown rows: the
// account-wide cooldown plus any model-scoped ones. Each row carries the
// backoff ladder and previous kind for its own scope so per-model backoff
// and last-kind survive restart without cross-model confusion.
func cooldownRows(item Item) []CooldownRow {
	rows := make([]CooldownRow, 0, 1+len(item.ModelDownUntil))
	if !item.DownUntil.IsZero() {
		rows = append(rows, CooldownRow{
			AccountID: item.ID, DownUntil: item.DownUntil,
			BackoffLevel: item.BackoffLevel, Kind: item.LastKind, Message: item.LastError,
		})
	}
	for model, until := range item.ModelDownUntil {
		if until.IsZero() {
			continue
		}
		// Model-scoped rows carry only that model's own backoff ladder,
		// not the account-wide BackoffLevel. Persisting the account-wide
		// level here would, on restore, write it into item.BackoffLevel
		// and pollute the account-level ladder for every other model.
		level := 0
		if item.ModelBackoff != nil {
			level = item.ModelBackoff[model]
		}
		kind := item.LastKind
		if item.ModelLastKind != nil {
			if mk, ok := item.ModelLastKind[model]; ok && mk != "" {
				kind = mk
			}
		}
		rows = append(rows, CooldownRow{
			AccountID: item.ID, Model: model, DownUntil: until,
			BackoffLevel: level, Kind: kind, Message: item.LastError,
			ModelKind: kind,
		})
	}
	return rows
}

// restoreCooldowns reloads persisted cooldowns into the pool after accounts
// are registered. Managed updates recreate the container regularly, and
// without this a rate-limited account would be retried immediately on boot.
func (m *Manager) restoreCooldowns(ctx context.Context) {
	rows, err := m.store.LoadCooldowns(ctx)
	if err != nil {
		log.Printf("restore cooldowns: %v", err)
		return
	}
	restored := 0
	for _, row := range rows {
		item, ok := m.pool.ByID(row.AccountID)
		if !ok {
			continue
		}
		if row.Model == "" {
			level := clampBackoffLevel(row.BackoffLevel)
			if level > item.BackoffLevel {
				item.BackoffLevel = level
			}
			item.DownUntil = row.DownUntil
		} else {
			// Model-scoped row: restore only the model's own backoff,
			// not the account-wide ladder. Writing the model's level into
			// item.BackoffLevel would pollute the account-level ladder so
			// that a later account-wide failure starts at the model's
			// level instead of 0. The account-wide BackoffLevel is
			// restored exclusively from the account-wide row above.
			if item.ModelDownUntil == nil {
				item.ModelDownUntil = map[string]time.Time{}
			}
			item.ModelDownUntil[row.Model] = row.DownUntil
			if level := clampBackoffLevel(row.BackoffLevel); level > 0 {
				if item.ModelBackoff == nil {
					item.ModelBackoff = map[string]int{}
				}
				if level > item.ModelBackoff[row.Model] {
					item.ModelBackoff[row.Model] = level
				}
			}
			if row.ModelKind != "" {
				if item.ModelLastKind == nil {
					item.ModelLastKind = map[string]string{}
				}
				if item.ModelLastKind[row.Model] == "" {
					item.ModelLastKind[row.Model] = row.ModelKind
				}
			}
		}
		m.pool.Upsert(item)
		restored++
	}
	if restored > 0 {
		log.Printf("restored %d cooldown(s) from SQLite", restored)
	}
}

func (m *Manager) Start(ctx context.Context) error {
	accounts, err := m.store.List(ctx)
	if err != nil {
		return err
	}
	for _, account := range accounts {
		if !account.Enabled {
			continue
		}
		if err := m.startAccountWithRecovery(ctx, account); err != nil {
			log.Printf("account %s initial start failed: %v", account.ID, err)
		}
	}
	// After registration so there is an item to restore into.
	m.restoreCooldowns(ctx)
	return nil
}

func (m *Manager) Pool() *Pool   { return m.pool }
func (m *Manager) Store() *Store { return m.store }

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

// SetProviders wires optional in-process account probers (WorkBuddy, etc.).
func (m *Manager) SetProviders(registry *providers.Registry) {
	if m == nil {
		return
	}
	m.providers = registry
}

// SetMaintainer wires a provider's check-in / keepalive ops without a second
// scheduler package. Passing nil ops removes the provider.
func (m *Manager) SetMaintainer(provider string, ops AccountMaintainer) {
	if m == nil {
		return
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.maintainers == nil {
		m.maintainers = map[string]AccountMaintainer{}
	}
	if ops == nil {
		delete(m.maintainers, provider)
		return
	}
	m.maintainers[provider] = ops
}

func (m *Manager) maintainerFor(provider string) (AccountMaintainer, bool) {
	if m == nil {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	ops, ok := m.maintainers[strings.ToLower(strings.TrimSpace(provider))]
	return ops, ok
}

// SupportsCheckin reports whether the provider has check-in ops registered.
func (m *Manager) SupportsCheckin(provider string) bool {
	_, ok := m.maintainerFor(provider)
	return ok
}

func (m *Manager) Close() error {
	// Stop accepting observer updates under the same lock the enqueues use,
	// then signal the drainer to drain the remainder and exit. Setting
	// persistClosed under persistMu guarantees no observer can enqueue after
	// this point — there is no channel to close, so the old "send on closed
	// channel" panic cannot occur. Closing persistCloseCh unblocks a drainer
	// that is backed off retrying a stuck DB: runCtx is only canceled after
	// the drainer has exited (below), so without persistCloseCh the retry
	// loop would block Close forever on a persistent DB outage.
	m.persistMu.Lock()
	if !m.persistClosed {
		m.persistClosed = true
		close(m.persistCloseCh)
	}
	m.persistCond.Broadcast()
	m.persistMu.Unlock()
	m.persistDone.Wait()
	m.mu.Lock()
	m.cancel()
	m.mu.Unlock()
	m.recoverDone.Wait()
	m.mu.Lock()
	processes := make([]ManagedProcess, 0, len(m.processes))
	for _, process := range m.processes {
		processes = append(processes, process)
	}
	m.processes = map[string]ManagedProcess{}
	m.mu.Unlock()
	var joined error
	for _, process := range processes {
		joined = errors.Join(joined, process.Stop())
	}
	return joined
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

func materializeHome(ctx context.Context, store *Store, account Account, home string) error {
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
func (s *ExecStarter) configSnapshot() ManagerConfig {
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
	config := s.configSnapshot()
	env, err := starterEnv(config, account, home, port)
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
func starterEnv(config ManagerConfig, account Account, home string, port int) ([]string, error) {
	if config.DaemonPath == "" {
		return nil, fmt.Errorf("worker daemon path required")
	}
	cliPath, site, configDir, configEnv, err := qoderRuntimeSpec(config, account, home)
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
	// Request sanitization applies per request, so sync it into the pool
	// without restarting anything.
	if before.DropSystemPrompt != after.DropSystemPrompt {
		m.pool.SetDropSystemPrompt(id, after.DropSystemPrompt)
	}
	if before.Priority != after.Priority {
		m.pool.SetWeight(id, after.Priority)
	}
	if before.Enabled && !after.Enabled {
		return m.stopAccount(id)
	}
	if !before.Enabled && after.Enabled {
		return m.startAccountWithRecovery(ctx, after)
	}
	if before.Enabled && after.Enabled && before.ProxyURL != after.ProxyURL {
		descriptor, _, resolveErr := providers.Resolve(after.Provider, after.ProviderRegion)
		if resolveErr == nil && descriptor.Runtime == providers.RuntimeChildProcess {
			if err := m.stopAccount(id); err != nil {
				return err
			}
			return m.startAccountWithRecovery(ctx, after)
		}
	}
	if before.Enabled && after.Enabled && before.MaxInFlight != after.MaxInFlight {
		if err := m.stopAccount(id); err != nil {
			return err
		}
		return m.startAccountWithRecovery(ctx, after)
	}
	return nil
}

func (m *Manager) Delete(ctx context.Context, id string) error {
	if err := m.stopAccount(id); err != nil {
		return err
	}
	if err := m.store.Delete(ctx, id); err != nil {
		return err
	}
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

func qoderRuntimeSpec(cfg ManagerConfig, account Account, home string) (cliPath, site, configDir, configEnv string, err error) {
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

func (m *Manager) startAccountWithRecovery(ctx context.Context, account Account) error {
	err := m.startAccount(ctx, account)
	if err != nil && !errors.Is(err, errManagerClosed) && m.runCtx.Err() == nil {
		m.beginAccountRecovery(account.ID, err)
	}
	return err
}

func (m *Manager) beginAccountRecovery(id string, startErr error) {
	if m == nil || m.store == nil || strings.TrimSpace(id) == "" {
		return
	}
	message := "account daemon failed to start"
	if startErr != nil {
		message = startErr.Error()
	}
	m.mu.Lock()
	if m.runCtx.Err() != nil {
		m.mu.Unlock()
		return
	}
	if m.recovering[id] {
		m.mu.Unlock()
		return
	}
	m.recovering[id] = true
	m.restarts[id]++
	restarts := m.restarts[id]
	if m.restartBackoff[id] < backoffMaxLevel {
		m.restartBackoff[id]++
	}
	backoffLevel := m.restartBackoff[id]
	m.recoverDone.Add(1)
	go m.recoverAccount(id, restarts, backoffLevel, message)
	m.mu.Unlock()
	m.pool.SetRuntimeState(id, "dead", time.Now().Add(m.restartDelay(backoffLevel)), backoffLevel, message)
}

func (m *Manager) recoverAccount(id string, restarts, backoffLevel int, message string) {
	defer func() {
		m.mu.Lock()
		delete(m.recovering, id)
		m.mu.Unlock()
		m.recoverDone.Done()
	}()

	_ = m.store.Observe(context.Background(), id, "", "dead", message, KindUnavailable)

	for {
		if m.runCtx.Err() != nil {
			return
		}
		account, err := m.store.Get(m.runCtx, id)
		if errors.Is(err, ErrAccountNotFound) || (err == nil && !account.Enabled) {
			m.pool.SetRuntimeState(id, "disabled", time.Time{}, restarts, message)
			return
		}
		if err != nil {
			message = err.Error()
		}

		delay := m.restartDelay(backoffLevel)
		m.pool.SetRuntimeState(id, "dead", time.Now().Add(delay), backoffLevel, message)
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-m.runCtx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		}

		if m.runCtx.Err() != nil {
			return
		}
		account, err = m.store.Get(m.runCtx, id)
		if errors.Is(err, ErrAccountNotFound) || (err == nil && !account.Enabled) {
			m.pool.SetRuntimeState(id, "disabled", time.Time{}, restarts, message)
			return
		}
		if err != nil {
			message = err.Error()
			m.mu.Lock()
			m.restarts[id]++
			restarts = m.restarts[id]
			m.mu.Unlock()
			continue
		}

		if m.runCtx.Err() != nil {
			return
		}
		err = m.startAccount(m.runCtx, account)
		if err == nil || errors.Is(err, errManagerClosed) || m.runCtx.Err() != nil {
			return
		}
		message = err.Error()
		m.mu.Lock()
		m.restarts[id]++
		restarts = m.restarts[id]
		if m.restartBackoff[id] < backoffMaxLevel {
			m.restartBackoff[id]++
		}
		backoffLevel = m.restartBackoff[id]
		m.mu.Unlock()
		_ = m.store.Observe(context.Background(), id, "", "dead", message, KindUnavailable)
	}
}

type ImportAccount struct {
	Name                 string
	Provider             string
	Region               string
	Enabled              bool
	MaxInFlight          int
	Priority             int
	DropSystemPrompt     *bool
	WorkBuddyAutoCheckin *bool
	WorkBuddyCheckinTime string
	ProxyURL             string
	Credential           NativeCredential
}

type AccountView struct {
	Account
	Ready               bool              `json:"ready"`
	Hot                 bool              `json:"hot"`
	InFlight            int               `json:"in_flight"`
	Restarts            int               `json:"restarts"`
	RuntimeState        string            `json:"runtime_state,omitempty"`
	NextRestartAt       string            `json:"next_restart_at,omitempty"`
	RestartBackoffLevel int               `json:"restart_backoff_level,omitempty"`
	DownUntil           string            `json:"down_until,omitempty"`
	ModelCooldowns      map[string]string `json:"model_cooldowns,omitempty"`
	Quota               *QuotaSnapshot    `json:"quota,omitempty"`
	ProxyURL            string            `json:"proxy_url,omitempty"`
}

func (m *Manager) Import(ctx context.Context, input ImportAccount) (Account, error) {
	account, err := m.store.Create(ctx, CreateAccount{
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
// single card can update without re-probing the whole pool.
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
// account, so a single-card refresh can update in place.
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

func (m *Manager) AccountURL(id string) (string, bool) {
	item, ok := m.pool.ByID(id)
	return item.URL, ok
}

func (m *Manager) resetRestartBackoff(id string) {
	if m == nil || id == "" {
		return
	}
	m.mu.Lock()
	delete(m.restartBackoff, id)
	m.mu.Unlock()
}

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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, item.URL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		ready := false
		hot := false
		m.pool.MergeHealth(item.ID, ready, hot, 0, item.Restarts, err.Error())
		_ = m.store.Observe(ctx, item.ID, "", "error", err.Error(), KindUnavailable)
		return fmt.Errorf("health account %s: %w", item.ID, err)
	}
	defer resp.Body.Close()
	var health struct {
		OK        bool   `json:"ok"`
		Ready     bool   `json:"ready"`
		Hot       bool   `json:"hot"`
		UID       string `json:"uid"`
		InFlight  int    `json:"inFlight"`
		LastError string `json:"lastError"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return fmt.Errorf("decode account %s health: %w", item.ID, err)
	}
	ready := resp.StatusCode < 300 && health.OK && health.Ready
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

// fetchProviderQuotaFor refreshes the quota snapshot for an account's provider
// when that provider exposes an in-process prober.
func (m *Manager) fetchProviderQuotaFor(ctx context.Context, accountID, provider string) {
	if adapter, ok := m.providers.Get(provider); ok && adapter.Prober != nil {
		m.fetchProviderQuota(ctx, accountID, adapter.Prober)
	}
}

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

func (m *Manager) forceReady(accountID string) bool {
	if m == nil || strings.TrimSpace(accountID) == "" || strings.TrimSpace(m.config.DataDir) == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(m.config.DataDir, "force-ready", accountID))
	return err == nil
}

const modelCatalogTTL = 5 * time.Minute

// persistRetryBackoff paces retries after a failed SQLite write. A stuck DB
// must not drive the drainer into a hot spin, but the backoff must stay
// short enough that a transient lock clears and the newest state reaches
// disk promptly. It is fixed rather than exponential because the dirty-set
// merge already collapses intermediate snapshots.
const persistRetryBackoff = 500 * time.Millisecond

// EnsureModelCatalogs refreshes per-account catalogs that are missing or
// older than the worker TTL. Failures leave the previous snapshot in place
// and never flip readiness or cooldown.
//
// Refreshes run concurrently in the background with a bounded semaphore so
// the first chat request is never blocked by N serial 15-second timeouts
// when multiple accounts are offline. The caller returns immediately; a
// nil Models slice means unknown (fail open) until the background refresh
// completes and subsequent requests use the fresh catalog.
func (m *Manager) EnsureModelCatalogs(ctx context.Context, force bool) {
	if m == nil || m.pool == nil {
		return
	}
	now := time.Now()
	var stale []Item
	for _, item := range m.pool.Items() {
		if !force && item.Models != nil && !item.ModelsAt.IsZero() && now.Sub(item.ModelsAt) < modelCatalogTTL {
			continue
		}
		stale = append(stale, item)
	}
	if len(stale) == 0 {
		return
	}
	// Fire background refreshes concurrently with a bounded semaphore.
	// Use m.runCtx so refreshes survive the caller's request context and
	// are canceled only on shutdown.
	if ctx.Err() != nil {
		return // caller context already cancelled — no point firing goroutines
	}
	const maxConcurrent = 4
	sem := make(chan struct{}, maxConcurrent)
	for _, item := range stale {
		go func(it Item) {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				m.fetchAccountModels(m.runCtx, it)
			case <-m.runCtx.Done():
			}
		}(item)
	}
}

func (m *Manager) fetchAccountModels(ctx context.Context, item Item) {
	if m == nil || m.pool == nil || item.ID == "" {
		return
	}
	if item.Runtime == string(providers.RuntimeInProcess) || strings.TrimSpace(item.URL) == "" {
		m.fetchProviderModels(ctx, item)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(item.URL, "/")+"/admin/models", nil)
	if err != nil {
		log.Printf("catalog refresh failed account=%s provider=%s stage=request: %v", item.ID, item.Provider, err)
		return
	}
	if m.config.ProxyAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+m.config.ProxyAPIKey)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("catalog refresh failed account=%s provider=%s stage=http: %v", item.ID, item.Provider, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		log.Printf("catalog refresh failed account=%s provider=%s stage=status status=%d body=%q", item.ID, item.Provider, resp.StatusCode, strings.TrimSpace(string(body)))
		return
	}
	var parsed struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		log.Printf("catalog refresh failed account=%s provider=%s stage=decode: %v", item.ID, item.Provider, err)
		return
	}
	m.pool.MergeModels(item.ID, catalogIDs(parsed.Data, nil))
}

func (m *Manager) fetchProviderModels(ctx context.Context, item Item) {
	if m.providers == nil {
		return
	}
	adapter, ok := m.providers.Get(item.Provider)
	if !ok || adapter.Models == nil {
		return
	}
	models, err := adapter.Models.Models(ctx, item.ID)
	if err != nil {
		log.Printf("catalog fetch failed account=%s provider=%s: %v", item.ID, item.Provider, err)
		return
	}
	ids := make([]string, 0, len(models)*2)
	for _, model := range models {
		ids = append(ids, model.PublicModel, model.NativeModel, model.DisplayName)
	}
	m.pool.MergeModels(item.ID, ids)
}

func catalogIDs(entries []map[string]any, extras []string) []string {
	ids := append([]string{}, extras...)
	for _, entry := range entries {
		for _, key := range []string{"id", "mapped_key", "native_model", "display_name"} {
			value, _ := entry[key].(string)
			if strings.TrimSpace(value) != "" {
				ids = append(ids, value)
			}
		}
	}
	return ids
}

// fetchQuota pulls the account quota snapshot from the worker daemon. Errors
// only clear the displayed quota; they are not surfaced as account errors.
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

func (m *Manager) watchAccount(id string, process ManagedProcess) {
	var exitErr error
	select {
	case exitErr = <-process.Done():
	case <-m.runCtx.Done():
		return
	}
	m.mu.Lock()
	if m.processes[id] != process {
		m.mu.Unlock()
		return
	}
	delete(m.processes, id)
	m.mu.Unlock()
	message := "account daemon exited"
	if exitErr != nil {
		message = exitErr.Error()
	}
	m.beginAccountRecovery(id, errors.New(message))
}

func (m *Manager) restartDelay(level int) time.Duration {
	if level <= 0 {
		level = 1
	}
	delay := m.config.RestartDelay
	for step := 1; step < level && delay < m.config.RestartMaxDelay; step++ {
		if delay > m.config.RestartMaxDelay/2 {
			delay = m.config.RestartMaxDelay
			break
		}
		delay *= 2
	}
	if delay > m.config.RestartMaxDelay {
		return m.config.RestartMaxDelay
	}
	return delay
}

// alreadyCheckedIn is implemented by workbuddy.AlreadyCheckedInError without
// importing that package (accounts <-> workbuddy would cycle).
type alreadyCheckedIn interface {
	AlreadyCheckedIn() bool
}

// CheckinAccount runs one daily check-in for the account's provider, records
// display fields, and refreshes credits. Failures never write chat cooldown.
func (m *Manager) CheckinAccount(ctx context.Context, accountID string) (Account, error) {
	if m == nil {
		return Account{}, errManagerClosed
	}
	account, err := m.store.Get(ctx, accountID)
	if err != nil {
		return Account{}, err
	}
	ops, ok := m.maintainerFor(account.Provider)
	if !ok {
		return account, fmt.Errorf("check-in is not available for %s accounts", account.Provider)
	}
	if checkedInLocalDay(account.LastCheckinAt, account.LastCheckinStatus, time.Now()) {
		m.fetchProviderQuotaFor(ctx, accountID, account.Provider)
		return m.store.Get(ctx, accountID)
	}
	msg, checkErr := ops.DailyCheckin(ctx, accountID)
	if msg == "" && checkErr != nil {
		msg = checkErr.Error()
	}
	if msg == "" {
		msg = "ok"
	}
	status := "success"
	var already alreadyCheckedIn
	if checkErr != nil {
		status = "error"
		if errors.As(checkErr, &already) && already.AlreadyCheckedIn() {
			status = "already"
		}
	}
	_ = m.store.RecordCheckin(ctx, accountID, status, msg, time.Now().UTC())
	m.fetchProviderQuotaFor(ctx, accountID, account.Provider)
	account, getErr := m.store.Get(ctx, accountID)
	if getErr != nil {
		return account, getErr
	}
	if checkErr == nil || status == "already" {
		return account, nil
	}
	return account, checkErr
}

// CheckinOptedIn runs check-in for every enabled account whose provider has
// check-in ops and that opted in. Cooldown accounts are included; disabled skip.
func (m *Manager) CheckinOptedIn(ctx context.Context) {
	m.checkinOptedIn(ctx, time.Now(), "", false)
}

func (m *Manager) checkinOptedIn(ctx context.Context, now time.Time, scheduledTime string, retryDue bool) {
	if m == nil {
		return
	}
	accounts, err := m.store.List(ctx)
	if err != nil {
		log.Printf("checkin list: %v", err)
		return
	}
	for _, account := range accounts {
		if !account.Enabled || !account.WorkBuddyAutoCheckin {
			continue
		}
		if _, ok := m.maintainerFor(account.Provider); !ok {
			continue
		}
		if scheduledTime != "" {
			if retryDue {
				if account.WorkBuddyCheckinTime == scheduledTime || !workBuddyCheckinDue(account.WorkBuddyCheckinTime, now) {
					continue
				}
			} else if account.WorkBuddyCheckinTime != scheduledTime {
				continue
			}
		}
		if checkedInLocalDay(account.LastCheckinAt, account.LastCheckinStatus, now) {
			continue
		}
		if _, err := m.CheckinAccount(ctx, account.ID); err != nil {
			log.Printf("checkin account_id=%s provider=%s op=checkin err=%v", account.ID, account.Provider, err)
		}
	}
}

func workBuddyCheckinDue(value string, now time.Time) bool {
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		parsed, _ = time.Parse("15:04", DefaultWorkBuddyCheckinTime)
	}
	due := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, now.Location())
	return !due.After(now)
}

// checkedInLocalDay is true when the last recorded check-in is success or
// already on the process-local calendar day. Error rows do not skip, so the
// evening slot can retry a morning miss.
func checkedInLocalDay(at, status string, now time.Time) bool {
	switch strings.TrimSpace(status) {
	case "success", "already":
	default:
		return false
	}
	raw := strings.TrimSpace(at)
	if raw == "" {
		return false
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return false
		}
	}
	loc := now.Location()
	localAt := parsed.In(loc)
	localNow := now.In(loc)
	return localAt.Year() == localNow.Year() && localAt.YearDay() == localNow.YearDay()
}

// KeepaliveAccounts refreshes tokens for enabled accounts whose provider has
// keepalive ops. When onlyOptIn is true, only auto-checkin accounts are touched
// (scheduled path). Manual/batch keepalive can pass false.
func (m *Manager) KeepaliveAccounts(ctx context.Context, onlyOptIn bool) {
	if m == nil {
		return
	}
	accounts, err := m.store.List(ctx)
	if err != nil {
		log.Printf("keepalive list: %v", err)
		return
	}
	for _, account := range accounts {
		if !account.Enabled {
			continue
		}
		if onlyOptIn && !account.WorkBuddyAutoCheckin {
			continue
		}
		ops, ok := m.maintainerFor(account.Provider)
		if !ok {
			continue
		}
		if err := ops.Keepalive(ctx, account.ID); err != nil {
			log.Printf("keepalive account_id=%s provider=%s op=keepalive err=%v", account.ID, account.Provider, err)
		}
	}
}

// RunMaintenanceLoop fires each opted-in account at its configured local time,
// retries due failures near 21:00, and keeps tokens alive near 22:00. Stop by
// closing stop.
func (m *Manager) RunMaintenanceLoop(stop <-chan struct{}) {
	if m == nil {
		return
	}
	for {
		accounts, err := m.store.List(context.Background())
		if err != nil {
			log.Printf("maintenance schedule list: %v", err)
		}
		delay, fire := nextCheckinFire(time.Now(), accounts)
		if delay > time.Minute {
			delay = time.Minute
			fire = checkinFire{}
		}
		timer := time.NewTimer(delay)
		select {
		case <-stop:
			timer.Stop()
			return
		case <-m.runCtx.Done():
			timer.Stop()
			return
		case <-timer.C:
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			now := time.Now()
			for _, scheduledTime := range fire.checkinTimes {
				m.checkinOptedIn(ctx, now, scheduledTime, false)
			}
			if fire.retry {
				m.checkinOptedIn(ctx, now, "21:00", true)
			}
			if fire.keepalive {
				m.KeepaliveAccounts(ctx, true)
			}
			cancel()
		}
	}
}

type checkinFire struct {
	checkinTimes []string
	retry        bool
	keepalive    bool
}

func nextCheckinFire(now time.Time, accounts []Account) (time.Duration, checkinFire) {
	type slot struct {
		time string
		kind string
	}
	slots := []slot{{"21:00", "retry"}, {"22:00", "keepalive"}}
	seen := map[string]bool{}
	for _, account := range accounts {
		if !account.Enabled || !account.WorkBuddyAutoCheckin {
			continue
		}
		checkinTime, err := NormalizeWorkBuddyCheckinTime(account.WorkBuddyCheckinTime)
		if err != nil || seen[checkinTime] {
			continue
		}
		seen[checkinTime] = true
		slots = append(slots, slot{checkinTime, "checkin"})
	}
	loc := now.Location()
	var best time.Time
	var fire checkinFire
	for _, slot := range slots {
		parsed, _ := time.Parse("15:04", slot.time)
		candidate := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, loc)
		candidate = candidate.Add(time.Duration(candidate.Unix()%15) * time.Minute)
		if !candidate.After(now) {
			candidate = candidate.Add(24 * time.Hour)
		}
		if best.IsZero() || candidate.Before(best) {
			best = candidate
			fire = checkinFire{}
		}
		if !candidate.Equal(best) {
			continue
		}
		switch slot.kind {
		case "checkin":
			fire.checkinTimes = append(fire.checkinTimes, slot.time)
		case "retry":
			fire.retry = true
		case "keepalive":
			fire.keepalive = true
		}
	}
	return best.Sub(now), fire
}
