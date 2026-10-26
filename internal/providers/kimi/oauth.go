package kimi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

// deviceAuthorization is one RFC 8628 device-code round.
type deviceAuthorization struct {
	UserCode                string
	DeviceCode              string
	VerificationURI         string
	VerificationURIComplete string
	ExpiresIn               time.Duration
	Interval                time.Duration
}

// tokenPayload is the OAuth token endpoint response. Kimi returns the RFC 8628
// error members in the same body, sometimes with HTTP 200, so every field is
// optional and callers branch on presence rather than status alone.
type tokenPayload struct {
	AccessToken      string          `json:"access_token"`
	RefreshToken     string          `json:"refresh_token"`
	ExpiresIn        json.RawMessage `json:"expires_in"`
	Scope            string          `json:"scope"`
	TokenType        string          `json:"token_type"`
	Error            string          `json:"error"`
	ErrorDescription string          `json:"error_description"`
	Message          string          `json:"message"`
	Detail           string          `json:"detail"`
}

func (p tokenPayload) expiresIn() int64 {
	raw := strings.TrimSpace(string(p.ExpiresIn))
	if raw == "" || raw == "null" {
		return 0
	}
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return seconds
	}
	var text string
	if json.Unmarshal(p.ExpiresIn, &text) == nil {
		seconds, _ := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		return seconds
	}
	return 0
}

func (p tokenPayload) reason() string {
	return firstNonEmpty(p.ErrorDescription, p.Message, p.Detail, p.Error)
}

// requestDeviceCode starts a device authorization round.
func (c *Client) requestDeviceCode(ctx context.Context, authHost, deviceID string) (deviceAuthorization, error) {
	form := url.Values{}
	form.Set("client_id", ClientID)
	status, body, err := c.postForm(ctx, authHost, DeviceAuthorizationPath, form, deviceID)
	if err != nil {
		return deviceAuthorization{}, err
	}
	if status != http.StatusOK {
		return deviceAuthorization{}, fmt.Errorf("kimi device authorization failed (HTTP %d): %s", status, apiMessage(body, "unknown error"))
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
		return deviceAuthorization{}, fmt.Errorf("kimi device authorization response is not valid json")
	}
	if payload.UserCode == "" || payload.DeviceCode == "" || payload.VerificationURIComplete == "" {
		return deviceAuthorization{}, fmt.Errorf("kimi device authorization response is incomplete")
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
		VerificationURIComplete: payload.VerificationURIComplete,
		ExpiresIn:               expiresIn,
		Interval:                interval,
	}, nil
}

// devicePollKind classifies one device-token poll.
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

// pollDeviceToken performs exactly one device-token poll. The caller owns the
// pacing loop, so a console poll and a background login share one code path.
func (c *Client) pollDeviceToken(ctx context.Context, authHost, deviceCode, deviceID string) (devicePollResult, error) {
	form := url.Values{}
	form.Set("client_id", ClientID)
	form.Set("device_code", deviceCode)
	form.Set("grant_type", DeviceGrantType)
	status, body, err := c.postForm(ctx, authHost, TokenPath, form, deviceID)
	if err != nil {
		return devicePollResult{}, err
	}
	var payload tokenPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		if status >= 500 {
			return devicePollResult{}, fmt.Errorf("kimi token polling failed (HTTP %d)", status)
		}
		return devicePollResult{}, fmt.Errorf("kimi token polling response is not valid json")
	}
	if payload.AccessToken != "" {
		return devicePollResult{Kind: devicePollSuccess, Token: payload}, nil
	}
	if status >= 500 {
		return devicePollResult{}, fmt.Errorf("kimi token polling server error (HTTP %d): %s", status, apiMessage(body, "unknown error"))
	}
	switch payload.Error {
	case "authorization_pending":
		return devicePollResult{Kind: devicePollPending, Message: "waiting for authorization"}, nil
	case "slow_down":
		// RFC 8628 §3.5: the client must slow its polling down. The caller owns
		// the interval, so report the distinct kind instead of folding it into
		// pending.
		return devicePollResult{Kind: devicePollSlowDown, Message: "waiting for authorization (slowed down)"}, nil
	case "expired_token":
		return devicePollResult{Kind: devicePollExpired, Message: "the device code expired"}, nil
	case "access_denied":
		return devicePollResult{Kind: devicePollDenied, Message: "authorization was denied"}, nil
	}
	return devicePollResult{}, fmt.Errorf("kimi token polling failed (HTTP %d): %s", status, apiMessage(body, "unknown error"))
}

// refreshCredential exchanges the refresh token for a fresh access token. Kimi
// rotates the refresh token, so the caller must persist the returned credential.
func (c *Client) refreshCredential(ctx context.Context, cred Credential) (Credential, error) {
	if strings.TrimSpace(cred.RefreshToken) == "" {
		return Credential{}, &providers.Error{
			Kind:    accounts.KindAuth,
			Status:  http.StatusUnauthorized,
			Message: "kimi refresh token is missing; sign in again",
		}
	}
	form := url.Values{}
	form.Set("client_id", ClientID)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", cred.RefreshToken)

	attempts := 3
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			wait := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return Credential{}, ctx.Err()
			case <-time.After(wait):
			}
		}
		status, body, err := c.postForm(ctx, cred.AuthHost(), TokenPath, form, cred.DeviceIDOr())
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return Credential{}, ctx.Err()
			}
			continue
		}
		var payload tokenPayload
		_ = json.Unmarshal(body, &payload)
		if status == http.StatusOK && payload.AccessToken != "" {
			return applyToken(cred, payload, c.now()), nil
		}
		reason := apiMessage(body, "token refresh failed")
		if status == http.StatusUnauthorized || status == http.StatusForbidden || payload.Error == "invalid_grant" {
			return Credential{}, &providers.Error{
				Kind:    accounts.KindAuth,
				Status:  firstStatus(status, http.StatusUnauthorized),
				Message: "kimi refresh token rejected; sign in again: " + reason,
			}
		}
		if retryableStatus(status) {
			lastErr = fmt.Errorf("kimi token refresh failed (HTTP %d): %s", status, reason)
			continue
		}
		return Credential{}, &providers.Error{
			Kind:    accounts.KindUnavailable,
			Status:  firstStatus(status, http.StatusBadGateway),
			Message: fmt.Sprintf("kimi token refresh failed (HTTP %d): %s", status, reason),
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("kimi token refresh failed")
	}
	return Credential{}, &providers.Error{
		Kind:    accounts.KindUnavailable,
		Status:  http.StatusBadGateway,
		Message: lastErr.Error(),
	}
}

// applyToken folds a token response into a credential without persisting it.
func applyToken(cred Credential, payload tokenPayload, now time.Time) Credential {
	cred.AccessToken = strings.TrimSpace(payload.AccessToken)
	if refresh := strings.TrimSpace(payload.RefreshToken); refresh != "" {
		cred.RefreshToken = refresh
	}
	if seconds := payload.expiresIn(); seconds > 0 {
		cred.ExpiresIn = seconds
		cred.ExpiresAt = now.Unix() + seconds
	}
	if scope := strings.TrimSpace(payload.Scope); scope != "" {
		cred.Scope = scope
	}
	if tokenType := strings.TrimSpace(payload.TokenType); tokenType != "" {
		cred.TokenType = tokenType
	}
	if cred.DeviceID == "" {
		cred.DeviceID = newDeviceID()
	}
	return cred
}

// postForm posts an application/x-www-form-urlencoded OAuth request.
func (c *Client) postForm(ctx context.Context, authHost, path string, form url.Values, deviceID string) (int, []byte, error) {
	target := strings.TrimRight(firstNonEmpty(authHost, CNOAuthHost), "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	applyIdentityHeaders(req, deviceID)
	client, err := c.httpClient(ctx, "")
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

// apiMessage extracts the most specific server message from an error body.
func apiMessage(body []byte, fallback string) string {
	var payload tokenPayload
	if err := json.Unmarshal(body, &payload); err == nil {
		if reason := payload.reason(); reason != "" {
			return trimTo(reason, 400)
		}
	}
	var generic map[string]any
	if err := json.Unmarshal(body, &generic); err == nil {
		for _, key := range []string{"error_description", "message", "detail", "error"} {
			switch value := generic[key].(type) {
			case string:
				if strings.TrimSpace(value) != "" {
					return trimTo(value, 400)
				}
			case map[string]any:
				if nested, ok := value["message"].(string); ok && strings.TrimSpace(nested) != "" {
					return trimTo(nested, 400)
				}
			}
		}
	}
	if text := strings.TrimSpace(string(body)); text != "" && len(text) <= 400 {
		return text
	}
	return fallback
}

func retryableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

func firstStatus(status, fallback int) int {
	if status > 0 {
		return status
	}
	return fallback
}

func trimTo(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}
