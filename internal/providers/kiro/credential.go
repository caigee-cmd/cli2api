package kiro

import (
	"bytes"
	"crypto/sha256"
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
	Nickname      string `json:"nickname,omitempty"`
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
		Email         string `json:"email"`
		Nickname      string `json:"nickname"`
		MachineID     string `json:"machineId"`
		APIKey        string `json:"apiKey"`
		KiroAPIKey    string `json:"kiroApiKey"`
		Provider      string `json:"provider"`
		Region        string `json:"region"`
		StartURL      string `json:"startUrl"`
		TokenEndpoint string `json:"tokenEndpoint"`
		IssuerURL     string `json:"issuerUrl"`
		Scopes        string `json:"scopes"`
	}
	_ = json.Unmarshal(payload, &alias)
	cred := Credential{
		Format:        firstNonEmpty(flat.Format, CredentialFormat),
		AuthMethod:    strings.ToLower(firstNonEmpty(flat.AuthMethod, alias.AuthMethod)),
		Region:        normalizeRegion(firstNonEmpty(flat.Region, alias.Region)),
		AccessToken:   strings.TrimSpace(firstNonEmpty(flat.AccessToken, alias.AccessToken)),
		RefreshToken:  strings.TrimSpace(firstNonEmpty(flat.RefreshToken, alias.RefreshToken)),
		ExpiresAt:     flat.ExpiresAt,
		ClientID:      strings.TrimSpace(firstNonEmpty(flat.ClientID, alias.ClientID)),
		ClientSecret:  strings.TrimSpace(firstNonEmpty(flat.ClientSecret, alias.ClientSecret)),
		ProfileArn:    strings.TrimSpace(firstNonEmpty(flat.ProfileArn, alias.ProfileArn)),
		Email:         strings.TrimSpace(firstNonEmpty(flat.Email, alias.Email)),
		Nickname:      strings.TrimSpace(firstNonEmpty(flat.Nickname, alias.Nickname)),
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
	if key, keyRegion, ok := splitAPIKeyRegion(cred.APIKey); ok {
		cred.APIKey = key
		if !regionStated(flat.Region) && !regionStated(alias.Region) && keyRegion != "" {
			cred.Region = keyRegion
		}
	}
	if key, keyRegion, ok := splitAPIKeyRegion(cred.AccessToken); ok && cred.APIKey == "" {
		cred.APIKey = key
		cred.AccessToken = key
		if !regionStated(flat.Region) && !regionStated(alias.Region) && keyRegion != "" {
			cred.Region = keyRegion
		}
	}
	if strings.HasPrefix(cred.APIKey, "ksk_") && (cred.AccessToken == "" || strings.Contains(cred.AccessToken, "|")) {
		cred.AccessToken = cred.APIKey
	}
	if cred.isAPIKey() {
		cred.AuthMethod = "api_key"
		cred.RefreshToken = ""
		cred.ClientID = ""
		cred.ClientSecret = ""
		cred.StartURL = ""
		cred.ProfileArn = ""
		cred.ExpiresAt = 0
		if cred.MachineID == "" {
			cred.MachineID = machineIDFromAPIKey(cred.APIKey)
		}
		if cred.Email == "" {
			cred.Email = "api-key-" + apiKeyFingerprint(cred.APIKey)[:12]
		}
	}
	if cred.TokenEndpoint != "" && cred.AuthMethod == "" {
		cred.AuthMethod = "external_idp"
	}
	if cred.AuthMethod == "" && (cred.RefreshToken != "" || cred.AccessToken != "") {
		if cred.ClientID != "" && cred.ClientSecret != "" {
			cred.AuthMethod = "builder_id"
		} else if socialProvider(cred.Provider) || cred.RefreshToken != "" {
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

// regionStated reports whether the JSON named a region. An empty field is not a
// statement, so a ksk_|region suffix may fill it. "global" and a real region are.
func socialProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "github", "google", "social":
		return true
	default:
		return false
	}
}

func regionStated(region string) bool {
	return strings.TrimSpace(region) != ""
}

// splitAPIKeyRegion accepts the Kiro CLI export "ksk_…|region". A bare key has
// no region. More than one separator is not that form.
func splitAPIKeyRegion(raw string) (key, region string, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "ksk_") {
		return "", "", false
	}
	parts := strings.Split(trimmed, "|")
	if len(parts) > 2 {
		return "", "", false
	}
	key = strings.TrimSpace(parts[0])
	if key == "" || strings.ContainsAny(key, " \t") {
		return "", "", false
	}
	if len(parts) == 2 {
		region = normalizeRegion(parts[1])
		if !regionPattern.MatchString(region) {
			region = ""
		}
	}
	return key, region, true
}

func machineIDFromAPIKey(apiKey string) string {
	sum := sha256.Sum256([]byte("KiroAPIKey/" + apiKey))
	return fmt.Sprintf("%x", sum[:])
}

func apiKeyFingerprint(apiKey string) string {
	sum := sha256.Sum256([]byte(apiKey))
	return fmt.Sprintf("%x", sum[:])
}

// importRegion is the data plane an import should store on the account. It is
// set only when the JSON states one: a region field, a ksk_|region suffix, or
// the region inside a profile ARN. A missing hint stays empty so the operator's
// chosen region is kept. Profile ARN wins for OAuth because that is the data plane.
func (c Credential) importRegion(stated bool) string {
	// The fixed Builder ID profile is a request default, not something the JSON
	// named, so it must not move the account off the region the operator chose.
	if !c.isAPIKey() && strings.TrimSpace(c.ProfileArn) != "" {
		if region := regionFromProfile(c.ProfileArn); region != "" {
			return region
		}
	}
	if stated {
		return normalizeRegion(c.Region)
	}
	return ""
}

func (c Credential) importName() string {
	return firstNonEmpty(c.Nickname, c.Email, c.UserID)
}

// credentialStatesRegion reports whether the import JSON named a region, either
// as a field or as the suffix of a ksk_|region key. Decode fills defaults, so
// the raw object is what decides whether the account region should move.
// splitCredentialImport unwraps an upstream export that wraps one or more Kiro
// credentials under accounts[]. Anything else, including a WorkBuddy hub pack,
// stays one payload so another provider's importer can reject it.
func splitCredentialImport(payload []byte) ([][]byte, error) {
	var wrapped struct {
		Accounts []json.RawMessage `json:"accounts"`
	}
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var list []json.RawMessage
		if json.Unmarshal(trimmed, &list) == nil && len(list) > 0 {
			out := make([][]byte, 0, len(list))
			for _, entry := range list {
				item := bytes.TrimSpace(entry)
				if len(item) == 0 || string(item) == "null" {
					continue
				}
				out = append(out, append([]byte(nil), item...))
			}
			if len(out) > 0 {
				return out, nil
			}
		}
	}
	if json.Unmarshal(payload, &wrapped) != nil || len(wrapped.Accounts) == 0 || !packageLooksLikeKiro(payload) {
		return [][]byte{payload}, nil
	}
	out := make([][]byte, 0, len(wrapped.Accounts))
	for _, entry := range wrapped.Accounts {
		trimmed := bytes.TrimSpace(entry)
		if len(trimmed) == 0 || string(trimmed) == "null" || !payloadLooksLikeKiro(trimmed) {
			continue
		}
		out = append(out, append([]byte(nil), trimmed...))
	}
	if len(out) == 0 {
		return [][]byte{payload}, nil
	}
	return out, nil
}

func packageLooksLikeKiro(payload []byte) bool {
	if payloadLooksLikeKiro(payload) {
		return true
	}
	var wrapped struct {
		Accounts []json.RawMessage `json:"accounts"`
	}
	if json.Unmarshal(payload, &wrapped) != nil {
		return false
	}
	for _, entry := range wrapped.Accounts {
		if payloadLooksLikeKiro(entry) {
			return true
		}
	}
	return false
}

func payloadLooksLikeKiro(payload []byte) bool {
	var raw struct {
		Format        string `json:"format"`
		AuthMethod    string `json:"authMethod"`
		AuthMethodAlt string `json:"auth_method"`
		Provider      string `json:"provider"`
		Region        string `json:"region"`
		ProfileArn    string `json:"profileArn"`
		ProfileAlt    string `json:"profile_arn"`
		ClientID      string `json:"clientId"`
		ClientAlt     string `json:"client_id"`
		ClientSecret  string `json:"clientSecret"`
		SecretAlt     string `json:"client_secret"`
		RefreshToken  string `json:"refreshToken"`
		RefreshAlt    string `json:"refresh_token"`
		AccessToken   string `json:"accessToken"`
		AccessAlt     string `json:"access_token"`
		APIKey        string `json:"apiKey"`
		APIKeyAlt     string `json:"api_key"`
		KiroAPIKey    string `json:"kiroApiKey"`
		StartURL      string `json:"startUrl"`
		StartAlt      string `json:"start_url"`
		TokenEndpoint string `json:"tokenEndpoint"`
		IssuerURL     string `json:"issuerUrl"`
	}
	if json.Unmarshal(payload, &raw) != nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(raw.Format), CredentialFormat) {
		return true
	}
	method := strings.ToLower(firstNonEmpty(raw.AuthMethod, raw.AuthMethodAlt))
	switch method {
	case "builder_id", "builderid", "idc", "social", "external_idp", "api_key", "apikey":
		return true
	}
	provider := strings.ToLower(strings.TrimSpace(raw.Provider))
	if provider == "builderid" || provider == "kiro" || provider == "apikey" || socialProvider(provider) {
		return true
	}
	for _, value := range []string{raw.KiroAPIKey, raw.APIKey, raw.APIKeyAlt, raw.AccessToken, raw.AccessAlt} {
		if strings.HasPrefix(strings.TrimSpace(value), "ksk_") {
			return true
		}
	}
	refresh := firstNonEmpty(raw.RefreshToken, raw.RefreshAlt)
	if firstNonEmpty(raw.ClientID, raw.ClientAlt) != "" && firstNonEmpty(raw.ClientSecret, raw.SecretAlt) != "" && refresh != "" {
		return true
	}
	if strings.Contains(strings.ToLower(firstNonEmpty(raw.StartURL, raw.StartAlt)), "awsapps.com") && refresh != "" {
		return true
	}
	if strings.Contains(firstNonEmpty(raw.ProfileArn, raw.ProfileAlt), "codewhisperer") && (refresh != "" || firstNonEmpty(raw.AccessToken, raw.AccessAlt) != "") {
		return true
	}
	if strings.Contains(strings.ToLower(raw.TokenEndpoint+raw.IssuerURL), "microsoftonline") && refresh != "" {
		return true
	}
	region := strings.ToLower(strings.TrimSpace(raw.Region))
	if (region == "us-east-1" || region == "eu-central-1" || region == "eu" || region == "europe") && (refresh != "" || firstNonEmpty(raw.AccessToken, raw.AccessAlt) != "") {
		return true
	}
	return false
}

func credentialStatesRegion(payload []byte) bool {
	var raw struct {
		Region      string `json:"region"`
		APIKey      string `json:"api_key"`
		APIKeyCamel string `json:"apiKey"`
		KiroAPIKey  string `json:"kiroApiKey"`
		AccessToken string `json:"access_token"`
		AccessCamel string `json:"accessToken"`
	}
	if json.Unmarshal(payload, &raw) != nil {
		return false
	}
	if regionStated(raw.Region) {
		return true
	}
	for _, value := range []string{raw.APIKey, raw.APIKeyCamel, raw.KiroAPIKey, raw.AccessToken, raw.AccessCamel} {
		_, region, ok := splitAPIKeyRegion(value)
		if ok && region != "" {
			return true
		}
	}
	return false
}

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
