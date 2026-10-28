package meta

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Credential is the stored payload for provider=meta. Field names stay
// CPA-interoperable with MetaTokenStorage (dca_token / api_key / email / …)
// so an operator can import a CPA auth file.
type Credential struct {
	Format string `json:"format"`

	// APIKey is the minted (or pasted) LLM Bearer token used on chat.
	APIKey string `json:"api_key,omitempty"`
	// DCAToken is the device-client-access token from the OAuth token endpoint
	// (typically prefixed "dca:"). Used to remint; never sent as chat Bearer.
	DCAToken string `json:"dca_token,omitempty"`
	// AccessToken mirrors CPA: after login it is the DCA; after mint chat uses APIKey.
	AccessToken string `json:"access_token,omitempty"`

	TokenType    string `json:"token_type,omitempty"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	Expired      string `json:"expired,omitempty"`
	DCAExpired   string `json:"dca_expired,omitempty"`
	DCAExpiresAt int64  `json:"dca_expires_at,omitempty"`
	LastRefresh  string `json:"last_refresh,omitempty"`

	BaseURL string `json:"base_url,omitempty"`
	Email   string `json:"email,omitempty"`
	Name    string `json:"name,omitempty"`

	SubsTierName string `json:"subs_tier_name,omitempty"`
	SubsTierID   string `json:"subs_tier_id,omitempty"`
	IsSubsActive bool   `json:"is_subs_active,omitempty"`
}

type wireCredential struct {
	Format       string `json:"format"`
	Type         string `json:"type"`
	AuthKind     string `json:"auth_kind"`
	APIKey       string `json:"api_key"`
	APIKeyCamel  string `json:"apiKey"`
	Key          string `json:"key"`
	Token        string `json:"token"`
	DCAToken     string `json:"dca_token"`
	DCATokenCam  string `json:"dcaToken"`
	AccessToken  string `json:"access_token"`
	AccessTokenC string `json:"accessToken"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Expired      string `json:"expired"`
	DCAExpired   string `json:"dca_expired"`
	DCAExpiresAt int64  `json:"dca_expires_at"`
	LastRefresh  string `json:"last_refresh"`
	BaseURL      string `json:"base_url"`
	BaseURLCam   string `json:"baseUrl"`
	Email        string `json:"email"`
	UserEmail    string `json:"user_email"`
	Name         string `json:"name"`
	UserFullName string `json:"user_full_name"`
	SubsTierName string `json:"subs_tier_name"`
	SubsTierID   string `json:"subs_tier_id"`
	IsSubsActive bool   `json:"is_subs_active"`
}

// DecodeCredential accepts canonical meta-oauth-v1 / meta-key-v1 plus CPA
// MetaTokenStorage aliases.
func DecodeCredential(payload []byte) (Credential, error) {
	var wire wireCredential
	if err := json.Unmarshal(payload, &wire); err != nil {
		return Credential{}, fmt.Errorf("meta credential is not valid json")
	}
	apiKey := strings.TrimSpace(firstNonEmpty(wire.APIKey, wire.APIKeyCamel, wire.Key))
	dca := strings.TrimSpace(firstNonEmpty(wire.DCAToken, wire.DCATokenCam))
	access := strings.TrimSpace(firstNonEmpty(wire.AccessToken, wire.AccessTokenC, wire.Token))
	// CPA often stores the DCA in access_token; promote when prefixed.
	if dca == "" && strings.HasPrefix(access, "dca:") {
		dca = access
	}
	// Bare pasted key (no dca) lands in api_key / token / access_token.
	if apiKey == "" && access != "" && !strings.HasPrefix(access, "dca:") {
		apiKey = access
	}
	format := strings.TrimSpace(wire.Format)
	if format == "" {
		if dca != "" || strings.EqualFold(wire.AuthKind, "oauth") || strings.EqualFold(wire.Type, "meta") {
			format = CredentialFormatOAuth
		} else {
			format = CredentialFormatKey
		}
	}
	return Credential{
		Format:       format,
		APIKey:       apiKey,
		DCAToken:     dca,
		AccessToken:  access,
		TokenType:    strings.TrimSpace(wire.TokenType),
		ExpiresIn:    wire.ExpiresIn,
		Expired:      strings.TrimSpace(wire.Expired),
		DCAExpired:   strings.TrimSpace(wire.DCAExpired),
		DCAExpiresAt: wire.DCAExpiresAt,
		LastRefresh:  strings.TrimSpace(wire.LastRefresh),
		BaseURL:      normalizeBase(firstNonEmpty(wire.BaseURL, wire.BaseURLCam)),
		Email:        strings.TrimSpace(firstNonEmpty(wire.Email, wire.UserEmail)),
		Name:         strings.TrimSpace(firstNonEmpty(wire.Name, wire.UserFullName)),
		SubsTierName: strings.TrimSpace(wire.SubsTierName),
		SubsTierID:   strings.TrimSpace(wire.SubsTierID),
		IsSubsActive: wire.IsSubsActive,
	}, nil
}

func (c Credential) Encode() ([]byte, error) {
	c.Format = strings.TrimSpace(c.Format)
	if c.Format == "" {
		if c.DCAToken != "" {
			c.Format = CredentialFormatOAuth
		} else {
			c.Format = CredentialFormatKey
		}
	}
	c.APIKey = strings.TrimSpace(c.APIKey)
	c.DCAToken = strings.TrimSpace(c.DCAToken)
	c.AccessToken = strings.TrimSpace(c.AccessToken)
	c.BaseURL = normalizeBase(c.BaseURL)
	return json.Marshal(c)
}

// Ready reports whether chat can authenticate (minted or pasted API key).
func (c Credential) Ready() bool {
	return strings.TrimSpace(c.Bearer()) != ""
}

// Bearer returns the token used on Authorization for chat/Responses.
func (c Credential) Bearer() string {
	if key := strings.TrimSpace(c.APIKey); key != "" {
		return key
	}
	access := strings.TrimSpace(c.AccessToken)
	if access != "" && !strings.HasPrefix(access, "dca:") {
		return access
	}
	return ""
}

// ChatBaseURL returns the Responses origin for this credential.
func (c Credential) ChatBaseURL() string {
	if base := normalizeBase(c.BaseURL); base != "" {
		return base
	}
	return ChatBase
}

var errNoAPIKey = errors.New("meta credential requires an api_key (or non-dca access_token)")

// ValidateCredential is the CredentialCodec entry point for both formats.
func ValidateCredential(payload []byte) error {
	cred, err := DecodeCredential(payload)
	if err != nil {
		return err
	}
	switch cred.Format {
	case CredentialFormatOAuth, CredentialFormatKey, "":
	default:
		return fmt.Errorf("unsupported meta credential format %q", cred.Format)
	}
	if !cred.Ready() {
		// OAuth mid-flow may only have dca until mint; reject incomplete store.
		if strings.TrimSpace(cred.DCAToken) != "" {
			return fmt.Errorf("meta oauth credential is missing minted api_key; complete login again")
		}
		return errNoAPIKey
	}
	return nil
}

func normalizeBase(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimRight(raw, "/")
	return raw
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func formatRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// acceptTestEndpoint lets package tests drive the adapter against an httptest
// origin. Production keeps HTTPS-only base URLs.
var acceptTestEndpoint = false

func allowBaseURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	if strings.HasPrefix(strings.ToLower(raw), "https://") {
		return true
	}
	return acceptTestEndpoint && strings.HasPrefix(strings.ToLower(raw), "http://")
}
