package zhipu

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Credential is the canonical storage payload for provider=zhipu.
//
// Auth is one API key. Mode selects the official base URL when BaseURL is
// empty. Organization and Project are optional Coding Plan team bindings; they
// are sent only on the quota probe, as bigmodel-organization / bigmodel-project.
type Credential struct {
	Format       string `json:"format"`
	APIKey       string `json:"api_key"`
	Mode         string `json:"mode,omitempty"`
	BaseURL      string `json:"base_url,omitempty"`
	Organization string `json:"organization,omitempty"`
	Project      string `json:"project,omitempty"`
}

// FormatAPIKey trims a pasted key and an accidental Bearer prefix.
func FormatAPIKey(raw string) string {
	key := strings.TrimSpace(raw)
	key = strings.TrimPrefix(key, "Bearer ")
	key = strings.TrimPrefix(key, "bearer ")
	return strings.TrimSpace(key)
}

// DecodeCredential accepts the canonical form or a small set of aliases so a
// pasted {"apiKey":…} or {"token":…} still imports.
func DecodeCredential(payload []byte) (Credential, error) {
	var alias struct {
		Format       string `json:"format"`
		APIKey       string `json:"api_key"`
		APIKeyCamel  string `json:"apiKey"`
		Key          string `json:"key"`
		Token        string `json:"token"`
		Mode         string `json:"mode"`
		AccountMode  string `json:"account_mode"`
		BaseURL      string `json:"base_url"`
		BaseURLCamel string `json:"baseUrl"`
		Organization string `json:"organization"`
		Org          string `json:"zhipu_organization"`
		Project      string `json:"project"`
		ProjectAlias string `json:"zhipu_project"`
	}
	if err := json.Unmarshal(payload, &alias); err != nil {
		return Credential{}, fmt.Errorf("zhipu credential is not valid json")
	}
	key := firstNonEmpty(alias.APIKey, alias.APIKeyCamel, alias.Key, alias.Token)
	if strings.TrimSpace(key) == "" {
		return Credential{}, fmt.Errorf("zhipu credential requires an api key")
	}
	return Credential{
		Format:       firstNonEmpty(alias.Format, CredentialFormat),
		APIKey:       FormatAPIKey(key),
		Mode:         NormalizeMode(firstNonEmpty(alias.Mode, alias.AccountMode)),
		BaseURL:      strings.TrimSpace(firstNonEmpty(alias.BaseURL, alias.BaseURLCamel)),
		Organization: strings.TrimSpace(firstNonEmpty(alias.Organization, alias.Org)),
		Project:      strings.TrimSpace(firstNonEmpty(alias.Project, alias.ProjectAlias)),
	}, nil
}

func (c Credential) Encode() ([]byte, error) {
	c.Format = CredentialFormat
	c.APIKey = FormatAPIKey(c.APIKey)
	c.Mode = NormalizeMode(c.Mode)
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	c.Organization = strings.TrimSpace(c.Organization)
	c.Project = strings.TrimSpace(c.Project)
	return json.Marshal(c)
}

// Ready reports whether the credential can authenticate a request.
func (c Credential) Ready() bool {
	return strings.TrimSpace(c.APIKey) != ""
}

// NormalizeMode accepts only the two official account modes. Anything else,
// including an empty value, is pay-as-you-go.
func NormalizeMode(raw string) string {
	if strings.EqualFold(strings.TrimSpace(raw), ModeCoding) {
		return ModeCoding
	}
	return ModePayG
}

// acceptChatBase reports whether an explicit base URL may be used. Tests in
// this package replace it to allow httptest's local http origin.
var acceptChatBase = func(parsed *url.URL) bool {
	return parsed != nil && strings.EqualFold(parsed.Scheme, "https") && parsed.Host != ""
}

// ChatBase returns the OpenAI-compatible origin for this credential.
// An explicit https base URL wins; otherwise mode selects the official host.
func (c Credential) ChatBase() (string, error) {
	if raw := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/"); raw != "" {
		parsed, err := url.Parse(raw)
		if err != nil || !acceptChatBase(parsed) {
			return "", fmt.Errorf("zhipu base_url must be an https URL")
		}
		if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", fmt.Errorf("zhipu base_url must not carry credentials, a query, or a fragment")
		}
		return raw, nil
	}
	if NormalizeMode(c.Mode) == ModeCoding {
		return CodingBaseURL, nil
	}
	return PayGBaseURL, nil
}

// ValidateCredential is the CredentialCodec entry point.
func ValidateCredential(payload []byte) error {
	credential, err := DecodeCredential(payload)
	if err != nil {
		return err
	}
	if !credential.Ready() {
		return fmt.Errorf("zhipu credential requires an api key")
	}
	if _, err := credential.ChatBase(); err != nil {
		return err
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
