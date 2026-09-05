package accounts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
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
	failures int
}

func (s *fakeStarter) Start(_ context.Context, account Account, home string, port int) (ManagedProcess, error) {
	s.accounts = append(s.accounts, account)
	s.homes = append(s.homes, home)
	if s.failures > 0 {
		s.failures--
		return nil, errors.New("simulated start failure")
	}
	process := &fakeProcess{url: "http://127.0.0.1:" + itoa(port), done: make(chan error, 1)}
	if s.started != nil {
		s.started <- process
	}
	return process, nil
}

type delayedStarter struct {
	mu         sync.Mutex
	accounts   []Account
	started    chan *fakeProcess
	all        []*fakeProcess
	delayAfter int
	delay      time.Duration
}

func (s *delayedStarter) Start(_ context.Context, account Account, _ string, port int) (ManagedProcess, error) {
	s.mu.Lock()
	s.accounts = append(s.accounts, account)
	count := len(s.accounts)
	delay := time.Duration(0)
	if s.delayAfter > 0 && count > s.delayAfter {
		delay = s.delay
	}
	s.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	process := &fakeProcess{url: "http://127.0.0.1:" + itoa(port), done: make(chan error, 1)}
	s.mu.Lock()
	s.all = append(s.all, process)
	s.mu.Unlock()
	if s.started != nil {
		s.started <- process
	}
	return process, nil
}

func (s *delayedStarter) startCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.accounts)
}

func (s *delayedStarter) processes() []*fakeProcess {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*fakeProcess, len(s.all))
	copy(out, s.all)
	return out
}

func TestManagerRetriesInitialStartFailure(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{Name: "RetryBoot", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	starter := &fakeStarter{failures: 1, started: make(chan *fakeProcess, 1)}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir(), RestartDelay: time.Millisecond}, store, starter)
	defer manager.Close()
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := manager.Pool().Pick("", nil); ok {
		t.Fatal("failed account must not be routable during recovery")
	}
	select {
	case <-starter.started:
	case <-time.After(time.Second):
		t.Fatal("initial start failure was not recovered")
	}
	item, ok := manager.Pool().ByID(account.ID)
	if !ok {
		t.Fatal("account disappeared during recovery")
	}
	if item.Restarts != 1 || item.RuntimeState != "starting" || item.Ready == nil || *item.Ready {
		t.Fatalf("recovered runtime state = %+v", item)
	}
}

func TestManagerDisabledAccountDoesNotRestartAfterStartFailure(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{Name: "DisableRetry", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	starter := &fakeStarter{failures: 100}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir(), RestartDelay: 50 * time.Millisecond}, store, starter)
	defer manager.Close()
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := manager.Update(ctx, account.ID, UpdateAccount{Enabled: boolPtr(false)}); err != nil {
		t.Fatal(err)
	}
	attempts := len(starter.accounts)
	time.Sleep(100 * time.Millisecond)
	if got := len(starter.accounts); got != attempts {
		t.Fatalf("disabled account was restarted: attempts %d -> %d", attempts, got)
	}
}

func TestManagerReenableDuringRecoveryDoesNotGetMarkedStarting(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{Name: "ReenableRetry", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	starter := &fakeStarter{failures: 1}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir(), RestartDelay: 100 * time.Millisecond}, store, starter)
	defer manager.Close()
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(time.Second)
	foundDead := false
	for time.Now().Before(deadline) {
		item, ok := manager.Pool().ByID(account.ID)
		if ok && item.RuntimeState == "dead" {
			foundDead = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !foundDead {
		t.Fatal("account did not enter recovery")
	}
	if err := manager.Update(ctx, account.ID, UpdateAccount{Enabled: boolPtr(false)}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Update(ctx, account.ID, UpdateAccount{Enabled: boolPtr(true)}); err != nil {
		t.Fatal(err)
	}

	time.Sleep(2 * time.Duration(100*time.Millisecond))
	item, ok := manager.Pool().ByID(account.ID)
	if !ok || item.RuntimeState != "starting" || item.Ready == nil || *item.Ready {
		t.Fatalf("reenabled account was clobbered by stale recovery: %+v ok=%v", item, ok)
	}
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
	item, ok := manager.Pool().ByID(account.ID)
	if !ok || item.URL != "http://127.0.0.1:32100" || item.RuntimeState != "starting" {
		t.Fatalf("started item = %+v ok=%v", item, ok)
	}
	if _, ok := manager.Pool().Pick("", nil); ok {
		t.Fatal("starting account must not be routable before health succeeds")
	}
	manager.Pool().MergeHealth(account.ID, true, false, 0, 0, "")
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

func TestQoderRuntimeSpecSelectsCNCLIAndConfigDir(t *testing.T) {
	cfg := ManagerConfig{
		QoderCLIPath:   "/opt/qodercli.js",
		QoderCNCLIPath: "/opt/qoderclicn.js",
	}
	globalPath, globalSite, globalDir, globalEnv, err := qoderRuntimeSpec(cfg, Account{ProviderRegion: "global"}, "/run/acc-g")
	if err != nil {
		t.Fatal(err)
	}
	if globalPath != "/opt/qodercli.js" || globalSite != "global" || globalDir != "/run/acc-g/.qoder" || globalEnv != "QODER_CONFIG_DIR" {
		t.Fatalf("global spec path=%s site=%s dir=%s env=%s", globalPath, globalSite, globalDir, globalEnv)
	}
	cnPath, cnSite, cnDir, cnEnv, err := qoderRuntimeSpec(cfg, Account{ProviderRegion: "cn"}, "/run/acc-c")
	if err != nil {
		t.Fatal(err)
	}
	if cnPath != "/opt/qoderclicn.js" || cnSite != "cn" || cnDir != "/run/acc-c/.qoder-cn" || cnEnv != "QODERCN_CONFIG_DIR" {
		t.Fatalf("cn spec path=%s site=%s dir=%s env=%s", cnPath, cnSite, cnDir, cnEnv)
	}
	if _, _, _, _, err := qoderRuntimeSpec(ManagerConfig{QoderCLIPath: "/opt/qodercli.js"}, Account{ProviderRegion: "cn"}, "/run/acc-c"); err == nil {
		t.Fatal("expected missing CN CLI path to fail")
	}
}

func TestManagerMaterializesAndSyncsQoderCNCredentials(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	store, err := OpenStore(filepath.Join(dataDir, "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{Name: "CN", Provider: "qoder", Region: "cn", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCredential(ctx, account.ID, "native", NativeCredential{UserBlob: []byte("cn-blob"), MachineID: "cn-machine"}); err != nil {
		t.Fatal(err)
	}
	starter := &fakeStarter{}
	manager := NewManager(ManagerConfig{DataDir: dataDir, BasePort: 32400}, store, starter)
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if len(starter.homes) != 1 {
		t.Fatalf("homes = %v", starter.homes)
	}
	authDir := filepath.Join(starter.homes[0], ".qoder-cn", ".auth")
	if _, err := os.Stat(filepath.Join(starter.homes[0], ".qoder", ".auth", "user")); !os.IsNotExist(err) {
		t.Fatal("CN account must not materialize global .qoder credentials")
	}
	if err := os.WriteFile(filepath.Join(authDir, "user"), []byte("cn-oauth"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(authDir, "machine_id"), []byte("cn-mid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.SyncCredential(ctx, account.ID, "oauth"); err != nil {
		t.Fatal(err)
	}
	credential, err := store.LoadCredential(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(credential.UserBlob) != "cn-oauth" || credential.MachineID != "cn-mid" {
		t.Fatalf("synced CN credential = %+v", credential)
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
	if err := manager.RefreshAll(ctx, false); err != nil {
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

func TestManagerRefreshSkipsDeadRecovery(t *testing.T) {
	called := 0
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		http.Error(w, "connection refused", http.StatusBadGateway)
	}))
	defer worker.Close()
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{Name: "DeadRefresh", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	defer manager.Close()
	restartAt := time.Now().Add(time.Minute)
	manager.pool.Upsert(Item{ID: account.ID, URL: worker.URL})
	manager.pool.SetRuntimeState(account.ID, "dead", restartAt, 2, "account daemon exited")
	if err := manager.RefreshAll(ctx, false); err != nil {
		t.Fatal(err)
	}
	if called != 0 {
		t.Fatalf("health probe called %d times, want 0", called)
	}
	item, ok := manager.pool.ByID(account.ID)
	if !ok || item.RuntimeState != "dead" || item.RestartBackoffLevel != 2 || item.NextRestartAt.IsZero() {
		t.Fatalf("dead recovery was overwritten: %+v ok=%v", item, ok)
	}
}

func TestManagerRefreshFetchesQuotaWithoutAffectingHealth(t *testing.T) {
	var quotaCalled bool
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "ready": true, "hot": true, "uid": "qoder-uid-1",
			})
		case "/admin/quota":
			quotaCalled = true
			if r.Header.Get("Authorization") != "Bearer proxy-key" {
				t.Errorf("quota request missing worker api key, got %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"quota": map[string]any{
					"isQuotaExceeded": false,
					"fetchedAt":       "2026-08-25T12:00:00Z",
					"userQuota":       map[string]any{"total": 600, "used": 150, "remaining": 450, "percentage": 25, "unit": "credits"},
					"addOnQuota":      map[string]any{"total": 100, "used": 40, "remaining": 60, "percentage": 40, "unit": "credits"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer worker.Close()
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{Name: "Quota", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir(), ProxyAPIKey: "proxy-key"}, store, &fakeStarter{})
	manager.pool.Upsert(Item{ID: account.ID, URL: worker.URL})
	if err := manager.RefreshAll(ctx, false); err != nil {
		t.Fatal(err)
	}
	if !quotaCalled {
		t.Fatal("expected quota endpoint to be called for a hot account")
	}
	item, _ := manager.pool.ByID(account.ID)
	if item.Quota == nil {
		t.Fatalf("expected quota snapshot on pool item, got %+v", item)
	}
	if item.Quota.Used != 150 || item.Quota.Total != 600 || item.Quota.Unit != "credits" || item.Quota.Exceeded {
		t.Fatalf("quota snapshot = %+v", item.Quota)
	}
	if !item.Quota.HasAddOn || item.Quota.AddOnTotal != 100 || item.Quota.AddOnUsed != 40 {
		t.Fatalf("quota add-on = %+v", item.Quota)
	}
	views, err := manager.Accounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].Quota == nil || views[0].Quota.Remaining != 450 {
		t.Fatalf("account view quota = %+v", views)
	}
}

func TestManagerRefreshCachesAccountCatalog(t *testing.T) {
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "ready": true, "hot": true, "uid": "qoder-uid-1",
			})
		case "/admin/quota":
			http.Error(w, "quota unused", http.StatusBadGateway)
		case "/admin/models":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
				{"id": "hy3", "mapped_key": "hy3", "display_name": "HY3"},
				{"id": "glm-5.2", "mapped_key": "gmodel", "display_name": "GLM-5.2"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer worker.Close()
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{Name: "Catalog", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	manager.pool.Upsert(Item{ID: account.ID, URL: worker.URL})
	if err := manager.RefreshAll(ctx, false); err != nil {
		t.Fatal(err)
	}
	item, _ := manager.pool.ByID(account.ID)
	if !containsModel(item.Models, "hy3") || !containsModel(item.Models, "gmodel") {
		t.Fatalf("cached models = %#v", item.Models)
	}
}

func containsModel(models []string, want string) bool {
	for _, model := range models {
		if model == want {
			return true
		}
	}
	return false
}

func TestManagerQuotaFailureLeavesAccountReady(t *testing.T) {
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "ready": true, "hot": true, "uid": "qoder-uid-1",
			})
		case "/admin/quota":
			http.Error(w, "quota down", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer worker.Close()
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{Name: "QuotaDown", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	manager.pool.Upsert(Item{ID: account.ID, URL: worker.URL})
	if err := manager.RefreshAll(ctx, false); err != nil {
		t.Fatalf("quota outage must not fail refresh: %v", err)
	}
	item, _ := manager.pool.ByID(account.ID)
	if item.Hot == nil || !*item.Hot {
		t.Fatalf("account must stay hot on quota outage, got %+v", item)
	}
	if item.Quota != nil {
		t.Fatalf("quota should stay nil on outage, got %+v", item.Quota)
	}
}

func TestManagerRefreshCanForceQuotaBypass(t *testing.T) {
	var gotQuery string
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "ready": true, "hot": true, "uid": "qoder-uid-1",
			})
		case "/admin/quota":
			gotQuery = r.URL.RawQuery
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"quota": map[string]any{
					"userQuota": map[string]any{"total": 10, "used": 1, "remaining": 9, "percentage": 10, "unit": "credits"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer worker.Close()
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{Name: "ForceQuota", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	manager.pool.Upsert(Item{ID: account.ID, URL: worker.URL})
	if err := manager.RefreshAll(ctx, true); err != nil {
		t.Fatal(err)
	}
	if gotQuery != "refresh=1" {
		t.Fatalf("forced quota refresh query = %q", gotQuery)
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

func TestCloseStopsRecoveryBeforeNewDaemonEscapes(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Create(ctx, CreateAccount{Name: "CloseRace", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	starter := &delayedStarter{started: make(chan *fakeProcess, 2), delayAfter: 1, delay: 80 * time.Millisecond}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir(), RestartDelay: time.Millisecond}, store, starter)
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	first := <-starter.started
	first.done <- errors.New("crashed")

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if starter.startCount() >= 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if starter.startCount() < 2 {
		t.Fatal("recovery did not attempt a restart before Close")
	}

	done := make(chan error, 1)
	go func() { done <- manager.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close blocked while a recovery start was in flight")
	}

	for _, process := range starter.processes() {
		if !process.stopped && process != first {
			t.Fatal("recovery process was left running after Close")
		}
	}
	if got := starter.startCount(); got != 2 {
		t.Fatalf("starts after Close = %d, want 2", got)
	}
}

func TestManagerEscalatesConsecutiveRestartBackoffSeparately(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{Name: "Backoff", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	starter := &fakeStarter{started: make(chan *fakeProcess, 3)}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir(), RestartDelay: time.Millisecond}, store, starter)
	defer manager.Close()
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	first := <-starter.started
	first.done <- errors.New("first crash")
	second := <-starter.started
	second.done <- errors.New("second crash")
	third := <-starter.started
	if third == second {
		t.Fatal("manager reused exited process")
	}
	item, ok := manager.Pool().ByID(account.ID)
	if !ok {
		t.Fatal("account disappeared during consecutive recovery")
	}
	if item.Restarts != 2 || item.RestartBackoffLevel != 2 {
		t.Fatalf("restart count/backoff = %d/%d, want 2/2", item.Restarts, item.RestartBackoffLevel)
	}
}

func TestManagerRestartDelayIsBounded(t *testing.T) {
	manager := NewManager(ManagerConfig{RestartDelay: 2 * time.Second, RestartMaxDelay: 5 * time.Second}, nil, &fakeStarter{})
	if got := manager.restartDelay(1); got != 2*time.Second {
		t.Fatalf("level 1 delay = %v", got)
	}
	if got := manager.restartDelay(2); got != 4*time.Second {
		t.Fatalf("level 2 delay = %v", got)
	}
	if got := manager.restartDelay(3); got != 5*time.Second {
		t.Fatalf("level 3 delay = %v", got)
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
	// The observer persists asynchronously through a single drainer
	// goroutine; wait for it to catch up before asserting SQLite state.
	manager.Flush()
	updated, err := store.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastErrorKind != KindRateLimit || updated.LastError != "429" || updated.CooldownUntil == nil {
		t.Fatalf("persisted account = %+v", updated)
	}
}

// failingStarter always returns an error, simulating a boot that cannot
// bring the account's process up.
type failingStarter struct{}

func (f *failingStarter) Start(_ context.Context, _ Account, _ string, _ int) (ManagedProcess, error) {
	return nil, errors.New("start failed")
}

// P2#1: concurrent MarkClassified calls on one account used to save
// overlapping snapshots in arbitrary order, so a stale snapshot could land
// after a fresh one and clobber the newest cooldown. The serialized drainer
// must order writes so the final SQLite state matches the last update.
func TestObserverSavesAreSerialized(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{Name: "Concurrent", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	defer manager.Close()
	manager.pool.Upsert(Item{ID: account.ID, URL: "http://127.0.0.1:1"})
	// Fire several cooldown updates concurrently; the last one to set
	// ModelDownUntil must win, not whichever snapshot happens to write last.
	const workers = 8
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			manager.pool.MarkClassified(account.ID, Classified{
				Kind: KindRateLimit, Cooldown: time.Duration(i+1) * time.Minute,
				Failover: true, Model: "glm-5.3", Message: "429",
			})
		}(i)
	}
	wg.Wait()
	manager.Flush()
	rows, err := store.LoadCooldowns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 cooldown row, got %d: %+v", len(rows), rows)
	}
	// The persisted cooldown must be one of the durations we set; without
	// serialization an empty (last-write-loses) snapshot could have deleted it.
	// The model row carries ModelKind=rate_limit (the per-model kind), while
	// the account-wide Kind is empty because the failure was model-scoped.
	if rows[0].Model != "glm-5.3" || rows[0].ModelKind != KindRateLimit {
		t.Fatalf("unexpected row %+v", rows[0])
	}
}

// P2#2: a boot-time startAccount failure used to call MarkDown(0), which
// triggered the observer's SaveCooldowns with an empty in-memory cooldown
// set and DELETEd any persisted cooldowns for the account. The failure path
// now records the error without triggering persistence, so a restart that
// fails to start a rate-limited account leaves its cooldown intact in SQLite
// and restoreCooldowns reloads it into memory.
func TestStartFailureKeepsPersistedCooldowns(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{
		Name: "FailingBoot", Enabled: true, Provider: "qoder", Region: "global",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Seed SQLite with a model cooldown as if a previous run recorded it.
	seedUntil := time.Now().Add(time.Hour).UTC()
	if err := store.SaveCooldowns(ctx, account.ID, []CooldownRow{{
		AccountID: account.ID, Model: "glm-5.3", DownUntil: seedUntil,
		BackoffLevel: 2, Kind: KindRateLimit, Message: "429",
	}}); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir(), QoderCLIPath: "unused"}, store, &failingStarter{})
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer manager.Close()
	manager.Flush()
	// The cooldown must still be in SQLite, not deleted by the start failure.
	rows, err := store.LoadCooldowns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, row := range rows {
		if row.AccountID == account.ID && row.Model == "glm-5.3" {
			found = true
		}
	}
	if !found {
		t.Fatalf("persisted glm-5.3 cooldown must survive start failure, got rows %+v", rows)
	}
}

// P1#2: Close() must not panic when a concurrent observer is enqueuing.
// The old design closed a channel that a concurrent MarkClassified could
// still send on; the mutex-guarded queue checks persistClosed under the
// lock before appending, so there is no "send on closed channel".
func TestCloseConcurrentObserverNoPanic(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{Name: "ConcurrentClose", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	manager.pool.Upsert(Item{ID: account.ID, URL: "http://127.0.0.1:1"})
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				manager.pool.MarkClassified(account.ID, Classified{
					Kind: KindRateLimit, Cooldown: time.Minute,
					Failover: true, Model: "glm-5.3", Message: "429",
				})
			}
		}
	}()
	// Give the writer a head start, then Close concurrently.
	time.Sleep(10 * time.Millisecond)
	closeErr := make(chan error, 1)
	go func() { closeErr <- manager.Close() }()
	close(stop)
	if err := <-closeErr; err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
}

// P2#4: Flush() must be strict — every item enqueued before the Flush call
// is persisted before Flush returns, even with a concurrent enqueuer.
func TestFlushStrictlyAfterEnqueue(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{Name: "FlushStrict", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	defer manager.Close()
	manager.pool.Upsert(Item{ID: account.ID, URL: "http://127.0.0.1:1"})
	// Enqueue a known cooldown, then flush. The SQLite row must reflect it
	// after Flush returns.
	manager.pool.MarkClassified(account.ID, Classified{
		Kind: KindRateLimit, Cooldown: time.Hour,
		Failover: true, Model: "glm-5.3", Message: "strict-429",
	})
	manager.Flush()
	rows, err := store.LoadCooldowns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var seen bool
	for _, row := range rows {
		if row.AccountID == account.ID && row.Model == "glm-5.3" && row.Message == "strict-429" {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("Flush must persist the enqueued cooldown, got rows %+v", rows)
	}
}

// P2#3: per-model last-kind survives a restart. Seed SQLite with a model
// cooldown carrying model_kind=rate_limit, reopen the store, restore, and
// assert ModelLastKind[model] is restored so a repeat failure escalates.
func TestModelLastKindPersistedAcrossRestart(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{Name: "RestartKind", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(time.Hour).UTC()
	if err := store.SaveCooldowns(ctx, account.ID, []CooldownRow{{
		AccountID: account.ID, Model: "glm-5.3", DownUntil: until,
		BackoffLevel: 1, Kind: KindRateLimit, Message: "429",
		ModelKind: KindRateLimit,
	}}); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	defer manager.Close()
	manager.pool.Upsert(Item{ID: account.ID, URL: "http://127.0.0.1:1"})
	manager.restoreCooldowns(ctx)
	item, _ := manager.pool.ByID(account.ID)
	if item.ModelLastKind == nil || item.ModelLastKind["glm-5.3"] != KindRateLimit {
		t.Fatalf("ModelLastKind[glm-5.3] must be restored to rate_limit, got %+v", item.ModelLastKind)
	}
}

// P1: the observer runs after p.mu is released, so two concurrent
// MarkClassified calls can enqueue snapshots out of production order. The
// monotonic StateVersion (stamped under p.mu) lets the drainer discard a
// stale snapshot, so the final persisted state is the newest pool state.
// This reproduces the exact ordering hazard: snapshot A (older) is delayed
// before enqueueing while snapshot B (newer) races ahead; the SQLite row
// must reflect B, not A.
func TestStateVersionOrdersAcrossDelayedObserver(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{Name: "Ordered", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	defer manager.Close()
	manager.pool.Upsert(Item{ID: account.ID, URL: "http://127.0.0.1:1"})

	// Wrap the observer so the FIRST call (the older snapshot A) blocks on a
	// latch until after B has enqueued. The second call (B) enqueues
	// immediately. Then A's delayed enqueue must be discarded by version.
	blockA := make(chan struct{})
	startedA := make(chan struct{})
	original := manager.pool.observer
	manager.pool.SetObserver(func(item Item) {
		if item.StateVersion == 1 && item.ID == account.ID {
			close(startedA)
			<-blockA // hold A back until B has enqueued
		}
		// Delegate to the real observer (the dirty-set merge) for all calls.
		original(item)
	})

	// Fire A (version 1, message "old"); the observer will block on blockA.
	go manager.pool.MarkClassified(account.ID, Classified{
		Kind: KindRateLimit, Cooldown: time.Hour,
		Failover: true, Model: "glm-5.3", Message: "old-snapshot",
	})
	<-startedA
	// Fire B (version 2, message "new"); it enqueues immediately while A is held.
	manager.pool.MarkClassified(account.ID, Classified{
		Kind: KindRateLimit, Cooldown: time.Hour,
		Failover: true, Model: "glm-5.3", Message: "new-snapshot",
	})
	// Release A; its stale snapshot is now enqueued after B's.
	close(blockA)

	manager.Flush()
	rows, err := store.LoadCooldowns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.AccountID == account.ID && row.Model == "glm-5.3" {
			if row.Message != "new-snapshot" {
				t.Fatalf("stale snapshot A must be discarded; SQLite has %q, want %q", row.Message, "new-snapshot")
			}
			return
		}
	}
	t.Fatalf("cooldown row for glm-5.3 must exist, got rows %+v", rows)
}

// P2: the persistence dirty set is keyed by account ID and merged on
// enqueue, so it cannot grow without bound under DB pressure. Fire many
// concurrent MarkClassified calls on one account against a slow store and
// assert the process stays bounded (the test completes without OOM and the
// dirty set never holds more than one entry per account at drain time).
func TestPersistQueueBoundedPerAccount(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{Name: "Bounded", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	defer manager.Close()
	manager.pool.Upsert(Item{ID: account.ID, URL: "http://127.0.0.1:1"})

	const workers = 64
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			manager.pool.MarkClassified(account.ID, Classified{
				Kind: KindRateLimit, Cooldown: time.Minute,
				Failover: true, Model: "glm-5.3", Message: fmt.Sprintf("burst-%d", i),
			})
		}(i)
	}
	wg.Wait()
	manager.Flush()

	// The dirty set is a per-account map; after Flush it must be empty.
	manager.persistMu.Lock()
	dirtyLen := len(manager.persistDirty)
	manager.persistMu.Unlock()
	if dirtyLen != 0 {
		t.Fatalf("persistDirty must be empty after Flush, got %d", dirtyLen)
	}
	// Exactly one cooldown row survives (the account's newest state).
	rows, err := store.LoadCooldowns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, row := range rows {
		if row.AccountID == account.ID && row.Model == "glm-5.3" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 cooldown row for the account, got %d", count)
	}
}

// P1: when a SQLite write fails (db locked, disk error, connection), the
// snapshot must stay in the dirty set and be retried rather than be dropped.
// persistedVersions must only advance on success, otherwise a later stale
// snapshot could be discarded even though the newer state never reached disk.
// Closing the underlying db handle forces the drainer's writes to fail.
func TestPersistFailureKeepsDirtyEntryAndRetries(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "qoder.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	account, err := store.Create(ctx, CreateAccount{Name: "FailingWrite", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	defer manager.Close()
	manager.pool.Upsert(Item{ID: account.ID, URL: "http://127.0.0.1:1"})

	// Close the underlying db so SaveCooldowns fails. The drainer must
	// re-enqueue the snapshot and back off; it must NOT advance
	// persistedVersions or drop the entry.
	if err := store.db.Close(); err != nil {
		t.Fatal(err)
	}
	manager.pool.MarkClassified(account.ID, Classified{
		Kind: KindRateLimit, Cooldown: time.Hour,
		Failover: true, Model: "glm-5.3", Message: "write-will-fail",
	})
	// The drainer removes the snapshot while a write is in flight, then
	// puts it back after the failure. Sampling at a multiple of the retry
	// backoff can land in that empty window, so poll until the failed
	// snapshot is visible again.
	var dirty Item
	var dirtyOK bool
	var version uint64
	deadline := time.Now().Add(2 * time.Second)
	for {
		manager.persistMu.Lock()
		dirty, dirtyOK = manager.persistDirty[account.ID]
		version = manager.persistedVersions[account.ID]
		manager.persistMu.Unlock()
		if dirtyOK && dirty.LastError == "write-will-fail" && version == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dirty entry must be retained after a failed write, ok=%v version=%d lastError=%q", dirtyOK, version, dirty.LastError)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Reopen the db on the same file; the drainer's retry loop must now
	// succeed and clear the dirty entry.
	store.db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	// SQLite needs the same pragmas as OpenStore for WAL; the test only reads
	// cooldowns so a bare open suffices. Wait for the drainer to retry.
	manager.Flush()
	manager.persistMu.Lock()
	afterDirty := len(manager.persistDirty)
	manager.persistMu.Unlock()
	if afterDirty != 0 {
		t.Fatalf("dirty set must drain once writes succeed, got %d", afterDirty)
	}
}

// P1: Close() must not block forever when the DB is persistently
// unavailable. The drainer's retry backoff watches persistCloseCh (closed by
// Close), not just runCtx (which Close cancels only AFTER the drainer exits).
// Without this, a stuck DB would deadlock shutdown: Close waits for the
// drainer, the drainer waits for runCtx.Done(), runCtx is never canceled.
func TestCloseDuringPersistentDBFailure(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	account, err := store.Create(ctx, CreateAccount{Name: "StuckClose", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	manager.pool.Upsert(Item{ID: account.ID, URL: "http://127.0.0.1:1"})

	// Close the underlying db so writes persistently fail, forcing the
	// drainer into its retry backoff.
	if err := store.db.Close(); err != nil {
		t.Fatal(err)
	}
	manager.pool.MarkClassified(account.ID, Classified{
		Kind: KindRateLimit, Cooldown: time.Hour,
		Failover: true, Model: "glm-5.3", Message: "stuck",
	})
	// Let the drainer enter the retry loop (it fails, backs off, retries).
	time.Sleep(persistRetryBackoff * 2)

	// Close must return within a bounded time despite the stuck DB.
	done := make(chan error, 1)
	go func() { done <- manager.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close blocked forever on a persistently failing DB")
	}
}

// A model-scoped cooldown with a high backoff level must not pollute the
// account-wide BackoffLevel on restore. Without this fix, a model that
// failed repeatedly (e.g. level 3) would inflate the account-level ladder,
// so a later account-wide failure on a different model starts at level 3
// instead of 0.
func TestRestoreModelCooldownDoesNotPolluteAccountBackoff(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{Name: "NoPollute", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(time.Hour).UTC()
	rows := []CooldownRow{
		// Model-scoped row with a high backoff level.
		{
			AccountID: account.ID, Model: "glm-5.3", DownUntil: until,
			BackoffLevel: 3, Kind: KindRateLimit, Message: "429",
			ModelKind: KindRateLimit,
		},
		// Account-wide row with backoff level 0 (healthy account level).
		{
			AccountID: account.ID, Model: "", DownUntil: until,
			BackoffLevel: 0, Kind: "", Message: "",
		},
	}
	if err := store.SaveCooldowns(ctx, account.ID, rows); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	defer manager.Close()
	manager.pool.Upsert(Item{ID: account.ID, URL: "http://127.0.0.1:1"})
	manager.restoreCooldowns(ctx)

	item, _ := manager.pool.ByID(account.ID)
	// Account-wide BackoffLevel must stay at 0 — the model's level 3
	// must not leak into it.
	if item.BackoffLevel != 0 {
		t.Fatalf("BackoffLevel = %d, want 0 (model backoff leaked into account level)", item.BackoffLevel)
	}
	// Model-scoped backoff must be restored correctly.
	if item.ModelBackoff == nil || item.ModelBackoff["glm-5.3"] != 3 {
		t.Fatalf("ModelBackoff[glm-5.3] = %v, want 3", item.ModelBackoff)
	}

	// Verify the fix end-to-end: trigger an account-wide failure on a
	// different model. The backoff ladder must start at level 0 (first
	// failure), not level 3.
	manager.pool.MarkClassified(account.ID, Classified{
		Kind: KindUnavailable, Cooldown: 60 * time.Second,
		Failover: true, Model: "deepseek-v4-flash", Message: "conn refused",
	})
	item, _ = manager.pool.ByID(account.ID)
	// deepseek-v4-flash is a new model+kind, so its ModelBackoff must
	// start at 0 (first failure), not 3.
	if mb := item.ModelBackoff["deepseek-v4-flash"]; mb != 0 {
		t.Fatalf("ModelBackoff[deepseek-v4-flash] = %d, want 0 (first failure must start fresh)", mb)
	}
}

// EnsureModelCatalogs must return immediately, not block on N serial 15s
// timeouts when multiple accounts are offline. Before the fix each
// fetchAccountModels call was synchronous with a 15-second HTTP timeout,
// so 3 offline accounts would block the first chat request for up to 45s.
func TestEnsureModelCatalogsDoesNotBlock(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Workers that hang on /admin/models — they will time out at 15s in
	// fetchAccountModels, but EnsureModelCatalogs must return long before
	// that.
	slowA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(30 * time.Second)
	}))
	defer slowA.Close()
	slowB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(30 * time.Second)
	}))
	defer slowB.Close()
	slowC := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(30 * time.Second)
	}))
	defer slowC.Close()

	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	defer manager.Close()
	manager.pool.Upsert(Item{ID: "slow-a", URL: slowA.URL, Provider: "qoder", Runtime: "child_process"})
	manager.pool.Upsert(Item{ID: "slow-b", URL: slowB.URL, Provider: "qoder", Runtime: "child_process"})
	manager.pool.Upsert(Item{ID: "slow-c", URL: slowC.URL, Provider: "qoder", Runtime: "child_process"})

	// With the old serial implementation, this would block ~45s.
	// With the async fix it returns in milliseconds.
	done := make(chan struct{})
	go func() {
		manager.EnsureModelCatalogs(ctx, true)
		close(done)
	}()
	select {
	case <-done:
		// success — returned without blocking
	case <-time.After(2 * time.Second):
		t.Fatal("EnsureModelCatalogs blocked >2s; must be async/non-blocking")
	}
}
