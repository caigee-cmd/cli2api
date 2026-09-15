package devin

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

// PKCECodes holds a PKCE verifier/challenge pair.
type PKCECodes struct {
	CodeVerifier  string
	CodeChallenge string
}

func GeneratePKCE() (PKCECodes, error) {
	raw := make([]byte, 64)
	if _, err := rand.Read(raw); err != nil {
		return PKCECodes{}, fmt.Errorf("generate pkce: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(verifier))
	return PKCECodes{
		CodeVerifier:  verifier,
		CodeChallenge: base64.RawURLEncoding.EncodeToString(hash[:]),
	}, nil
}

// BuildAuthorizationURL constructs the PKCE login URL with exact query order
// matching the official Devin CLI binary.
func BuildAuthorizationURL(appBase, redirectURI, codeChallenge, state string) string {
	base := strings.TrimRight(firstNonEmpty(appBase, AppBase), "/")
	trimmedRedirect := strings.TrimSpace(redirectURI)
	var queryParts []string
	if trimmedRedirect != "" {
		queryParts = append(queryParts, "redirect_uri="+url.QueryEscape(trimmedRedirect))
	}
	if state != "" {
		queryParts = append(queryParts, "state="+url.QueryEscape(state))
	}
	queryParts = append(queryParts,
		"prompt=select_account",
		"code_challenge="+url.QueryEscape(codeChallenge),
		"code_challenge_method=S256",
	)
	if trimmedRedirect == "" {
		queryParts = append(queryParts, "cli_pkce_marker=1")
	}
	return fmt.Sprintf("%s%s?%s", base, PathAuthContinue, strings.Join(queryParts, "&"))
}

func ExchangeCode(ctx context.Context, client *http.Client, apiBase, code, codeVerifier string) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	body, err := json.Marshal(map[string]string{
		"code":          strings.TrimSpace(code),
		"code_verifier": strings.TrimSpace(codeVerifier),
	})
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimRight(firstNonEmpty(apiBase, APIBase), "/") + PathAuthToken
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("devin token exchange failed: %w", err)
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read token exchange response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(respBytes))
	}
	var parsed struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return "", fmt.Errorf("decode token exchange response: %w", err)
	}
	token := strings.TrimSpace(parsed.Token)
	if token == "" {
		return "", fmt.Errorf("response did not contain a valid token")
	}
	return token, nil
}

func FetchSelfProfile(ctx context.Context, client *http.Client, apiBase, sessionToken string) (userName, userID, orgID string, err error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	endpoint := strings.TrimRight(firstNonEmpty(apiBase, APIBase), "/") + PathSelf
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", "", nil
	}
	var parsed struct {
		UserName string `json:"user_name"`
		UserID   string `json:"user_id"`
		OrgID    string `json:"org_id"`
	}
	_ = json.Unmarshal(respBytes, &parsed)
	return parsed.UserName, parsed.UserID, parsed.OrgID, nil
}

// ParseCallbackOrPaste accepts a full callback URL, bare code, or pasted session token.
func ParseCallbackOrPaste(raw string) (code, state, sessionToken string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", "", fmt.Errorf("empty callback input")
	}
	if strings.HasPrefix(raw, TokenPrefix) || strings.HasPrefix(raw, "eyJ") {
		return "", "", FormatSessionToken(raw), nil
	}
	if strings.Contains(raw, "://") || strings.Contains(raw, "?") {
		u, parseErr := url.Parse(raw)
		if parseErr != nil {
			return "", "", "", fmt.Errorf("parse callback url: %w", parseErr)
		}
		q := u.Query()
		if token := firstNonEmpty(q.Get("session_token"), q.Get("token")); token != "" {
			return "", q.Get("state"), FormatSessionToken(token), nil
		}
		code = firstNonEmpty(q.Get("code"), q.Get("authorization_code"))
		state = q.Get("state")
		if code == "" {
			if errMsg := firstNonEmpty(q.Get("error_description"), q.Get("error")); errMsg != "" {
				return "", state, "", fmt.Errorf("oauth error: %s", errMsg)
			}
			return "", state, "", fmt.Errorf("callback missing code")
		}
		return code, state, "", nil
	}
	// Bare authorization code.
	return raw, "", "", nil
}
