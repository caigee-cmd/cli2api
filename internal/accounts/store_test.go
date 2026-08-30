package accounts

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

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

func TestProviderModelMaxModeIsIndependentOfQoderContext(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SetModelContext(ctx, "glm-5.2", 500000); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProviderModelMaxMode(ctx, "trae", "glm-5.2", true); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.GetModelContext(ctx, "glm-5.2")
	if err != nil || !ok || got != 500000 {
		t.Fatalf("qoder context changed: %d %v %v", got, ok, err)
	}
	maxMode, err := store.GetProviderModelMaxMode(ctx, "trae", "glm-5.2")
	if err != nil || !maxMode {
		t.Fatalf("trae max mode=%v %v", maxMode, err)
	}
	if err := store.SetProviderModelMaxMode(ctx, "trae", "glm-5.2", false); err != nil {
		t.Fatal(err)
	}
	maxMode, err = store.GetProviderModelMaxMode(ctx, "trae", "glm-5.2")
	if err != nil || maxMode {
		t.Fatalf("reset max mode=%v %v", maxMode, err)
	}
}

func TestProviderModelReasoningEffortPersistsWithMaxMode(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SetProviderModelSetting(ctx, "workbuddy", "glm-5.3", ProviderModelSetting{ReasoningEffort: "xhigh"}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetProviderModelSetting(ctx, "workbuddy", "glm-5.3")
	if err != nil || got.MaxMode || got.ReasoningEffort != "xhigh" {
		t.Fatalf("workbuddy setting=%+v %v", got, err)
	}
	if err := store.SetProviderModelSetting(ctx, "trae", "glm-5.3", ProviderModelSetting{MaxMode: true, ReasoningEffort: "low"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProviderModelMaxMode(ctx, "trae", "glm-5.3", false); err != nil {
		t.Fatal(err)
	}
	got, err = store.GetProviderModelSetting(ctx, "trae", "glm-5.3")
	if err != nil || got.MaxMode || got.ReasoningEffort != "low" {
		t.Fatalf("trae setting after max off=%+v %v", got, err)
	}
}

func TestStoreCreatesNamedAPIKeys(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	all, err := store.CreateAPIKey(ctx, CreateAPIKey{Name: "All", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if all.Providers == nil || len(all.Providers) != 0 {
		t.Fatalf("empty allowlist providers = %#v", all.Providers)
	}
	listedAll, err := store.ListAPIKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listedAll) != 1 || listedAll[0].Providers == nil {
		t.Fatalf("listed empty allowlist = %+v", listedAll)
	}
	if err := store.DeleteAPIKey(ctx, all.ID); err != nil {
		t.Fatal(err)
	}

	created, err := store.CreateAPIKey(ctx, CreateAPIKey{Name: "CI", Providers: []string{"qoder"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if created.Secret == "" || created.ID == "" || created.Prefix == "" {
		t.Fatalf("created key missing secret fields: %+v", created)
	}
	listed, err := store.ListAPIKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Secret != "" || listed[0].ID != created.ID {
		t.Fatalf("listed = %+v", listed)
	}
	got, ok, err := store.LookupAPIKey(ctx, created.Secret)
	if err != nil || !ok || got.ID != created.ID {
		t.Fatalf("lookup = %+v ok=%v err=%v", got, ok, err)
	}
	disabled := false
	updated, err := store.UpdateAPIKey(ctx, created.ID, UpdateAPIKey{Name: "CI prod", Providers: []string{"qoder", "trae"}, Enabled: &disabled})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "CI prod" || updated.Enabled || len(updated.Providers) != 2 {
		t.Fatalf("updated = %+v", updated)
	}
	if err := store.DeleteAPIKey(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetAPIKey(ctx, created.ID); !errors.Is(err, ErrAPIKeyNotFound) {
		t.Fatalf("deleted key err = %v", err)
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

func TestStoreDefaultsDropSystemPromptOn(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{Name: "wb", Provider: "workbuddy", Region: "cn"})
	if err != nil {
		t.Fatal(err)
	}
	if !account.DropSystemPrompt {
		t.Fatalf("new account must default to dropping system prompts: %+v", account)
	}
	reloaded, err := store.Get(ctx, account.ID)
	if err != nil || !reloaded.DropSystemPrompt {
		t.Fatalf("reloaded=%+v err=%v", reloaded, err)
	}
	if err := store.Update(ctx, account.ID, UpdateAccount{DropSystemPrompt: boolPtr(false)}); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Get(ctx, account.ID)
	if err != nil || updated.DropSystemPrompt {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
}

func TestStoreCreateHonorsDropSystemPromptAndInFlight(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{
		Name: "wb", Provider: "workbuddy", Region: "cn",
		MaxInFlight: 6, Priority: 80, DropSystemPrompt: boolPtr(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	if account.MaxInFlight != 6 || account.Priority != 80 || account.DropSystemPrompt {
		t.Fatalf("created=%+v", account)
	}
	reloaded, err := store.Get(ctx, account.ID)
	if err != nil || reloaded.MaxInFlight != 6 || reloaded.Priority != 80 || reloaded.DropSystemPrompt {
		t.Fatalf("reloaded=%+v err=%v", reloaded, err)
	}
}

func TestStoreDefaultsWorkBuddyAutoCheckinOff(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Create(ctx, CreateAccount{Name: "wb", Provider: "workbuddy", Region: "cn"})
	if err != nil {
		t.Fatal(err)
	}
	if account.WorkBuddyAutoCheckin {
		t.Fatalf("auto check-in must default off: %+v", account)
	}
	if err := store.Update(ctx, account.ID, UpdateAccount{WorkBuddyAutoCheckin: boolPtr(true)}); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Get(ctx, account.ID)
	if err != nil || !updated.WorkBuddyAutoCheckin {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	at := time.Date(2026, 8, 30, 9, 5, 0, 0, time.UTC)
	if err := store.RecordCheckin(ctx, account.ID, "签到成功", at); err != nil {
		t.Fatal(err)
	}
	recorded, err := store.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recorded.LastCheckinMsg != "签到成功" || recorded.LastCheckinAt == "" {
		t.Fatalf("recorded=%+v", recorded)
	}
}

func TestNextWorkBuddyFireKinds(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 8, 30, 8, 0, 0, 0, loc)
	delay, kind := nextWorkBuddyFire(now)
	if kind != "checkin" {
		t.Fatalf("kind=%q", kind)
	}
	if delay <= 0 || delay > 2*time.Hour {
		t.Fatalf("delay=%v", delay)
	}
	evening := time.Date(2026, 8, 30, 21, 30, 0, 0, loc)
	_, kind = nextWorkBuddyFire(evening)
	if kind != "keepalive" {
		t.Fatalf("after 21:30 want keepalive, got %q", kind)
	}
}

func boolPtr(value bool) *bool { return &value }
func intPtr(value int) *int    { return &value }
