package runtime_test

import (
	"context"
	"encoding/json"
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
	return f.quota, nil
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

func TestQoderAdapterRegistrationDoesNotProbeEmptyURL(t *testing.T) {
	ctx := context.Background()
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, accounts.CreateAccount{Name: "EmptyQoder", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	manager := accountruntime.NewManager(accountruntime.ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	client := qoder.NewClient()
	client.Bind(manager.AccountURL, manager.ProxyAPIKey)
	registry := providers.NewRegistry()
	registry.Register(client.Adapter())
	manager.SetProviders(registry)
	manager.Pool().Upsert(executor.Item{ID: account.ID, Provider: "qoder", Runtime: "child_process"})
	if err := manager.RefreshAll(ctx, false); err != nil {
		t.Fatalf("empty-URL qoder with Adapter must stay a no-op, got %v", err)
	}
	item, _ := manager.Pool().ByID(account.ID)
	if item.Ready != nil || item.LastError != "" || item.Models != nil {
		t.Fatalf("pool should be untouched, got %+v", item)
	}
}
