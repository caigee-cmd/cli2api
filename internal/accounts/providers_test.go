package accounts_test

import (
	"context"
	"github.com/caigee-cmd/cli2api/internal/accounts"
	"path/filepath"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
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
	manager := accounts.NewManager(accounts.ManagerConfig{DataDir: t.TempDir(), BasePort: 32300}, store, starter)
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

	manager := accounts.NewManager(accounts.ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	manager.SetProviders(registry)
	manager.Pool().Upsert(accounts.Item{ID: account.ID, Provider: "workbuddy", Runtime: "in_process"})

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
	var item accounts.Item
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
	manager := accounts.NewManager(accounts.ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	manager.Pool().Upsert(accounts.Item{ID: account.ID, Provider: "workbuddy", Runtime: "in_process"})
	if err := manager.RefreshAll(ctx, false); err != nil {
		t.Fatalf("refresh without prober must be a no-op, got %v", err)
	}
	item, _ := manager.Pool().ByID(account.ID)
	if item.Ready != nil || item.LastError != "" {
		t.Fatalf("pool should be untouched, got %+v", item)
	}
}
