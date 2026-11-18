package kiro

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// microsoftPortal is what Kiro's sign-in page sends back on the first redirect:
// the enterprise IdP that should take over. It is not an authorization code.
type microsoftPortal struct {
	IssuerURL string
	ClientID  string
	Scopes    string
	LoginHint string
}

// microsoftProvider is the second leg, built from the portal's descriptor and
// the IdP's discovery document. The verifier is what redeems the code.
type microsoftProvider struct {
	State        string
	Verifier     string
	IssuerURL    string
	AuthorizeURL string
	TokenURL     string
	ClientID     string
	Scopes       string
}

// microsoftSignInURL builds Kiro's sign-in URL. The challenge is sent because
// the page expects the query shape; this leg never checks a verifier.
func microsoftSignIn(state, challenge string) string {
	params := url.Values{}
	params.Set("state", state)
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")
	params.Set("redirect_uri", microsoftLoopbackBase)
	params.Set("redirect_from", "KiroIDE")
	return microsoftSignInURL + "?" + params.Encode()
}

// readMicrosoftPortal checks the first callback and returns the IdP descriptor.
// A state that is present must match; some portal builds omit it.
func readMicrosoftPortal(rawURL, expectedState string) (microsoftPortal, error) {
	parsed, err := loopbackCallback(rawURL, microsoftPortalPath)
	if err != nil {
		return microsoftPortal{}, err
	}
	query := parsed.Query()
	if failure := query.Get("error"); failure != "" {
		return microsoftPortal{}, fmt.Errorf("kiro sign-in failed: %s", failure)
	}
	if state := query.Get("state"); state != "" && !fixedEqual(state, expectedState) {
		return microsoftPortal{}, fmt.Errorf("kiro sign-in state does not match this login")
	}
	if !strings.EqualFold(query.Get("login_option"), "external_idp") {
		return microsoftPortal{}, fmt.Errorf("kiro sign-in did not return a microsoft login")
	}
	portal := microsoftPortal{
		IssuerURL: query.Get("issuer_url"), ClientID: query.Get("client_id"),
		Scopes: query.Get("scopes"), LoginHint: query.Get("login_hint"),
	}
	if portal.IssuerURL == "" || portal.ClientID == "" || portal.Scopes == "" {
		return microsoftPortal{}, fmt.Errorf("kiro sign-in callback is missing the identity provider")
	}
	if _, err := uuid.Parse(portal.ClientID); err != nil {
		return microsoftPortal{}, fmt.Errorf("kiro sign-in client id is not a uuid")
	}
	scopes, err := externalScopes(portal.Scopes, portal.ClientID)
	if err != nil {
		return microsoftPortal{}, err
	}
	portal.Scopes = scopes
	return portal, nil
}

// discoverMicrosoftOIDC reads the tenant's discovery document and checks that
// both endpoints belong to that tenant.
func discoverMicrosoftOIDC(ctx context.Context, client *http.Client, issuerURL string) (authorizeURL, tokenURL string, err error) {
	issuer, tenant, err := parseEntraIssuer(issuerURL)
	if err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(issuer.String(), "/")+"/.well-known/openid-configuration", nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("microsoft discovery failed (HTTP %d)", resp.StatusCode)
	}
	var document struct {
		Issuer    string `json:"issuer"`
		Authorize string `json:"authorization_endpoint"`
		Token     string `json:"token_endpoint"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return "", "", fmt.Errorf("microsoft discovery: %w", err)
	}
	if !strings.EqualFold(strings.TrimRight(document.Issuer, "/"), strings.TrimRight(issuer.String(), "/")) {
		return "", "", fmt.Errorf("microsoft discovery issuer does not match")
	}
	if err := entraPath(document.Authorize, issuer.Hostname(), tenant, "/oauth2/v2.0/authorize"); err != nil {
		return "", "", fmt.Errorf("microsoft authorization endpoint: %w", err)
	}
	if err := entraPath(document.Token, issuer.Hostname(), tenant, "/oauth2/v2.0/token"); err != nil {
		return "", "", fmt.Errorf("microsoft token endpoint: %w", err)
	}
	return document.Authorize, document.Token, nil
}

// microsoftAuthorizeURL is the second redirect, sent to the tenant that the
// portal named.
func microsoftAuthorizeURL(provider microsoftProvider, loginHint string) string {
	params := url.Values{}
	params.Set("client_id", provider.ClientID)
	params.Set("response_type", "code")
	params.Set("redirect_uri", microsoftLoopbackBase+microsoftOAuthPath)
	params.Set("scope", provider.Scopes)
	params.Set("code_challenge", pkceChallenge(provider.Verifier))
	params.Set("code_challenge_method", "S256")
	params.Set("response_mode", "query")
	params.Set("state", provider.State)
	if loginHint != "" {
		params.Set("login_hint", loginHint)
	}
	return provider.AuthorizeURL + "?" + params.Encode()
}

// exchangeMicrosoftCode redeems the second callback. The state must match the
// one this login minted.
func exchangeMicrosoftCode(ctx context.Context, client *http.Client, provider microsoftProvider, rawURL string) (Credential, error) {
	parsed, err := loopbackCallback(rawURL, microsoftOAuthPath)
	if err != nil {
		return Credential{}, err
	}
	query := parsed.Query()
	if !fixedEqual(query.Get("state"), provider.State) {
		return Credential{}, fmt.Errorf("microsoft callback state does not match this login")
	}
	if failure := query.Get("error"); failure != "" {
		return Credential{}, fmt.Errorf("microsoft authorization failed: %s", failure)
	}
	code := query.Get("code")
	if code == "" {
		return Credential{}, fmt.Errorf("microsoft callback has no authorization code")
	}
	form := url.Values{}
	form.Set("client_id", provider.ClientID)
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", microsoftLoopbackBase+microsoftOAuthPath)
	form.Set("code_verifier", provider.Verifier)
	form.Set("scope", provider.Scopes)
	token, err := postFormToken(ctx, client, provider.TokenURL, form)
	if err != nil {
		return Credential{}, err
	}
	return Credential{
		AuthMethod: "external_idp", AccessToken: token.AccessToken, RefreshToken: token.RefreshToken,
		ExpiresAt: token.ExpiresAt.Unix(), ClientID: provider.ClientID,
		TokenEndpoint: provider.TokenURL, IssuerURL: provider.IssuerURL, Scopes: provider.Scopes,
	}, nil
}

type formToken struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

func postFormToken(ctx context.Context, client *http.Client, endpoint string, form url.Values) (formToken, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return formToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return formToken{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var parsed struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return formToken{}, fmt.Errorf("microsoft token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		if parsed.Error != "" {
			return formToken{}, fmt.Errorf("microsoft token request failed: %s", parsed.Error)
		}
		return formToken{}, fmt.Errorf("microsoft token request failed (HTTP %d)", resp.StatusCode)
	}
	if parsed.AccessToken == "" || parsed.RefreshToken == "" || parsed.ExpiresIn <= 0 {
		return formToken{}, fmt.Errorf("microsoft token response is incomplete")
	}
	return formToken{
		AccessToken: parsed.AccessToken, RefreshToken: parsed.RefreshToken,
		ExpiresAt: time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second),
	}, nil
}

// loopbackCallback accepts only the fixed localhost:3128 redirect Kiro uses.
func loopbackCallback(rawURL, path string) (*url.URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" || len(rawURL) > 8192 {
		return nil, fmt.Errorf("callback url is missing or too long")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || !parsed.IsAbs() {
		return nil, fmt.Errorf("callback url is invalid")
	}
	if !strings.EqualFold(parsed.Scheme, "http") || !strings.EqualFold(parsed.Hostname(), "localhost") ||
		parsed.Port() != "3128" || parsed.User != nil || parsed.Fragment != "" || parsed.Path != path {
		return nil, fmt.Errorf("callback url must be %s%s", microsoftLoopbackBase, path)
	}
	return parsed, nil
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func fixedEqual(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
