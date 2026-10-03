package orcarouter

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Credential is the canonical storage payload for both OrcaRouter entries. The
// PKCE flow returns the same durable sk-orca-… key the pasted-key entry stores,
// so one shape serves both; Format records which entry minted it and is never
// used to branch inference behaviour.
//
// There is no refresh token and no expiry: an OrcaRouter key is reused until the
// user revokes it. State is "ready" or "needs_reauth" and nothing else.
type Credential struct {
	Format string `json:"format"`
	APIKey string `json:"api_key"`
	// APIBase overrides the inference origin for this account (self-hosted).
	APIBase string `json:"api_base_url,omitempty"`
	// AuthBase overrides the authentication origin for this account.
	AuthBase string `json:"auth_base_url,omitempty"`
	// UserID / Scope are what the exchange returned: the account the key belongs
	// to and the scope that was *granted* (which may be narrower than requested).
	UserID string `json:"user_id,omitempty"`
	Scope  string `json:"scope,omitempty"`
	Email  string `json:"email,omitempty"`
	// State is "" / "ready" / "needs_reauth".
	State string `json:"state,omitempty"`
	// Generation increments on every successful credential replacement so a late
	// failure from an older request cannot mark a newer credential broken.
	Generation int64 `json:"generation,omitempty"`
	// Headers carries extra request headers for self-hosted deployments that
	// require additional auth headers alongside the bearer key. It is empty for
	// the public service and is only ever filled from the operator's own values.
	Headers map[string]string `json:"headers,omitempty"`
}

// CredentialStateNeedsReauth is written when the relay rejects a key with 401.
const CredentialStateNeedsReauth = "needs_reauth"

// CredentialStateReady is written after a successful store or a successful probe.
const CredentialStateReady = "ready"

// FormatAPIKey trims a pasted key and drops an accidental inline "Bearer ".
func FormatAPIKey(rawKey string) string {
	key := strings.TrimSpace(rawKey)
	key = strings.TrimPrefix(key, "Bearer ")
	key = strings.TrimPrefix(key, "bearer ")
	return strings.TrimSpace(key)
}

// DecodeCredential accepts the canonical shape plus the common aliases an
// operator might paste ({"key":…}, {"apiKey":…}, or the exchange response body
// verbatim).
func DecodeCredential(payload []byte) (Credential, error) {
	var flat Credential
	if err := json.Unmarshal(payload, &flat); err == nil && strings.TrimSpace(flat.APIKey) != "" {
		flat.APIKey = FormatAPIKey(flat.APIKey)
		if flat.Format == "" {
			flat.Format = CredentialFormat
		}
		return flat, nil
	}
	var alias struct {
		Format      string            `json:"format"`
		APIKey      string            `json:"api_key"`
		APIKeyCamel string            `json:"apiKey"`
		Key         string            `json:"key"`
		Token       string            `json:"token"`
		AccessToken string            `json:"access_token"`
		UserID      string            `json:"user_id"`
		UserIDCamel string            `json:"userId"`
		Scope       string            `json:"scope"`
		Email       string            `json:"email"`
		APIBase     string            `json:"api_base_url"`
		APIBasCamel string            `json:"base_url"`
		AuthBase    string            `json:"auth_base_url"`
		State       string            `json:"state"`
		Generation  int64             `json:"generation"`
		Headers     map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(payload, &alias); err == nil {
		key := firstNonEmpty(alias.APIKey, alias.APIKeyCamel, alias.Key, alias.Token, alias.AccessToken)
		if strings.TrimSpace(key) != "" {
			return Credential{
				Format:     firstNonEmpty(alias.Format, CredentialFormat),
				APIKey:     FormatAPIKey(key),
				APIBase:    firstNonEmpty(alias.APIBase, alias.APIBasCamel),
				AuthBase:   alias.AuthBase,
				UserID:     firstNonEmpty(alias.UserID, alias.UserIDCamel),
				Scope:      alias.Scope,
				Email:      alias.Email,
				State:      alias.State,
				Generation: alias.Generation,
				Headers:    alias.Headers,
			}, nil
		}
	}
	return Credential{}, fmt.Errorf("orcarouter credential requires an api key")
}

// Encode serializes the credential, normalizing the key and defaulting the format.
func (c Credential) Encode() ([]byte, error) {
	if strings.TrimSpace(c.Format) == "" {
		c.Format = CredentialFormat
	}
	c.APIKey = FormatAPIKey(c.APIKey)
	if strings.TrimSpace(c.State) == "" {
		c.State = CredentialStateReady
	}
	return json.Marshal(c)
}

// Ready reports whether the credential can be used for a request. A credential
// marked needs_reauth is deliberately unusable until a new login succeeds.
func (c Credential) Ready() bool {
	return strings.TrimSpace(c.APIKey) != "" && c.State != CredentialStateNeedsReauth
}

// NeedsReauth reports whether the relay rejected this credential generation.
func (c Credential) NeedsReauth() bool {
	return c.State == CredentialStateNeedsReauth
}

// ValidateCredential is the CredentialCodec entry point used by SaveCredentialPayload.
func ValidateCredential(payload []byte) error {
	credential, err := DecodeCredential(payload)
	if err != nil {
		return err
	}
	key := strings.TrimSpace(credential.APIKey)
	if key == "" {
		return fmt.Errorf("orcarouter credential requires an api key")
	}
	if !strings.HasPrefix(key, KeyPrefix) {
		return fmt.Errorf("orcarouter api key must start with %q", KeyPrefix)
	}
	for name := range credential.Headers {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("orcarouter credential has an empty header name")
		}
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
