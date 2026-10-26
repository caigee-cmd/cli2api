package kimi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Credential is the canonical storage payload for provider=kimi.
//
// The shape is deliberately wire-compatible with the file the Kimi Code CLI
// writes to <KIMI_CODE_HOME>/credentials/kimi-code.json (snake_case tokens plus
// a unix-seconds expires_at), so a user can paste that file into the console
// import tab and keep the login they already have. The extra fields carry
// gateway-side state: the stable device identity Kimi's clients report, the
// region endpoints the token belongs to, and the profile the console shows.
type Credential struct {
	Format       string `json:"format"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	// ExpiresAt is the access-token expiry in unix seconds; 0 means unknown,
	// and an unknown expiry only ever refreshes reactively on a 401.
	ExpiresAt int64  `json:"expires_at,omitempty"`
	ExpiresIn int64  `json:"expires_in,omitempty"`
	TokenType string `json:"token_type,omitempty"`
	Scope     string `json:"scope,omitempty"`
	// DeviceID is the stable X-Msh-Device-Id this account reports. Kimi treats
	// it as the device identity, so it must not change between requests.
	DeviceID  string `json:"device_id,omitempty"`
	OAuthHost string `json:"oauth_host,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
	// UserID/UserName/Plan come from GET /me and are display-only.
	UserID   string `json:"user_id,omitempty"`
	UserName string `json:"user_name,omitempty"`
	Plan     string `json:"plan,omitempty"`
}

// wireCredential accepts every alias seen in the wild: the Kimi Code CLI file,
// CLIProxyAPI auth files (RFC3339 "expired"), and hand-written JSON.
type wireCredential struct {
	Format        string          `json:"format"`
	AccessToken   string          `json:"access_token"`
	AccessTokenCC string          `json:"accessToken"`
	Token         string          `json:"token"`
	RefreshToken  string          `json:"refresh_token"`
	RefreshTokenC string          `json:"refreshToken"`
	ExpiresAt     json.RawMessage `json:"expires_at"`
	ExpiresAtCC   json.RawMessage `json:"expiresAt"`
	Expired       json.RawMessage `json:"expired"`
	ExpiresIn     int64           `json:"expires_in"`
	ExpiresInCC   int64           `json:"expiresIn"`
	TokenType     string          `json:"token_type"`
	TokenTypeCC   string          `json:"tokenType"`
	Scope         string          `json:"scope"`
	DeviceID      string          `json:"device_id"`
	DeviceIDCC    string          `json:"deviceId"`
	OAuthHost     string          `json:"oauth_host"`
	OAuthHostCC   string          `json:"oauthHost"`
	BaseURL       string          `json:"base_url"`
	BaseURLCC     string          `json:"baseUrl"`
	UserID        string          `json:"user_id"`
	UserName      string          `json:"user_name"`
	Nickname      string          `json:"nickname"`
	Plan          string          `json:"plan"`
	PlanName      string          `json:"user_level_name"`
	// ImportFrom requests a read of the local Kimi Code CLI credential file
	// instead of tokens in the payload ("local" is the only accepted value).
	ImportFrom string `json:"import_from"`
}

// DecodeCredential accepts the canonical form plus the aliases above.
func DecodeCredential(payload []byte) (Credential, error) {
	var wire wireCredential
	if err := json.Unmarshal(payload, &wire); err != nil {
		return Credential{}, fmt.Errorf("kimi credential is not valid json")
	}
	credential := Credential{
		Format:       firstNonEmpty(wire.Format, CredentialFormat),
		AccessToken:  strings.TrimSpace(firstNonEmpty(wire.AccessToken, wire.AccessTokenCC, wire.Token)),
		RefreshToken: strings.TrimSpace(firstNonEmpty(wire.RefreshToken, wire.RefreshTokenC)),
		ExpiresAt:    parseExpiry(firstNonEmptyRaw(wire.ExpiresAt, wire.ExpiresAtCC, wire.Expired)),
		ExpiresIn:    firstNonZero(wire.ExpiresIn, wire.ExpiresInCC),
		TokenType:    strings.TrimSpace(firstNonEmpty(wire.TokenType, wire.TokenTypeCC)),
		Scope:        strings.TrimSpace(wire.Scope),
		DeviceID:     strings.TrimSpace(firstNonEmpty(wire.DeviceID, wire.DeviceIDCC)),
		OAuthHost:    normalizeHost(firstNonEmpty(wire.OAuthHost, wire.OAuthHostCC)),
		BaseURL:      normalizeBase(firstNonEmpty(wire.BaseURL, wire.BaseURLCC)),
		UserID:       strings.TrimSpace(wire.UserID),
		UserName:     strings.TrimSpace(firstNonEmpty(wire.UserName, wire.Nickname)),
		Plan:         strings.TrimSpace(firstNonEmpty(wire.Plan, wire.PlanName)),
	}
	return credential, nil
}

// Encode writes the canonical payload.
func (c Credential) Encode() ([]byte, error) {
	c.Format = CredentialFormat
	c.AccessToken = strings.TrimSpace(c.AccessToken)
	c.RefreshToken = strings.TrimSpace(c.RefreshToken)
	c.TokenType = strings.TrimSpace(c.TokenType)
	c.DeviceID = strings.TrimSpace(c.DeviceID)
	c.OAuthHost = normalizeHost(c.OAuthHost)
	c.BaseURL = normalizeBase(c.BaseURL)
	return json.Marshal(c)
}

// Ready reports whether the credential can authenticate a request.
func (c Credential) Ready() bool {
	return strings.TrimSpace(c.AccessToken) != ""
}

// NeedsRefresh reports whether the access token should be refreshed before a
// request. An unknown expiry never triggers a proactive refresh: the token is
// used until the upstream rejects it.
func (c Credential) NeedsRefresh(now time.Time) bool {
	if c.ExpiresAt <= 0 {
		return false
	}
	return now.Add(refreshLead).Unix() >= c.ExpiresAt
}

// AuthHost returns the OAuth host the credential's tokens belong to.
func (c Credential) AuthHost() string {
	return normalizeHost(c.OAuthHost)
}

// ChatBase returns the managed API base the credential's tokens belong to.
func (c Credential) ChatBase() string {
	return normalizeBase(c.BaseURL)
}

// DeviceIDOr returns the stored device identity, minting a placeholder when the
// credential has none. The caller persists the returned value.
func (c Credential) DeviceIDOr() string {
	if id := strings.TrimSpace(c.DeviceID); id != "" {
		return id
	}
	return newDeviceID()
}

// errNoAccessToken reports a credential payload without a bearer token.
var errNoAccessToken = errors.New("kimi credential requires an access token")

// ValidateCredential is the CredentialCodec entry point.
func ValidateCredential(payload []byte) error {
	wire := struct {
		AccessToken   string `json:"access_token"`
		AccessTokenCC string `json:"accessToken"`
		Token         string `json:"token"`
		ImportFrom    string `json:"import_from"`
	}{}
	if err := json.Unmarshal(payload, &wire); err != nil {
		return fmt.Errorf("kimi credential is not valid json")
	}
	if strings.EqualFold(strings.TrimSpace(wire.ImportFrom), ImportFromLocal) {
		return nil
	}
	credential, err := DecodeCredential(payload)
	if err != nil {
		return err
	}
	if !credential.Ready() {
		return errNoAccessToken
	}
	return ValidateEndpoints(credential.AuthHost(), credential.ChatBase())
}

// ValidateEndpoints rejects non-https or malformed endpoint overrides. An empty
// value means "use the China-region default".
func ValidateEndpoints(oauthHost, baseURL string) error {
	for _, raw := range []string{oauthHost, baseURL} {
		if raw == "" {
			continue
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" {
			return fmt.Errorf("kimi endpoint must be an absolute URL")
		}
		if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("kimi endpoint must not carry credentials, a query, or a fragment")
		}
		if !strings.EqualFold(parsed.Scheme, "https") && !acceptTestEndpoint {
			return fmt.Errorf("kimi endpoint must be an https URL")
		}
	}
	return nil
}

// acceptTestEndpoint lets package tests drive the adapter against an httptest
// origin. Production paths only ever pass https URLs through ValidateEndpoints.
var acceptTestEndpoint = false

func normalizeHost(raw string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return ""
	}
	return trimmed
}

func normalizeBase(raw string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return ""
	}
	return trimmed
}

func parseExpiry(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return 0
	}
	if len(raw) > 1 && raw[0] == '"' {
		var text string
		if json.Unmarshal([]byte(raw), &text) != nil {
			return 0
		}
		text = strings.TrimSpace(text)
		if text == "" {
			return 0
		}
		if ts, err := time.Parse(time.RFC3339, text); err == nil {
			return ts.Unix()
		}
		raw = text
	}
	var seconds int64
	if json.Unmarshal([]byte(raw), &seconds) == nil {
		return seconds
	}
	// Second chance for a quoted numeric string.
	var quoted string
	if json.Unmarshal([]byte(`"`+strings.Trim(raw, `"`)+`"`), &quoted) == nil {
		var parsed int64
		if json.Unmarshal([]byte(quoted), &parsed) == nil {
			return parsed
		}
	}
	return 0
}

func firstNonEmptyRaw(values ...json.RawMessage) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(string(value))
		if trimmed != "" && trimmed != "null" {
			return trimmed
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonZero(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}
