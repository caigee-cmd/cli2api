package minimaxcode

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type pkcePair struct {
	Verifier  string
	Challenge string
}

func newPKCE() (pkcePair, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return pkcePair{}, err
	}
	verifier := base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	return pkcePair{Verifier: verifier, Challenge: base64.RawURLEncoding.EncodeToString(sum[:])}, nil
}

type deviceAuth struct {
	DeviceCode string
	UserCode   string
	VerifyURI  string
	ExpiresIn  time.Duration
	Interval   time.Duration
	PollByUser bool
}

type tokenResult struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64
	TokenType    string
	Scope        string
}

type pollKind int

const (
	pollReady pollKind = iota
	pollPending
	pollSlowDown
	pollDenied
	pollExpired
	pollFailed
)

type pollResult struct {
	Kind  pollKind
	Token tokenResult
	Error string
}

func applyToken(credential Credential, token tokenResult, now time.Time) Credential {
	credential.AccessToken = token.AccessToken
	if token.RefreshToken != "" {
		credential.RefreshToken = token.RefreshToken
	}
	if token.TokenType != "" {
		credential.TokenType = token.TokenType
	}
	if token.Scope != "" {
		credential.Scope = token.Scope
	}
	expiresIn := token.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	credential.ExpiresAt = now.Add(time.Duration(expiresIn) * time.Second).Unix()
	return credential
}

func (c *Client) requestDeviceCode(ctx context.Context, accountBase string, pair pkcePair) (deviceAuth, error) {
	form := url.Values{
		"client_id":             {ClientID},
		"scope":                 {Scope},
		"audience":              {Audience},
		"code_challenge":        {pair.Challenge},
		"code_challenge_method": {"S256"},
	}
	body, status, err := c.postForm(ctx, accountBase+deviceCodePath, form)
	if err != nil {
		return deviceAuth{}, err
	}
	var payload map[string]any
	_ = json.Unmarshal(body, &payload)
	userCode := jsonString(payload, "user_code")
	uri := first(jsonString(payload, "verification_uri_complete"), jsonString(payload, "verification_uri"), jsonString(payload, "verification_url"))
	deviceCode := jsonString(payload, "device_code")
	if status >= 400 || userCode == "" || uri == "" {
		return deviceAuth{}, fmt.Errorf("minimaxcode device code request failed (HTTP %d): %s", status, truncate(string(body), 300))
	}
	expires := jsonDeadline(payload)
	interval := jsonInterval(payload, deviceCode == "")
	if interval <= 0 {
		interval = 2 * time.Second
	}
	return deviceAuth{
		DeviceCode: first(deviceCode, userCode),
		UserCode:   userCode,
		VerifyURI:  uri,
		ExpiresIn:  expires,
		Interval:   interval,
		PollByUser: deviceCode == "",
	}, nil
}

func (c *Client) pollDeviceToken(ctx context.Context, accountBase, deviceCode, verifier string, pollByUser bool) (pollResult, error) {
	form := url.Values{
		"grant_type":    {DeviceGrantType},
		"client_id":     {ClientID},
		"code_verifier": {verifier},
	}
	if pollByUser {
		form.Set("user_code", deviceCode)
	} else {
		form.Set("device_code", deviceCode)
	}
	body, status, err := c.postForm(ctx, accountBase+tokenPath, form)
	if err != nil {
		return pollResult{}, err
	}
	var payload map[string]any
	_ = json.Unmarshal(body, &payload)
	if st := jsonString(payload, "status"); st != "" {
		switch st {
		case "pending":
			return pollResult{Kind: pollPending}, nil
		case "slow_down":
			return pollResult{Kind: pollSlowDown}, nil
		case "denied", "access_denied":
			return pollResult{Kind: pollDenied}, nil
		case "expired", "expired_token":
			return pollResult{Kind: pollExpired}, nil
		default:
			return pollResult{Kind: pollFailed, Error: "unexpected_status:" + st}, nil
		}
	}
	if code := jsonString(payload, "error"); code != "" {
		return pollResult{Kind: pollFailed, Error: code + ": " + jsonString(payload, "error_description")}, nil
	}
	access := jsonString(payload, "access_token")
	if access == "" {
		if status >= 400 {
			return pollResult{Kind: pollFailed, Error: fmt.Sprintf("no access token (HTTP %d)", status)}, nil
		}
		return pollResult{Kind: pollPending}, nil
	}
	return pollResult{Kind: pollReady, Token: tokenFromJSON(payload)}, nil
}

func (c *Client) refreshToken(ctx context.Context, credential Credential) (tokenResult, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {credential.RefreshToken},
		"client_id":     {ClientID},
		"scope":         {Scope},
		"audience":      {Audience},
	}
	body, status, err := c.postForm(ctx, credential.accountBase()+tokenPath, form)
	if err != nil {
		return tokenResult{}, err
	}
	var payload map[string]any
	_ = json.Unmarshal(body, &payload)
	if jsonString(payload, "access_token") == "" {
		return tokenResult{}, fmt.Errorf("minimaxcode token refresh failed (HTTP %d)", status)
	}
	return tokenFromJSON(payload), nil
}

func (c *Client) postForm(ctx context.Context, endpoint string, form url.Values) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	client, err := c.httpClient(ctx, "")
	if err != nil {
		return nil, 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return body, resp.StatusCode, nil
}

func tokenFromJSON(payload map[string]any) tokenResult {
	return tokenResult{
		AccessToken:  jsonString(payload, "access_token"),
		RefreshToken: jsonString(payload, "refresh_token"),
		ExpiresIn:    jsonInt(payload, "expires_in", "expired_in"),
		TokenType:    jsonString(payload, "token_type"),
		Scope:        jsonString(payload, "scope"),
	}
}

func jsonString(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

func jsonInt(payload map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch value := payload[key].(type) {
		case float64:
			return int64(value)
		case json.Number:
			n, _ := strconv.ParseInt(value.String(), 10, 64)
			return n
		case string:
			n, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if n != 0 {
				return n
			}
		}
	}
	return 0
}

func jsonDeadline(payload map[string]any) time.Duration {
	seconds := jsonInt(payload, "expires_in", "expired_in")
	if seconds <= 0 {
		return 10 * time.Minute
	}
	if seconds > 10_000_000 {
		return time.Duration(maxInt64(1, seconds-time.Now().UnixMilli()/1000)) * time.Second
	}
	return time.Duration(seconds) * time.Second
}

func jsonInterval(payload map[string]any, pollByUser bool) time.Duration {
	raw := jsonInt(payload, "interval")
	if raw <= 0 {
		if pollByUser {
			return 5 * time.Second
		}
		return 2 * time.Second
	}
	if pollByUser && raw > 100 {
		return time.Duration(maxInt64(1, raw/1000)) * time.Second
	}
	return time.Duration(raw) * time.Second
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func truncate(text string, limit int) string {
	if len(text) > limit {
		return text[:limit]
	}
	return text
}
