package kimi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// profileInfo is the display-only account profile from GET /me.
type profileInfo struct {
	UserID   string `json:"user_id"`
	UserName string `json:"user_name"`
	Plan     string `json:"plan"`
	Status   string `json:"status"`
	Region   string `json:"region"`
}

// fetchProfile reads GET /me. Kimi's own clients call this with only the bearer
// token and Accept, so the device identity headers are deliberately omitted.
func (c *Client) fetchProfile(ctx context.Context, accountID string) (profileInfo, error) {
	credential, token, err := c.accessToken(ctx, accountID)
	if err != nil {
		return profileInfo{}, err
	}
	base := c.apiBase(credential)
	requestCtx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, base+mePath, nil)
	if err != nil {
		return profileInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return profileInfo{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return profileInfo{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return profileInfo{}, err
	}
	if resp.StatusCode >= 300 {
		return profileInfo{}, newProviderError(resp.StatusCode, string(body))
	}
	return ParseProfileJSON(body)
}

// ParseProfileJSON maps the /me payload. Only user_id is load-bearing; every
// other field degrades independently.
func ParseProfileJSON(raw []byte) (profileInfo, error) {
	var payload struct {
		UserID        string `json:"user_id"`
		Nickname      string `json:"nickname"`
		Username      string `json:"username"`
		UserLevelName string `json:"user_level_name"`
		Status        string `json:"status"`
		Region        string `json:"region"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return profileInfo{}, fmt.Errorf("kimi profile: %w", err)
	}
	profile := profileInfo{
		UserID:   strings.TrimSpace(payload.UserID),
		UserName: firstNonEmpty(payload.Nickname, payload.Username),
		Plan:     strings.TrimSpace(payload.UserLevelName),
		Status:   strings.TrimSpace(payload.Status),
		Region:   strings.TrimSpace(payload.Region),
	}
	return profile, nil
}
