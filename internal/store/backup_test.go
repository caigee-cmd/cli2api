package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"github.com/caigee-cmd/cli2api/internal/accounts"
	"os"
	"path/filepath"
	"testing"
	"time"

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

	account, err := store.Create(ctx, accounts.CreateAccount{Name: "Primary", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCredential(ctx, account.ID, "native", accounts.NativeCredential{
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

func TestPublishedMigrationsKeepOrderedFilenameAndSQLDigest(t *testing.T) {
	want := []struct {
		filename string
		checksum string
	}{
		{"001_initial_schema.sql", "c4a754531f1842133eb8deed76f89a2416df84b3ca50428f300327dea7032072"},
		{"002_normalize_model_settings.sql", "0a498995252285ab7ff7ba7d0be07a174e74ef5cbdc65b19a5d2782a8849afa5"},
		{"003_account_providers.sql", "24bfa90f6d2022daa4c5afaba8b99ab58092e97675565962ad2029b822251e45"},
		{"004_request_logs.sql", "726ad8cc20408b8974afbbaa8be3c5a1da99815fef65e290a323013074302caf"},
		{"005_account_drop_system_prompt.sql", "364e363ca8689d9c4ada8bc67e724680c3631ad231ac6da8c3d62328318f86ca"},
		{"006_request_log_provider.sql", requestLogProviderChecksum},
		{"007_provider_model_settings.sql", providerModelSettingsChecksum},
		{"008_api_keys.sql", "6f203268ddecdb1d94c2b58e7c6e2ead750d96bafd8d82839814f645a5c7dbd5"},
		{"009_provider_model_reasoning.sql", "98679b669666db6844fec8c761ad6251fecc05ec2945fc65c286958f918d6eb1"},
		{"010_workbuddy_auto_checkin.sql", "3e74bb499322e90102fc19d13781294effb872d27390309e5dd5399ad7e45ca0"},
		{"011_account_cooldowns.sql", "8dbf7b362dff4d4ea9f87f12d7534cc441f7b1b1158d6ac2380c1ad14dff8aa1"},
		{"012_account_cooldown_model_kind.sql", "a448c3433adfa82606a9deef93d25bfe086745fbc67c1c91845a27b4597f4261"},
		{"013_checkin_records.sql", "378a9abe2aa3ef82bd7cd3f2422bfced951c3ee5857b1bb25a50204df59947f4"},
		{"014_request_log_routing.sql", "a9f4cb61c312641c09d4cf32c83b03682b52258b0f30a10092a26a4c0f245a68"},
		{"015_account_quota_state.sql", "fb8c8bf9b072b7c0b69368067b0439c40764801055e19aa820beddd150fbcaad"},
		{"016_request_stream_diagnostics.sql", "9bc9ac985096ad1ffb5921f314b62a47b03c72cdd9f875b645e1be8c8996bf82"},
		{"017_request_message_shape.sql", "2d705b35b5a316e1f77e00de4f16c59cb0e3893da4f14f8aeb5a60b18765340e"},
		{"018_workbuddy_checkin_time.sql", "b0568fd514bc9462e2bf85d35210fdee6726d498fba84908766d9353aafdd5f6"},
		{"019_account_proxy.sql", "7d27f4b71568d285bea46459bb3e5718c97f842b12149e1922fce7502b68ff6e"},
		{"020_request_usage_details.sql", "ddc2881cd29c84eb7652a9243b05fc22485e8fc3ffcad9f3879b7dcea9e15022"},
	}
	if len(sqliteMigrations) != len(want) {
		t.Fatalf("migration count = %d, want %d", len(sqliteMigrations), len(want))
	}
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for i, item := range sqliteMigrations {
		if item.filename != want[i].filename {
			t.Fatalf("order[%d]=%s want %s", i, item.filename, want[i].filename)
		}
		got := migrationChecksum(item.sql)
		if got != want[i].checksum {
			t.Fatalf("%s checksum=%s want %s", item.filename, got, want[i].checksum)
		}
		digest := sha256.Sum256([]byte(item.sql))
		if hex.EncodeToString(digest[:]) != got {
			t.Fatalf("%s digest helper drifted", item.filename)
		}
		var recorded string
		if err := store.DB().QueryRow("SELECT checksum FROM schema_migrations WHERE filename = ?", item.filename).Scan(&recorded); err != nil {
			t.Fatal(err)
		}
		if recorded != want[i].checksum {
			t.Fatalf("recorded %s=%s want %s", item.filename, recorded, want[i].checksum)
		}
	}
}

func TestStoreBackupPrunesOlderSnapshots(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := OpenStore(filepath.Join(root, "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	dir := filepath.Join(root, "backups")
	if _, err := store.Backup(ctx, dir, 2); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := store.Backup(ctx, dir, 2); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := store.Backup(ctx, dir, 2); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var backups int
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".db" {
			backups++
		}
	}
	if backups != 2 {
		t.Fatalf("kept %d backups, want 2 (%v)", backups, entries)
	}
}

func TestStoreRecordsImmutableMigrationChecksums(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	rows, err := store.DB().QueryContext(ctx, "SELECT filename, checksum FROM schema_migrations ORDER BY filename")
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
	if _, err := store.DB().Exec("UPDATE schema_migrations SET checksum = 'changed' WHERE filename = '001_initial_schema.sql'"); err != nil {
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
	if err := store.DB().QueryRow("SELECT checksum FROM schema_migrations WHERE filename = ?", requestLogProviderMigration).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != requestLogProviderChecksum {
		t.Fatalf("fresh 006 checksum = %s, want %s", recorded, requestLogProviderChecksum)
	}
	if _, err := store.DB().Exec("UPDATE schema_migrations SET checksum = ? WHERE filename = ?", requestLogProviderV0219, requestLogProviderMigration); err != nil {
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
	if err := store.DB().QueryRow("SELECT checksum FROM schema_migrations WHERE filename = ?", providerModelSettingsMigration).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != providerModelSettingsChecksum {
		t.Fatalf("fresh 007 checksum = %s, want %s", recorded, providerModelSettingsChecksum)
	}
	if _, err := store.DB().Exec("UPDATE schema_migrations SET checksum = ? WHERE filename = ?", providerModelSettingsRetabbed, providerModelSettingsMigration); err != nil {
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
