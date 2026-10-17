package runtime

import (
	"context"
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
// unlistened port) must be stopped and handed to recovery after the liveness
// threshold of consecutive probe failures. This is the watchdog path for a
// worker that stays "alive" as a process but stops serving.
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

	// Threshold consecutive failures must trigger stopAccount. The account's
	// /health URL is not serving, so every probe fails on transport.
	for i := 1; i <= 3; i++ {
		manager.probeWorkerLiveness(ctx)
	}
	if !proc.stopped {
		t.Fatalf("want stuck worker stopped after %d probe failures, got stopped=%v", 3, proc.stopped)
	}
	// The failure counter is cleared once stop is issued so recovery does not
	// inherit a stale count.
	manager.mu.Lock()
	got := manager.healthFailures[account.ID]
	manager.mu.Unlock()
	if got != 0 {
		t.Fatalf("healthFailures after stop = %d, want 0", got)
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
