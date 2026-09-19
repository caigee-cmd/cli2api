package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func TestProviderCheckinInheritanceAndReset(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "checkin.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, providerID := range []string{"workbuddy", "qoder"} {
		account, err := store.Create(ctx, accounts.CreateAccount{Name: providerID, Provider: providerID, Region: "cn"})
		if err != nil {
			t.Fatal(err)
		}
		if account.AutoCheckin || account.CheckinTime != "" {
			t.Fatalf("new account does not inherit with opt-in off: %+v", account)
		}
		if err := store.SetSecret(ctx, accounts.CheckinTimeSecret(providerID), "10:30"); err != nil {
			t.Fatal(err)
		}
		effective, err := accounts.ResolveCheckinTime(ctx, store, account)
		if err != nil || effective != "10:30" {
			t.Fatalf("effective=%q err=%v", effective, err)
		}
		if err := store.Update(ctx, account.ID, accounts.UpdateAccount{AutoCheckin: boolPtr(true), CheckinTime: stringPtr("18:30")}); err != nil {
			t.Fatal(err)
		}
		if err := store.SetSecret(ctx, accounts.CheckinTimeSecret(providerID), "11:30"); err != nil {
			t.Fatal(err)
		}
		account, err = store.Get(ctx, account.ID)
		if err != nil {
			t.Fatal(err)
		}
		effective, err = accounts.ResolveCheckinTime(ctx, store, account)
		if err != nil || effective != "18:30" || !account.AutoCheckin {
			t.Fatalf("override=%q err=%v account=%+v", effective, err, account)
		}
		if err := store.Update(ctx, account.ID, accounts.UpdateAccount{CheckinTime: stringPtr("")}); err != nil {
			t.Fatal(err)
		}
		account, err = store.Get(ctx, account.ID)
		if err != nil {
			t.Fatal(err)
		}
		effective, err = accounts.ResolveCheckinTime(ctx, store, account)
		if err != nil || effective != "11:30" || account.CheckinTime != "" {
			t.Fatalf("reset=%q err=%v", effective, err)
		}
	}
}

func TestProviderCheckinRejectsUnsupportedRegionsAndInvalidTimes(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "checkin.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, input := range []accounts.CreateAccount{
		{Name: "global", Provider: "qoder", Region: "global", AutoCheckin: boolPtr(true)},
		{Name: "unsupported", Provider: "devin", Region: "global", CheckinTime: "10:00"},
		{Name: "invalid", Provider: "qoder", Region: "cn", CheckinTime: "9:00"},
		{Name: "invalid", Provider: "qoder", Region: "cn", CheckinTime: "24:00"},
	} {
		if _, err := store.Create(ctx, input); err == nil {
			t.Fatalf("accepted %+v", input)
		}
	}
	account, err := store.Create(ctx, accounts.CreateAccount{Name: "global", Provider: "qoder", Region: "global"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(ctx, account.ID, accounts.UpdateAccount{AutoCheckin: boolPtr(true)}); err == nil {
		t.Fatal("enabled unsupported region")
	}
}

func TestProviderCheckinMigrationPreservesLegacyAccounts(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "legacy.db")
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	legacy := &Store{db: database}
	if _, err := database.ExecContext(ctx, schemaMigrationsDDL); err != nil {
		t.Fatal(err)
	}
	for _, migration := range sqliteMigrations[:20] {
		if err := legacy.applyMigration(ctx, migration); err != nil {
			t.Fatal(err)
		}
	}
	_, err = database.ExecContext(ctx, `INSERT INTO accounts (id, name, provider, provider_region, workbuddy_auto_checkin, workbuddy_checkin_time, created_at, updated_at) VALUES ('wb', 'wb', 'workbuddy', 'cn', 1, '18:30', '', ''), ('qoder', 'qoder', 'qoder', 'cn', 1, '10:00', '', '')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.Get(ctx, "wb")
	if err != nil || !account.AutoCheckin || account.CheckinTime != "18:30" {
		t.Fatalf("legacy account=%+v err=%v", account, err)
	}
	other, err := store.Get(ctx, "qoder")
	if err != nil || other.AutoCheckin || other.CheckinTime != "" {
		t.Fatalf("unexpected opt-in=%+v err=%v", other, err)
	}
}
