package accounts

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestStoreCreatesAndReloadsAccount(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "qoder.db")

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.Create(ctx, CreateAccount{
		Name:        "Work",
		Enabled:     true,
		MaxInFlight: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" {
		t.Fatal("expected generated account id")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Work" || !got.Enabled || got.MaxInFlight != 4 {
		t.Fatalf("reloaded account = %+v", got)
	}
}

func TestStorePersistsModelContextSettings(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "qoder.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetModelContext(ctx, "mmodel", 500000); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, ok, err := reopened.GetModelContext(ctx, "mmodel")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got != 500000 {
		t.Fatalf("model context = %d, %v", got, ok)
	}
	if err := reopened.SetModelContext(ctx, "mmodel", 1); err == nil {
		t.Fatal("expected too-small context length to fail")
	}
	if err := reopened.SetModelContext(ctx, "mmodel", 0); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := reopened.GetModelContext(ctx, "mmodel"); err != nil || ok {
		t.Fatalf("deleted model context ok=%v err=%v", ok, err)
	}
}

func TestStoreSavesNativeCredential(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{Name: "Imported", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	want := NativeCredential{UserBlob: []byte("encrypted-user"), MachineID: "machine-1"}
	if err := store.SaveCredential(ctx, account.ID, "native", want); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadCredential(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.UserBlob) != string(want.UserBlob) || got.MachineID != want.MachineID {
		t.Fatalf("credential = %+v", got)
	}
	updated, err := store.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.AuthType != "native" {
		t.Fatalf("auth type = %q", updated.AuthType)
	}
}

func TestStoreListsUpdatesAndDeletesAccounts(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	first, _ := store.Create(ctx, CreateAccount{Name: "A", Enabled: true})
	_, _ = store.Create(ctx, CreateAccount{Name: "B", Enabled: false})

	if err := store.Update(ctx, first.ID, UpdateAccount{Name: "Primary", Enabled: boolPtr(false), MaxInFlight: intPtr(7)}); err != nil {
		t.Fatal(err)
	}
	items, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Name != "Primary" || items[0].Enabled || items[0].MaxInFlight != 7 {
		t.Fatalf("accounts = %+v", items)
	}
	if err := store.Delete(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, first.ID); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("get deleted account error = %v", err)
	}
}

func boolPtr(value bool) *bool { return &value }
func intPtr(value int) *int    { return &value }
