package accounts

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
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
		return []string{}, nil
	}
	grants := make([]ProviderGrant, 0, len(ids))
	for _, id := range ids {
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "" {
			continue
		}
		grant, err := ParseProviderGrant(id)
		if err != nil {
			return nil, err
		}
		grants = append(grants, grant)
	}
	// A bare family entry subsumes every region-scoped entry of the same
	// family: ["workbuddy", "workbuddy:cn"] stores only "workbuddy". Keep the
	// bare entry (not the union) so a future new region stays denied unless
	// the caller re-authorizes it explicitly.
	bare := map[string]bool{}
	for _, grant := range grants {
		if grant.Region == "" {
			bare[grant.Provider] = true
		}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(grants))
	for _, grant := range grants {
		if grant.Region != "" && bare[grant.Provider] {
			continue
		}
		canonical := grant.String()
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		out = append(out, canonical)
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

func ConstantTimeEqual(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
