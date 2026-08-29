package accounts

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestStoreBackupCreatesConsistentSQLiteSnapshot(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := OpenStore(filepath.Join(root, "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	account, err := store.Create(ctx, CreateAccount{Name: "Primary", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCredential(ctx, account.ID, "native", NativeCredential{
		UserBlob: []byte("encrypted-user"), MachineID: "machine-1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecret(ctx, "proxy_api_key", "secret-value"); err != nil {
		t.Fatal(err)
	}

	backup, err := store.Backup(ctx, filepath.Join(root, "backups"), 5)
	if err != nil {
		t.Fatal(err)
	}
	if backup.Path == "" || backup.Name == "" {
		t.Fatalf("backup = %+v", backup)
	}
	info, err := os.Stat(backup.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode = %o, want 600", info.Mode().Perm())
	}

	db, err := sql.Open("sqlite", backup.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var accountCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM accounts WHERE id = ?", account.ID).Scan(&accountCount); err != nil {
		t.Fatal(err)
	}
	if accountCount != 1 {
		t.Fatalf("account count = %d", accountCount)
	}
	var secret string
	if err := db.QueryRowContext(ctx, "SELECT value FROM app_secrets WHERE name = 'proxy_api_key'").Scan(&secret); err != nil {
		t.Fatal(err)
	}
	if secret != "secret-value" {
		t.Fatalf("secret = %q", secret)
	}
}

func TestStoreRecordsImmutableMigrationChecksums(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	rows, err := store.db.QueryContext(ctx, "SELECT filename, checksum FROM schema_migrations ORDER BY filename")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var filename, checksum string
		if err := rows.Scan(&filename, &checksum); err != nil {
			t.Fatal(err)
		}
		if filename == "" || len(checksum) != 64 {
			t.Fatalf("migration filename=%q checksum=%q", filename, checksum)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count < 2 {
		t.Fatalf("migration count = %d, want at least 2", count)
	}
}

func TestStoreRejectsChangedAppliedMigration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "qoder.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("UPDATE schema_migrations SET checksum = 'changed' WHERE filename = '001_initial_schema.sql'"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(dbPath); err == nil {
		t.Fatal("expected migration checksum mismatch")
	}
}

const (
	requestLogProviderMigration = "006_request_log_provider.sql"
	requestLogProviderChecksum  = "2deb3ef3aa94df34a8ffd0ac50a69b6cb710e7bf2e9041fc1c1f0bdfd7cb0d67"
	requestLogProviderV0219     = "9b96b8d63286519b20791d2a3688c0b71efba6204015250ae9864c5cbcd1f0b4"
)

func TestRequestLogProviderMigrationKeepsV0218Bytes(t *testing.T) {
	var migration sqliteMigration
	for _, item := range sqliteMigrations {
		if item.filename == requestLogProviderMigration {
			migration = item
			break
		}
	}
	if migration.filename == "" {
		t.Fatal("missing 006_request_log_provider.sql")
	}
	got := migrationChecksum(migration.sql)
	if got != requestLogProviderChecksum {
		t.Fatalf("006 checksum = %s, want v0.2.18 %s", got, requestLogProviderChecksum)
	}
	if !checksumAccepted(migration, requestLogProviderV0219) {
		t.Fatal("expected v0.2.19 tab-indented checksum to remain accepted")
	}
}

func TestStoreOpensDatabaseWithV0219RequestLogProviderChecksum(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "qoder.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	var recorded string
	if err := store.db.QueryRow("SELECT checksum FROM schema_migrations WHERE filename = ?", requestLogProviderMigration).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != requestLogProviderChecksum {
		t.Fatalf("fresh 006 checksum = %s, want %s", recorded, requestLogProviderChecksum)
	}
	if _, err := store.db.Exec("UPDATE schema_migrations SET checksum = ? WHERE filename = ?", requestLogProviderV0219, requestLogProviderMigration); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	reopened.Close()
}

const (
	providerModelSettingsMigration = "007_provider_model_settings.sql"
	providerModelSettingsChecksum  = "b48b62c578bff658ee5843776fe16dd35d11d86c84800398c98d666eb777968a"
	providerModelSettingsRetabbed  = "8940e0c639008f811dd844add98865f700f32ca0a4fd672c1bcc0adf3f69ef71"
)

func TestProviderModelSettingsMigrationKeepsV0220Bytes(t *testing.T) {
	var migration sqliteMigration
	for _, item := range sqliteMigrations {
		if item.filename == providerModelSettingsMigration {
			migration = item
			break
		}
	}
	if migration.filename == "" {
		t.Fatal("missing 007_provider_model_settings.sql")
	}
	got := migrationChecksum(migration.sql)
	if got != providerModelSettingsChecksum {
		t.Fatalf("007 checksum = %s, want v0.2.20 %s", got, providerModelSettingsChecksum)
	}
	if !checksumAccepted(migration, providerModelSettingsRetabbed) {
		t.Fatal("expected retabbed 007 checksum to remain accepted")
	}
}

func TestStoreOpensDatabaseWithRetabbedProviderModelSettingsChecksum(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "qoder.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	var recorded string
	if err := store.db.QueryRow("SELECT checksum FROM schema_migrations WHERE filename = ?", providerModelSettingsMigration).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != providerModelSettingsChecksum {
		t.Fatalf("fresh 007 checksum = %s, want %s", recorded, providerModelSettingsChecksum)
	}
	if _, err := store.db.Exec("UPDATE schema_migrations SET checksum = ? WHERE filename = ?", providerModelSettingsRetabbed, providerModelSettingsMigration); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	reopened.Close()
}
