package accounts

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

func TestStorePersistsProviderAndRegion(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	created, err := store.Create(ctx, CreateAccount{
		Name: "WB", Provider: "workbuddy", Region: "cn", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Provider != "workbuddy" || created.ProviderRegion != "cn" {
		t.Fatalf("created = %+v", created)
	}
	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != "workbuddy" || got.ProviderRegion != "cn" {
		t.Fatalf("reloaded provider=%q region=%q", got.Provider, got.ProviderRegion)
	}

	legacy, err := store.Create(ctx, CreateAccount{Name: "Legacy", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Provider != "qoder" || legacy.ProviderRegion != "global" {
		t.Fatalf("legacy defaults = %+v", legacy)
	}
}

func TestStoreRejectsUnknownProviderAndRegion(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Create(ctx, CreateAccount{Name: "X", Provider: "cursor"}); err == nil {
		t.Fatal("expected unknown provider rejection")
	}
	if _, err := store.Create(ctx, CreateAccount{Name: "X", Provider: "qoder", Region: "eu"}); err == nil {
		t.Fatal("expected unknown region rejection")
	}
	created, err := store.Create(ctx, CreateAccount{Name: "CN", Provider: "qoder", Region: "cn"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Provider != "qoder" || created.ProviderRegion != "cn" {
		t.Fatalf("qoder cn = %+v", created)
	}
	trae, err := store.Create(ctx, CreateAccount{Name: "Trae", Provider: "trae", Region: "cn"})
	if err != nil {
		t.Fatal(err)
	}
	if trae.Provider != "trae" || trae.ProviderRegion != "cn" {
		t.Fatalf("trae cn = %+v", trae)
	}
	if _, err := store.Create(ctx, CreateAccount{Name: "X", Provider: "trae", Region: "global"}); err == nil {
		t.Fatal("expected trae global rejection")
	}
}

func TestManagerDoesNotSpawnDaemonForInProcessProvider(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	starter := &fakeStarter{}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir(), BasePort: 32300}, store, starter)
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	account, err := manager.Create(ctx, CreateAccount{
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

	qoder, err := manager.Create(ctx, CreateAccount{Name: "Q", Provider: "qoder", Enabled: true})
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
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{
		Name: "WB", Provider: "workbuddy", Region: "cn", Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Observe(ctx, account.ID, "", "error", "Get \"/health\": unsupported protocol scheme \"\"", KindUnavailable)

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

	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	manager.SetProviders(registry)
	manager.pool.Upsert(Item{ID: account.ID, Provider: "workbuddy", Runtime: "in_process"})

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
	item, _ := manager.pool.ByID(account.ID)
	if item.Ready == nil || !*item.Ready || item.Hot == nil || !*item.Hot || item.LastError != "" {
		t.Fatalf("pool item = %+v", item)
	}
	if item.Quota == nil || item.Quota.Remaining != 900 || item.Quota.Total != 1000 {
		t.Fatalf("quota = %+v", item.Quota)
	}
	updated, err := store.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "ready" || updated.RemoteUID != "wb-uid" || updated.LastError != "" {
		t.Fatalf("store account = %+v", updated)
	}
	if err := manager.startAccount(ctx, updated); err != nil {
		t.Fatal(err)
	}
	item, _ = manager.pool.ByID(account.ID)
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
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{
		Name: "WB", Provider: "workbuddy", Region: "cn", Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, &fakeStarter{})
	manager.pool.Upsert(Item{ID: account.ID, Provider: "workbuddy", Runtime: "in_process"})
	if err := manager.RefreshAll(ctx, false); err != nil {
		t.Fatalf("refresh without prober must be a no-op, got %v", err)
	}
	item, _ := manager.pool.ByID(account.ID)
	if item.Ready != nil || item.LastError != "" {
		t.Fatalf("pool should be untouched, got %+v", item)
	}
}
