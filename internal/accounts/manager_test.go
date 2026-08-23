package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeProcess struct {
	url     string
	stopped bool
	done    chan error
}

func (p *fakeProcess) URL() string        { return p.url }
func (p *fakeProcess) Done() <-chan error { return p.done }
func (p *fakeProcess) Stop() error {
	p.stopped = true
	select {
	case p.done <- nil:
	default:
	}
	return nil
}

type fakeStarter struct {
	accounts []Account
	homes    []string
	started  chan *fakeProcess
}

func (s *fakeStarter) Start(_ context.Context, account Account, home string, port int) (ManagedProcess, error) {
	s.accounts = append(s.accounts, account)
	s.homes = append(s.homes, home)
	process := &fakeProcess{url: "http://127.0.0.1:" + itoa(port), done: make(chan error, 1)}
	if s.started != nil {
		s.started <- process
	}
	return process, nil
}

func TestManagerStartsEnabledAccountsAndMaterializesCredentials(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	store, err := OpenStore(filepath.Join(dataDir, "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{Name: "Work", Enabled: true, MaxInFlight: 3})
	if err != nil {
		t.Fatal(err)
	}
	credential := NativeCredential{UserBlob: []byte("ciphertext"), MachineID: "machine-1"}
	if err := store.SaveCredential(ctx, account.ID, "native", credential); err != nil {
		t.Fatal(err)
	}
	starter := &fakeStarter{}
	manager := NewManager(ManagerConfig{DataDir: dataDir, BasePort: 32100}, store, starter)
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	if len(starter.accounts) != 1 || starter.accounts[0].ID != account.ID {
		t.Fatalf("started accounts = %+v", starter.accounts)
	}
	home := starter.homes[0]
	userBlob, err := os.ReadFile(filepath.Join(home, ".qoder", ".auth", "user"))
	if err != nil {
		t.Fatal(err)
	}
	machineID, err := os.ReadFile(filepath.Join(home, ".qoder", ".auth", "machine_id"))
	if err != nil {
		t.Fatal(err)
	}
	if string(userBlob) != "ciphertext" || string(machineID) != "machine-1" {
		t.Fatalf("materialized user=%q machine=%q", userBlob, machineID)
	}
	picked, ok := manager.Pool().Pick("", nil)
	if !ok || picked.ID != account.ID || picked.URL != "http://127.0.0.1:32100" {
		t.Fatalf("picked = %+v ok=%v", picked, ok)
	}
}

func TestManagerCreatesDisablesAndDeletesAccount(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	store, err := OpenStore(filepath.Join(dataDir, "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	starter := &fakeStarter{}
	manager := NewManager(ManagerConfig{DataDir: dataDir, BasePort: 32200}, store, starter)

	account, err := manager.Create(ctx, CreateAccount{Name: "New", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := manager.Pool().ByID(account.ID); !ok {
		t.Fatal("created account was not added to pool")
	}
	process := manager.processes[account.ID].(*fakeProcess)
	if err := manager.Update(ctx, account.ID, UpdateAccount{Enabled: boolPtr(false)}); err != nil {
		t.Fatal(err)
	}
	if !process.stopped {
		t.Fatal("disabled account process was not stopped")
	}
	if _, ok := manager.Pool().ByID(account.ID); ok {
		t.Fatal("disabled account remained in pool")
	}
	if err := manager.Delete(ctx, account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, account.ID); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("deleted account error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "runtime", account.ID)); !os.IsNotExist(err) {
		t.Fatalf("runtime directory still exists: %v", err)
	}
}

func TestManagerSyncsCredentialWrittenByQoderCLI(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	store, err := OpenStore(filepath.Join(dataDir, "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	starter := &fakeStarter{}
	manager := NewManager(ManagerConfig{DataDir: dataDir}, store, starter)
	account, err := manager.Create(ctx, CreateAccount{Name: "OAuth", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	authDir := filepath.Join(starter.homes[0], ".qoder", ".auth")
	if err := os.WriteFile(filepath.Join(authDir, "user"), []byte("new-oauth-blob"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(authDir, "machine_id"), []byte("machine-oauth"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.SyncCredential(ctx, account.ID, "oauth"); err != nil {
		t.Fatal(err)
	}
	credential, err := store.LoadCredential(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(credential.UserBlob) != "new-oauth-blob" || credential.MachineID != "machine-oauth" {
		t.Fatalf("synced credential = %+v", credential)
	}
}

func TestManagerRefreshesHealthAndPersistsUID(t *testing.T) {
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "ready": true, "hot": true, "uid": "qoder-uid-1", "inFlight": 2,
		})
	}))
	defer worker.Close()
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{Name: "Health", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	manager.pool.Upsert(Item{ID: account.ID, URL: worker.URL})
	if err := manager.RefreshAll(ctx); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.RemoteUID != "qoder-uid-1" || updated.Status != "ready" {
		t.Fatalf("updated account = %+v", updated)
	}
	item, _ := manager.pool.ByID(account.ID)
	if item.Hot == nil || !*item.Hot || item.InFlight != 2 {
		t.Fatalf("pool health = %+v", item)
	}
}

func TestImportLegacyHomeCreatesFirstAccountOnlyOnce(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	legacyHome := t.TempDir()
	authDir := filepath.Join(legacyHome, ".auth")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(authDir, "user"), []byte("legacy-user"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(authDir, "machine_id"), []byte("legacy-machine"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(filepath.Join(dataDir, "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	first, err := ImportLegacyHome(ctx, store, legacyHome)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ImportLegacyHome(ctx, store, legacyHome)
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || second != nil {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	items, _ := store.List(ctx)
	if len(items) != 1 || items[0].AuthType != "oauth" || !items[0].Enabled {
		t.Fatalf("legacy accounts = %+v", items)
	}
}

func TestManagerRestartsUnexpectedlyExitedEnabledAccount(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.Create(ctx, CreateAccount{Name: "Restart", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	starter := &fakeStarter{started: make(chan *fakeProcess, 2)}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir(), RestartDelay: time.Millisecond}, store, starter)
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	first := <-starter.started
	first.done <- errors.New("crashed")
	select {
	case second := <-starter.started:
		if second == first {
			t.Fatal("manager reused exited process")
		}
		item, ok := manager.pool.ByID(starter.accounts[0].ID)
		if !ok || item.Restarts != 1 {
			t.Fatalf("restart state = %+v ok=%v", item, ok)
		}
	case <-time.After(time.Second):
		t.Fatal("account process was not restarted")
	}
}

func TestManagerPersistsSchedulerCooldown(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{Name: "Cooldown", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	manager.pool.Upsert(Item{ID: account.ID, URL: "http://127.0.0.1:1"})
	manager.pool.MarkClassified(account.ID, Classified{Kind: KindRateLimit, Message: "429", Cooldown: time.Minute, Failover: true})
	updated, err := store.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastErrorKind != KindRateLimit || updated.LastError != "429" || updated.CooldownUntil == nil {
		t.Fatalf("persisted account = %+v", updated)
	}
}
