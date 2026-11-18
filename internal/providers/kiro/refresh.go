package kiro

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
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
	case cred.AuthMethod == "social" || (cred.ClientID == "" && cred.ClientSecret == ""):
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
	if err := validateTokenEndpoint(cred.TokenEndpoint); err != nil {
		return cred, err
	}
	form := url.Values{}
	form.Set("client_id", cred.ClientID)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", cred.RefreshToken)
	if cred.Scopes != "" {
		form.Set("scope", cred.Scopes)
	}
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
	if expires > 0 {
		cred.ExpiresAt = time.Now().Unix() + int64(expires)
	}
	if strings.TrimSpace(parsed.ProfileArn) != "" {
		cred.ProfileArn = parsed.ProfileArn
	}
	return cred, nil
}

// validateTokenEndpoint refuses anything that is not https on a public host.
// The endpoint is operator-supplied and receives the refresh token.
func validateTokenEndpoint(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.Scheme != "https" || parsed.User != nil {
		return fmt.Errorf("kiro token endpoint must be an https url")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".local") || netLooksPrivate(host) {
		return fmt.Errorf("kiro token endpoint host is not allowed")
	}
	return nil
}

func netLooksPrivate(host string) bool {
	return host == "127.0.0.1" || host == "::1" || strings.HasPrefix(host, "10.") ||
		strings.HasPrefix(host, "192.168.") || strings.HasPrefix(host, "169.254.")
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
