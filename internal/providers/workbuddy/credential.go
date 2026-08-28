// Package workbuddy implements the WorkBuddy / CodeBuddy in-process provider.
// Protocol constants live only in this package.
package workbuddy

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	CredentialFormat = "workbuddy-oauth-v1"

	DomainCN     = "codebuddy.cn"
	DomainGlobal = "workbuddy.ai"

	ChatBaseCN     = "https://copilot.tencent.com"
	ChatBaseGlobal = "https://www.workbuddy.ai"
	BillingBaseCN  = "https://www.codebuddy.cn"

	// UserAgent matches official @tencent-ai/codebuddy-code 2.139.0 in the
	// dual-token form WorkBuddy chat still accepts. Origin/Referer stay
	// region-specific and must never mix CN with Global.
	CLIVersion = "2.139.0"
	UserAgent  = "CLI/" + CLIVersion + " CodeBuddy/" + CLIVersion

	productTypeCLI     = "CLI"
	agentIntentDefault = "craft"
	agentTypeMain      = "main"

	pathAuthState    = "/v2/plugin/auth/state"
	pathAuthToken    = "/v2/plugin/auth/token"
	pathAuthAccount  = "/v2/plugin/login/account"
	pathTokenRefresh = "/v2/plugin/auth/token/refresh"
	pathChat         = "/v2/chat/completions"
	pathModels       = "/console/enterprises/personal/models"
	pathUserResource = "/v2/billing/meter/get-user-resource"
	pathDailyCheckin = "/v2/billing/meter/daily-checkin"

	sessionDeadCode = 12153
	sessionDeadText = "Offline user session not found"
)

// Credential is the canonical storage payload shape.
type Credential struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
	Domain       string `json:"domain"`
	UID          string `json:"uid"`
	EnterpriseID string `json:"enterprise_id"`
	Nickname     string `json:"nickname"`
}

// DecodeCredential accepts both the canonical flat payload and the nested
// {account, auth} export shape.
func DecodeCredential(payload []byte) (Credential, error) {
	var nested struct {
		Account struct {
			UID          string `json:"uid"`
			EnterpriseID string `json:"enterpriseId"`
			Nickname     string `json:"nickname"`
		} `json:"account"`
		Auth struct {
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
			ExpiresAt    int64  `json:"expiresAt"`
			Domain       string `json:"domain"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(payload, &nested); err == nil &&
		(nested.Auth.AccessToken != "" || nested.Account.UID != "") {
		return Credential{
			AccessToken:  nested.Auth.AccessToken,
			RefreshToken: nested.Auth.RefreshToken,
			ExpiresAt:    nested.Auth.ExpiresAt,
			Domain:       nested.Auth.Domain,
			UID:          nested.Account.UID,
			EnterpriseID: nested.Account.EnterpriseID,
			Nickname:     nested.Account.Nickname,
		}, nil
	}
	var flat struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresAt    int64  `json:"expires_at"`
		Domain       string `json:"domain"`
		UID          string `json:"uid"`
		EnterpriseID string `json:"enterprise_id"`
		Nickname     string `json:"nickname"`
	}
	if err := json.Unmarshal(payload, &flat); err == nil && flat.AccessToken != "" {
		return Credential{
			AccessToken:  flat.AccessToken,
			RefreshToken: flat.RefreshToken,
			ExpiresAt:    flat.ExpiresAt,
			Domain:       flat.Domain,
			UID:          flat.UID,
			EnterpriseID: flat.EnterpriseID,
			Nickname:     flat.Nickname,
		}, nil
	}
	// Also accept flat upstream-style camelCase keys.
	var camel struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresAt    int64  `json:"expiresAt"`
		Domain       string `json:"domain"`
		UID          string `json:"uid"`
		EnterpriseID string `json:"enterpriseId"`
		Nickname     string `json:"nickname"`
	}
	if err := json.Unmarshal(payload, &camel); err != nil && camel.AccessToken != "" {
		return Credential(camel), nil
	}
	return Credential{}, fmt.Errorf("workbuddy credential requires access_token")
}

func (c Credential) Encode() ([]byte, error) {
	return json.Marshal(c)
}

func (c Credential) Ready() bool {
	return strings.TrimSpace(c.AccessToken) != "" && strings.TrimSpace(c.UID) != ""
}

func (c Credential) ChatBase() string {
	if c.IsGlobal() {
		return ChatBaseGlobal
	}
	return ChatBaseCN
}

func (c Credential) BillingBase() string {
	if c.IsGlobal() {
		return ChatBaseGlobal
	}
	return BillingBaseCN
}

// ValidateCredential enforces the import contract: token plus uid are required
// for an account to be ready. A missing uid is stored but never ready.
func ValidateCredential(payload []byte) error {
	credential, err := DecodeCredential(payload)
	if err != nil {
		return err
	}
	if strings.TrimSpace(credential.AccessToken) == "" {
		return fmt.Errorf("workbuddy credential requires accessToken")
	}
	return nil
}

func (c Credential) IsGlobal() bool {
	domain := strings.ToLower(strings.TrimSpace(c.Domain))
	if domain == "" {
		return false
	}
	if strings.Contains(domain, DomainCN) {
		return false
	}
	return strings.Contains(domain, DomainGlobal) || strings.Contains(domain, "workbuddy")
}

func isCLIAgent(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "cli", "codebuddy", "workbuddy":
		return true
	default:
		return false
	}
}
