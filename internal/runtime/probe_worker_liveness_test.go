package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	sqlstore "github.com/caigee-cmd/cli2api/internal/store"
)

// livenessProcess is a minimal ManagedProcess for the watchdog test: its URL
// points at an unlistened port so probeHealthOnly's /health round-trip fails on
// transport, and Stop records that it was invoked.
type livenessProcess struct {
	url     string
	stopped bool
	done    chan error
}

func (p *livenessProcess) URL() string        { return p.url }
func (p *livenessProcess) Done() <-chan error { return p.done }
func (p *livenessProcess) PID() int           { return 0 }
func (p *livenessProcess) Stop() error {
	p.stopped = true
	select {
	case p.done <- nil:
	default:
	}
	return nil
}

type livenessStarter struct {
	started chan *livenessProcess
}

func (s *livenessStarter) Start(_ context.Context, _ accounts.Account, _ string, port int) (ManagedProcess, error) {
	p := &livenessProcess{
		url:  "http://127.0.0.1:" + strconv.Itoa(port),
		done: make(chan error, 1),
	}
	s.started <- p
	return p, nil
}

// A worker whose /health never answers (the starter URL points at an
// unlistened port) must be stopped AND handed to recovery after the liveness
// threshold of consecutive probe failures. This is the watchdog path for a
// worker that stays "alive" as a process but stops serving; recovery must
// spawn a replacement process, otherwise the stuck account is permanently dead.
func TestProbeWorkerLivenessRestartsStuckWorker(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	store, err := sqlstore.OpenStore(filepath.Join(dataDir, "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, accounts.CreateAccount{Name: "stuck", Enabled: true, MaxInFlight: 3})
	if err != nil {
		t.Fatal(err)
	}
	credential := accounts.NativeCredential{UserBlob: []byte("ciphertext"), MachineID: "machine-1"}
	if err := store.SaveCredential(ctx, account.ID, "native", credential); err != nil {
		t.Fatal(err)
	}
	starter := &livenessStarter{started: make(chan *livenessProcess, 4)}
	manager := NewManager(ManagerConfig{
		DataDir:      dataDir,
		BasePort:     32500,
		RestartDelay: 50 * time.Millisecond,
	}, store, starter)
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	var proc *livenessProcess
	select {
	case proc = <-starter.started:
	case <-time.After(2 * time.Second):
		t.Fatal("starter was not invoked")
	}

	// Threshold consecutive failures must trigger restartStuckWorker. The account's
	// /health URL is not serving, so every probe fails on transport.
	for i := 1; i <= 3; i++ {
		manager.probeWorkerLiveness(ctx)
	}
	if !proc.stopped {
		t.Fatalf("want stuck worker stopped after %d probe failures, got stopped=%v", 3, proc.stopped)
	}
	// The failure counter is cleared once a restart is issued so recovery does
	// not inherit a stale count.
	manager.mu.Lock()
	got := manager.healthFailures[account.ID]
	manager.mu.Unlock()
	if got != 0 {
		t.Fatalf("healthFailures after stop = %d, want 0", got)
	}
	// Recovery must actually start a replacement process: the old bug deleted
	// m.processes[id] before Stop(), so watchAccount saw a mismatch and returned
	// without beginAccountRecovery, leaving the account permanently dead.
	select {
	case replacement := <-starter.started:
		if replacement == proc {
			t.Fatalf("recovery reused the stuck process instead of starting a new one")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog stopped the stuck worker but never restarted it")
	}
}

// A transient failure must not restart the worker: only the threshold-th
// consecutive failure triggers stop. Two failures leave the counter
// incremented and the process running.
func TestProbeWorkerLivenessToleratesTransientFailures(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	store, err := sqlstore.OpenStore(filepath.Join(dataDir, "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, accounts.CreateAccount{Name: "flaky", Enabled: true, MaxInFlight: 3})
	if err != nil {
		t.Fatal(err)
	}
	credential := accounts.NativeCredential{UserBlob: []byte("ciphertext"), MachineID: "machine-1"}
	if err := store.SaveCredential(ctx, account.ID, "native", credential); err != nil {
		t.Fatal(err)
	}
	starter := &livenessStarter{started: make(chan *livenessProcess, 4)}
	manager := NewManager(ManagerConfig{
		DataDir:  dataDir,
		BasePort: 32600,
	}, store, starter)
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	var proc *livenessProcess
	select {
	case proc = <-starter.started:
	case <-time.After(2 * time.Second):
		t.Fatal("starter was not invoked")
	}

	manager.probeWorkerLiveness(ctx)
	manager.probeWorkerLiveness(ctx)
	if proc.stopped {
		t.Fatalf("worker must not be stopped before threshold, got stopped=true")
	}
	manager.mu.Lock()
	got := manager.healthFailures[account.ID]
	manager.mu.Unlock()
	if got != 2 {
		t.Fatalf("healthFailures = %d, want 2", got)
	}
}

// A worker that answers /health with ok=true but ready=false is still alive:
// it is starting, rewarming, or waiting for login. The liveness probe must not
// treat that as a failure, or the watchdog would reboot a healthy-but-warming
// account every ~90s.
func TestProbeHealthOnlyTreatsWarmingWorkerAsAlive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"ready":false,"hot":false,"hasAuthManager":true}`))
	}))
	t.Cleanup(srv.Close)

	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, nil, nil)
	alive, err := manager.probeHealthOnly(context.Background(), Item{
		ID:      "warming",
		Runtime: "child_process",
		URL:     srv.URL,
	})
	if err != nil {
		t.Fatalf("probeHealthOnly: %v", err)
	}
	if !alive {
		t.Fatalf("warming worker (ok=true, ready=false) must be treated as alive")
	}
}

// A 200 that reports ok=false is an unhealthy daemon and must count as a
// failure so it can be rebooted.
func TestProbeHealthOnlyTreatsUnhealthyAsDead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"ready":false}`))
	}))
	t.Cleanup(srv.Close)

	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, nil, nil)
	alive, err := manager.probeHealthOnly(context.Background(), Item{
		ID:      "unhealthy",
		Runtime: "child_process",
		URL:     srv.URL,
	})
	if err != nil {
		t.Fatalf("probeHealthOnly: %v", err)
	}
	if alive {
		t.Fatalf("ok=false daemon must be treated as dead")
	}
}
