package minimaxcode

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Credential is the canonical stored payload for provider=minimaxcode.
type Credential struct {
	Format       string `json:"format"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	Scope        string `json:"scope,omitempty"`
	// Site is cn/global; AccountBase/AgentBase record the endpoints this token
	// belongs to (empty means the site defaults).
	Site        string `json:"site,omitempty"`
	AccountBase string `json:"account_base,omitempty"`
	AgentBase   string `json:"agent_base,omitempty"`
	Region      string `json:"region,omitempty"`
	// Model/plan/user are display-only.
	UserID   string `json:"user_id,omitempty"`
	UserName string `json:"user_name,omitempty"`
	Plan     string `json:"plan,omitempty"`
}

type wireCredential struct {
	Format        string `json:"format"`
	AccessToken   string `json:"access_token"`
	AccessTokenC  string `json:"accessToken"`
	Token         string `json:"token"`
	RefreshToken  string `json:"refresh_token"`
	RefreshTokenC string `json:"refreshToken"`
	ExpiresAt     int64  `json:"expires_at"`
	ExpiresAtC    int64  `json:"expiresAt"`
	ExpiresIn     int64  `json:"expires_in"`
	TokenType     string `json:"token_type"`
	Scope         string `json:"scope"`
	Site          string `json:"site"`
	AccountBase   string `json:"account_base"`
	AgentBase     string `json:"agent_base"`
	Region        string `json:"region"`
	UserID        string `json:"user_id"`
	UserName      string `json:"user_name"`
	Plan          string `json:"plan"`
}

func DecodeCredential(payload []byte) (Credential, error) {
	var wire wireCredential
	if err := json.Unmarshal(payload, &wire); err != nil {
		return Credential{}, fmt.Errorf("minimaxcode credential is not valid json")
	}
	expiresAt := firstNonZero(wire.ExpiresAt, wire.ExpiresAtC)
	if expiresAt <= 0 && wire.ExpiresIn > 0 {
		expiresAt = time.Now().Unix() + wire.ExpiresIn
	}
	credential := Credential{
		Format:       first(wire.Format, CredentialFormat),
		AccessToken:  strings.TrimSpace(first(wire.AccessToken, wire.AccessTokenC, wire.Token)),
		RefreshToken: strings.TrimSpace(first(wire.RefreshToken, wire.RefreshTokenC)),
		ExpiresAt:    expiresAt,
		ExpiresIn:    wire.ExpiresIn,
		TokenType:    strings.TrimSpace(wire.TokenType),
		Scope:        strings.TrimSpace(wire.Scope),
		Site:         normalizeSite(wire.Site),
		AccountBase:  normalizeURL(wire.AccountBase),
		AgentBase:    normalizeURL(wire.AgentBase),
		Region:       strings.TrimSpace(wire.Region),
		UserID:       strings.TrimSpace(wire.UserID),
		UserName:     strings.TrimSpace(wire.UserName),
		Plan:         strings.TrimSpace(wire.Plan),
	}
	if credential.Site == "" {
		credential.Site = DefaultSite
	}
	if credential.Region == "" {
		credential.Region = sites[credential.Site].Region
	}
	return credential, nil
}

func (c Credential) Encode() ([]byte, error) {
	c.Format = CredentialFormat
	c.AccessToken = strings.TrimSpace(c.AccessToken)
	c.RefreshToken = strings.TrimSpace(c.RefreshToken)
	c.Site = normalizeSite(c.Site)
	c.AccountBase = normalizeURL(c.AccountBase)
	c.AgentBase = normalizeURL(c.AgentBase)
	return json.Marshal(c)
}

func (c Credential) Ready() bool { return strings.TrimSpace(c.AccessToken) != "" }

func (c Credential) NeedsRefresh(now time.Time) bool {
	if c.ExpiresAt <= 0 {
		return false
	}
	return now.Add(refreshLead).Unix() >= c.ExpiresAt
}

func (c Credential) accountBase() string {
	if base := normalizeURL(c.AccountBase); base != "" {
		return base
	}
	return sites[c.Site].Account
}

func (c Credential) agentBase() string {
	if base := normalizeURL(c.AgentBase); base != "" {
		return base
	}
	return sites[c.Site].Agent
}

func (c Credential) region() string {
	if c.Region != "" {
		return c.Region
	}
	return sites[c.Site].Region
}

var errNoAccessToken = errors.New("minimaxcode credential requires an access token")

func ValidateCredential(payload []byte) error {
	credential, err := DecodeCredential(payload)
	if err != nil {
		return err
	}
	if !credential.Ready() {
		return errNoAccessToken
	}
	return ValidateEndpoints(credential.AccountBase, credential.AgentBase)
}

func ValidateEndpoints(accountBase, agentBase string) error {
	for _, raw := range []string{accountBase, agentBase} {
		if raw == "" {
			continue
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("minimaxcode endpoint must be an absolute URL without credentials or query")
		}
		if !strings.EqualFold(parsed.Scheme, "https") && !acceptTestEndpoint {
			return fmt.Errorf("minimaxcode endpoint must be an https URL")
		}
	}
	return nil
}

var acceptTestEndpoint = false

func normalizeSite(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "global" {
		return "global"
	}
	return DefaultSite
}

func normalizeURL(raw string) string { return strings.TrimRight(strings.TrimSpace(raw), "/") }

func first(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
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
