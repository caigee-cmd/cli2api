package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/caigee-cmd/cli2api/internal/accounts"
	"strings"
	"time"
)

func newAPIKeyID() string {
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("key_%d", time.Now().UnixNano())
	}
	return "key_" + hex.EncodeToString(raw)
}

func (s *Store) CreateAPIKey(ctx context.Context, input accounts.CreateAPIKey) (accounts.APIKey, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return accounts.APIKey{}, fmt.Errorf("api key name required")
	}
	allowed, err := accounts.NormalizeAPIKeyProviders(input.Providers)
	if err != nil {
		return accounts.APIKey{}, err
	}
	secret, err := accounts.GenerateAPIKeySecret()
	if err != nil {
		return accounts.APIKey{}, err
	}
	now := time.Now().UTC()
	key := accounts.APIKey{
		ID:         newAPIKeyID(),
		Name:       name,
		Prefix:     accounts.APIKeyPrefix(secret),
		Providers:  allowed,
		Enabled:    input.Enabled,
		CreatedAt:  now,
		UpdatedAt:  now,
		Secret:     secret,
		SecretOnce: true,
	}
	payload, err := json.Marshal(allowed)
	if err != nil {
		return accounts.APIKey{}, err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO api_keys (id, name, key_hash, prefix, providers_json, enabled, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		key.ID, key.Name, accounts.HashAPIKey(secret), key.Prefix, string(payload), boolToInt(key.Enabled),
		formatTime(key.CreatedAt), formatTime(key.UpdatedAt),
	)
	if err != nil {
		return accounts.APIKey{}, fmt.Errorf("create api key: %w", err)
	}
	return key, nil
}

func (s *Store) ListAPIKeys(ctx context.Context) ([]accounts.APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, prefix, providers_json, enabled, last_used_at, created_at, updated_at
FROM api_keys ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()
	var keys []accounts.APIKey
	for rows.Next() {
		key, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (s *Store) GetAPIKey(ctx context.Context, id string) (accounts.APIKey, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, name, prefix, providers_json, enabled, last_used_at, created_at, updated_at
FROM api_keys WHERE id = ?`, strings.TrimSpace(id))
	key, err := scanAPIKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return accounts.APIKey{}, accounts.ErrAPIKeyNotFound
	}
	if err != nil {
		return accounts.APIKey{}, fmt.Errorf("get api key: %w", err)
	}
	return key, nil
}

func (s *Store) LookupAPIKey(ctx context.Context, secret string) (accounts.APIKey, bool, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return accounts.APIKey{}, false, nil
	}
	row := s.db.QueryRowContext(ctx, `
SELECT id, name, prefix, providers_json, enabled, last_used_at, created_at, updated_at
FROM api_keys WHERE key_hash = ?`, accounts.HashAPIKey(secret))
	key, err := scanAPIKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return accounts.APIKey{}, false, nil
	}
	if err != nil {
		return accounts.APIKey{}, false, fmt.Errorf("lookup api key: %w", err)
	}
	return key, true, nil
}

func (s *Store) UpdateAPIKey(ctx context.Context, id string, input accounts.UpdateAPIKey) (accounts.APIKey, error) {
	key, err := s.GetAPIKey(ctx, id)
	if err != nil {
		return accounts.APIKey{}, err
	}
	if name := strings.TrimSpace(input.Name); name != "" {
		key.Name = name
	}
	if input.Providers != nil {
		allowed, err := accounts.NormalizeAPIKeyProviders(input.Providers)
		if err != nil {
			return accounts.APIKey{}, err
		}
		key.Providers = allowed
	}
	if input.Enabled != nil {
		key.Enabled = *input.Enabled
	}
	key.UpdatedAt = time.Now().UTC()
	payload, err := json.Marshal(key.Providers)
	if err != nil {
		return accounts.APIKey{}, err
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE api_keys SET name = ?, providers_json = ?, enabled = ?, updated_at = ?
WHERE id = ?`, key.Name, string(payload), boolToInt(key.Enabled), formatTime(key.UpdatedAt), key.ID)
	if err != nil {
		return accounts.APIKey{}, fmt.Errorf("update api key: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return accounts.APIKey{}, accounts.ErrAPIKeyNotFound
	}
	return key, nil
}

func (s *Store) DeleteAPIKey(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ?`, strings.TrimSpace(id))
	if err != nil {
		return fmt.Errorf("delete api key: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return accounts.ErrAPIKeyNotFound
	}
	return nil
}

func (s *Store) TouchAPIKey(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	now := formatTime(time.Now().UTC())
	_, err := s.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = ? WHERE id = ?`, now, id)
	if err != nil {
		return fmt.Errorf("touch api key: %w", err)
	}
	return nil
}

func scanAPIKey(row rowScanner) (accounts.APIKey, error) {
	var key accounts.APIKey
	var providersJSON string
	var lastUsed, created, updated sql.NullString
	var enabled int
	if err := row.Scan(&key.ID, &key.Name, &key.Prefix, &providersJSON, &enabled, &lastUsed, &created, &updated); err != nil {
		return accounts.APIKey{}, err
	}
	key.Enabled = enabled != 0
	if strings.TrimSpace(providersJSON) != "" && providersJSON != "null" {
		if err := json.Unmarshal([]byte(providersJSON), &key.Providers); err != nil {
			return accounts.APIKey{}, fmt.Errorf("decode api key providers: %w", err)
		}
	}
	if key.Providers == nil {
		key.Providers = []string{}
	}
	if lastUsed.Valid {
		parsed := parseTime(lastUsed.String)
		key.LastUsedAt = &parsed
	}
	key.CreatedAt = parseTime(created.String)
	key.UpdatedAt = parseTime(updated.String)
	return key, nil
}
