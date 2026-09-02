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
)

type ManagerConfig struct {
	DataDir        string
	BasePort       int
	NodeBinary     string
	DaemonPath     string
	QoderCLIPath   string
	QoderCNCLIPath string
	TemplatePath   string
	ProxyAPIKey    string
	MaxLogWriters  io.Writer
	RestartDelay   time.Duration
}

type ManagedProcess interface {
	URL() string
	Done() <-chan error
	Stop() error
}

type ProcessStarter interface {
	Start(context.Context, Account, string, int) (ManagedProcess, error)
}

// WorkBuddyMaintainer is the Phase N ops surface. Implemented by
// workbuddy.Client; kept narrow so accounts does not grow a generic
// check-in capability on AccountProber.
type WorkBuddyMaintainer interface {
	DailyCheckin(ctx context.Context, accountID string) (string, error)
	Keepalive(ctx context.Context, accountID string) error
}

type Manager struct {
	config     ManagerConfig
	store      *Store
	starter    ProcessStarter
	pool       *Pool
	providers  *providers.Registry
	workbuddy  WorkBuddyMaintainer
	mu         sync.Mutex
	processes  map[string]ManagedProcess
	restarts   map[string]int
	nextPort   int
	httpClient *http.Client
	runCtx     context.Context
	cancel     context.CancelFunc
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
}

func NewManager(config ManagerConfig, store *Store, starter ProcessStarter) *Manager {
	if config.BasePort <= 0 {
		config.BasePort = 32100
	}
	if config.RestartDelay <= 0 {
		config.RestartDelay = time.Second
	}
	if starter == nil {
		starter = ExecStarter{Config: config}
	}
	runCtx, cancel := context.WithCancel(context.Background())
	manager := &Manager{
		config:            config,
		store:             store,
		starter:           starter,
		pool:              NewPool(nil, nil),
		processes:         map[string]ManagedProcess{},
		restarts:          map[string]int{},
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
		if err := m.startAccount(ctx, account); err != nil {
			// Mark the failure in memory without triggering the observer: a
			// boot-time MarkDown(0) used to enqueue SaveCooldowns, whose
			// empty in-memory cooldown set DELETEd any persisted cooldowns
			// for this account — so restoreCooldowns below had nothing to
			// reload and a rate-limited account walked straight back into
			// rotation after a restart that failed to start it. MergeHealth
			// records the error state without persisting, leaving SQLite
			// intact for restoreCooldowns.
			m.pool.MergeHealth(account.ID, false, false, 0, m.restarts[account.ID], err.Error())
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
	if starter, ok := m.starter.(ExecStarter); ok {
		starter.Config.ProxyAPIKey = key
		m.starter = starter
	}
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
	for _, account := range accounts {
		if err := m.stopAccount(account.ID); err != nil {
			return err
		}
		if err := m.startAccount(ctx, account); err != nil {
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

// SetWorkBuddy wires Phase N check-in / keepalive without a second scheduler package.
func (m *Manager) SetWorkBuddy(ops WorkBuddyMaintainer) {
	if m == nil {
		return
	}
	m.workbuddy = ops
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
	m.cancel()
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
		m.pool.Upsert(Item{
			ID: account.ID, Provider: descriptor.ID, Region: account.ProviderRegion,
			Runtime: string(descriptor.Runtime), DropSystemPrompt: account.DropSystemPrompt,
			Weight: NormalizeWeight(account.Priority), MaxInFlight: account.MaxInFlight,
		})
		return nil
	}
	m.mu.Lock()
	if _, exists := m.processes[account.ID]; exists {
		m.mu.Unlock()
		return nil
	}
	port := m.nextPort
	m.nextPort++
	m.mu.Unlock()

	home := filepath.Join(m.config.DataDir, "runtime", account.ID)
	if err := materializeHome(ctx, m.store, account, home); err != nil {
		return err
	}
	process, err := m.starter.Start(ctx, account, home, port)
	if err != nil {
		return fmt.Errorf("start account %s: %w", account.ID, err)
	}
	m.mu.Lock()
	m.processes[account.ID] = process
	m.mu.Unlock()
	m.mu.Lock()
	restarts := m.restarts[account.ID]
	m.mu.Unlock()
	m.pool.Upsert(Item{
		ID: account.ID, URL: process.URL(), Provider: descriptor.ID,
		Region: account.ProviderRegion, Runtime: string(descriptor.Runtime), Restarts: restarts,
		Weight: NormalizeWeight(account.Priority), MaxInFlight: account.MaxInFlight,
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
	Config ManagerConfig
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

func (s ExecStarter) Start(_ context.Context, account Account, home string, port int) (ManagedProcess, error) {
	node := s.Config.NodeBinary
	if node == "" {
		node = "node"
	}
	if s.Config.DaemonPath == "" {
		return nil, fmt.Errorf("worker daemon path required")
	}
	cliPath, site, configDir, configEnv, err := qoderRuntimeSpec(s.Config, account, home)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(node, s.Config.DaemonPath)
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"QODER_HOME="+configDir,
		configEnv+"="+configDir,
		"QODER_SITE="+site,
		"QODER_ACCOUNT_ID="+account.ID,
		"QODER_MAX_INFLIGHT="+strconv.Itoa(account.MaxInFlight),
		"WORKER_HOST=127.0.0.1",
		"WORKER_PORT="+strconv.Itoa(port),
		"PROXY_API_KEY="+s.Config.ProxyAPIKey,
		"QODERCLI_JS="+cliPath,
		"PLAIN_TEMPLATE_PATH="+s.Config.TemplatePath,
		"QODER_WARMUP_CWD="+filepath.Join(home, "work"),
	)
	writer := s.Config.MaxLogWriters
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

func (m *Manager) Create(ctx context.Context, input CreateAccount) (Account, error) {
	account, err := m.store.Create(ctx, input)
	if err != nil {
		return Account{}, err
	}
	if account.Enabled {
		if err := m.startAccount(ctx, account); err != nil {
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
	// Request sanitization applies per request, so sync it into the pool
	// without restarting anything.
	if before.DropSystemPrompt != after.DropSystemPrompt {
		m.pool.SetDropSystemPrompt(id, after.DropSystemPrompt)
	}
	if before.Enabled && !after.Enabled {
		return m.stopAccount(id)
	}
	if !before.Enabled && after.Enabled {
		return m.startAccount(ctx, after)
	}
	if before.Enabled && after.Enabled && before.MaxInFlight != after.MaxInFlight {
		if err := m.stopAccount(id); err != nil {
			return err
		}
		return m.startAccount(ctx, after)
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

type ImportAccount struct {
	Name                 string
	Provider             string
	Region               string
	Enabled              bool
	MaxInFlight          int
	Priority             int
	DropSystemPrompt     *bool
	WorkBuddyAutoCheckin *bool
	Credential           NativeCredential
}

type AccountView struct {
	Account
	Ready          bool              `json:"ready"`
	Hot            bool              `json:"hot"`
	InFlight       int               `json:"in_flight"`
	Restarts       int               `json:"restarts"`
	DownUntil      string            `json:"down_until,omitempty"`
	ModelCooldowns map[string]string `json:"model_cooldowns,omitempty"`
	Quota          *QuotaSnapshot    `json:"quota,omitempty"`
}

func (m *Manager) Import(ctx context.Context, input ImportAccount) (Account, error) {
	account, err := m.store.Create(ctx, CreateAccount{
		Name: input.Name, Provider: input.Provider, Region: input.Region, Enabled: false,
		MaxInFlight: input.MaxInFlight, Priority: input.Priority, DropSystemPrompt: input.DropSystemPrompt,
		WorkBuddyAutoCheckin: input.WorkBuddyAutoCheckin,
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
		if err := m.startAccount(ctx, account); err != nil {
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
	view := AccountView{Account: account}
	if item, ok := m.pool.ByID(account.ID); ok {
		view.Ready = item.Ready == nil || *item.Ready
		view.Hot = item.Hot != nil && *item.Hot
		view.InFlight = item.InFlight
		view.Restarts = item.Restarts
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
	return view, nil
}

func (m *Manager) Accounts(ctx context.Context) ([]AccountView, error) {
	stored, err := m.store.List(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]AccountView, 0, len(stored))
	for _, account := range stored {
		view := AccountView{Account: account}
		if item, ok := m.pool.ByID(account.ID); ok {
			view.Ready = item.Ready == nil || *item.Ready
			view.Hot = item.Hot != nil && *item.Hot
			view.InFlight = item.InFlight
			view.Restarts = item.Restarts
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
	status := "login_required"
	if ready || health.Hot {
		status = "ready"
	} else if health.LastError != "" {
		status = "error"
	}
	if err := m.store.Observe(ctx, item.ID, health.UID, status, health.LastError, ""); err != nil {
		return err
	}
	// Quota is display-only: fetch after the health/observe path so a quota
	// outage never flips account readiness or scheduling state.
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
		m.fetchProviderQuota(ctx, item.ID, adapter.Prober)
		m.fetchAccountModels(ctx, item)
	}
	return nil
}

func (m *Manager) fetchProviderQuota(ctx context.Context, accountID string, prober providers.AccountProber) {
	if prober == nil {
		return
	}
	info, err := prober.Quota(ctx, accountID)
	if err != nil || info == nil {
		m.pool.MergeQuota(accountID, nil)
		return
	}
	unit := info.Unit
	if unit == "" {
		unit = "credits"
	}
	m.pool.MergeQuota(accountID, &QuotaSnapshot{
		Used:       info.Used,
		Total:      info.Total,
		Remaining:  info.Remaining,
		Percentage: info.Percentage,
		Unit:       unit,
		Exceeded:   info.Exceeded,
		FetchedAt:  info.FetchedAt,
	})
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
		return
	}
	if m.config.ProxyAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+m.config.ProxyAPIKey)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return
	}
	var parsed struct {
		Data []map[string]any `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&parsed) != nil {
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
	m.pool.MergeQuota(accountID, payload.Quota.snapshot())
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
		snapshot.AddOnUnit = w.AddOnQuota.Unit
		if snapshot.AddOnUnit == "" {
			snapshot.AddOnUnit = "credits"
		}
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
	m.restarts[id]++
	m.mu.Unlock()
	m.pool.Remove(id)
	message := "account daemon exited"
	if exitErr != nil {
		message = exitErr.Error()
	}
	_ = m.store.Observe(context.Background(), id, "", "error", message, KindUnavailable)
	select {
	case <-time.After(m.config.RestartDelay):
	case <-m.runCtx.Done():
		return
	}
	account, err := m.store.Get(context.Background(), id)
	if err != nil || !account.Enabled {
		return
	}
	_ = m.startAccount(context.Background(), account)
}

// alreadyCheckedIn is implemented by workbuddy.AlreadyCheckedInError without
// importing that package (accounts <-> workbuddy would cycle).
type alreadyCheckedIn interface {
	AlreadyCheckedIn() bool
}

// CheckinAccount runs one WorkBuddy daily-checkin, records display fields, and
// refreshes credits. Failures never write chat cooldown.
func (m *Manager) CheckinAccount(ctx context.Context, accountID string) (Account, error) {
	if m == nil || m.workbuddy == nil {
		return Account{}, fmt.Errorf("workbuddy maintainer not configured")
	}
	account, err := m.store.Get(ctx, accountID)
	if err != nil {
		return Account{}, err
	}
	if account.Provider != "workbuddy" {
		return account, fmt.Errorf("check-in is only available for WorkBuddy accounts")
	}
	msg, checkErr := m.workbuddy.DailyCheckin(ctx, accountID)
	if msg == "" && checkErr != nil {
		msg = checkErr.Error()
	}
	if msg == "" {
		msg = "ok"
	}
	_ = m.store.RecordCheckin(ctx, accountID, msg, time.Now().UTC())
	if adapter, ok := m.providers.Get("workbuddy"); ok && adapter.Prober != nil {
		m.fetchProviderQuota(ctx, accountID, adapter.Prober)
	}
	account, getErr := m.store.Get(ctx, accountID)
	if getErr != nil {
		return account, getErr
	}
	if checkErr == nil {
		return account, nil
	}
	var already alreadyCheckedIn
	if errors.As(checkErr, &already) && already.AlreadyCheckedIn() {
		return account, nil
	}
	return account, checkErr
}

// CheckinOptedIn runs check-in for every enabled WorkBuddy account with
// workbuddy_auto_checkin on. Cooldown accounts are included; disabled skip.
func (m *Manager) CheckinOptedIn(ctx context.Context) {
	if m == nil || m.workbuddy == nil {
		return
	}
	accounts, err := m.store.List(ctx)
	if err != nil {
		log.Printf("workbuddy checkin list: %v", err)
		return
	}
	for _, account := range accounts {
		if account.Provider != "workbuddy" || !account.Enabled || !account.WorkBuddyAutoCheckin {
			continue
		}
		if _, err := m.CheckinAccount(ctx, account.ID); err != nil {
			log.Printf("workbuddy checkin account_id=%s op=checkin err=%v", account.ID, err)
		}
	}
}

// KeepaliveWorkBuddy refreshes tokens for enabled WorkBuddy accounts.
// When onlyOptIn is true, only auto-checkin accounts are touched (scheduled
// path). Manual/batch keepalive can pass false.
func (m *Manager) KeepaliveWorkBuddy(ctx context.Context, onlyOptIn bool) {
	if m == nil || m.workbuddy == nil {
		return
	}
	accounts, err := m.store.List(ctx)
	if err != nil {
		log.Printf("workbuddy keepalive list: %v", err)
		return
	}
	for _, account := range accounts {
		if account.Provider != "workbuddy" || !account.Enabled {
			continue
		}
		if onlyOptIn && !account.WorkBuddyAutoCheckin {
			continue
		}
		if err := m.workbuddy.Keepalive(ctx, account.ID); err != nil {
			log.Printf("workbuddy keepalive account_id=%s op=keepalive err=%v", account.ID, err)
		}
	}
}

// RunWorkBuddyMaintenanceLoop fires check-in near 09:00/21:00 and keepalive
// near 22:00 in the process local zone, with minute jitter. Stop by closing stop.
func (m *Manager) RunWorkBuddyMaintenanceLoop(stop <-chan struct{}) {
	if m == nil {
		return
	}
	for {
		delay, kind := nextWorkBuddyFire(time.Now())
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
			switch kind {
			case "checkin":
				m.CheckinOptedIn(ctx)
			case "keepalive":
				m.KeepaliveWorkBuddy(ctx, true)
			}
			cancel()
		}
	}
}

func nextWorkBuddyFire(now time.Time) (time.Duration, string) {
	type slot struct {
		hour int
		kind string
	}
	slots := []slot{{9, "checkin"}, {21, "checkin"}, {22, "keepalive"}}
	loc := now.Location()
	var best time.Time
	var bestKind string
	for _, slot := range slots {
		candidate := time.Date(now.Year(), now.Month(), now.Day(), slot.hour, 0, 0, 0, loc)
		if !candidate.After(now) {
			candidate = candidate.Add(24 * time.Hour)
		}
		// Minute jitter 0–14 keeps multi-account fleets off the exact hour.
		jitter := time.Duration(candidate.UnixNano()%15) * time.Minute
		candidate = candidate.Add(jitter)
		if bestKind == "" || candidate.Before(best) {
			best = candidate
			bestKind = slot.kind
		}
	}
	return best.Sub(now), bestKind
}
