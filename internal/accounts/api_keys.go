package accounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

type APIKey struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Providers  []string   `json:"providers"`
	Enabled    bool       `json:"enabled"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	Secret     string     `json:"secret,omitempty"`
	SecretOnce bool       `json:"secret_once,omitempty"`
}

type CreateAPIKey struct {
	Name      string
	Providers []string
	Enabled   bool
}

type UpdateAPIKey struct {
	Name      string
	Providers []string
	Enabled   *bool
}

func HashAPIKey(secret string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(secret)))
	return hex.EncodeToString(sum[:])
}

func APIKeyPrefix(secret string) string {
	secret = strings.TrimSpace(secret)
	if len(secret) <= 12 {
		return secret
	}
	return secret[:8] + "…" + secret[len(secret)-4:]
}

func NormalizeAPIKeyProviders(ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "" {
			continue
		}
		if _, _, err := providers.Resolve(id, ""); err != nil {
			return nil, fmt.Errorf("unknown provider %q", id)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

func GenerateAPIKeySecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate api key: %w", err)
	}
	return "sk_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func newAPIKeyID() string {
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("key_%d", time.Now().UnixNano())
	}
	return "key_" + hex.EncodeToString(raw)
}

func (s *Store) CreateAPIKey(ctx context.Context, input CreateAPIKey) (APIKey, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return APIKey{}, fmt.Errorf("api key name required")
	}
	allowed, err := NormalizeAPIKeyProviders(input.Providers)
	if err != nil {
		return APIKey{}, err
	}
	secret, err := GenerateAPIKeySecret()
	if err != nil {
		return APIKey{}, err
	}
	now := time.Now().UTC()
	key := APIKey{
		ID:         newAPIKeyID(),
		Name:       name,
		Prefix:     APIKeyPrefix(secret),
		Providers:  allowed,
		Enabled:    input.Enabled,
		CreatedAt:  now,
		UpdatedAt:  now,
		Secret:     secret,
		SecretOnce: true,
	}
	payload, err := json.Marshal(allowed)
	if err != nil {
		return APIKey{}, err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO api_keys (id, name, key_hash, prefix, providers_json, enabled, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		key.ID, key.Name, HashAPIKey(secret), key.Prefix, string(payload), boolToInt(key.Enabled),
		formatTime(key.CreatedAt), formatTime(key.UpdatedAt),
	)
	if err != nil {
		return APIKey{}, fmt.Errorf("create api key: %w", err)
	}
	return key, nil
}

func (s *Store) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, prefix, providers_json, enabled, last_used_at, created_at, updated_at
FROM api_keys ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()
	var keys []APIKey
	for rows.Next() {
		key, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (s *Store) GetAPIKey(ctx context.Context, id string) (APIKey, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, name, prefix, providers_json, enabled, last_used_at, created_at, updated_at
FROM api_keys WHERE id = ?`, strings.TrimSpace(id))
	key, err := scanAPIKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return APIKey{}, ErrAPIKeyNotFound
	}
	if err != nil {
		return APIKey{}, fmt.Errorf("get api key: %w", err)
	}
	return key, nil
}

func (s *Store) LookupAPIKey(ctx context.Context, secret string) (APIKey, bool, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return APIKey{}, false, nil
	}
	row := s.db.QueryRowContext(ctx, `
SELECT id, name, prefix, providers_json, enabled, last_used_at, created_at, updated_at
FROM api_keys WHERE key_hash = ?`, HashAPIKey(secret))
	key, err := scanAPIKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return APIKey{}, false, nil
	}
	if err != nil {
		return APIKey{}, false, fmt.Errorf("lookup api key: %w", err)
	}
	return key, true, nil
}

func (s *Store) UpdateAPIKey(ctx context.Context, id string, input UpdateAPIKey) (APIKey, error) {
	key, err := s.GetAPIKey(ctx, id)
	if err != nil {
		return APIKey{}, err
	}
	if name := strings.TrimSpace(input.Name); name != "" {
		key.Name = name
	}
	if input.Providers != nil {
		allowed, err := NormalizeAPIKeyProviders(input.Providers)
		if err != nil {
			return APIKey{}, err
		}
		key.Providers = allowed
	}
	if input.Enabled != nil {
		key.Enabled = *input.Enabled
	}
	key.UpdatedAt = time.Now().UTC()
	payload, err := json.Marshal(key.Providers)
	if err != nil {
		return APIKey{}, err
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE api_keys SET name = ?, providers_json = ?, enabled = ?, updated_at = ?
WHERE id = ?`, key.Name, string(payload), boolToInt(key.Enabled), formatTime(key.UpdatedAt), key.ID)
	if err != nil {
		return APIKey{}, fmt.Errorf("update api key: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return APIKey{}, ErrAPIKeyNotFound
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
		return ErrAPIKeyNotFound
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

func scanAPIKey(row rowScanner) (APIKey, error) {
	var key APIKey
	var providersJSON string
	var lastUsed, created, updated sql.NullString
	var enabled int
	if err := row.Scan(&key.ID, &key.Name, &key.Prefix, &providersJSON, &enabled, &lastUsed, &created, &updated); err != nil {
		return APIKey{}, err
	}
	key.Enabled = enabled != 0
	if strings.TrimSpace(providersJSON) != "" && providersJSON != "null" {
		if err := json.Unmarshal([]byte(providersJSON), &key.Providers); err != nil {
			return APIKey{}, fmt.Errorf("decode api key providers: %w", err)
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

func ConstantTimeEqual(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
