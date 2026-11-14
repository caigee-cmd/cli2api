package runtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/executor"
	accountruntime "github.com/caigee-cmd/cli2api/internal/runtime"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/providers/qoder"
	sqlstore "github.com/caigee-cmd/cli2api/internal/store"
)

func TestManagerDoesNotSpawnDaemonForInProcessProvider(t *testing.T) {
	ctx := context.Background()
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	starter := &fakeStarter{}
	manager := accountruntime.NewManager(accountruntime.ManagerConfig{DataDir: t.TempDir(), BasePort: 32300}, store, starter)
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	account, err := manager.Create(ctx, accounts.CreateAccount{
		Name: "WB", Provider: "workbuddy", Region: "cn", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(starter.accounts) != 0 {
		t.Fatalf("in-process provider spawned %d daemons", len(starter.accounts))
	}
	item, ok := manager.Pool().ByID(account.ID)
	if !ok || item.Provider != "workbuddy" || item.Runtime != "in_process" {
		t.Fatalf("pool item = %+v ok=%v", item, ok)
	}

	qoder, err := manager.Create(ctx, accounts.CreateAccount{Name: "Q", Provider: "qoder", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(starter.accounts) != 1 || starter.accounts[0].ID != qoder.ID {
		t.Fatalf("qoder should spawn exactly one daemon, started=%+v", starter.accounts)
	}
	qItem, ok := manager.Pool().ByID(qoder.ID)
	if !ok || qItem.Provider != "qoder" || qItem.Runtime != "child_process" {
		t.Fatalf("qoder pool item = %+v ok=%v", qItem, ok)
	}
}

type fakeProber struct {
	health    providers.AccountHealth
	quota     *providers.QuotaInfo
	probeN    int
	quotaN    int
	quotaDone chan struct{}
	err       error
	quotaErr  error
}

func (f *fakeProber) Probe(ctx context.Context, accountID string) (providers.AccountHealth, error) {
	f.probeN++
	return f.health, f.err
}

func (f *fakeProber) Quota(ctx context.Context, accountID string) (*providers.QuotaInfo, error) {
	f.quotaN++
	if f.quotaDone != nil {
		close(f.quotaDone)
	}
	return f.quota, f.quotaErr
}

func TestManagerRefreshUsesInProcessProber(t *testing.T) {
	ctx := context.Background()
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, accounts.CreateAccount{
		Name: "WB", Provider: "workbuddy", Region: "cn", Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Observe(ctx, account.ID, "", "error", "Get \"/health\": unsupported protocol scheme \"\"", accounts.KindUnavailable)

	prober := &fakeProber{
		health: providers.AccountHealth{Ready: true, Hot: true, UID: "wb-uid"},
		quota: &providers.QuotaInfo{
			Used: 100, Total: 1000, Remaining: 900, Percentage: 10, Unit: "credits",
			FetchedAt: "2026-08-26T00:00:00Z",
		},
		quotaDone: make(chan struct{}),
	}
	registry := providers.NewRegistry()
	registry.Register(providers.Adapter{ID: "workbuddy", Prober: prober})

	manager := accountruntime.NewManager(accountruntime.ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	manager.SetProviders(registry)
	manager.Pool().Upsert(executor.Item{ID: account.ID, Provider: "workbuddy", Runtime: "in_process"})

	if err := manager.RefreshAll(ctx, false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-prober.quotaDone:
	case <-time.After(time.Second):
		t.Fatal("quota refresh did not complete")
	}
	if prober.probeN != 1 || prober.quotaN != 1 {
		t.Fatalf("probeN=%d quotaN=%d", prober.probeN, prober.quotaN)
	}
	// Quota() signals before persistQuota merges into the pool / SQLite; wait
	// for both instead of racing the channel alone.
	deadline := time.Now().Add(time.Second)
	var item executor.Item
	var updated accounts.Account
	for {
		item, _ = manager.Pool().ByID(account.ID)
		updated, err = store.Get(ctx, account.ID)
		if err != nil {
			t.Fatal(err)
		}
		if item.Quota != nil && item.Quota.Remaining == 900 && item.Quota.Total == 1000 &&
			updated.Quota != nil && updated.Quota.Remaining == 900 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("quota pool=%+v store=%+v", item.Quota, updated.Quota)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if item.Ready == nil || !*item.Ready || item.Hot == nil || !*item.Hot || item.LastError != "" {
		t.Fatalf("pool item = %+v", item)
	}
	if updated.Status != "ready" || updated.RemoteUID != "wb-uid" || updated.LastError != "" {
		t.Fatalf("store account = %+v", updated)
	}
	if err := manager.TestStartAccount(ctx, updated); err != nil {
		t.Fatal(err)
	}
	item, _ = manager.Pool().ByID(account.ID)
	if item.Quota == nil || item.Quota.Remaining != 900 {
		t.Fatalf("in-process upsert cleared quota: %+v", item.Quota)
	}
	views, err := manager.Accounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || !views[0].Ready || !views[0].Hot || views[0].Quota == nil || views[0].LastError != "" {
		t.Fatalf("view = %+v", views[0])
	}
}

// A manual single-account refresh with quota=1 (forceQuota) must fetch quota
// synchronously and persist it before returning, so the console row it replaces
// carries the freshly-read number; the background list path stays async.
func TestManagerRefreshAccountForceQuotaIsSynchronous(t *testing.T) {
	ctx := context.Background()
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, accounts.CreateAccount{
		Name: "WB", Provider: "workbuddy", Region: "cn", Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	prober := &fakeProber{
		health: providers.AccountHealth{Ready: true, Hot: true, UID: "wb-uid"},
		quota: &providers.QuotaInfo{
			Used: 5, Total: 100, Remaining: 95, Percentage: 5, Unit: "credits",
			FetchedAt: "2026-08-26T00:00:00Z",
		},
	}
	registry := providers.NewRegistry()
	registry.Register(providers.Adapter{ID: "workbuddy", Prober: prober})
	manager := accountruntime.NewManager(accountruntime.ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	manager.SetProviders(registry)
	manager.Pool().Upsert(executor.Item{ID: account.ID, Provider: "workbuddy", Runtime: "in_process"})

	if err := manager.RefreshAccount(ctx, account.ID, true); err != nil {
		t.Fatalf("refresh err = %v", err)
	}
	// No polling: the row must already carry the quota when RefreshAccount returns.
	item, _ := manager.Pool().ByID(account.ID)
	if item.Quota == nil || item.Quota.Remaining != 95 {
		t.Fatalf("quota not persisted synchronously: %+v", item.Quota)
	}
	stored, err := store.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Quota == nil || stored.Quota.Remaining != 95 {
		t.Fatalf("store quota not persisted synchronously: %+v", stored.Quota)
	}
}

// A failed forced quota refresh must surface as a QuotaRefreshError so the
// console can report "could not refresh" instead of silently leaving a stale
// number.
func TestManagerRefreshAccountForceQuotaSurfacesError(t *testing.T) {
	ctx := context.Background()
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, accounts.CreateAccount{
		Name: "WB", Provider: "workbuddy", Region: "cn", Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	prober := &fakeProber{
		health:   providers.AccountHealth{Ready: true, Hot: true, UID: "wb-uid"},
		quotaErr: errors.New("quota endpoint down"),
	}
	registry := providers.NewRegistry()
	registry.Register(providers.Adapter{ID: "workbuddy", Prober: prober})
	manager := accountruntime.NewManager(accountruntime.ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	manager.SetProviders(registry)
	manager.Pool().Upsert(executor.Item{ID: account.ID, Provider: "workbuddy", Runtime: "in_process"})

	err = manager.RefreshAccount(ctx, account.ID, true)
	var qerr *accountruntime.QuotaRefreshError
	if !errors.As(err, &qerr) {
		t.Fatalf("err = %v, want QuotaRefreshError", err)
	}
	// Readiness is unaffected by a quota outage.
	item, _ := manager.Pool().ByID(account.ID)
	if item.Ready == nil || !*item.Ready {
		t.Fatalf("quota failure flipped readiness: %+v", item)
	}
}

func TestManagerRefreshPersistsQuotaWindows(t *testing.T) {
	ctx := context.Background()
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, accounts.CreateAccount{
		Name: "Devin", Provider: "devin", Region: "global", Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	prober := &fakeProber{
		health: providers.AccountHealth{Ready: true, Hot: true, UID: "devin-uid"},
		quota: &providers.QuotaInfo{
			Used: 67, Total: 100, Remaining: 33, Percentage: 67, Unit: "percent",
			FetchedAt: "2026-08-26T00:00:00Z",
			Windows: []providers.QuotaWindow{
				{ID: "daily", Label: "Daily quota", Used: 0, Total: 100, Remaining: 100, Percentage: 0, Unit: "percent", ResetAt: "2026-08-26T16:00:00Z"},
				{ID: "weekly", Label: "Weekly quota", Used: 67, Total: 100, Remaining: 33, Percentage: 67, Unit: "percent", ResetAt: "2026-08-26T16:00:00Z"},
			},
		},
		quotaDone: make(chan struct{}),
	}
	registry := providers.NewRegistry()
	registry.Register(providers.Adapter{ID: "devin", Prober: prober})
	manager := accountruntime.NewManager(accountruntime.ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	manager.SetProviders(registry)
	manager.Pool().Upsert(executor.Item{ID: account.ID, Provider: "devin", Runtime: "in_process"})
	if err := manager.RefreshAll(ctx, false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-prober.quotaDone:
	case <-time.After(time.Second):
		t.Fatal("quota refresh did not complete")
	}
	deadline := time.Now().Add(time.Second)
	var item executor.Item
	var updated accounts.Account
	for {
		item, _ = manager.Pool().ByID(account.ID)
		updated, err = store.Get(ctx, account.ID)
		if err != nil {
			t.Fatal(err)
		}
		if item.Quota != nil && len(item.Quota.Windows) == 2 && updated.Quota != nil && len(updated.Quota.Windows) == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("quota pool=%+v store=%+v", item.Quota, updated.Quota)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if item.Quota.Remaining != 33 || item.Quota.Windows[0].ID != "daily" || item.Quota.Windows[0].Remaining != 100 || item.Quota.Windows[1].ID != "weekly" {
		t.Fatalf("pool quota = %+v", item.Quota)
	}
	if updated.Quota.Windows[0].ResetAt != "2026-08-26T16:00:00Z" || updated.Quota.Windows[1].Percentage != 67 {
		t.Fatalf("store quota = %+v", updated.Quota)
	}
}

// A Qoder-style QuotaInfo breakdown must survive the generic quota path so the
// persisted snapshot keeps its add-on / resource-package detail lines.
func TestManagerRefreshPersistsQoderBreakdown(t *testing.T) {
	ctx := context.Background()
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, accounts.CreateAccount{Name: "Q", Provider: "qoder", Region: "cn", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	addrTrue := true
	prober := &fakeProber{
		health: providers.AccountHealth{Ready: true, Hot: true, UID: "q-uid"},
		quota: &providers.QuotaInfo{
			Used: 400, Total: 3400, Remaining: 1499, Percentage: 11.8, Unit: "credits",
			FetchedAt: "2026-11-14T00:00:00Z", ProviderID: "qoder", Plan: "PLAN_TIER_FREE",
			HasAddOn: true, AddOnUsed: 1501, AddOnTotal: 3000, AddOnRemaining: 1499, AddOnUnit: "credits", AddOnAvailable: &addrTrue,
		},
	}
	registry := providers.NewRegistry()
	registry.Register(providers.Adapter{ID: "qoder", Prober: prober})
	manager := accountruntime.NewManager(accountruntime.ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	manager.SetProviders(registry)
	manager.Pool().Upsert(executor.Item{ID: account.ID, Provider: "qoder", Runtime: "in_process"})

	if err := manager.RefreshAccount(ctx, account.ID, true); err != nil {
		t.Fatalf("refresh err = %v", err)
	}
	stored, err := store.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Quota == nil || !stored.Quota.HasAddOn || stored.Quota.AddOnRemaining != 1499 || stored.Quota.AddOnAvailable == nil || !*stored.Quota.AddOnAvailable {
		t.Fatalf("qoder breakdown lost through generic path: %+v", stored.Quota)
	}
}

func TestManagerRefreshClearsRecoveredDevinQuotaCooldown(t *testing.T) {
	ctx := context.Background()
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, accounts.CreateAccount{
		Name: "Devin", Provider: "devin", Region: "global", Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	prober := &fakeProber{
		health: providers.AccountHealth{Ready: true, Hot: true, UID: "devin-uid"},
		quota: &providers.QuotaInfo{
			Used: 10, Total: 100, Remaining: 90, Percentage: 10, Unit: "percent",
			FetchedAt: "2026-08-26T00:00:00Z", ProviderID: "devin",
			Windows: []providers.QuotaWindow{
				{ID: "daily", Label: "Daily quota", Used: 10, Total: 100, Remaining: 90, Percentage: 10, Unit: "percent"},
				{ID: "weekly", Label: "Weekly quota", Used: 20, Total: 100, Remaining: 80, Percentage: 20, Unit: "percent"},
			},
		},
		quotaDone: make(chan struct{}),
	}
	registry := providers.NewRegistry()
	registry.Register(providers.Adapter{ID: "devin", Prober: prober})
	manager := accountruntime.NewManager(accountruntime.ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	manager.SetProviders(registry)
	manager.Pool().Upsert(executor.Item{ID: account.ID, Provider: "devin", Runtime: "in_process"})
	manager.Pool().MarkClassified(account.ID, executor.Classified{
		Kind: accounts.KindQuota, Message: "usage quota exhausted", Cooldown: time.Hour,
	})
	manager.Pool().MarkClassified(account.ID, executor.Classified{
		Kind: accounts.KindRateLimit, Model: "swe-2", Message: "capacity", Cooldown: time.Hour,
	})
	if err := manager.RefreshAll(ctx, false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-prober.quotaDone:
	case <-time.After(time.Second):
		t.Fatal("quota refresh did not complete")
	}
	deadline := time.Now().Add(time.Second)
	for {
		item, _ := manager.Pool().ByID(account.ID)
		if item.Quota != nil && item.DownUntil.IsZero() && item.LastKind == "" {
			if until, ok := item.ModelDownUntil["swe-2"]; !ok || until.IsZero() {
				t.Fatalf("model cooldown must remain: %+v", item.ModelDownUntil)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("recovered quota cooldown not cleared: %+v", item)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestManagerRefreshSkipsEmptyURLWithoutProber(t *testing.T) {
	ctx := context.Background()
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, accounts.CreateAccount{
		Name: "WB", Provider: "workbuddy", Region: "cn", Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	manager := accountruntime.NewManager(accountruntime.ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	manager.Pool().Upsert(executor.Item{ID: account.ID, Provider: "workbuddy", Runtime: "in_process"})
	if err := manager.RefreshAll(ctx, false); err != nil {
		t.Fatalf("refresh without prober must be a no-op, got %v", err)
	}
	item, _ := manager.Pool().ByID(account.ID)
	if item.Ready != nil || item.LastError != "" {
		t.Fatalf("pool should be untouched, got %+v", item)
	}
}

func TestManagerRefreshUsesQoderAdapterCatalog(t *testing.T) {
	var modelHits int
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "ready": true, "hot": true, "uid": "qoder-uid-1"})
		case "/admin/quota":
			http.Error(w, "quota unused", http.StatusBadGateway)
		case "/admin/models":
			modelHits++
			if r.Header.Get("Authorization") != "Bearer proxy-key" {
				t.Errorf("models auth = %q", r.Header.Get("Authorization"))
			}
			if r.Header.Get("X-Qoder-Account") != "" {
				t.Errorf("runtime catalog sent X-Qoder-Account = %q", r.Header.Get("X-Qoder-Account"))
			}
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
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, accounts.CreateAccount{Name: "Catalog", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	manager := accountruntime.NewManager(accountruntime.ManagerConfig{DataDir: t.TempDir(), ProxyAPIKey: "proxy-key"}, store, &fakeStarter{})
	client := qoder.NewClient()
	client.Bind(manager.AccountURL, manager.ProxyAPIKey)
	registry := providers.NewRegistry()
	registry.Register(client.Adapter())
	manager.SetProviders(registry)
	manager.Pool().Upsert(executor.Item{ID: account.ID, URL: worker.URL, Provider: "qoder", Runtime: "child_process"})
	if err := manager.RefreshAll(ctx, false); err != nil {
		t.Fatal(err)
	}
	item, _ := manager.Pool().ByID(account.ID)
	if !containsModel(item.Models, "hy3") || !containsModel(item.Models, "gmodel") {
		t.Fatalf("cached models = %#v", item.Models)
	}
	if modelHits != 1 {
		t.Fatalf("adapter catalog double-fetched models: hits=%d", modelHits)
	}
}

func TestQoderAdapterProberSkipsWorkerURLAccounts(t *testing.T) {
	ctx := context.Background()
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// Worker/legacy account: no PAT payload. Even with the Prober registered,
	// refreshOne must take the worker /health branch (URL is non-empty) and not
	// call Prober.Probe, which would otherwise hit the Bridge fallback.
	account, err := store.Create(ctx, accounts.CreateAccount{Name: "LegacyQoder", Provider: "qoder", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	var probeHits int
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			probeHits++
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "ready": true, "hot": true, "uid": "legacy-uid"})
		case "/admin/quota":
			_ = json.NewEncoder(w).Encode(map[string]any{"quota": map[string]any{
				"isQuotaExceeded": false, "fetchedAt": "now",
				"userQuota": map[string]any{"total": 100.0, "used": 10.0, "remaining": 90.0, "unit": "credits"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer worker.Close()
	manager := accountruntime.NewManager(accountruntime.ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	client := qoder.NewClient()
	client.Bind(manager.AccountURL, manager.ProxyAPIKey)
	registry := providers.NewRegistry()
	registry.Register(client.Adapter())
	manager.SetProviders(registry)
	manager.Pool().Upsert(executor.Item{ID: account.ID, URL: worker.URL, Provider: "qoder", Runtime: "child_process"})
	if err := manager.RefreshAccount(ctx, account.ID, true); err != nil {
		t.Fatalf("worker-URL qoder refresh failed: %v", err)
	}
	item, _ := manager.Pool().ByID(account.ID)
	if item.Quota == nil || item.Quota.Remaining != 90 {
		t.Fatalf("worker quota not persisted through worker branch: %+v", item.Quota)
	}
	stored, err := store.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.RemoteUID != "legacy-uid" {
		t.Fatalf("worker uid not observed: %q", stored.RemoteUID)
	}
	if probeHits == 0 {
		t.Fatal("worker branch was not exercised: /health never hit")
	}
}
