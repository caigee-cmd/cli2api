package store

import (
	"context"
	"github.com/caigee-cmd/cli2api/internal/accounts"
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

	created, err := store.Create(ctx, accounts.CreateAccount{
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

	legacy, err := store.Create(ctx, accounts.CreateAccount{Name: "Legacy", Enabled: false})
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
	if _, err := store.Create(ctx, accounts.CreateAccount{Name: "X", Provider: "cursor"}); err == nil {
		t.Fatal("expected unknown provider rejection")
	}
	if _, err := store.Create(ctx, accounts.CreateAccount{Name: "X", Provider: "qoder", Region: "eu"}); err == nil {
		t.Fatal("expected unknown region rejection")
	}
	created, err := store.Create(ctx, accounts.CreateAccount{Name: "CN", Provider: "qoder", Region: "cn"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Provider != "qoder" || created.ProviderRegion != "cn" {
		t.Fatalf("qoder cn = %+v", created)
	}
	trae, err := store.Create(ctx, accounts.CreateAccount{Name: "Trae", Provider: "trae", Region: "cn"})
	if err != nil {
		t.Fatal(err)
	}
	if trae.Provider != "trae" || trae.ProviderRegion != "cn" {
		t.Fatalf("trae cn = %+v", trae)
	}
	if _, err := store.Create(ctx, accounts.CreateAccount{Name: "X", Provider: "trae", Region: "global"}); err == nil {
		t.Fatal("expected trae global rejection")
	}
}
