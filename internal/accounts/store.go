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

	"github.com/caigee-cmd/cli2api/internal/providers"
)

var ErrAccountNotFound = errors.New("account not found")
var ErrSecretNotFound = errors.New("secret not found")

type Account struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	RemoteUID      string `json:"remote_uid,omitempty"`
	Provider       string `json:"provider"`
	ProviderRegion string `json:"region"`
	AuthType       string `json:"auth_type"`
	Enabled        bool   `json:"enabled"`
	MaxInFlight    int    `json:"max_inflight"`
	Priority       int    `json:"priority"`
	// DropSystemPrompt drops caller system prompts before provider-native chat.
	DropSystemPrompt bool       `json:"drop_system_prompt"`
	Status           string     `json:"status"`
	LastError        string     `json:"last_error,omitempty"`
	LastErrorKind    string     `json:"last_error_kind,omitempty"`
	CooldownUntil    *time.Time `json:"cooldown_until,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type CreateAccount struct {
	Name             string
	Provider         string
	Region           string
	Enabled          bool
	MaxInFlight      int
	Priority         int
	DropSystemPrompt *bool
}

type UpdateAccount struct {
	Name             string
	Enabled          *bool
	MaxInFlight      *int
	Priority         *int
	DropSystemPrompt *bool
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
	if _, err := db.Exec("PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("configure sqlite: %w", err)
	}
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
	return s.runMigrations(ctx)
}

func (s *Store) Create(ctx context.Context, input CreateAccount) (Account, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Account{}, fmt.Errorf("account name required")
	}
	descriptor, region, err := providers.Resolve(input.Provider, input.Region)
	if err != nil {
		return Account{}, err
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
	dropSystemPrompt := true
	if input.DropSystemPrompt != nil {
		dropSystemPrompt = *input.DropSystemPrompt
	}
	account := Account{
		ID:               newAccountID(),
		Name:             name,
		Provider:         descriptor.ID,
		ProviderRegion:   region.ID,
		AuthType:         "none",
		Enabled:          input.Enabled,
		MaxInFlight:      maxInFlight,
		Priority:         priority,
		DropSystemPrompt: dropSystemPrompt,
		Status:           "offline",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO accounts (
  id, name, provider, provider_region, auth_type, enabled, max_inflight, priority, drop_system_prompt, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		account.ID, account.Name, account.Provider, account.ProviderRegion, account.AuthType,
		account.Enabled, account.MaxInFlight, account.Priority, account.DropSystemPrompt, account.Status,
		formatTime(account.CreatedAt), formatTime(account.UpdatedAt),
	)
	if err != nil {
		return Account{}, fmt.Errorf("create account: %w", err)
	}
	return account, nil
}

func (s *Store) Get(ctx context.Context, id string) (Account, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, name, provider, provider_region, remote_uid, auth_type, enabled, max_inflight, priority,
       drop_system_prompt, status, last_error, last_error_kind, cooldown_until, created_at, updated_at
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
		&account.ID, &account.Name, &account.Provider, &account.ProviderRegion, &account.RemoteUID,
		&account.AuthType, &account.Enabled, &account.MaxInFlight, &account.Priority,
		&account.DropSystemPrompt, &account.Status,
		&account.LastError, &account.LastErrorKind, &cooldown, &created, &updated,
	)
	if err != nil {
		return Account{}, err
	}
	if account.Provider == "" {
		account.Provider = "qoder"
	}
	if account.ProviderRegion == "" {
		account.ProviderRegion = "global"
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
SELECT id, name, provider, provider_region, remote_uid, auth_type, enabled, max_inflight, priority,
       drop_system_prompt, status, last_error, last_error_kind, cooldown_until, created_at, updated_at
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
	if input.DropSystemPrompt != nil {
		account.DropSystemPrompt = *input.DropSystemPrompt
	}
	account.UpdatedAt = time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
UPDATE accounts SET name = ?, enabled = ?, max_inflight = ?, priority = ?, drop_system_prompt = ?, updated_at = ?
WHERE id = ?`, account.Name, account.Enabled, account.MaxInFlight, account.Priority, account.DropSystemPrompt, formatTime(account.UpdatedAt), account.ID)
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

func providerModelKey(provider, modelID string) (string, string, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	modelID = strings.TrimSpace(modelID)
	if provider == "" || modelID == "" {
		return "", "", fmt.Errorf("provider and model id required")
	}
	return provider, modelID, nil
}

func (s *Store) SetProviderModelMaxMode(ctx context.Context, provider, modelID string, maxMode bool) error {
	provider, modelID, err := providerModelKey(provider, modelID)
	if err != nil {
		return err
	}
	if !maxMode {
		_, err := s.db.ExecContext(ctx, `DELETE FROM provider_model_settings WHERE provider = ? AND model_id = ?`, provider, modelID)
		if err != nil {
			return fmt.Errorf("delete provider model setting: %w", err)
		}
		return nil
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO provider_model_settings (provider, model_id, max_mode, updated_at) VALUES (?, ?, 1, ?)
ON CONFLICT(provider, model_id) DO UPDATE SET max_mode=1, updated_at=excluded.updated_at`,
		provider, modelID, formatTime(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("save provider model setting: %w", err)
	}
	return nil
}

func (s *Store) GetProviderModelMaxMode(ctx context.Context, provider, modelID string) (bool, error) {
	provider, modelID, err := providerModelKey(provider, modelID)
	if err != nil {
		return false, err
	}
	var maxMode int
	err = s.db.QueryRowContext(ctx, `SELECT max_mode FROM provider_model_settings WHERE provider = ? AND model_id = ?`, provider, modelID).Scan(&maxMode)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get provider model setting: %w", err)
	}
	return maxMode != 0, nil
}

func (s *Store) ListProviderModelMaxModes(ctx context.Context, provider string) (map[string]bool, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return nil, fmt.Errorf("provider required")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT model_id, max_mode FROM provider_model_settings WHERE provider = ?`, provider)
	if err != nil {
		return nil, fmt.Errorf("list provider model settings: %w", err)
	}
	defer rows.Close()
	result := make(map[string]bool)
	for rows.Next() {
		var modelID string
		var maxMode int
		if err := rows.Scan(&modelID, &maxMode); err != nil {
			return nil, fmt.Errorf("scan provider model setting: %w", err)
		}
		result[modelID] = maxMode != 0
	}
	return result, rows.Err()
}

func (s *Store) GetSecret(ctx context.Context, name string) (string, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false, fmt.Errorf("secret name required")
	}
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM app_secrets WHERE name = ?`, name).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get secret: %w", err)
	}
	return value, true, nil
}

func (s *Store) SetSecret(ctx context.Context, name, value string) error {
	name = strings.TrimSpace(name)
	value = strings.TrimSpace(value)
	if name == "" {
		return fmt.Errorf("secret name required")
	}
	if value == "" {
		return fmt.Errorf("secret value required")
	}
	now := formatTime(time.Now().UTC())
	_, err := s.db.ExecContext(ctx, `
INSERT INTO app_secrets (name, value, created_at, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		name, value, now, now)
	if err != nil {
		return fmt.Errorf("save secret: %w", err)
	}
	return nil
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

func (s *Store) SaveCredentialPayload(ctx context.Context, accountID, format string, payload []byte) error {
	if len(payload) == 0 {
		return fmt.Errorf("credential payload required")
	}
	account, err := s.Get(ctx, accountID)
	if err != nil {
		return err
	}
	if err := providers.ValidateCredentialFormat(account.Provider, format); err != nil {
		return err
	}
	now := formatTime(time.Now().UTC())
	_, err = s.db.ExecContext(ctx, `
INSERT INTO account_credential_payloads (account_id, format, payload, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(account_id) DO UPDATE SET format=excluded.format, payload=excluded.payload, updated_at=excluded.updated_at`,
		accountID, format, payload, now)
	if err != nil {
		return fmt.Errorf("save credential payload: %w", err)
	}
	return nil
}

func (s *Store) LoadCredentialPayload(ctx context.Context, accountID string) (string, []byte, error) {
	var format string
	var payload []byte
	err := s.db.QueryRowContext(ctx, `
SELECT format, payload FROM account_credential_payloads WHERE account_id = ?`, accountID).
		Scan(&format, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, ErrAccountNotFound
	}
	if err != nil {
		return "", nil, fmt.Errorf("load credential payload: %w", err)
	}
	return format, payload, nil
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
