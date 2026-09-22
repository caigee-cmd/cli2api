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

	// Login uses the IDE product markers (PKCE authorization-code flow):
	// client_id below, auth_from=trae, x_app_version=3.3.62.
	ClientID = "ono9krqynydwx5"
	AuthFrom = "trae"
	AppID    = "6eefa01c-1036-4c7e-9ca5-d891f63bfcd8"
	// IdeVersion is the x_app_version / IDEVersion sent on login + exchange.
	IdeVersion     = "3.3.62"
	IdeVersionCode = "20260811"
	DeviceBrand    = "83DG"
	OSVersion      = "Windows 11 Pro"
	// Function is the fallback scene for a model that is in no fetched scene
	// (normally unreachable, since any servable model comes from a catalog).
	Function = "solo_work_lite"
	// PrimaryScene absorbs the whole merged catalog. Every model is served
	// through it by default — it is the superset scene and the only one that
	// carries max-mode tiers. See sceneFor.
	PrimaryScene = "chat_v3"
	// SecondaryScene is the fallback scene, used only for models the primary
	// scene does not list (e.g. kimi-k2.6, kimi-k2.7-code upstream-hide from it).
	SecondaryScene = "solo_work_lite"
	DefaultModel   = "glm-5.2"
	PluginVersion  = "2.3.62834"
	UserAgent      = "Trae/" + IdeVersion
	QuotaUnit      = "entitlement_pack"
	// LegacyClientID minted the refresh tokens of accounts created before the
	// PKCE login switch; those accounts carry it as refresh_client_id so token
	// refresh keeps working.
	LegacyClientID = "en1oxy7wnw8j9n"

	pathChat          = "/api/agent/v3/llm_utils_chat"
	pathModels        = "/api/ide/v1/get_detail_param"
	pathExchange      = "/cloudide/api/v3/trae/oauth/ExchangeToken"
	pathExchangeCode  = "/trae/api/v3/oauth/ExchangeToken"
	pathUserInfo      = "/cloudide/api/v3/trae/GetUserInfo"
	pathCheckinStatus = "/trae/api/v2/ug/checkin_credits/status"
	pathCheckinClaim  = "/trae/api/v2/ug/checkin_credits/claim"
	pathEntUsage      = "/trae/api/v2/pay/ide_user_ent_usage"
	pathAuthorization = "/authorization"
	pathCallback      = "/authorize"
)

const (
	refreshLead     = 24 * time.Hour
	loginPendingTTL = 10 * time.Minute
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
	// DevicePrivateKey is the PEM EC P-256 key backing the device proof the v3
	// code exchange requires. Never leaves the store.
	DevicePrivateKey string `json:"device_private_key,omitempty"`
	// RefreshClientID records which OAuth client minted RefreshToken when it is
	// not the current login client (accounts created before the PKCE switch).
	RefreshClientID string `json:"refresh_client_id,omitempty"`
	IdeVersion      string `json:"ide_version,omitempty"`
	IdeVersionCode  string `json:"ide_version_code,omitempty"`
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

// randomNumericDeviceID returns a 16-digit decimal id, the shape Trae's own
// IDE client sends as device_id. A bare 32-char hex id is rejected by the UG
// check-in backend (code 9074); a <=16-digit numeric id is accepted.
func randomNumericDeviceID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%016d", time.Now().UnixNano()%1e16)
	}
	n := uint64(0)
	for _, b := range buf {
		n = n<<8 | uint64(b)
	}
	return fmt.Sprintf("%016d", n%1e16)
}

func EnsureDevice(credential Credential) Credential {
	if strings.TrimSpace(credential.MachineID) == "" {
		// Trae's IDE client sends a 64-hex machine id; match that shape.
		credential.MachineID = randomHex(32)
	}
	if strings.TrimSpace(credential.DeviceID) == "" {
		credential.DeviceID = randomNumericDeviceID()
	}
	return credential
}

// EnsureDeviceKey creates an EC P-256 device keypair when the credential has
// none, so the v3 code exchange can present a device public key.
func EnsureDeviceKey(credential Credential) Credential {
	if strings.TrimSpace(credential.DevicePrivateKey) != "" {
		return credential
	}
	if priv, _, err := generateDeviceKey(); err == nil {
		credential.DevicePrivateKey = priv
	}
	return credential
}
