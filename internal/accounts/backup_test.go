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
