package accounts

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
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

// WorkBuddyMaintainer is the Phase N ops surface. Implemented by
// workbuddy.Client; kept narrow so accounts does not grow a generic
// check-in capability on AccountProber.
type WorkBuddyMaintainer interface {
	DailyCheckin(ctx context.Context, accountID string) (string, error)
	Keepalive(ctx context.Context, accountID string) error
}

type Manager struct {
	config         ManagerConfig
	store          *Store
	starter        ProcessStarter
	pool           *Pool
	providers      *providers.Registry
	workbuddy      WorkBuddyMaintainer
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

const persistRetryBackoff = 500 * time.Millisecond
