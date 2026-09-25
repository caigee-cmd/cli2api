package command

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Credential is the canonical storage payload for provider=command.
//
// Command Code authenticates with a single user_… Bearer key that the CLI and
// the API share. There is no refresh token, device seed, or org binding; the
// optional identity fields are cached for display only.
type Credential struct {
	Format  string `json:"format"`
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url,omitempty"`
	UserID  string `json:"user_id,omitempty"`
	Email   string `json:"email,omitempty"`
	Name    string `json:"name,omitempty"`
}

// FormatAPIKey trims a pasted key. Command Code keys are used verbatim; this
// only strips an accidental surrounding whitespace or an inline "Bearer ".
func FormatAPIKey(rawKey string) string {
	key := strings.TrimSpace(rawKey)
	key = strings.TrimPrefix(key, "Bearer ")
	key = strings.TrimPrefix(key, "bearer ")
	return strings.TrimSpace(key)
}

// DecodeCredential accepts either the canonical flat form or a camelCase /
// aliased bundle (so a pasted {"apiKey":…} or {"token":…} still imports).
func DecodeCredential(payload []byte) (Credential, error) {
	var flat Credential
	if err := json.Unmarshal(payload, &flat); err == nil && strings.TrimSpace(flat.APIKey) != "" {
		flat.APIKey = FormatAPIKey(flat.APIKey)
		if flat.Format == "" {
			flat.Format = CredentialFormat
		}
		return flat, nil
	}
	var alias struct {
		Format      string `json:"format"`
		APIKey      string `json:"api_key"`
		APIKeyCamel string `json:"apiKey"`
		Key         string `json:"key"`
		Token       string `json:"token"`
		UserID      string `json:"user_id"`
		UserIDCamel string `json:"userId"`
		Email       string `json:"email"`
		Name        string `json:"name"`
		BaseURL     string `json:"base_url"`
		BaseURLCam  string `json:"baseUrl"`
	}
	if err := json.Unmarshal(payload, &alias); err == nil {
		key := firstNonEmpty(alias.APIKey, alias.APIKeyCamel, alias.Key, alias.Token)
		if strings.TrimSpace(key) != "" {
			return Credential{
				Format:  firstNonEmpty(alias.Format, CredentialFormat),
				APIKey:  FormatAPIKey(key),
				BaseURL: firstNonEmpty(alias.BaseURL, alias.BaseURLCam),
				UserID:  firstNonEmpty(alias.UserID, alias.UserIDCamel),
				Email:   alias.Email,
				Name:    alias.Name,
			}, nil
		}
	}
	return Credential{}, fmt.Errorf("command credential requires an api key")
}

func (c Credential) Encode() ([]byte, error) {
	c.Format = CredentialFormat
	c.APIKey = FormatAPIKey(c.APIKey)
	if strings.TrimSpace(c.BaseURL) == "" {
		c.BaseURL = BaseURL
	}
	return json.Marshal(c)
}

// Ready reports whether the credential can be used.
func (c Credential) Ready() bool {
	return strings.TrimSpace(c.APIKey) != ""
}

// ValidateCredential is the CredentialCodec entry point.
func ValidateCredential(payload []byte) error {
	credential, err := DecodeCredential(payload)
	if err != nil {
		return err
	}
	key := strings.TrimSpace(credential.APIKey)
	if key == "" {
		return fmt.Errorf("command credential requires an api key")
	}
	if !strings.HasPrefix(key, KeyPrefix) {
		return fmt.Errorf("command api key must start with %q", KeyPrefix)
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
