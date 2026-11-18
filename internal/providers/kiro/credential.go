package kiro

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Credential is the stored Kiro login. Builder ID and Identity Center refresh
// through the regional OIDC token endpoint and need the client id and secret.
// Social logins refresh through the desktop endpoint with only the refresh
// token. A ksk_ key is stored as the access token and does not refresh.
type Credential struct {
	Format        string `json:"format"`
	AuthMethod    string `json:"auth_method"`
	Region        string `json:"region,omitempty"`
	AccessToken   string `json:"access_token"`
	RefreshToken  string `json:"refresh_token,omitempty"`
	ExpiresAt     int64  `json:"expires_at,omitempty"`
	ClientID      string `json:"client_id,omitempty"`
	ClientSecret  string `json:"client_secret,omitempty"`
	ProfileArn    string `json:"profile_arn,omitempty"`
	Email         string `json:"email,omitempty"`
	UserID        string `json:"user_id,omitempty"`
	MachineID     string `json:"machine_id,omitempty"`
	APIKey        string `json:"api_key,omitempty"`
	Provider      string `json:"provider,omitempty"`
	StartURL      string `json:"start_url,omitempty"`
	TokenEndpoint string `json:"token_endpoint,omitempty"`
	IssuerURL     string `json:"issuer_url,omitempty"`
	Scopes        string `json:"scopes,omitempty"`
}

func DecodeCredential(payload []byte) (Credential, error) {
	var flat Credential
	if err := json.Unmarshal(payload, &flat); err != nil {
		return Credential{}, fmt.Errorf("kiro credential is not json")
	}
	var alias struct {
		AuthMethod    string `json:"authMethod"`
		AccessToken   string `json:"accessToken"`
		RefreshToken  string `json:"refreshToken"`
		ExpiresAt     any    `json:"expiresAt"`
		ClientID      string `json:"clientId"`
		ClientSecret  string `json:"clientSecret"`
		ProfileArn    string `json:"profileArn"`
		MachineID     string `json:"machineId"`
		APIKey        string `json:"apiKey"`
		KiroAPIKey    string `json:"kiroApiKey"`
		Provider      string `json:"provider"`
		StartURL      string `json:"startUrl"`
		TokenEndpoint string `json:"tokenEndpoint"`
		IssuerURL     string `json:"issuerUrl"`
		Scopes        string `json:"scopes"`
	}
	_ = json.Unmarshal(payload, &alias)
	cred := Credential{
		Format:        firstNonEmpty(flat.Format, CredentialFormat),
		AuthMethod:    strings.ToLower(firstNonEmpty(flat.AuthMethod, alias.AuthMethod)),
		Region:        normalizeRegion(flat.Region),
		AccessToken:   strings.TrimSpace(firstNonEmpty(flat.AccessToken, alias.AccessToken)),
		RefreshToken:  strings.TrimSpace(firstNonEmpty(flat.RefreshToken, alias.RefreshToken)),
		ExpiresAt:     flat.ExpiresAt,
		ClientID:      strings.TrimSpace(firstNonEmpty(flat.ClientID, alias.ClientID)),
		ClientSecret:  strings.TrimSpace(firstNonEmpty(flat.ClientSecret, alias.ClientSecret)),
		ProfileArn:    strings.TrimSpace(firstNonEmpty(flat.ProfileArn, alias.ProfileArn)),
		Email:         strings.TrimSpace(flat.Email),
		UserID:        strings.TrimSpace(flat.UserID),
		MachineID:     strings.TrimSpace(firstNonEmpty(flat.MachineID, alias.MachineID)),
		APIKey:        strings.TrimSpace(firstNonEmpty(flat.APIKey, alias.APIKey, alias.KiroAPIKey)),
		Provider:      strings.TrimSpace(firstNonEmpty(flat.Provider, alias.Provider)),
		StartURL:      strings.TrimSpace(firstNonEmpty(flat.StartURL, alias.StartURL)),
		TokenEndpoint: strings.TrimSpace(firstNonEmpty(flat.TokenEndpoint, alias.TokenEndpoint)),
		IssuerURL:     strings.TrimSpace(firstNonEmpty(flat.IssuerURL, alias.IssuerURL)),
		Scopes:        strings.TrimSpace(firstNonEmpty(flat.Scopes, alias.Scopes)),
	}
	if cred.ExpiresAt == 0 {
		cred.ExpiresAt = parseExpires(alias.ExpiresAt)
	}
	if strings.HasPrefix(cred.APIKey, "ksk_") && cred.AccessToken == "" {
		cred.AccessToken = cred.APIKey
	}
	if strings.HasPrefix(cred.AccessToken, "ksk_") && cred.AuthMethod == "" {
		cred.AuthMethod = "api_key"
	}
	if cred.TokenEndpoint != "" && cred.AuthMethod == "" {
		cred.AuthMethod = "external_idp"
	}
	if cred.AuthMethod == "" && cred.RefreshToken != "" {
		if cred.ClientID != "" && cred.ClientSecret != "" {
			cred.AuthMethod = "builder_id"
		} else {
			cred.AuthMethod = "social"
		}
	}
	if cred.AccessToken == "" && cred.RefreshToken == "" {
		return Credential{}, fmt.Errorf("kiro credential requires an access token or a refresh token")
	}
	return cred, nil
}

func (c Credential) Encode() ([]byte, error) {
	c.Format = CredentialFormat
	c.Region = normalizeRegion(c.Region)
	c.AuthMethod = strings.ToLower(strings.TrimSpace(c.AuthMethod))
	return json.Marshal(c)
}

func (c Credential) Ready() bool {
	if strings.TrimSpace(c.AccessToken) != "" && !c.expired() {
		return true
	}
	return strings.TrimSpace(c.RefreshToken) != ""
}

func (c Credential) expired() bool {
	if c.ExpiresAt <= 0 {
		return false
	}
	return time.Now().Unix()+RefreshSkewSeconds >= c.ExpiresAt
}

func (c Credential) needsRefresh() bool {
	if c.AuthMethod == "api_key" || strings.TrimSpace(c.RefreshToken) == "" {
		return false
	}
	return strings.TrimSpace(c.AccessToken) == "" || c.expired()
}

func (c Credential) bearer() string { return strings.TrimSpace(c.AccessToken) }

var regionPattern = regexp.MustCompile(`^[a-z]{2}(?:-[a-z0-9]+)+-[0-9]+$`)

func normalizeRegion(region string) string {
	switch strings.TrimSpace(strings.ToLower(region)) {
	case "", "global":
		return DefaultRegion
	case "eu", "europe":
		return "eu-central-1"
	default:
		region = strings.TrimSpace(strings.ToLower(region))
		if !regionPattern.MatchString(region) {
			return DefaultRegion
		}
		return region
	}
}

// dataPlaneRegion is the host region for chat, models, and usage. The profile
// ARN wins for OAuth accounts because the OIDC region can differ from where the
// profile lives. An API key has no profile, so its own region is the data plane.
func (c Credential) dataPlaneRegion() string {
	if c.isAPIKey() {
		return normalizeRegion(c.Region)
	}
	if region := regionFromProfile(c.requestProfileArn()); region != "" {
		return region
	}
	return normalizeRegion(c.Region)
}

func (c Credential) isAPIKey() bool {
	return c.AuthMethod == "api_key" || strings.HasPrefix(c.AccessToken, "ksk_") || strings.HasPrefix(c.APIKey, "ksk_")
}

func (c Credential) isExternal() bool {
	return c.AuthMethod == "external_idp" || c.AuthMethod == "microsoft"
}

func (c Credential) isBuilderID() bool {
	if strings.EqualFold(c.Provider, "BuilderId") || c.AuthMethod == "builder_id" {
		return true
	}
	if c.isAPIKey() || c.isExternal() || !strings.EqualFold(c.AuthMethod, "idc") {
		return false
	}
	start := strings.TrimRight(c.StartURL, "/")
	return start == "" || strings.EqualFold(start, "https://view.awsapps.com/start")
}

// requestProfileArn is the profile a request names without a lookup. The fixed
// Builder ID profile is request-scoped and never written back to the credential.
func (c Credential) requestProfileArn() string {
	if c.isAPIKey() {
		return ""
	}
	if arn := strings.TrimSpace(c.ProfileArn); arn != "" {
		return arn
	}
	if c.isBuilderID() {
		return builderIDProfileArn
	}
	return ""
}

func (c Credential) tokenType() string {
	switch {
	case c.isAPIKey():
		return "API_KEY"
	case c.isExternal():
		return "EXTERNAL_IDP"
	default:
		return ""
	}
}

func parseExpires(value any) int64 {
	switch v := value.(type) {
	case float64:
		n := int64(v)
		if n > 0 && n < 10_000_000 {
			return time.Now().Unix() + n
		}
		if n > 10_000_000_000 {
			return n / 1000
		}
		return n
	case string:
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(v)); err == nil {
			return t.Unix()
		}
	}
	return 0
}

func firstNonEmpty(vals ...string) string {
	for _, value := range vals {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func ValidateCredential(payload []byte) error {
	cred, err := DecodeCredential(payload)
	if err != nil {
		return err
	}
	if !cred.Ready() {
		return fmt.Errorf("kiro credential requires an access token or a refresh token")
	}
	return nil
}
