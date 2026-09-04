// Package trae implements the Trae CN Solo in-process provider.
// Protocol constants live only in this package.
package trae

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	CredentialFormat = "trae-oauth-v1"

	DomainCN = "trae.cn"

	AgentHost   = "https://trae-api-cn.mchost.guru"
	UgHost      = "https://api.trae.cn"
	OAuthHost   = "https://api.trae.com.cn"
	ConsoleHost = "https://www.trae.cn"

	ClientID       = "en1oxy7wnw8j9n"
	AppID          = "6eefa01c-1036-4c7e-9ca5-d891f63bfcd8"
	IdeVersion     = "0.1.52"
	IdeVersionCode = "20260811"
	DeviceBrand    = "83DG"
	OSVersion      = "Windows 11 Pro"
	Function       = "solo_work_lite"
	DefaultModel   = "glm-5.2"
	PluginVersion  = "2.3.62834"
	UserAgent      = "Trae/" + IdeVersion
	QuotaUnit      = "entitlement_pack"

	pathChat          = "/api/agent/v3/llm_utils_chat"
	pathModels        = "/api/ide/v1/get_detail_param"
	pathExchange      = "/cloudide/api/v3/trae/oauth/ExchangeToken"
	pathUserInfo      = "/cloudide/api/v3/trae/GetUserInfo"
	pathCheckinStatus = "/trae/api/v2/ug/checkin_credits/status"
	pathCheckinClaim  = "/trae/api/v2/ug/checkin_credits/claim"
	pathEntUsage      = "/trae/api/v2/pay/ide_user_ent_usage"
	pathAuthorization = "/authorization"
	pathCallback      = "/authorize"
)

const (
	refreshLead      = 24 * time.Hour
	loginPendingTTL  = 10 * time.Minute
	hardRateCooldown = 5 * time.Minute
)

// Credential is the canonical storage payload shape.
type Credential struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresAt        int64  `json:"expires_at"`
	RefreshExpiresAt int64  `json:"refresh_expires_at,omitempty"`
	Domain           string `json:"domain"`
	APIHost          string `json:"api_host"`
	UID              string `json:"uid"`
	EnterpriseID     string `json:"enterprise_id"`
	Nickname         string `json:"nickname"`
	MachineID        string `json:"machine_id"`
	DeviceID         string `json:"device_id"`
	IdeVersion       string `json:"ide_version,omitempty"`
	IdeVersionCode   string `json:"ide_version_code,omitempty"`
}

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
			APIHost      string `json:"apiHost"`
			MachineID    string `json:"machineId"`
			DeviceID     string `json:"deviceId"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(payload, &nested); err == nil &&
		(nested.Auth.RefreshToken != "" || nested.Auth.AccessToken != "" || nested.Account.UID != "") {
		return Credential{
			AccessToken:  nested.Auth.AccessToken,
			RefreshToken: nested.Auth.RefreshToken,
			ExpiresAt:    unixSeconds(nested.Auth.ExpiresAt),
			Domain:       nested.Auth.Domain,
			APIHost:      nested.Auth.APIHost,
			UID:          nested.Account.UID,
			EnterpriseID: nested.Account.EnterpriseID,
			Nickname:     nested.Account.Nickname,
			MachineID:    nested.Auth.MachineID,
			DeviceID:     nested.Auth.DeviceID,
		}, nil
	}
	var flat Credential
	if err := json.Unmarshal(payload, &flat); err == nil &&
		(flat.AccessToken != "" || flat.RefreshToken != "" || flat.UID != "") {
		flat.ExpiresAt = unixSeconds(flat.ExpiresAt)
		flat.RefreshExpiresAt = unixSeconds(flat.RefreshExpiresAt)
		return flat, nil
	}
	var camel struct {
		AccessToken      string `json:"accessToken"`
		RefreshToken     string `json:"refreshToken"`
		ExpiresAt        int64  `json:"expiresAt"`
		RefreshExpiresAt int64  `json:"refreshExpireAt"`
		Domain           string `json:"domain"`
		APIHost          string `json:"apiHost"`
		UID              string `json:"uid"`
		EnterpriseID     string `json:"enterpriseId"`
		Nickname         string `json:"nickname"`
		MachineID        string `json:"machineId"`
		DeviceID         string `json:"deviceId"`
	}
	if err := json.Unmarshal(payload, &camel); err == nil &&
		(camel.AccessToken != "" || camel.RefreshToken != "" || camel.UID != "") {
		return Credential{
			AccessToken:      camel.AccessToken,
			RefreshToken:     camel.RefreshToken,
			ExpiresAt:        unixSeconds(camel.ExpiresAt),
			RefreshExpiresAt: unixSeconds(camel.RefreshExpiresAt),
			Domain:           camel.Domain,
			APIHost:          camel.APIHost,
			UID:              camel.UID,
			EnterpriseID:     camel.EnterpriseID,
			Nickname:         camel.Nickname,
			MachineID:        camel.MachineID,
			DeviceID:         camel.DeviceID,
		}, nil
	}
	return Credential{}, fmt.Errorf("trae credential requires refresh_token or access_token")
}

func (c Credential) Encode() ([]byte, error) {
	if c.Domain == "" {
		c.Domain = DomainCN
	}
	if c.APIHost == "" {
		c.APIHost = OAuthHost
	}
	if c.IdeVersion == "" {
		c.IdeVersion = IdeVersion
	}
	if c.IdeVersionCode == "" {
		c.IdeVersionCode = IdeVersionCode
	}
	return json.Marshal(c)
}

func (c Credential) Ready() bool {
	return strings.TrimSpace(c.RefreshToken) != "" && strings.TrimSpace(c.UID) != ""
}

func (c Credential) ChatBase() string    { return AgentHost }
func (c Credential) BillingBase() string { return UgHost }
func (c Credential) AuthBase() string {
	if strings.TrimSpace(c.APIHost) != "" {
		return strings.TrimRight(c.APIHost, "/")
	}
	return OAuthHost
}

func ValidateCredential(payload []byte) error {
	credential, err := DecodeCredential(payload)
	if err != nil {
		return err
	}
	if strings.TrimSpace(credential.RefreshToken) == "" && strings.TrimSpace(credential.AccessToken) == "" {
		return fmt.Errorf("trae credential requires refreshToken")
	}
	return nil
}

func (c Credential) needsRefresh(now time.Time) bool {
	if strings.TrimSpace(c.AccessToken) == "" {
		return true
	}
	if c.ExpiresAt <= 0 {
		return true
	}
	return now.Add(refreshLead).Unix() >= c.ExpiresAt
}

func (c Credential) refreshExpired(now time.Time) bool {
	return c.RefreshExpiresAt > 0 && now.Unix() >= c.RefreshExpiresAt
}

func unixSeconds(value int64) int64 {
	if value > 1e12 {
		return value / 1000
	}
	return value
}

func randomHex(n int) string {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw)
}

func EnsureDevice(credential Credential) Credential {
	if strings.TrimSpace(credential.MachineID) == "" {
		credential.MachineID = randomHex(16)
	}
	if strings.TrimSpace(credential.DeviceID) == "" {
		credential.DeviceID = randomHex(16)
	}
	return credential
}
