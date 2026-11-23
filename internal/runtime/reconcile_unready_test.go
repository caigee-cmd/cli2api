package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	sqlstore "github.com/caigee-cmd/cli2api/internal/store"
)

// fixedURLStarter spawns a fake worker whose /health is served by the given
// test server, so the liveness probe and refreshOne can be pointed at a worker
// that answers without a real child process.
type fixedURLStarter struct {
	url     string
	started chan *livenessProcess
}

func (s *fixedURLStarter) Start(_ context.Context, _ accounts.Account, _ string, _ int) (ManagedProcess, error) {
	p := &livenessProcess{url: s.url, done: make(chan error, 1)}
	s.started <- p
	return p, nil
}

// workerFixture boots a manager owning one enabled worker account backed by a
// fake /health server. healthHits counts the /health round-trips the account
// receives, which is how the throttle test observes whether a probe happened.
func workerFixture(t *testing.T, healthBody string, healthHits *int32) (*Manager, *sqlstore.Store, accounts.Account, context.Context) {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()
	store, err := sqlstore.OpenStore(filepath.Join(dataDir, "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	account, err := store.Create(ctx, accounts.CreateAccount{Name: "restarted", Enabled: true, MaxInFlight: 3})
	if err != nil {
		t.Fatal(err)
	}
	credential := accounts.NativeCredential{UserBlob: []byte("ciphertext"), MachineID: "machine-1"}
	if err := store.SaveCredential(ctx, account.ID, "native", credential); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		atomic.AddInt32(healthHits, 1)
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(healthBody))
	}))
	t.Cleanup(srv.Close)

	starter := &fixedURLStarter{url: srv.URL, started: make(chan *livenessProcess, 4)}
	manager := NewManager(ManagerConfig{
		DataDir:      dataDir,
		BasePort:     32700,
		RestartDelay: 30 * time.Millisecond,
	}, store, starter)
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close() })

	select {
	case <-starter.started:
	case <-time.After(2 * time.Second):
		t.Fatal("starter was not invoked")
	}
	return manager, store, account, ctx
}

// The boot-time RefreshAll probes every worker the moment the manager starts.
// A worker that has not bound its port yet fails that probe with a transport
// error, refreshOne persists status=error, and nothing else ever talks to the
// account — so the whole provider stays unrouteable even after the worker comes
// up seconds later. The liveness watchdog must notice the worker answering
// /health and promote it back, without an operator clicking Refresh.
func TestProbeWorkerLivenessPromotesAliveButUnreadyWorker(t *testing.T) {
	var hits int32
	manager, store, account, ctx := workerFixture(t, `{"ok":true,"ready":true,"hot":true,"inFlight":0}`, &hits)

	// Reproduce the persisted outcome of the boot race: the probe failed on
	// transport before the worker listened, so the pool item is unready and the
	// store carries status=error.
	manager.pool.MergeHealth(account.ID, false, false, 0, 0,
		`Get "http://127.0.0.1:32700/health": dial tcp 127.0.0.1:32700: connect: connection refused`)
	if err := store.Observe(ctx, account.ID, "", "error", "connection refused", accounts.KindUnavailable); err != nil {
		t.Fatal(err)
	}
	before, ok := manager.pool.ByID(account.ID)
	if !ok {
		t.Fatal("account missing from the pool after Start")
	}
	if before.Ready != nil && *before.Ready {
		t.Fatal("fixture must start unready to model the boot race")
	}

	manager.probeWorkerLiveness(ctx)

	after, ok := manager.pool.ByID(account.ID)
	if !ok {
		t.Fatal("account disappeared from the pool")
	}
	if after.Ready == nil || !*after.Ready {
		t.Fatalf("worker answers /health but is still not routable: %+v", after)
	}
	if after.RuntimeState != "ready" {
		t.Fatalf("runtime state = %q, want ready", after.RuntimeState)
	}
	stored, err := store.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "ready" {
		t.Fatalf("stored status = %q, want ready", stored.Status)
	}
}

// A worker that is alive but legitimately unready (waiting for login, still
// warming) is re-probed at most once per reconcile window, so the 30s
// maintenance tick cannot turn into a refresh loop against the worker.
func TestReconcileUnreadyWorkerThrottlesRepeatProbes(t *testing.T) {
	var hits int32
	manager, _, account, ctx := workerFixture(t, `{"ok":true,"ready":false,"hot":false,"hasAuthManager":true}`, &hits)

	item, ok := manager.pool.ByID(account.ID)
	if !ok {
		t.Fatal("account missing from the pool after Start")
	}
	manager.reconcileUnreadyWorker(ctx, item)
	first := atomic.LoadInt32(&hits)
	if first == 0 {
		t.Fatal("the first reconcile must probe the worker")
	}

	manager.reconcileUnreadyWorker(ctx, item)
	if got := atomic.LoadInt32(&hits); got != first {
		t.Fatalf("second reconcile re-probed the worker inside the window: /health hits %d -> %d", first, got)
	}
}

// A removed account never gets another probe that could clear its watchdog
// entries, so RemoveAccount must drop them; otherwise both maps grow with every
// account deletion.
func TestRemoveAccountDropsWatchdogState(t *testing.T) {
	manager, _, account, _ := workerFixture(t, `{"ok":true,"ready":true}`, new(int32))

	manager.mu.Lock()
	manager.healthFailures[account.ID] = 2
	manager.reconcileAt[account.ID] = time.Now()
	manager.mu.Unlock()

	if err := manager.RemoveAccount(account.ID); err != nil {
		t.Fatal(err)
	}

	manager.mu.Lock()
	defer manager.mu.Unlock()
	if _, ok := manager.healthFailures[account.ID]; ok {
		t.Fatal("healthFailures kept an entry for a removed account")
	}
	if _, ok := manager.reconcileAt[account.ID]; ok {
		t.Fatal("reconcileAt kept an entry for a removed account")
	}
}
