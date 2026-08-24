package accounts

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
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
	if err := store.SetModelContext(ctx, "minimax-m3", 500000); err != nil {
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
	got, ok, err := reopened.GetModelContext(ctx, "minimax-m3")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got != 500000 {
		t.Fatalf("model context = %d, %v", got, ok)
	}
	if err := reopened.SetModelContext(ctx, "minimax-m3", 1); err == nil {
		t.Fatal("expected too-small context length to fail")
	}
	if err := reopened.SetModelContext(ctx, "minimax-m3", 0); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := reopened.GetModelContext(ctx, "minimax-m3"); err != nil || ok {
		t.Fatalf("deleted model context ok=%v err=%v", ok, err)
	}
}

func TestStorePersistsAppSecrets(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "qoder.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecret(ctx, "proxy_api_key", "secret-value"); err != nil {
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
	got, ok, err := reopened.GetSecret(ctx, "proxy_api_key")
	if err != nil || !ok || got != "secret-value" {
		t.Fatalf("secret = %q, %v, %v", got, ok, err)
	}
}

func TestStoreMigratesLegacyModelContextKeys(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "qoder.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE model_settings (
  model_id TEXT PRIMARY KEY,
  context_length INTEGER NOT NULL,
  updated_at TEXT NOT NULL
);
INSERT INTO model_settings (model_id, context_length, updated_at) VALUES
  ('mmodel', 750000, '2026-01-01T00:00:00Z'),
  ('qmodel', 250000, '2026-01-01T00:00:00Z');`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	for model, want := range map[string]int{
		"minimax-m3":   750000,
		"qwen3.7-plus": 250000,
		"glm-5.2":      250000,
	} {
		got, ok, err := reopened.GetModelContext(ctx, model)
		if err != nil || !ok || got != want {
			t.Fatalf("model context %s = %d, %v, %v", model, got, ok, err)
		}
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
