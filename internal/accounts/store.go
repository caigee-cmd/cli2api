package accounts

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var ErrAccountNotFound = errors.New("account not found")

type Account struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	RemoteUID     string     `json:"remote_uid,omitempty"`
	AuthType      string     `json:"auth_type"`
	Enabled       bool       `json:"enabled"`
	MaxInFlight   int        `json:"max_inflight"`
	Priority      int        `json:"priority"`
	Status        string     `json:"status"`
	LastError     string     `json:"last_error,omitempty"`
	LastErrorKind string     `json:"last_error_kind,omitempty"`
	CooldownUntil *time.Time `json:"cooldown_until,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type CreateAccount struct {
	Name        string
	Enabled     bool
	MaxInFlight int
	Priority    int
}

type UpdateAccount struct {
	Name        string
	Enabled     *bool
	MaxInFlight *int
	Priority    *int
}

type NativeCredential struct {
	UserBlob  []byte `json:"-"`
	MachineID string `json:"machine_id"`
}

type Store struct {
	db *sql.DB
}

func OpenStore(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("sqlite path required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create sqlite directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	const schema = `
PRAGMA foreign_keys = ON;
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
);`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("migrate sqlite: %w", err)
	}
	const migrateModelSettings = `
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
WHERE model_id IN ('qmodel', 'dmodel', 'dfmodel', 'kmodel', 'mmodel', 'gm51model');`
	if _, err := s.db.ExecContext(ctx, migrateModelSettings); err != nil {
		return fmt.Errorf("migrate model settings: %w", err)
	}
	return nil
}

func (s *Store) Create(ctx context.Context, input CreateAccount) (Account, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Account{}, fmt.Errorf("account name required")
	}
	maxInFlight := input.MaxInFlight
	if maxInFlight <= 0 {
		maxInFlight = 4
	}
	priority := input.Priority
	if priority <= 0 {
		priority = 50
	}
	now := time.Now().UTC()
	account := Account{
		ID:          newAccountID(),
		Name:        name,
		AuthType:    "none",
		Enabled:     input.Enabled,
		MaxInFlight: maxInFlight,
		Priority:    priority,
		Status:      "offline",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO accounts (
  id, name, auth_type, enabled, max_inflight, priority, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		account.ID, account.Name, account.AuthType, account.Enabled, account.MaxInFlight,
		account.Priority, account.Status, formatTime(account.CreatedAt), formatTime(account.UpdatedAt),
	)
	if err != nil {
		return Account{}, fmt.Errorf("create account: %w", err)
	}
	return account, nil
}

func (s *Store) Get(ctx context.Context, id string) (Account, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, name, remote_uid, auth_type, enabled, max_inflight, priority, status,
       last_error, last_error_kind, cooldown_until, created_at, updated_at
FROM accounts WHERE id = ?`, strings.TrimSpace(id))
	account, err := scanAccount(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrAccountNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("get account: %w", err)
	}
	return account, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanAccount(row rowScanner) (Account, error) {
	var account Account
	var cooldown, created, updated sql.NullString
	err := row.Scan(
		&account.ID, &account.Name, &account.RemoteUID, &account.AuthType, &account.Enabled,
		&account.MaxInFlight, &account.Priority, &account.Status, &account.LastError,
		&account.LastErrorKind, &cooldown, &created, &updated,
	)
	if err != nil {
		return Account{}, err
	}
	account.CreatedAt = parseTime(created.String)
	account.UpdatedAt = parseTime(updated.String)
	if cooldown.Valid && cooldown.String != "" {
		parsed := parseTime(cooldown.String)
		account.CooldownUntil = &parsed
	}
	return account, nil
}

func newAccountID() string {
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("acc_%d", time.Now().UnixNano())
	}
	return "acc_" + hex.EncodeToString(raw)
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

func (s *Store) List(ctx context.Context) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, remote_uid, auth_type, enabled, max_inflight, priority, status,
       last_error, last_error_kind, cooldown_until, created_at, updated_at
FROM accounts ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	defer rows.Close()
	var accounts []Account
	for rows.Next() {
		account, err := scanAccount(rows)
		if err != nil {
			return nil, fmt.Errorf("scan account: %w", err)
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func (s *Store) Update(ctx context.Context, id string, input UpdateAccount) error {
	account, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if name := strings.TrimSpace(input.Name); name != "" {
		account.Name = name
	}
	if input.Enabled != nil {
		account.Enabled = *input.Enabled
	}
	if input.MaxInFlight != nil && *input.MaxInFlight > 0 {
		account.MaxInFlight = *input.MaxInFlight
	}
	if input.Priority != nil && *input.Priority > 0 {
		account.Priority = *input.Priority
	}
	account.UpdatedAt = time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
UPDATE accounts SET name = ?, enabled = ?, max_inflight = ?, priority = ?, updated_at = ?
WHERE id = ?`, account.Name, account.Enabled, account.MaxInFlight, account.Priority, formatTime(account.UpdatedAt), account.ID)
	if err != nil {
		return fmt.Errorf("update account: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return ErrAccountNotFound
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM accounts WHERE id = ?`, strings.TrimSpace(id))
	if err != nil {
		return fmt.Errorf("delete account: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return ErrAccountNotFound
	}
	return nil
}

func (s *Store) SetModelContext(ctx context.Context, modelID string, contextLength int) error {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return fmt.Errorf("model id required")
	}
	if contextLength < 0 || contextLength > 4_000_000 || (contextLength > 0 && contextLength < 1024) {
		return fmt.Errorf("context_length must be 0 or between 1024 and 4000000")
	}
	if contextLength == 0 {
		_, err := s.db.ExecContext(ctx, `DELETE FROM model_settings WHERE model_id = ?`, modelID)
		if err != nil {
			return fmt.Errorf("delete model context: %w", err)
		}
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO model_settings (model_id, context_length, updated_at) VALUES (?, ?, ?)
ON CONFLICT(model_id) DO UPDATE SET context_length=excluded.context_length, updated_at=excluded.updated_at`,
		modelID, contextLength, formatTime(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("save model context: %w", err)
	}
	return nil
}

func (s *Store) GetModelContext(ctx context.Context, modelID string) (int, bool, error) {
	var contextLength int
	err := s.db.QueryRowContext(ctx, `SELECT context_length FROM model_settings WHERE model_id = ?`, strings.TrimSpace(modelID)).Scan(&contextLength)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("get model context: %w", err)
	}
	return contextLength, true, nil
}

func (s *Store) ListModelContexts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT model_id, context_length FROM model_settings`)
	if err != nil {
		return nil, fmt.Errorf("list model contexts: %w", err)
	}
	defer rows.Close()
	result := make(map[string]int)
	for rows.Next() {
		var modelID string
		var contextLength int
		if err := rows.Scan(&modelID, &contextLength); err != nil {
			return nil, fmt.Errorf("scan model context: %w", err)
		}
		result[modelID] = contextLength
	}
	return result, rows.Err()
}

func (s *Store) SaveCredential(ctx context.Context, accountID, authType string, credential NativeCredential) error {
	if len(credential.UserBlob) == 0 || strings.TrimSpace(credential.MachineID) == "" {
		return fmt.Errorf("native credential requires user blob and machine id")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := formatTime(time.Now().UTC())
	if _, err := tx.ExecContext(ctx, `
INSERT INTO account_credentials (account_id, user_blob, machine_id, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(account_id) DO UPDATE SET user_blob=excluded.user_blob, machine_id=excluded.machine_id, updated_at=excluded.updated_at`,
		accountID, credential.UserBlob, strings.TrimSpace(credential.MachineID), now); err != nil {
		return fmt.Errorf("save credential: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE accounts SET auth_type = ?, updated_at = ? WHERE id = ?`, authType, now, accountID); err != nil {
		return fmt.Errorf("update credential type: %w", err)
	}
	return tx.Commit()
}

func (s *Store) LoadCredential(ctx context.Context, accountID string) (NativeCredential, error) {
	var credential NativeCredential
	err := s.db.QueryRowContext(ctx, `
SELECT user_blob, machine_id FROM account_credentials WHERE account_id = ?`, accountID).
		Scan(&credential.UserBlob, &credential.MachineID)
	if errors.Is(err, sql.ErrNoRows) {
		return NativeCredential{}, ErrAccountNotFound
	}
	if err != nil {
		return NativeCredential{}, fmt.Errorf("load credential: %w", err)
	}
	return credential, nil
}

func (s *Store) Observe(ctx context.Context, id, remoteUID, status, lastError, lastKind string) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE accounts SET remote_uid = ?, status = ?, last_error = ?, last_error_kind = ?, updated_at = ?
WHERE id = ?`, remoteUID, status, lastError, lastKind, formatTime(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("observe account: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return ErrAccountNotFound
	}
	return nil
}

func (s *Store) RecordPoolState(ctx context.Context, item Item) error {
	var cooldown any
	status := "ready"
	if !item.DownUntil.IsZero() && time.Now().Before(item.DownUntil) {
		cooldown = formatTime(item.DownUntil)
		status = "cooling"
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE accounts SET status = ?, last_error = ?, last_error_kind = ?, cooldown_until = ?, updated_at = ?
WHERE id = ?`, status, item.LastError, item.LastKind, cooldown, formatTime(time.Now().UTC()), item.ID)
	if err != nil {
		return fmt.Errorf("record pool state: %w", err)
	}
	return nil
}
