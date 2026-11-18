package kiro

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// oidcClient is a public client registered with the regional OIDC endpoint.
// The secret comes back from registration and is stored with the credential,
// since refreshing needs the same pair.
type oidcClient struct {
	ID     string
	Secret string
}

type deviceAuthorization struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	Interval        time.Duration
	ExpiresIn       time.Duration
}

// oidcToken is one token response. ExpiresAt is absolute; a response without an
// expiry is refused, since the credential could not know when to refresh.
type oidcToken struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	ProfileArn   string
}

func oidcBase(region string) string {
	return "https://" + "oidc." + normalizeRegion(region) + ".amazonaws.com"
}

// registerOIDCClient registers a public client. grantTypes selects the flow:
// the device-code grant for Builder ID, the authorization-code grant for
// Identity Center. redirectURI is empty for the device flow.
func registerOIDCClient(ctx context.Context, client *http.Client, region, issuer, redirectURI string, grantTypes []string) (oidcClient, error) {
	body := map[string]any{
		"clientName": "Kiro",
		"clientType": "public",
		"scopes":     kiroOIDCScopes,
		"grantTypes": grantTypes,
		"issuerUrl":  issuer,
	}
	if redirectURI != "" {
		body["redirectUris"] = []string{redirectURI}
	}
	var parsed struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	if err := postOIDC(ctx, client, oidcBase(region)+"/client/register", body, &parsed); err != nil {
		return oidcClient{}, err
	}
	if parsed.ClientID == "" || parsed.ClientSecret == "" {
		return oidcClient{}, fmt.Errorf("kiro oidc registration returned no client")
	}
	return oidcClient{ID: parsed.ClientID, Secret: parsed.ClientSecret}, nil
}

func startDeviceAuthorization(ctx context.Context, client *http.Client, region, startURL string, registered oidcClient) (deviceAuthorization, error) {
	body := map[string]string{
		"clientId": registered.ID, "clientSecret": registered.Secret, "startUrl": startURL,
	}
	var parsed struct {
		DeviceCode              string `json:"deviceCode"`
		UserCode                string `json:"userCode"`
		VerificationURI         string `json:"verificationUri"`
		VerificationURIComplete string `json:"verificationUriComplete"`
		Interval                int    `json:"interval"`
		ExpiresIn               int    `json:"expiresIn"`
	}
	if err := postOIDC(ctx, client, oidcBase(region)+"/device_authorization", body, &parsed); err != nil {
		return deviceAuthorization{}, err
	}
	if parsed.DeviceCode == "" || parsed.UserCode == "" {
		return deviceAuthorization{}, fmt.Errorf("kiro device authorization returned no code")
	}
	interval := parsed.Interval
	if interval <= 0 {
		interval = 5
	}
	expires := parsed.ExpiresIn
	if expires <= 0 {
		expires = int(loginTTL.Seconds())
	}
	verification := parsed.VerificationURIComplete
	if verification == "" {
		verification = parsed.VerificationURI
	}
	return deviceAuthorization{
		DeviceCode: parsed.DeviceCode, UserCode: parsed.UserCode, VerificationURI: verification,
		Interval: time.Duration(interval) * time.Second, ExpiresIn: time.Duration(expires) * time.Second,
	}, nil
}

// pollDeviceToken asks once. pending reports that the user has not finished;
// the caller decides when to ask again.
func pollDeviceToken(ctx context.Context, client *http.Client, region string, registered oidcClient, deviceCode string) (oidcToken, bool, error) {
	body := map[string]string{
		"clientId": registered.ID, "clientSecret": registered.Secret,
		"grantType": "urn:ietf:params:oauth:grant-type:device_code", "deviceCode": deviceCode,
	}
	return postToken(ctx, client, oidcBase(region)+"/token", body)
}

func exchangeAuthorizationCode(ctx context.Context, client *http.Client, region string, registered oidcClient, code, verifier, redirectURI string) (oidcToken, error) {
	body := map[string]string{
		"clientId": registered.ID, "clientSecret": registered.Secret,
		"grantType": "authorization_code", "redirectUri": redirectURI,
		"code": code, "codeVerifier": verifier,
	}
	token, pending, err := postToken(ctx, client, oidcBase(region)+"/token", body)
	if err != nil {
		return oidcToken{}, err
	}
	if pending {
		return oidcToken{}, fmt.Errorf("kiro authorization code was not accepted")
	}
	return token, nil
}

// authorizeURL is the Identity Center authorization URL. Scopes are joined with
// commas, which is the shape that endpoint expects.
func authorizeURL(region, clientID, redirectURI, state, challenge string) string {
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", clientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("scopes", strings.Join(kiroOIDCScopes, ","))
	params.Set("state", state)
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")
	return oidcBase(region) + "/authorize?" + params.Encode()
}

// postToken reads a token response. pending is set for authorization_pending
// and slow_down; every other error fails the login.
func postToken(ctx context.Context, client *http.Client, endpoint string, body any) (oidcToken, bool, error) {
	raw, status, err := postJSON(ctx, client, endpoint, body)
	if err != nil {
		return oidcToken{}, false, err
	}
	if status == http.StatusBadRequest {
		var parsed struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &parsed)
		switch parsed.Error {
		case "authorization_pending", "slow_down":
			return oidcToken{}, true, nil
		case "":
			return oidcToken{}, false, fmt.Errorf("kiro token request was rejected")
		default:
			return oidcToken{}, false, fmt.Errorf("kiro authorization: %s", parsed.Error)
		}
	}
	if status != http.StatusOK {
		return oidcToken{}, false, newProviderError(status, string(raw))
	}
	var parsed struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int    `json:"expiresIn"`
		ProfileArn   string `json:"profileArn"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return oidcToken{}, false, fmt.Errorf("kiro token response: %w", err)
	}
	if parsed.AccessToken == "" || parsed.ExpiresIn <= 0 {
		return oidcToken{}, false, fmt.Errorf("kiro token response is incomplete")
	}
	return oidcToken{
		AccessToken: parsed.AccessToken, RefreshToken: parsed.RefreshToken,
		ExpiresAt: time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second), ProfileArn: parsed.ProfileArn,
	}, false, nil
}

func postOIDC(ctx context.Context, client *http.Client, endpoint string, body, dest any) error {
	raw, status, err := postJSON(ctx, client, endpoint, body)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return newProviderError(status, string(raw))
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("kiro oidc response: %w", err)
	}
	return nil
}

func postJSON(ctx context.Context, client *http.Client, endpoint string, body any) ([]byte, int, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return raw, resp.StatusCode, nil
}

func pkcePair() (verifier, challenge string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}
