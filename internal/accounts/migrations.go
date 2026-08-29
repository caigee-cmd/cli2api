package accounts

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

type sqliteMigration struct {
	filename        string
	sql             string
	legacyChecksums []string
}

var sqliteMigrations = []sqliteMigration{
	{filename: "001_initial_schema.sql", sql: `
CREATE TABLE IF NOT EXISTS accounts (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  remote_uid TEXT NOT NULL DEFAULT '',
  auth_type TEXT NOT NULL DEFAULT 'none',
  enabled INTEGER NOT NULL DEFAULT 1,
  max_inflight INTEGER NOT NULL DEFAULT 4,
  priority INTEGER NOT NULL DEFAULT 50,
  status TEXT NOT NULL DEFAULT 'offline',
  last_error TEXT NOT NULL DEFAULT '',
  last_error_kind TEXT NOT NULL DEFAULT '',
  cooldown_until TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS account_credentials (
  account_id TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
  user_blob BLOB NOT NULL,
  machine_id TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS model_settings (
  model_id TEXT PRIMARY KEY,
  context_length INTEGER NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS app_secrets (
  name TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`},
	{filename: "002_normalize_model_settings.sql", sql: `
INSERT INTO model_settings (model_id, context_length, updated_at)
SELECT CASE model_id
  WHEN 'qmodel' THEN 'qwen3.7-plus'
  WHEN 'dmodel' THEN 'deepseek-v4-pro'
  WHEN 'dfmodel' THEN 'deepseek-v4-flash'
  WHEN 'kmodel' THEN 'kimi-k2.7-code'
  WHEN 'mmodel' THEN 'minimax-m3'
  WHEN 'gm51model' THEN 'glm-5.1'
END, context_length, updated_at
FROM model_settings
WHERE model_id IN ('qmodel', 'dmodel', 'dfmodel', 'kmodel', 'mmodel', 'gm51model')
ON CONFLICT(model_id) DO NOTHING;
INSERT INTO model_settings (model_id, context_length, updated_at)
SELECT 'glm-5.2', context_length, updated_at
FROM model_settings
WHERE model_id = 'qmodel'
ON CONFLICT(model_id) DO NOTHING;
DELETE FROM model_settings
WHERE model_id IN ('qmodel', 'dmodel', 'dfmodel', 'kmodel', 'mmodel', 'gm51model');`},
	{filename: "003_account_providers.sql", sql: `
ALTER TABLE accounts ADD COLUMN provider TEXT NOT NULL DEFAULT 'qoder';
ALTER TABLE accounts ADD COLUMN provider_region TEXT NOT NULL DEFAULT 'global';
CREATE TABLE IF NOT EXISTS account_credential_payloads (
  account_id TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
  format TEXT NOT NULL,
  payload BLOB NOT NULL,
  updated_at TEXT NOT NULL
);`},
	{filename: "004_request_logs.sql", sql: `
CREATE TABLE IF NOT EXISTS request_logs (
  id TEXT PRIMARY KEY,
  created_at TEXT NOT NULL,
  finished_at TEXT,
  stream INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL,
  requested_model TEXT NOT NULL DEFAULT '',
  mapped_model TEXT NOT NULL DEFAULT '',
  account_id TEXT,
  prompt_tokens INTEGER,
  completion_tokens INTEGER,
  cache_read_tokens INTEGER,
  cache_write_tokens INTEGER,
  usage_source TEXT NOT NULL DEFAULT '',
  credits REAL,
  latency_ms INTEGER,
  ttfb_ms INTEGER,
  error_kind TEXT NOT NULL DEFAULT '',
  error_code TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  attempt_count INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS request_logs_created_at ON request_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS request_logs_account_id ON request_logs(account_id);
CREATE INDEX IF NOT EXISTS request_logs_status ON request_logs(status);
CREATE TABLE IF NOT EXISTS request_attempts (
  id TEXT PRIMARY KEY,
  request_id TEXT NOT NULL REFERENCES request_logs(id) ON DELETE CASCADE,
  attempt_index INTEGER NOT NULL,
  account_id TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL,
  finished_at TEXT,
  status TEXT NOT NULL,
  http_status INTEGER,
  error_kind TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  latency_ms INTEGER,
  prompt_tokens INTEGER,
  completion_tokens INTEGER,
  usage_source TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS request_attempts_request_id ON request_attempts(request_id);`},
	{filename: "005_account_drop_system_prompt.sql", sql: `
ALTER TABLE accounts ADD COLUMN drop_system_prompt INTEGER NOT NULL DEFAULT 1;`},
	{filename: "006_request_log_provider.sql", sql: `
ALTER TABLE request_logs ADD COLUMN provider TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS request_logs_provider ON request_logs(provider);`,
		// v0.2.19 retabbed this SQL; keep that checksum bootable.
		legacyChecksums: []string{"9b96b8d63286519b20791d2a3688c0b71efba6204015250ae9864c5cbcd1f0b4"}},
	{filename: "007_provider_model_settings.sql", sql: `
		CREATE TABLE IF NOT EXISTS provider_model_settings (
		  provider TEXT NOT NULL,
		  model_id TEXT NOT NULL,
		  max_mode INTEGER NOT NULL DEFAULT 0,
		  updated_at TEXT NOT NULL,
		  PRIMARY KEY (provider, model_id)
		);`},
	{filename: "008_api_keys.sql", sql: `
CREATE TABLE IF NOT EXISTS api_keys (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  key_hash TEXT NOT NULL UNIQUE,
  prefix TEXT NOT NULL,
  providers_json TEXT NOT NULL DEFAULT '[]',
  enabled INTEGER NOT NULL DEFAULT 1,
  last_used_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS api_keys_hash ON api_keys(key_hash);
CREATE INDEX IF NOT EXISTS api_keys_created_at ON api_keys(created_at DESC);`},
}

const schemaMigrationsDDL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  filename TEXT PRIMARY KEY,
  checksum TEXT NOT NULL,
  applied_at TEXT NOT NULL
);`

func (s *Store) runMigrations(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schemaMigrationsDDL); err != nil {
		return fmt.Errorf("create schema migrations table: %w", err)
	}
	for _, migration := range sqliteMigrations {
		if err := s.applyMigration(ctx, migration); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) applyMigration(ctx context.Context, migration sqliteMigration) error {
	checksum := migrationChecksum(migration.sql)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", migration.filename, err)
	}
	defer tx.Rollback()

	var appliedChecksum string
	err = tx.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE filename = ?", migration.filename).Scan(&appliedChecksum)
	switch {
	case err == nil:
		if !checksumAccepted(migration, appliedChecksum) {
			return fmt.Errorf("migration %s checksum mismatch", migration.filename)
		}
		return tx.Commit()
	case err != sql.ErrNoRows:
		return fmt.Errorf("read migration %s: %w", migration.filename, err)
	}

	if _, err := tx.ExecContext(ctx, migration.sql); err != nil {
		return fmt.Errorf("apply migration %s: %w", migration.filename, err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (filename, checksum, applied_at) VALUES (?, ?, ?)", migration.filename, checksum, formatTime(time.Now().UTC())); err != nil {
		return fmt.Errorf("record migration %s: %w", migration.filename, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", migration.filename, err)
	}
	return nil
}

func checksumAccepted(migration sqliteMigration, appliedChecksum string) bool {
	if appliedChecksum == migrationChecksum(migration.sql) {
		return true
	}
	for _, legacy := range migration.legacyChecksums {
		if appliedChecksum != "" && appliedChecksum == legacy {
			return true
		}
	}
	return false
}

func migrationChecksum(sqlText string) string {
	sum := sha256.Sum256([]byte(sqlText))
	return hex.EncodeToString(sum[:])
}
