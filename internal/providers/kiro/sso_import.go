package kiro

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// importSSOToken turns a portal SSO bearer into a Builder ID credential. The
// bearer is verified, exchanged for a device session, and that session approves
// a device-code grant this process started, so the result refreshes like any
// other Builder ID login. The bearer itself is not stored.
func importSSOToken(ctx context.Context, client *http.Client, region, bearer string) (Credential, error) {
	bearer = strings.TrimSpace(bearer)
	if bearer == "" {
		return Credential{}, fmt.Errorf("kiro sso import requires a bearer token")
	}
	if err := verifySSOBearer(ctx, client, bearer); err != nil {
		return Credential{}, fmt.Errorf("kiro sso token was rejected: %w", err)
	}
	registered, err := registerOIDCClient(ctx, client, region, builderIDStartURL, "", []string{
		"urn:ietf:params:oauth:grant-type:device_code", "refresh_token",
	})
	if err != nil {
		return Credential{}, err
	}
	device, err := startDeviceAuthorization(ctx, client, region, builderIDStartURL, registered)
	if err != nil {
		return Credential{}, err
	}
	session, err := ssoDeviceSession(ctx, client, bearer)
	if err != nil {
		return Credential{}, err
	}
	deviceContext, err := acceptSSOUserCode(ctx, client, region, device.UserCode, session)
	if err != nil {
		return Credential{}, err
	}
	if deviceContext != nil {
		if err := associateSSOToken(ctx, client, region, session, *deviceContext); err != nil {
			return Credential{}, err
		}
	}
	token, pending, err := pollDeviceToken(ctx, client, region, registered, device.DeviceCode)
	if err != nil {
		return Credential{}, err
	}
	if pending {
		return Credential{}, fmt.Errorf("kiro sso import was not approved")
	}
	cred := Credential{
		AuthMethod: "builder_id", Region: normalizeRegion(region), Provider: "BuilderId",
		AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, ExpiresAt: token.ExpiresAt.Unix(),
		ClientID: registered.ID, ClientSecret: registered.Secret, StartURL: builderIDStartURL,
	}
	if token.ProfileArn != "" {
		cred.ProfileArn = token.ProfileArn
	}
	return cred, nil
}

func verifySSOBearer(ctx context.Context, client *http.Client, bearer string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ssoPortalBase+"/token/whoAmI", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func ssoDeviceSession(ctx context.Context, client *http.Client, bearer string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ssoPortalBase+"/session/device", bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", newProviderError(resp.StatusCode, string(raw))
	}
	var parsed struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || parsed.Token == "" {
		return "", fmt.Errorf("kiro sso device session returned no token")
	}
	return parsed.Token, nil
}

type ssoDeviceContext struct {
	ID         string `json:"deviceContextId"`
	ClientID   string `json:"clientId"`
	ClientType string `json:"clientType"`
}

func acceptSSOUserCode(ctx context.Context, client *http.Client, region, userCode, session string) (*ssoDeviceContext, error) {
	body := map[string]string{"userCode": userCode, "userSessionId": session}
	var parsed struct {
		DeviceContext *ssoDeviceContext `json:"deviceContext"`
	}
	if err := postOIDCReferer(ctx, client, oidcBase(region)+"/device_authorization/accept_user_code", body, &parsed); err != nil {
		return nil, err
	}
	return parsed.DeviceContext, nil
}

func associateSSOToken(ctx context.Context, client *http.Client, region, session string, device ssoDeviceContext) error {
	body := map[string]any{
		"deviceContext": map[string]string{
			"deviceContextId": device.ID, "clientId": device.ClientID, "clientType": device.ClientType,
		},
		"userSessionId": session,
	}
	return postOIDCReferer(ctx, client, oidcBase(region)+"/device_authorization/associate_token", body, &struct{}{})
}

// postOIDCReferer is postOIDC with the referer the portal's device pages send.
func postOIDCReferer(ctx context.Context, client *http.Client, endpoint string, body, dest any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", "https://view.awsapps.com/")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return newProviderError(resp.StatusCode, string(raw))
	}
	if dest == nil {
		return nil
	}
	return json.Unmarshal(raw, dest)
}
