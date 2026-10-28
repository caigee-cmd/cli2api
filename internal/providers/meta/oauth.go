package meta

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type deviceAuthorization struct {
	UserCode                string
	DeviceCode              string
	VerificationURI         string
	VerificationURIComplete string
	ExpiresIn               time.Duration
	Interval                time.Duration
}

type tokenPayload struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int64  `json:"expires_in"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type mintedKeyResponse struct {
	APIKey           string `json:"api_key"`
	BaseURL          string `json:"base_url"`
	UserEmail        string `json:"user_email"`
	UserFullName     string `json:"user_full_name"`
	SubsTierName     string `json:"subs_tier_name"`
	SubsTierID       string `json:"subs_tier_id"`
	IsSubsActive     bool   `json:"is_subs_active"`
	HasPaymentMethod bool   `json:"has_payment_method"`
	RequirePayment   bool   `json:"require_payment"`
	CanSubscribe     bool   `json:"can_subscribe"`
}

type devicePollKind string

const (
	devicePollPending  devicePollKind = "pending"
	devicePollSlowDown devicePollKind = "slow_down"
	devicePollSuccess  devicePollKind = "success"
	devicePollExpired  devicePollKind = "expired"
	devicePollDenied   devicePollKind = "denied"
)

type devicePollResult struct {
	Kind    devicePollKind
	Token   tokenPayload
	Message string
}

func (c *Client) requestDeviceCode(ctx context.Context, accountID string) (deviceAuthorization, error) {
	form := url.Values{}
	form.Set("client_id", ClientID)
	status, body, err := c.postAuthForm(ctx, accountID, c.authBase()+DeviceAuthorizationPath, form)
	if err != nil {
		return deviceAuthorization{}, err
	}
	if status < 200 || status >= 300 {
		return deviceAuthorization{}, fmt.Errorf("meta device authorization failed (HTTP %d): %s", status, truncate(body))
	}
	var payload struct {
		UserCode                string `json:"user_code"`
		DeviceCode              string `json:"device_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int64  `json:"expires_in"`
		Interval                int64  `json:"interval"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return deviceAuthorization{}, fmt.Errorf("meta device authorization response is not valid json")
	}
	if payload.UserCode == "" || payload.DeviceCode == "" {
		return deviceAuthorization{}, fmt.Errorf("meta device authorization response is incomplete")
	}
	verify := payload.VerificationURIComplete
	if verify == "" && payload.VerificationURI != "" && payload.UserCode != "" {
		verify = payload.VerificationURI
	}
	if verify == "" {
		return deviceAuthorization{}, fmt.Errorf("meta device authorization missing verification URI")
	}
	interval := time.Duration(payload.Interval) * time.Second
	if interval < pollFloor {
		interval = pollFloor
	}
	expiresIn := time.Duration(payload.ExpiresIn) * time.Second
	if expiresIn <= 0 || expiresIn > deviceLoginTimeout {
		expiresIn = deviceLoginTimeout
	}
	return deviceAuthorization{
		UserCode:                payload.UserCode,
		DeviceCode:              payload.DeviceCode,
		VerificationURI:         payload.VerificationURI,
		VerificationURIComplete: verify,
		ExpiresIn:               expiresIn,
		Interval:                interval,
	}, nil
}

func (c *Client) pollDeviceToken(ctx context.Context, accountID, deviceCode string) (devicePollResult, error) {
	form := url.Values{}
	form.Set("client_id", ClientID)
	form.Set("device_code", deviceCode)
	form.Set("grant_type", DeviceGrantType)
	status, body, err := c.postAuthForm(ctx, accountID, c.authBase()+TokenPath, form)
	if err != nil {
		return devicePollResult{}, err
	}
	var payload tokenPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		if status >= 500 {
			return devicePollResult{}, fmt.Errorf("meta token polling failed (HTTP %d)", status)
		}
		return devicePollResult{}, fmt.Errorf("meta token polling response is not valid json")
	}
	if payload.AccessToken != "" {
		return devicePollResult{Kind: devicePollSuccess, Token: payload}, nil
	}
	if status >= 500 {
		return devicePollResult{}, fmt.Errorf("meta token polling server error (HTTP %d): %s", status, truncate(body))
	}
	switch payload.Error {
	case "authorization_pending":
		return devicePollResult{Kind: devicePollPending, Message: "waiting for authorization"}, nil
	case "slow_down":
		return devicePollResult{Kind: devicePollSlowDown, Message: "waiting for authorization (slowed down)"}, nil
	case "expired_token":
		return devicePollResult{Kind: devicePollExpired, Message: "the device code expired"}, nil
	case "access_denied":
		return devicePollResult{Kind: devicePollDenied, Message: "authorization was denied"}, nil
	}
	msg := firstNonEmpty(payload.ErrorDescription, payload.Error, truncate(body))
	return devicePollResult{}, fmt.Errorf("meta token polling failed (HTTP %d): %s", status, msg)
}

// mintAPIKey exchanges a DCA token for an LLM API key (CPA MintAPIKey).
func (c *Client) mintAPIKey(ctx context.Context, accountID, dcaToken string) (mintedKeyResponse, error) {
	dcaToken = strings.TrimSpace(dcaToken)
	if dcaToken == "" {
		return mintedKeyResponse{}, fmt.Errorf("meta auth: missing dca token")
	}
	reqBody, _ := json.Marshal(map[string]string{"dca_token": dcaToken})
	mintURL := c.mintURL
	if mintURL == "" {
		mintURL = MintURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mintURL, bytes.NewReader(reqBody))
	if err != nil {
		return mintedKeyResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+dcaToken)
	req.Header.Set("User-Agent", AuthUserAgent)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return mintedKeyResponse{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return mintedKeyResponse{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return mintedKeyResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return mintedKeyResponse{}, fmt.Errorf("meta mint failed (HTTP %d): %s", resp.StatusCode, truncate(body))
	}
	var minted mintedKeyResponse
	if err := json.Unmarshal(body, &minted); err != nil {
		return mintedKeyResponse{}, fmt.Errorf("meta mint response is not valid json")
	}
	if strings.TrimSpace(minted.APIKey) == "" {
		return mintedKeyResponse{}, fmt.Errorf("meta mint response missing api_key")
	}
	return minted, nil
}

func (c *Client) postAuthForm(ctx context.Context, accountID, endpoint string, form url.Values) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", AuthUserAgent)
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return 0, nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

func truncate(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 512 {
		return s[:512] + "…"
	}
	return s
}

func applyLoginBundle(token tokenPayload, minted mintedKeyResponse, now time.Time) Credential {
	dca := strings.TrimSpace(token.AccessToken)
	cred := Credential{
		Format:      CredentialFormatOAuth,
		DCAToken:    dca,
		AccessToken: dca,
		TokenType:   firstNonEmpty(token.TokenType, "Bearer"),
		ExpiresIn:   token.ExpiresIn,
		APIKey:      strings.TrimSpace(minted.APIKey),
		BaseURL:     normalizeBase(minted.BaseURL),
		Email:       strings.TrimSpace(minted.UserEmail),
		Name:        strings.TrimSpace(minted.UserFullName),
		SubsTierName: strings.TrimSpace(minted.SubsTierName),
		SubsTierID:   strings.TrimSpace(minted.SubsTierID),
		IsSubsActive: minted.IsSubsActive,
		LastRefresh:  formatRFC3339(now),
	}
	if token.ExpiresIn > 0 {
		exp := now.Add(time.Duration(token.ExpiresIn) * time.Second)
		cred.DCAExpiresAt = exp.Unix()
		cred.DCAExpired = formatRFC3339(exp)
		cred.Expired = cred.DCAExpired
	}
	return cred
}

func (c *Client) authBase() string {
	if c != nil && strings.TrimSpace(c.authHost) != "" {
		return strings.TrimRight(strings.TrimSpace(c.authHost), "/")
	}
	return AuthHost
}
