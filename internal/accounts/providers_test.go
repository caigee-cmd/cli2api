package accounts

import (
	"context"
	"path/filepath"
	"testing"
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
	if _, err := store.Create(ctx, CreateAccount{Name: "X", Provider: "qoder", Region: "cn"}); err == nil {
		t.Fatal("expected unknown region rejection")
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
