// Package codex implements the OpenAI Codex (ChatGPT subscription) in-process
// provider. Protocol constants live only in this package.
//
// Ported from github.com/router-for-me/CLIProxyAPI (MIT).
package codex

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

const (
	CredentialFormat = "codex-oauth-v1"

	// OAuth endpoints and the public client_id the official codex CLI uses.
	AuthURL   = "https://auth.openai.com/oauth/authorize"
	TokenURL  = "https://auth.openai.com/oauth/token"
	ClientID  = "app_EMoamEEZ73f0CkXaXp7hrann"
	Scope     = "openid email profile offline_access"
	AuthScope = "openid profile email"

	// Sign in with ChatGPT uses a dynamically issued client and a different
	// token endpoint from the Codex CLI OAuth client above.
	AuthMethodCLI     = "cli"
	AuthMethodChatGPT = "chatgpt"
	SIWCAuthorizeURL  = "https://auth.openai.com/api/accounts/authorize"
	SIWCTokenURL      = "https://auth.openai.com/api/accounts/oauth/token"
	SIWCDynamicClient = "dynamic_agent_client"
	SIWCAgentName     = "cli2api"
	SIWCResource      = "https://api.openai.com/v1"
	SIWCBaseURL       = "https://api.openai.com/v1"
	SIWCScope         = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"
	SIWCDirectScope   = "chatgpt.tokens.use.direct"
	SIWCRedirectURI   = "http://127.0.0.1:1455/auth/callback"

	// LoopbackAddr is the fixed redirect the OpenAI OAuth client registers.
	// Unlike Trae's random-port loopback this port is part of the registered
	// redirect_uri, so it cannot change.
	LoopbackAddr = "127.0.0.1:1455"
	pathCallback = "/auth/callback"
	redirectURI  = "http://localhost:1455/auth/callback"

	ChatBase      = "https://chatgpt.com/backend-api/codex"
	pathResponses = "/responses"
	// pathWhamUsage is the account usage probe. It returns the same rate_limits
	// object the websocket codex.rate_limits event carries, without spending a
	// chat turn. CLIProxyAPI reads those fields from response headers instead.
	pathWhamUsage = "/wham/usage"

	// Cloaking: upstream gates on the official codex CLI UA/originator.
	UserAgent  = "codex-tui/0.166.0 (Mac OS 26.5.2; arm64) iTerm.app/3.6.11 (codex-tui; 0.166.0)"
	Originator = "codex-tui"

	QuotaUnit = "rate_limit_window"
)

const (
	// outerRefreshLead matches CLIProxyAPI CodexOuterRefreshLead (feb6398): the
	// nominal 10-day Codex OAuth JWT may be invalidated early upstream, so outer
	// / CLI tokens refresh well ahead of expiry.
	outerRefreshLead = 96 * time.Hour
	// siwcRefreshLead matches CLIProxyAPI CodexSIWCRefreshLead / pi's
	// DEFAULT_OAUTH_MINIMUM_VALIDITY_MS (5 minutes).
	siwcRefreshLead = 5 * time.Minute
	loginPendingTTL = 10 * time.Minute
)

// Credential is the canonical storage payload shape. Field names match the
// CodexTokenStorage JSON CLIProxyAPI writes so import/export round-trips.
// OuterPartition holds the ChatGPT backend (non-SIWC) OAuth tokens kept
// alongside a Sign in with ChatGPT partition when use_siwc was imported.
type OuterPartition struct {
	IDToken      string `json:"id_token,omitempty"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
	LastRefresh  string `json:"last_refresh,omitempty"`
}

type Credential struct {
	IDToken      string `json:"id_token,omitempty"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	AccountID    string `json:"account_id"`
	Email        string `json:"email,omitempty"`
	// ExpiresAt is a Unix second timestamp for the access token.
	ExpiresAt   int64  `json:"expires_at"`
	LastRefresh string `json:"last_refresh,omitempty"`
	// AuthMethod is cli for the Codex CLI OAuth client, or chatgpt for Sign in
	// with ChatGPT. Empty means cli so credentials written before that flow
	// keep refreshing at the CLI token endpoint.
	AuthMethod string `json:"auth_method,omitempty"`
	// ClientID and DeviceID are issued by Sign in with ChatGPT. Refresh fails
	// closed without them rather than falling back to the CLI client.
	ClientID string `json:"client_id,omitempty"`
	DeviceID string `json:"device_id,omitempty"`
	// UseSIWC is true when this credential prefers the nested Sign in with
	// ChatGPT partition for non-Codex clients. Codex client User-Agents ignore
	// it and must use Outer (aligned with CLIProxyAPI d946c4f).
	UseSIWC bool `json:"use_siwc,omitempty"`
	// Outer is the non-SIWC ChatGPT OAuth partition preserved on import so
	// Codex client requests can use it without discarding SIWC tokens.
	Outer *OuterPartition `json:"outer,omitempty"`
}

func DecodeCredential(payload []byte) (Credential, error) {
	var flat Credential
	if err := json.Unmarshal(payload, &flat); err == nil &&
		(flat.AccessToken != "" || flat.RefreshToken != "") {
		flat.ExpiresAt = unixSeconds(flat.ExpiresAt)
		flat.AuthMethod = normalizeAuthMethod(flat.AuthMethod)
		if flat.Outer != nil {
			flat.Outer.ExpiresAt = unixSeconds(flat.Outer.ExpiresAt)
		}
		return flat, nil
	}
	// Accept CLIProxyAPI's nested token_data shape, including a Sign in with
	// ChatGPT credential stored under siwc.
	var nested struct {
		TokenData struct {
			IDToken      string `json:"id_token"`
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			AccountID    string `json:"account_id"`
			Email        string `json:"email"`
			Expire       string `json:"expired"`
		} `json:"token_data"`
		LastRefresh string `json:"last_refresh"`
		UseSIWC     *bool  `json:"use_siwc"`
		SIWC        struct {
			IDToken      string `json:"id_token"`
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			ClientID     string `json:"client_id"`
			DeviceID     string `json:"device_id"`
			Expire       string `json:"expired"`
			LastRefresh  string `json:"last_refresh"`
		} `json:"siwc"`
	}
	if err := json.Unmarshal(payload, &nested); err == nil {
		if nested.UseSIWC != nil && *nested.UseSIWC &&
			(nested.SIWC.AccessToken != "" || nested.SIWC.RefreshToken != "") {
			cred := Credential{
				IDToken:      nested.SIWC.IDToken,
				AccessToken:  nested.SIWC.AccessToken,
				RefreshToken: nested.SIWC.RefreshToken,
				AccountID:    nested.TokenData.AccountID,
				Email:        nested.TokenData.Email,
				ExpiresAt:    parseExpire(nested.SIWC.Expire),
				LastRefresh:  firstNonEmpty(nested.SIWC.LastRefresh, nested.LastRefresh),
				AuthMethod:   AuthMethodChatGPT,
				ClientID:     nested.SIWC.ClientID,
				DeviceID:     nested.SIWC.DeviceID,
				UseSIWC:      true,
			}
			// Keep the outer ChatGPT OAuth partition so Codex client requests can
			// ignore use_siwc and still authenticate without falling back to SIWC.
			if nested.TokenData.AccessToken != "" || nested.TokenData.RefreshToken != "" {
				cred.Outer = &OuterPartition{
					IDToken:      nested.TokenData.IDToken,
					AccessToken:  nested.TokenData.AccessToken,
					RefreshToken: nested.TokenData.RefreshToken,
					ExpiresAt:    parseExpire(nested.TokenData.Expire),
					LastRefresh:  nested.LastRefresh,
				}
			}
			return cred, nil
		}
		if nested.TokenData.AccessToken != "" || nested.TokenData.RefreshToken != "" {
			return Credential{
				IDToken:      nested.TokenData.IDToken,
				AccessToken:  nested.TokenData.AccessToken,
				RefreshToken: nested.TokenData.RefreshToken,
				AccountID:    nested.TokenData.AccountID,
				Email:        nested.TokenData.Email,
				ExpiresAt:    parseExpire(nested.TokenData.Expire),
				LastRefresh:  nested.LastRefresh,
				AuthMethod:   AuthMethodCLI,
			}, nil
		}
	}
	return Credential{}, fmt.Errorf("codex credential requires access_token or refresh_token")
}

func (c Credential) Encode() ([]byte, error) {
	return json.Marshal(c)
}

func (c Credential) Ready() bool {
	if strings.TrimSpace(c.RefreshToken) == "" || strings.TrimSpace(c.AccountID) == "" {
		return false
	}
	if c.UsesChatGPT() && strings.TrimSpace(c.ClientID) == "" {
		return false
	}
	return true
}

// UsesChatGPT reports a Sign in with ChatGPT credential. Those tokens are
// refreshed and sent differently from the Codex CLI OAuth client.
func (c Credential) UsesChatGPT() bool {
	return normalizeAuthMethod(c.AuthMethod) == AuthMethodChatGPT
}

func normalizeAuthMethod(method string) string {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case AuthMethodChatGPT, "siwc", "sign-in-with-chatgpt":
		return AuthMethodChatGPT
	default:
		return AuthMethodCLI
	}
}

// UseSIWCForRequest reports whether this request should use the nested SIWC
// partition. Codex client User-Agents always ignore use_siwc (CLIProxyAPI d946c4f).
func (c Credential) UseSIWCForRequest(userAgent string) bool {
	if IsCodexClientUserAgent(userAgent) {
		return false
	}
	return c.UseSIWC
}

// EffectiveForRequest selects the OAuth partition for the inbound User-Agent.
// Codex clients require the outer (non-SIWC) access_token and never fall back
// to SIWC; other clients keep the SIWC primary when UseSIWC is set.
func (c Credential) EffectiveForRequest(userAgent string) (Credential, error) {
	if !c.UseSIWC || !IsCodexClientUserAgent(userAgent) {
		return c, nil
	}
	if c.Outer == nil || strings.TrimSpace(c.Outer.AccessToken) == "" {
		return Credential{}, &providers.Error{
			Kind:    accounts.KindAuth,
			Status:  401,
			Message: "codex client requests require the outer OAuth access_token when use_siwc is enabled",
		}
	}
	out := c
	out.IDToken = c.Outer.IDToken
	out.AccessToken = c.Outer.AccessToken
	out.RefreshToken = c.Outer.RefreshToken
	out.ExpiresAt = c.Outer.ExpiresAt
	out.LastRefresh = c.Outer.LastRefresh
	out.AuthMethod = AuthMethodCLI
	out.ClientID = ""
	out.DeviceID = ""
	return out, nil
}

// refreshingOuter reports that EffectiveForRequest selected the outer partition
// on a dual SIWC credential, so token refresh must only update Outer.
func (c Credential) refreshingOuter() bool {
	return c.UseSIWC && !c.UsesChatGPT() && c.Outer != nil
}

func ValidateCredential(payload []byte) error {
	credential, err := DecodeCredential(payload)
	if err != nil {
		return err
	}
	if strings.TrimSpace(credential.RefreshToken) == "" && strings.TrimSpace(credential.AccessToken) == "" {
		return fmt.Errorf("codex credential requires refresh_token or access_token")
	}
	return nil
}

// refreshLead returns the proactive refresh window for this partition.
// Sign in with ChatGPT keeps the short SIWC lead; outer / CLI OAuth uses the
// conservative 96h lead (CLIProxyAPI feb6398).
func (c Credential) refreshLead() time.Duration {
	if c.UsesChatGPT() {
		return siwcRefreshLead
	}
	return outerRefreshLead
}

func (c Credential) needsRefresh(now time.Time) bool {
	if strings.TrimSpace(c.AccessToken) == "" {
		return true
	}
	if c.ExpiresAt <= 0 {
		return true
	}
	return now.Add(c.refreshLead()).Unix() >= c.ExpiresAt
}

func unixSeconds(value int64) int64 {
	if value > 1e12 {
		return value / 1000
	}
	return value
}

// parseExpire accepts both Unix seconds and RFC3339-ish strings; CLIProxyAPI
// stores Expire as a string timestamp.
func parseExpire(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.Unix()
	}
	var n int64
	if _, err := fmt.Sscanf(raw, "%d", &n); err == nil {
		return unixSeconds(n)
	}
	return 0
}
