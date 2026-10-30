package codearts

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// Credential is the canonical storage payload. The field names match the
// upstream OAuth response and the credential files written by the official
// CodeArts integrations, so an exported auth file can be pasted directly.
type Credential struct {
	Format          string            `json:"format"`
	AccessKeyID     string            `json:"access_key_id"`
	SecretAccessKey string            `json:"secret_access_key"`
	SecurityToken   string            `json:"security_token,omitempty"`
	ExpiresAt       string            `json:"expires_at,omitempty"`
	DomainID        string            `json:"domain_id,omitempty"`
	UserID          string            `json:"user_id,omitempty"`
	UserName        string            `json:"user_name,omitempty"`
	RefreshToken    string            `json:"refresh_token,omitempty"`
	CodeVerifier    string            `json:"code_verifier,omitempty"`
	ClientID        string            `json:"client_id,omitempty"`
	DPoPPrivateKey  map[string]string `json:"dpop_private_key,omitempty"`
	Plan            string            `json:"plan,omitempty"`
	BaseURL         string            `json:"base_url,omitempty"`
}

// wireCredential accepts the aliases seen in exported credential files: the
// STS trio may be nested under credentials/credential, security_token may be
// cloud_dragon_token, and expiry may be expiration/expires_at.
type wireCredential struct {
	Format          string            `json:"format"`
	AccessKeyID     string            `json:"access_key_id"`
	SecretAccessKey string            `json:"secret_access_key"`
	SecurityToken   string            `json:"security_token"`
	CloudDragonTok  string            `json:"cloud_dragon_token"`
	ExpiresAt       string            `json:"expires_at"`
	Expiration      string            `json:"expiration"`
	DomainID        string            `json:"domain_id"`
	AccountID       string            `json:"account_id"`
	UserID          string            `json:"user_id"`
	UserName        string            `json:"user_name"`
	RefreshToken    string            `json:"refresh_token"`
	CodeVerifier    string            `json:"code_verifier"`
	ClientID        string            `json:"client_id"`
	DPoPPrivateKey  map[string]string `json:"dpop_private_key"`
	Plan            string            `json:"plan"`
	BaseURL         string            `json:"base_url"`
	Credentials     struct {
		AccessKeyID     string `json:"access_key_id"`
		SecretAccessKey string `json:"secret_access_key"`
		SecurityToken   string `json:"security_token"`
		Expiration      string `json:"expiration"`
	} `json:"credentials"`
	Credential struct {
		Access        string `json:"access"`
		Secret        string `json:"secret"`
		SecurityToken string `json:"securitytoken"`
		ExpiresAt     string `json:"expires_at"`
	} `json:"credential"`
	OAuthContext struct {
		PKCEPair struct {
			CodeVerifier string `json:"code_verifier"`
		} `json:"pkce_pair"`
		DPoPKeyPair struct {
			PrivateKeyJWK map[string]string `json:"private_key_jwk"`
		} `json:"dpop_key_pair"`
	} `json:"oauth_context"`
}

// DecodeCredential accepts the canonical shape plus the aliases above.
func DecodeCredential(payload []byte) (Credential, error) {
	var wrapper map[string]json.RawMessage
	if json.Unmarshal(payload, &wrapper) == nil {
		if nested, ok := wrapper["codearts_provider_credential"]; ok {
			return DecodeCredential(nested)
		}
	}
	var wire wireCredential
	if err := json.Unmarshal(payload, &wire); err != nil {
		return Credential{}, fmt.Errorf("codearts credential is not valid json")
	}
	credential := Credential{
		Format:          firstNonEmpty(wire.Format, CredentialFormat),
		AccessKeyID:     strings.TrimSpace(wire.AccessKeyID),
		SecretAccessKey: strings.TrimSpace(wire.SecretAccessKey),
		SecurityToken:   strings.TrimSpace(firstNonEmpty(wire.SecurityToken, wire.CloudDragonTok)),
		ExpiresAt:       strings.TrimSpace(firstNonEmpty(wire.ExpiresAt, wire.Expiration)),
		DomainID:        strings.TrimSpace(firstNonEmpty(wire.DomainID, wire.AccountID)),
		UserID:          strings.TrimSpace(wire.UserID),
		UserName:        strings.TrimSpace(wire.UserName),
		RefreshToken:    strings.TrimSpace(wire.RefreshToken),
		CodeVerifier:    strings.TrimSpace(wire.CodeVerifier),
		ClientID:        strings.TrimSpace(wire.ClientID),
		DPoPPrivateKey:  cloneStringMap(wire.DPoPPrivateKey),
		Plan:            strings.TrimSpace(wire.Plan),
		BaseURL:         normalizeBase(wire.BaseURL),
	}
	if credential.AccessKeyID == "" {
		credential.AccessKeyID = strings.TrimSpace(wire.Credentials.AccessKeyID)
	}
	if credential.SecretAccessKey == "" {
		credential.SecretAccessKey = strings.TrimSpace(wire.Credentials.SecretAccessKey)
	}
	if credential.SecurityToken == "" {
		credential.SecurityToken = strings.TrimSpace(wire.Credentials.SecurityToken)
	}
	if credential.ExpiresAt == "" {
		credential.ExpiresAt = strings.TrimSpace(wire.Credentials.Expiration)
	}
	if credential.AccessKeyID == "" {
		credential.AccessKeyID = strings.TrimSpace(wire.Credential.Access)
	}
	if credential.SecretAccessKey == "" {
		credential.SecretAccessKey = strings.TrimSpace(wire.Credential.Secret)
	}
	if credential.SecurityToken == "" {
		credential.SecurityToken = strings.TrimSpace(wire.Credential.SecurityToken)
	}
	if credential.ExpiresAt == "" {
		credential.ExpiresAt = strings.TrimSpace(wire.Credential.ExpiresAt)
	}
	if credential.CodeVerifier == "" {
		credential.CodeVerifier = strings.TrimSpace(wire.OAuthContext.PKCEPair.CodeVerifier)
	}
	if len(credential.DPoPPrivateKey) == 0 {
		credential.DPoPPrivateKey = cloneStringMap(wire.OAuthContext.DPoPKeyPair.PrivateKeyJWK)
	}
	return credential, nil
}

// Encode writes the canonical payload.
func (c Credential) Encode() ([]byte, error) {
	c.Format = CredentialFormat
	c.AccessKeyID = strings.TrimSpace(c.AccessKeyID)
	c.SecretAccessKey = strings.TrimSpace(c.SecretAccessKey)
	c.SecurityToken = strings.TrimSpace(c.SecurityToken)
	c.ExpiresAt = strings.TrimSpace(c.ExpiresAt)
	c.RefreshToken = strings.TrimSpace(c.RefreshToken)
	c.CodeVerifier = strings.TrimSpace(c.CodeVerifier)
	c.ClientID = strings.TrimSpace(c.ClientID)
	c.BaseURL = normalizeBase(c.BaseURL)
	return json.Marshal(c)
}

// Ready reports whether the credential can authenticate a chat request now.
// A credential with only a refresh token is not ready until refresh succeeds.
func (c Credential) Ready() bool {
	return strings.TrimSpace(c.AccessKeyID) != "" &&
		strings.TrimSpace(c.SecretAccessKey) != "" &&
		strings.TrimSpace(c.SecurityToken) != ""
}

// Valid reports whether the signing material exists. Unlike Ready it does not
// require the STS token, so an expired credential can still be refreshed.
func (c Credential) Valid() bool {
	return strings.TrimSpace(c.AccessKeyID) != "" && strings.TrimSpace(c.SecretAccessKey) != ""
}

// CanRefresh reports whether the long-lived half needed for refresh is present.
func (c Credential) CanRefresh() bool {
	return strings.TrimSpace(c.RefreshToken) != "" && strings.TrimSpace(c.CodeVerifier) != ""
}

// NeedsRefresh reports whether the STS credential should be rotated before use.
// An empty or unparseable expiry is treated as stale (not fresh), because a bad
// timestamp would otherwise hide an expired credential forever.
func (c Credential) NeedsRefresh(now time.Time) bool {
	if !c.Ready() {
		return c.CanRefresh()
	}
	raw := strings.TrimSpace(c.ExpiresAt)
	if raw == "" {
		return false
	}
	expiry, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return true
	}
	return !now.Add(refreshLead).Before(expiry)
}

// Expiry returns the parsed expiry, or the zero time.
func (c Credential) Expiry() time.Time {
	expiry, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(c.ExpiresAt))
	if err != nil {
		return time.Time{}
	}
	return expiry
}

// Sign returns the AK/SK material used for request signing.
func (c Credential) Sign() SignCredential {
	return SignCredential{
		AccessKeyID:     c.AccessKeyID,
		SecretAccessKey: c.SecretAccessKey,
		SecurityToken:   c.SecurityToken,
	}
}

// AuthBase returns the optional per-credential gateway override (tests only).
func (c Credential) AuthBase() string { return normalizeBase(c.BaseURL) }

// ValidateCredential is the CredentialCodec entry point.
func ValidateCredential(payload []byte) error {
	credential, err := DecodeCredential(payload)
	if err != nil {
		return err
	}
	if !credential.Ready() && !credential.CanRefresh() {
		return errors.New("codearts credential requires STS access keys or a refresh token")
	}
	if credential.BaseURL != "" {
		if err := validateBaseURL(credential.BaseURL); err != nil {
			return err
		}
	}
	return nil
}

// validateBaseURL rejects non-https or malformed overrides. Production never
// sets a base URL; it exists so package tests can drive an httptest origin.
func validateBaseURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("codearts endpoint must be an absolute URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("codearts endpoint must not carry credentials, a query, or a fragment")
	}
	if !strings.EqualFold(parsed.Scheme, "https") && !acceptTestEndpoint {
		return fmt.Errorf("codearts endpoint must be an https URL")
	}
	if !acceptTestEndpoint {
		host := strings.ToLower(parsed.Hostname())
		if host != "myhuaweicloud.com" && !strings.HasSuffix(host, ".myhuaweicloud.com") &&
			host != "huaweicloud.com" && !strings.HasSuffix(host, ".huaweicloud.com") {
			return fmt.Errorf("codearts endpoint must be a Huawei Cloud host")
		}
	}
	return nil
}

// acceptTestEndpoint lets package tests drive the adapter against httptest.
var acceptTestEndpoint = false

func normalizeBase(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// importFormat is the credential format accepted by the importer.
func importFormat() string { return CredentialFormat }

// PrepareImport validates and normalizes a pasted credential. The Ready flag is
// true when the credential can either chat now or refresh itself.
func PrepareImport(payload []byte) (providers.CredentialImport, error) {
	credential, err := DecodeCredential(payload)
	if err != nil {
		return providers.CredentialImport{}, err
	}
	if !credential.Ready() && !credential.CanRefresh() {
		return providers.CredentialImport{}, errors.New("codearts credential requires STS access keys or a refresh token")
	}
	if credential.BaseURL != "" {
		if err := validateBaseURL(credential.BaseURL); err != nil {
			return providers.CredentialImport{}, err
		}
	}
	encoded, err := credential.Encode()
	if err != nil {
		return providers.CredentialImport{}, err
	}
	return providers.CredentialImport{Payload: encoded, Ready: true}, nil
}
