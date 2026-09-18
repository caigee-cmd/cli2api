package devin

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Credential is the canonical storage payload for provider=devin.
type Credential struct {
	Format       string `json:"format"`
	SessionToken string `json:"session_token"`
	DeviceSeed   string `json:"device_seed,omitempty"`
	UserID       string `json:"user_id,omitempty"`
	OrgID        string `json:"org_id,omitempty"`
	UserName     string `json:"user_name,omitempty"`
	Email        string `json:"email,omitempty"`
	BaseURL      string `json:"base_url,omitempty"`
}

func FormatSessionToken(rawToken string) string {
	t := strings.TrimSpace(rawToken)
	if strings.HasPrefix(t, TokenPrefix) {
		return t
	}
	if strings.HasPrefix(t, "eyJ") {
		return TokenPrefix + t
	}
	return t
}

func DecodeCredential(payload []byte) (Credential, error) {
	var flat Credential
	if err := json.Unmarshal(payload, &flat); err == nil && strings.TrimSpace(flat.SessionToken) != "" {
		flat.SessionToken = FormatSessionToken(flat.SessionToken)
		if flat.Format == "" {
			flat.Format = CredentialFormat
		}
		return flat, nil
	}
	var camel struct {
		Format       string `json:"format"`
		SessionToken string `json:"sessionToken"`
		DeviceSeed   string `json:"deviceSeed"`
		UserID       string `json:"userId"`
		OrgID        string `json:"orgId"`
		UserName     string `json:"userName"`
		Email        string `json:"email"`
		BaseURL      string `json:"baseUrl"`
		APIKey       string `json:"api_key"`
		APIKeyCamel  string `json:"apiKey"`
	}
	if err := json.Unmarshal(payload, &camel); err == nil {
		token := firstNonEmpty(camel.SessionToken, camel.APIKey, camel.APIKeyCamel)
		if strings.TrimSpace(token) != "" {
			return Credential{
				Format:       firstNonEmpty(camel.Format, CredentialFormat),
				SessionToken: FormatSessionToken(token),
				DeviceSeed:   camel.DeviceSeed,
				UserID:       camel.UserID,
				OrgID:        camel.OrgID,
				UserName:     camel.UserName,
				Email:        camel.Email,
				BaseURL:      camel.BaseURL,
			}, nil
		}
	}
	return Credential{}, fmt.Errorf("devin credential requires session_token")
}

func (c Credential) Encode() ([]byte, error) {
	c.Format = CredentialFormat
	c.SessionToken = FormatSessionToken(c.SessionToken)
	if strings.TrimSpace(c.BaseURL) == "" {
		c.BaseURL = ServerBase
	}
	c = EnsureDeviceSeed(c)
	return json.Marshal(c)
}

func (c Credential) Ready() bool {
	token := strings.TrimSpace(c.SessionToken)
	return token != "" && (strings.HasPrefix(token, TokenPrefix) || strings.HasPrefix(token, "eyJ"))
}

func ValidateCredential(payload []byte) error {
	credential, err := DecodeCredential(payload)
	if err != nil {
		return err
	}
	token := strings.TrimSpace(credential.SessionToken)
	if token == "" {
		return fmt.Errorf("devin credential requires session_token")
	}
	if !(strings.HasPrefix(token, TokenPrefix) || strings.HasPrefix(token, "eyJ")) {
		return fmt.Errorf("devin session_token must be a JWT or %s-prefixed token", TokenPrefix)
	}
	return nil
}

func EnsureDeviceSeed(credential Credential) Credential {
	if strings.TrimSpace(credential.DeviceSeed) == "" {
		credential.DeviceSeed = randomHex(16)
	}
	return credential
}

func randomHex(n int) string {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
