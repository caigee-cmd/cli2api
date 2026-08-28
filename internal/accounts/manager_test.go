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
