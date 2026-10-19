// Package qwenwork implements the QwenWork (千问办公) in-process provider.
// QwenWork is a rebranded QoderWork: device-flow login plus COSY-signed
// agent_chat_generation, sharing Qoder's RSA key but not its CLI, client type,
// or business fields. Protocol constants live only in this package.
package qwenwork

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	CredentialFormat = "qwenwork-oauth-v1"

	pathDeviceAuth     = "/device/selectAccounts"
	pathDevicePoll     = "/api/v1/deviceToken/poll"
	pathDeviceRefresh  = "/api/v1/deviceToken/refresh"
	pathUserInfo       = "/api/v1/userinfo"
	pathAccountContext = pathAccountContextPath + "?include=user,plan,quota"
	pathModelList      = "/algo/api/v2/model/list"
	// Plain JSON body: unlike Qoder's CLI route, QwenWork does not use Encode=1.
	pathChat = pathChatPath + "?FetchKeys=llm_model_result&AgentId=agent_common"

	// Path-only constants for handlers that match r.URL.Path.
	pathChatPath           = "/algo/api/v2/service/pro/sse/agent_chat_generation"
	pathAccountContextPath = "/api/v1/adapter/user/account-context"

	cosyVersion     = "1.1.18"
	clientType      = "6"
	businessProduct = "qoder_work"
	businessType    = "agent"
	scene           = "qwork"
	machineOS       = "x86_64_win32"

	// defaultTokenLifetime applies when a grant carries no expiry.
	defaultTokenLifetime = 30 * 24 * time.Hour
)

// site is the per-region desktop identity. Both regions share the device flow,
// COSY key, paths, and scene; only the gateway, OAuth client, app version,
// and default model differ. Values come from the QwenWork desktop bundles
// (intl 1.0.4, CN 1.0.5).
type site struct {
	Gateway      string
	ClientID     string
	RedirectURI  string
	AppVersion   string
	Build        string
	DefaultModel string
}

var sites = map[string]site{
	"global": {
		Gateway:      "https://gateway.qwenwork.ai",
		ClientID:     "cc65e5fc-05bd-4f5d-a4e8-0df19aa3d75a",
		RedirectURI:  "qwenwork://",
		AppVersion:   "1.0.4",
		Build:        "26090501",
		DefaultModel: "qwork-advanced",
	},
	"cn": {
		Gateway:      "https://gateway.qwenwork.cn",
		ClientID:     "e883ade2-e6e3-4d6d-adf7-f92ceff5fdcb",
		RedirectURI:  "qwenwork-cn://",
		AppVersion:   "1.0.5",
		Build:        "26090901",
		DefaultModel: "pro",
	},
}

// siteFor resolves a provider region. Empty means the descriptor default.
func siteFor(region string) (site, error) {
	region = strings.ToLower(strings.TrimSpace(region))
	if region == "" {
		region = "global"
	}
	s, ok := sites[region]
	if !ok {
		return site{}, fmt.Errorf("qwenwork: unknown region %q", region)
	}
	return s, nil
}

func (s site) userAgent() string      { return "qoderwork/" + s.AppVersion }
func (s site) releaseVersion() string { return s.AppVersion + "-" + s.Build }

// Credential is the canonical storage payload shape.
type Credential struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
	UID          string `json:"uid"`
	Nickname     string `json:"nickname"`
	Email        string `json:"email"`
	MachineID    string `json:"machine_id"`
}

// DecodeCredential accepts the canonical snake_case payload and the camelCase
// account shape exported by qwenwork2api (accessToken / refreshToken /
// expiresAt). An expiry in Unix milliseconds is normalized to seconds.
func DecodeCredential(payload []byte) (Credential, error) {
	var flat Credential
	if err := json.Unmarshal(payload, &flat); err == nil && strings.TrimSpace(flat.AccessToken) != "" {
		flat.ExpiresAt = unixSeconds(flat.ExpiresAt)
		return flat, nil
	}
	var camel struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresAt    int64  `json:"expiresAt"`
		UID          string `json:"uid"`
		Nickname     string `json:"nickname"`
		Email        string `json:"email"`
		MachineID    string `json:"machineId"`
	}
	if err := json.Unmarshal(payload, &camel); err == nil && strings.TrimSpace(camel.AccessToken) != "" {
		credential := Credential(camel)
		credential.ExpiresAt = unixSeconds(credential.ExpiresAt)
		return credential, nil
	}
	return Credential{}, fmt.Errorf("qwenwork credential requires access_token")
}

// unixSeconds treats implausibly large timestamps as Unix milliseconds.
func unixSeconds(n int64) int64 {
	if n > 1e12 {
		return n / 1000
	}
	return n
}

func (c Credential) Encode() ([]byte, error) {
	return json.Marshal(c)
}

func (c Credential) Ready() bool {
	return strings.TrimSpace(c.AccessToken) != "" && strings.TrimSpace(c.UID) != ""
}

// ValidateCredential enforces the import contract: an access token is
// required. A missing refresh token is accepted (the token is used until it
// expires); a missing uid is stored not-ready until Probe or the next refresh
// fills it from /api/v1/userinfo.
func ValidateCredential(payload []byte) error {
	_, err := DecodeCredential(payload)
	return err
}

// grant is the token document returned by deviceToken/poll and /refresh.
type grant struct {
	Token        string          `json:"token"`
	DeviceToken  string          `json:"device_token"`
	RefreshToken string          `json:"refresh_token"`
	ExpiresAt    json.RawMessage `json:"expires_at"`
	ExpiresIn    int64           `json:"expires_in"`
	UserID       string          `json:"user_id"`
	UserName     string          `json:"user_name"`
}

func (g grant) accessToken() string {
	if strings.TrimSpace(g.Token) != "" {
		return strings.TrimSpace(g.Token)
	}
	return strings.TrimSpace(g.DeviceToken)
}

// expiry returns the grant's absolute expiry in Unix seconds. expires_at may be
// an RFC 3339 string or a Unix number; expires_in may be seconds or, when
// implausibly large, milliseconds.
func (g grant) expiry(now time.Time) int64 {
	if raw := strings.TrimSpace(string(g.ExpiresAt)); raw != "" && raw != "null" {
		var text string
		if json.Unmarshal(g.ExpiresAt, &text) == nil {
			if t, err := time.Parse(time.RFC3339, text); err == nil {
				return t.Unix()
			}
		}
		var n int64
		if json.Unmarshal(g.ExpiresAt, &n) == nil && n > 0 {
			return unixSeconds(n)
		}
	}
	if g.ExpiresIn > 0 {
		if g.ExpiresIn > 10_000_000 {
			return now.Add(time.Duration(g.ExpiresIn) * time.Millisecond).Unix()
		}
		return now.Add(time.Duration(g.ExpiresIn) * time.Second).Unix()
	}
	return now.Add(defaultTokenLifetime).Unix()
}
