package kiro

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// refresh exchanges a refresh token. Builder ID and Identity Center post to
// the regional OIDC token endpoint; social logins post to the desktop
// refresh endpoint; Microsoft and other external IdPs post a form to their
// own token endpoint. API keys are not refreshable.
//
// One refresh runs per account at a time. A second caller waits and then
// reloads the credential the first caller stored, so two refreshes cannot
// rotate the same refresh token.
func (c *Client) refreshCredential(ctx context.Context, accountID string, cred Credential) (Credential, error) {
	if !cred.needsRefresh() {
		return cred, nil
	}
	unlock, err := lockRefresh(ctx, accountID)
	if err != nil {
		return Credential{}, err
	}
	defer unlock()

	_, payload, err := c.store.LoadCredentialPayload(ctx, accountID)
	if err != nil {
		return Credential{}, err
	}
	latest, err := DecodeCredential(payload)
	if err != nil {
		return Credential{}, err
	}
	if !latest.needsRefresh() {
		return latest, nil
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return Credential{}, err
	}
	refreshed, err := refresh(ctx, client, latest)
	if err != nil {
		return Credential{}, err
	}
	encoded, err := refreshed.Encode()
	if err != nil {
		return Credential{}, err
	}
	if err := c.store.SaveCredentialPayload(ctx, accountID, CredentialFormat, encoded); err != nil {
		return Credential{}, err
	}
	return refreshed, nil
}

func refresh(ctx context.Context, client *http.Client, cred Credential) (Credential, error) {
	switch {
	case cred.isAPIKey() || strings.TrimSpace(cred.RefreshToken) == "":
		return cred, fmt.Errorf("kiro credential cannot be refreshed")
	case cred.isExternal():
		return refreshExternal(ctx, client, cred)
	case cred.AuthMethod == "social":
		body, _ := json.Marshal(map[string]string{"refreshToken": cred.RefreshToken})
		return postRefresh(ctx, client, cred, socialRefreshURL, body, "application/json")
	default:
		if cred.ClientID == "" || cred.ClientSecret == "" {
			return cred, fmt.Errorf("kiro oidc refresh requires client id and client secret")
		}
		body, _ := json.Marshal(map[string]string{
			"clientId": cred.ClientID, "clientSecret": cred.ClientSecret,
			"refreshToken": cred.RefreshToken, "grantType": "refresh_token",
		})
		endpoint := "https://oidc." + normalizeRegion(cred.Region) + ".amazonaws.com/token"
		return postRefresh(ctx, client, cred, endpoint, body, "application/json")
	}
}

func refreshExternal(ctx context.Context, client *http.Client, cred Credential) (Credential, error) {
	if cred.ClientID == "" || cred.TokenEndpoint == "" {
		return cred, fmt.Errorf("kiro external refresh requires client id and token endpoint")
	}
	scopes, err := externalScopes(cred.Scopes, cred.ClientID)
	if err != nil {
		return cred, err
	}
	if err := validateExternalConfig(cred.ClientID, cred.TokenEndpoint, cred.IssuerURL); err != nil {
		return cred, err
	}
	form := url.Values{}
	form.Set("client_id", cred.ClientID)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", cred.RefreshToken)
	form.Set("scope", scopes)
	return postRefresh(ctx, client, cred, cred.TokenEndpoint, []byte(form.Encode()), "application/x-www-form-urlencoded")
}

func postRefresh(ctx context.Context, client *http.Client, cred Credential, endpoint string, body []byte, contentType string) (Credential, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return cred, err
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := client.Do(req)
	if err != nil {
		return cred, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return cred, newProviderError(resp.StatusCode, string(raw))
	}
	var parsed struct {
		AccessToken      string `json:"accessToken"`
		RefreshToken     string `json:"refreshToken"`
		ExpiresIn        int    `json:"expiresIn"`
		ProfileArn       string `json:"profileArn"`
		AccessTokenSnake string `json:"access_token"`
		RefreshSnake     string `json:"refresh_token"`
		ExpiresSnake     int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return cred, fmt.Errorf("kiro refresh response: %w", err)
	}
	access := firstNonEmpty(parsed.AccessToken, parsed.AccessTokenSnake)
	if access == "" {
		return cred, fmt.Errorf("kiro refresh returned an empty access token")
	}
	cred.AccessToken = access
	if rotated := firstNonEmpty(parsed.RefreshToken, parsed.RefreshSnake); rotated != "" {
		cred.RefreshToken = rotated
	}
	expires := parsed.ExpiresIn
	if expires == 0 {
		expires = parsed.ExpiresSnake
	}
	if expires <= 0 {
		return cred, fmt.Errorf("kiro refresh returned no expiry")
	}
	cred.ExpiresAt = time.Now().Unix() + int64(expires)
	if strings.TrimSpace(parsed.ProfileArn) != "" {
		cred.ProfileArn = parsed.ProfileArn
	}
	return cred, nil
}

// entraHosts are the Microsoft login clouds an external IdP refresh may call.
// The endpoint is operator-supplied and receives the refresh token, so anything
// else is refused rather than treated as a generic https host.
var entraHosts = map[string]bool{
	"login.microsoftonline.com":        true,
	"login.microsoftonline.us":         true,
	"login.partner.microsoftonline.cn": true,
}

// validateExternalConfig checks the endpoint and, when the issuer is present,
// that both name the same tenant. A credential imported without an issuer keeps
// the host check only.
func validateExternalConfig(clientID, tokenEndpoint, issuerURL string) error {
	if _, err := uuid.Parse(strings.TrimSpace(clientID)); err != nil {
		return fmt.Errorf("kiro external refresh requires a uuid client id")
	}
	if err := validateEntraURL(tokenEndpoint); err != nil {
		return fmt.Errorf("kiro token endpoint: %w", err)
	}
	if strings.TrimSpace(issuerURL) == "" {
		return nil
	}
	issuer, tenant, err := parseEntraIssuer(issuerURL)
	if err != nil {
		return err
	}
	if err := entraPath(tokenEndpoint, issuer.Hostname(), tenant, "/oauth2/v2.0/token"); err != nil {
		return fmt.Errorf("kiro token endpoint: %w", err)
	}
	return nil
}

func validateEntraURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !parsed.IsAbs() || parsed.Opaque != "" {
		return fmt.Errorf("must be an https url")
	}
	if !strings.EqualFold(parsed.Scheme, "https") || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("must be https without user info or a fragment")
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return fmt.Errorf("must use the default https port")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" || net.ParseIP(host) != nil || !entraHosts[host] {
		return fmt.Errorf("host %q is not a microsoft login host", host)
	}
	return nil
}

// parseEntraIssuer accepts https://<login host>/<tenant>/v2.0 and returns the
// tenant. The tenant is a UUID, so a path cannot smuggle a different host.
func parseEntraIssuer(raw string) (*url.URL, string, error) {
	if err := validateEntraURL(raw); err != nil {
		return nil, "", fmt.Errorf("kiro issuer: %w", err)
	}
	parsed, _ := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if parsed.RawQuery != "" {
		return nil, "", fmt.Errorf("kiro issuer must not carry a query")
	}
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(parts) != 2 || !strings.EqualFold(parts[1], "v2.0") {
		return nil, "", fmt.Errorf("kiro issuer must end in /<tenant>/v2.0")
	}
	tenant, err := url.PathUnescape(parts[0])
	if err != nil {
		return nil, "", fmt.Errorf("kiro issuer tenant is invalid")
	}
	if _, err := uuid.Parse(tenant); err != nil {
		return nil, "", fmt.Errorf("kiro issuer tenant must be a uuid")
	}
	return parsed, tenant, nil
}

func entraPath(raw, host, tenant, suffix string) error {
	if err := validateEntraURL(raw); err != nil {
		return err
	}
	parsed, _ := url.Parse(strings.TrimSpace(raw))
	if !strings.EqualFold(parsed.Hostname(), host) {
		return fmt.Errorf("host does not match the issuer")
	}
	if parsed.RawQuery != "" {
		return fmt.Errorf("must not carry a query")
	}
	if !strings.EqualFold(strings.TrimRight(parsed.EscapedPath(), "/"), "/"+tenant+suffix) {
		return fmt.Errorf("path does not match the issuer tenant")
	}
	return nil
}

// externalScopes checks the scopes stored for a Microsoft login and returns
// them in a stable order. Kiro's own scopes arrive as api://<client id>/<scope>;
// offline_access is what lets the refresh token be issued again.
func externalScopes(raw, clientID string) (string, error) {
	if len(raw) > 4096 {
		return "", fmt.Errorf("kiro external scopes are too long")
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 || len(fields) > 16 {
		return "", fmt.Errorf("kiro external refresh requires scopes")
	}
	standard := map[string]bool{"openid": true, "profile": true, "email": true, "offline_access": true}
	kiro := map[string]bool{
		"codewhisperer:completions": true, "codewhisperer:analysis": true,
		"codewhisperer:conversations": true, "codewhisperer:transformations": true,
		"codewhisperer:taskassist": true,
	}
	prefix := "api://" + strings.TrimSpace(clientID) + "/"
	seen := map[string]bool{}
	offline, kiroScope := false, false
	out := make([]string, 0, len(fields))
	for _, scope := range fields {
		if seen[scope] {
			continue
		}
		switch {
		case standard[scope]:
			offline = offline || scope == "offline_access"
		case strings.HasPrefix(scope, prefix) && kiro[strings.TrimPrefix(scope, prefix)]:
			kiroScope = true
		default:
			return "", fmt.Errorf("kiro external scope %q is not allowed", scope)
		}
		seen[scope] = true
		out = append(out, scope)
	}
	if !offline {
		return "", fmt.Errorf("kiro external scopes must include offline_access")
	}
	if !kiroScope {
		return "", fmt.Errorf("kiro external scopes must include a kiro scope")
	}
	return strings.Join(out, " "), nil
}

type refreshLock struct {
	gate chan struct{}
	refs int
}

var refreshLocks = struct {
	sync.Mutex
	accounts map[string]*refreshLock
}{accounts: map[string]*refreshLock{}}

func lockRefresh(ctx context.Context, id string) (func(), error) {
	if strings.TrimSpace(id) == "" {
		return func() {}, nil
	}
	refreshLocks.Lock()
	entry := refreshLocks.accounts[id]
	if entry == nil {
		entry = &refreshLock{gate: make(chan struct{}, 1)}
		refreshLocks.accounts[id] = entry
	}
	entry.refs++
	refreshLocks.Unlock()
	release := func() {
		refreshLocks.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(refreshLocks.accounts, id)
		}
		refreshLocks.Unlock()
	}
	select {
	case entry.gate <- struct{}{}:
		return func() { <-entry.gate; release() }, nil
	case <-ctx.Done():
		release()
		return nil, ctx.Err()
	}
}
