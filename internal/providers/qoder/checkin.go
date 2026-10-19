package qoder

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/providers/qoder/qauth"
)

// checkin performs the daily credit-claim against the Qoder OpenAPI
// campaigns endpoint, fully in-process. It replaces the worker's
// /admin/checkin HTTP path: the Bridge supplies the bearer (securityOauthToken
// on CN, OpenAPI token on Global; rotated via EnsureFreshSession before the
// call and force-refreshed on a 401).
//
// The device fingerprint is derived from the PAT (deriveMachineIdentity /
// deriveMachineCode): stable per account, and no two accounts on the same
// host share an identity.
//
// Wire shape mirrors worker/src/checkin.mjs exactly:
//   - GET  {openapi}/sash/api/v1/me/campaigns  → list of campaigns
//   - POST {openapi}/sash/api/v1/me/campaigns/{id}/claim  → claim one
//
// A campaign is claimable when actionType == "CLAIM_BENEFIT" and
// claimStatus == "CLAIMABLE"; an already-CLAIMED campaign reports
// "今日已签到".
func (c *Client) Checkin(ctx context.Context, accountID string) (providers.CheckinResult, error) {
	b, err := c.bridge(ctx, accountID)
	if err != nil {
		// Legacy accounts without a PAT fall back to the worker check-in path.
		if errors.Is(err, ErrPATRequired) {
			return c.workerCheckin(ctx, accountID)
		}
		return providers.CheckinResult{}, err
	}
	return b.Checkin(ctx)
}

// workerCheckin is the legacy fallback for accounts that still carry a
// qoder-native-v1 blob instead of a PAT. It POSTs the worker's /admin/checkin
// endpoint and decodes its response. It will be removed with the worker in
// Phase E.
func (c *Client) workerCheckin(ctx context.Context, accountID string) (providers.CheckinResult, error) {
	workerURL, err := c.lookup(accountID)
	if err != nil {
		return providers.CheckinResult{}, err
	}
	c.mu.RLock()
	httpClient := c.adminHTTP
	c.mu.RUnlock()
	status, _, body, err := c.accountWorker(httpClient, accountID).Admin(ctx, workerURL, http.MethodPost, "/admin/checkin", "application/json", []byte("{}"))
	if err != nil {
		return providers.CheckinResult{}, err
	}
	var payload struct {
		OK bool `json:"ok"`
		providers.CheckinResult
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return providers.CheckinResult{}, fmt.Errorf("invalid worker check-in response")
	}
	if status != http.StatusOK || !payload.OK {
		if payload.Error.Message != "" {
			return providers.CheckinResult{}, fmt.Errorf("%s", payload.Error.Message)
		}
		return providers.CheckinResult{}, fmt.Errorf("worker check-in failed (HTTP %d)", status)
	}
	if !payload.CheckinResult.Valid() {
		return providers.CheckinResult{}, fmt.Errorf("invalid worker check-in status")
	}
	return payload.CheckinResult, nil
}

// Checkin runs the daily credit-claim using the bridge's in-process session.
// It is a method on Bridge so the per-account session, machine identity, and
// token-rotation state are all reused across catalog/quota/checkin.
func (b *Bridge) Checkin(ctx context.Context) (providers.CheckinResult, error) {
	// Rotate the session if needed before the call: a stale token surfaces as
	// a 401 on the campaigns endpoint, and while the request path below
	// retries once with ForceRefresh, proactively refreshing avoids the round
	// trip.
	if err := b.EnsureFreshSession(ctx); err != nil {
		return providers.CheckinResult{}, fmt.Errorf("qoder check-in: session: %w", err)
	}

	endpoint := checkinEndpoint(b.Region)
	if endpoint == nil {
		return providers.CheckinResult{Status: "skipped", Message: "签到活动未开放"}, nil
	}

	// PAT-derived identity only: stable per account and unique across accounts
	// on the same host, so multi-account setups never share one fingerprint.
	machineID := b.MachineID()
	machineToken := b.MachineToken()
	machineType := b.MachineType()
	machineCode := deriveMachineCode(b.PAT)
	machineOS := machineOSString()
	machineHostname := machineHostnameString()

	// The campaign endpoint's bearer differs by region: CN accepts the COSY
	// securityOauthToken; Global rejects it (401) and needs a separately-issued
	// OpenAPI bearer. quotaBearer returns the right one and caches the global
	// token. Fall back to the COSY token if the exchange fails.
	bearer, err := b.quotaBearer(ctx)
	if err != nil || bearer == "" {
		bearer = b.CurrentSecurityOauth()
	}

	var doRequest func(path, method string, refreshed bool) (map[string]any, int, error)
	doRequest = func(path, method string, refreshed bool) (map[string]any, int, error) {
		token := bearer
		if token == "" {
			return nil, 0, errors.New("qoder check-in: token unavailable")
		}
		reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(reqCtx, method, endpoint.base+path, nil)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Qoder")
		req.Header.Set("Cosy-ClientType", "10")
		req.Header.Set("Cosy-Version", "0.3.4")
		req.Header.Set("Cosy-MachineId", machineID)
		req.Header.Set("Cosy-MachineToken", machineToken)
		req.Header.Set("Cosy-MachineType", machineType)
		req.Header.Set("Cosy-MachineCode", machineCode)
		if machineOS != "" {
			req.Header.Set("Cosy-MachineOS", machineOS)
		}
		if machineHostname != "" {
			req.Header.Set("Cosy-MachineHostname", machineHostname)
		}
		req.Header.Set("Origin", endpoint.origin)
		req.Header.Set("Referer", endpoint.base+"/growth-page/activity-iframe")
		// redirect: manual — the campaigns endpoint 302s to a login page when
		// the token is rejected, and we want to see the 302 rather than follow it.
		resp, err := checkinHTTPClient.Do(req)
		if err != nil {
			return nil, 0, fmt.Errorf("qoder check-in: request failed: %w", err)
		}
		if resp.StatusCode == http.StatusUnauthorized && !refreshed {
			io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
			resp.Body.Close()
			// A global-region bearer is the cached OpenAPI token; if it was
			// rejected, drop it so the next quotaBearer call re-exchanges rather
			// than handing back the same stale token. Then rotate the COSY
			// session and re-resolve the bearer before the single retry.
			if b.Region != nil && b.Region.NeedsOpenAPIToken {
				b.invalidateOpenAPIBearer()
			}
			if err := b.ForceRefresh(ctx); err != nil {
				return nil, 0, fmt.Errorf("qoder check-in: refresh failed: %w", err)
			}
			if fresh, berr := b.quotaBearer(ctx); berr == nil && fresh != "" {
				bearer = fresh
			} else {
				bearer = b.CurrentSecurityOauth()
			}
			return doRequest(path, method, true)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		if resp.StatusCode >= 300 {
			return nil, resp.StatusCode, &checkinHTTPError{Code: resp.StatusCode, Body: truncateOneLine(string(body))}
		}
		var parsed map[string]any
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, resp.StatusCode, fmt.Errorf("qoder check-in: invalid JSON: %w", err)
		}
		return parsed, resp.StatusCode, nil
	}

	// 1) List campaigns.
	listRaw, _, err := doRequest(checkinCampaignsPath, http.MethodGet, false)
	if err != nil {
		// 404 means the credit campaign is not open today; surface as skipped
		// rather than an error so the scheduler logs a clean "already/none".
		var statusErr interface{ StatusCode() int }
		if errors.As(err, &statusErr) && statusErr.StatusCode() == http.StatusNotFound {
			return providers.CheckinResult{Status: "skipped", Message: "签到活动未开放"}, nil
		}
		return providers.CheckinResult{}, err
	}
	items := campaignsFrom(listRaw)
	benefits := creditCampaigns(items)
	if len(benefits) == 0 {
		return providers.CheckinResult{Status: "skipped", Message: "签到活动未开放"}, nil
	}
	claimable := claimableCampaigns(benefits)
	if len(claimable) == 0 {
		for _, c := range benefits {
			if c.ClaimStatus == "CLAIMED" {
				return providers.CheckinResult{Status: "already", Message: "今日已签到"}, nil
			}
		}
		return providers.CheckinResult{Status: "skipped", Message: "签到活动未开放"}, nil
	}

	// 2) Claim each claimable campaign. A claim that returns a non-CLAIMED
	// status is retried by re-listing and checking the campaign's status: the
	// gateway sometimes confirms the claim asynchronously.
	confirmed := 0
	recovered := 0
	reward := 0.0
	hasReward := false
	for _, campaign := range claimable {
		path := fmt.Sprintf("%s/%s/claim", checkinCampaignsPath, url.PathEscape(campaign.CampaignID))
		claimRaw, _, err := doRequest(path, http.MethodPost, false)
		if err != nil {
			// Re-list to see if the claim actually succeeded despite the error.
			relist, _, relistErr := doRequest(checkinCampaignsPath, http.MethodGet, true)
			if relistErr != nil {
				return providers.CheckinResult{}, err
			}
			if status := claimStatusOf(campaignsFrom(relist), campaign.CampaignID); status == "CLAIMED" {
				recovered++
			} else {
				return providers.CheckinResult{}, err
			}
		} else {
			status := unwrapClaimStatus(claimRaw)
			if status != "CLAIMED" {
				// Fall back to re-listing; the claim may have been recorded server-side.
				relist, _, relistErr := doRequest(checkinCampaignsPath, http.MethodGet, true)
				if relistErr != nil {
					return providers.CheckinResult{}, fmt.Errorf("qoder check-in: claim unconfirmed")
				}
				if status := claimStatusOf(campaignsFrom(relist), campaign.CampaignID); status == "CLAIMED" {
					recovered++
				} else {
					return providers.CheckinResult{}, fmt.Errorf("qoder check-in: claim unconfirmed")
				}
			} else {
				confirmed++
			}
		}
		if amount, ok := rewardFrom(campaign); ok {
			hasReward = true
			reward += amount
		}
	}
	if confirmed == 0 && recovered == 0 {
		return providers.CheckinResult{}, fmt.Errorf("qoder check-in: claim unconfirmed")
	}
	if confirmed == 0 {
		return providers.CheckinResult{Status: "already", Message: "已签到（复查确认）"}, nil
	}
	out := providers.CheckinResult{Status: "success"}
	if hasReward {
		out.Message = fmt.Sprintf("签到成功 +%g 积分", reward)
		out.RewardCredits = reward
	} else {
		out.Message = "签到成功"
	}
	return out, nil
}

// checkinHTTPClient is dedicated to OpenAPI calls so a slow campaigns
// endpoint does not pin a chat-stream transport. It is unexported so tests
// can swap it via a package var if needed (mirrors qauth.SetStreamTimeouts).
var checkinHTTPClient = &http.Client{
	Timeout: 15 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		// redirect: manual — see doRequest.
		return http.ErrUseLastResponse
	},
}

const checkinCampaignsPath = "/sash/api/v1/me/campaigns"

type checkinRegion struct {
	base   string
	origin string
}

func checkinEndpoint(region *qauth.RegionConfig) *checkinRegion {
	if region == nil {
		return nil
	}
	if CheckinEndpointHook != nil {
		return CheckinEndpointHook(region)
	}
	switch region.Name {
	case "global":
		return &checkinRegion{base: "https://openapi.qoder.sh", origin: "https://qoder.sh"}
	default:
		return &checkinRegion{base: "https://openapi.qoder.com.cn", origin: "https://qoder.com.cn"}
	}
}

// CheckinEndpointHook overrides the OpenAPI base URL in tests. Production
// leaves it nil so the real region-configured endpoint is used.
var CheckinEndpointHook func(region *qauth.RegionConfig) *checkinRegion

// checkinHTTPError carries the upstream HTTP status so the caller can map a
// 404 (activity not open today) to a clean "skipped" result instead of an
// error. The body is already truncated + redacted by truncateOneLine.
type checkinHTTPError struct {
	Code int
	Body string
}

func (e *checkinHTTPError) Error() string {
	return fmt.Sprintf("qoder check-in: HTTP %d: %s", e.Code, e.Body)
}

// StatusCode lets errors.As(&someErr, &checkinHTTPError) feed the 404 path.
func (e *checkinHTTPError) StatusCode() int { return e.Code }

// campaign is a single entry in the /me/campaigns response.
type campaign struct {
	CampaignID  string `json:"campaignId"`
	ActionType  string `json:"actionType"`
	ClaimStatus string `json:"claimStatus"`
	Benefit     struct {
		Kind   string  `json:"kind"`
		Amount float64 `json:"amount"`
	} `json:"benefit"`
}

func campaignsFrom(raw map[string]any) []campaign {
	if raw == nil {
		return nil
	}
	var list []any
	if v, ok := raw["campaigns"].([]any); ok {
		list = v
	} else if data, ok := raw["data"].(map[string]any); ok {
		if c, ok := data["campaigns"].([]any); ok {
			list = c
		}
	} else if v, ok := raw["data"].([]any); ok {
		list = v
	}
	out := make([]campaign, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		encoded, _ := json.Marshal(m)
		var c campaign
		_ = json.Unmarshal(encoded, &c)
		out = append(out, c)
	}
	return out
}

func creditCampaigns(items []campaign) []campaign {
	out := make([]campaign, 0, len(items))
	for _, c := range items {
		if c.ActionType == "CLAIM_BENEFIT" && strings.TrimSpace(c.CampaignID) != "" {
			out = append(out, c)
		}
	}
	return out
}

func claimableCampaigns(items []campaign) []campaign {
	out := make([]campaign, 0, len(items))
	for _, c := range items {
		if c.ClaimStatus == "CLAIMABLE" {
			out = append(out, c)
		}
	}
	return out
}

func claimStatusOf(items []campaign, id string) string {
	for _, c := range items {
		if c.CampaignID == id {
			return c.ClaimStatus
		}
	}
	return ""
}

func unwrapClaimStatus(raw map[string]any) string {
	if raw == nil {
		return ""
	}
	if s, ok := raw["status"].(string); ok {
		return s
	}
	if data, ok := raw["data"].(map[string]any); ok {
		if s, ok := data["status"].(string); ok {
			return s
		}
	}
	return ""
}

func rewardFrom(c campaign) (float64, bool) {
	if c.Benefit.Kind != "CREDITS" {
		return 0, false
	}
	if c.Benefit.Amount < 0 {
		return 0, false
	}
	return c.Benefit.Amount, true
}

// truncateOneLine collapses a response body to a single short line for error
// messages; the campaigns endpoint returns JSON with embedded user ids that
// must not reach the log verbatim.
func truncateOneLine(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// deriveMachineCode is the fourth machine-identity header the OpenAPI
// campaigns endpoint expects (Cosy-MachineCode), alongside Id/Token/Type.
// We derive a stable value from the PAT, mirroring the other derived
// identities. A partial identity (missing any of the four) causes the
// campaigns endpoint to silently drop the claimable credit campaign, so this
// MUST be non-empty.
func deriveMachineCode(pat string) string {
	sum := md5.Sum([]byte("machinecode:" + pat))
	return hex.EncodeToString(sum[:])
}

// machineOSString mirrors checkin.mjs's machineOS(): "<arch>_<platform>" in
// the format the desktop client sends (e.g. "aarch64_darwin").
func machineOSString() string {
	arch := "x86_64"
	switch runtime.GOARCH {
	case "arm64":
		arch = "aarch64"
	case "amd64":
		arch = "x86_64"
	}
	return arch + "_" + runtime.GOOS
}

// machineHostnameString mirrors checkin.mjs's machineHostname(): the host
// name if printable ASCII (≤96 bytes), otherwise a sha256-prefixed sanitized
// form. Empty when neither form is producible.
func machineHostnameString() string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return ""
	}
	trimmed := strings.TrimSpace(name)
	if len([]byte(trimmed)) > 96 {
		return ""
	}
	if isPrintableASCII(trimmed) {
		return trimmed
	}
	// Fall back to a sanitized form for non-ASCII hostnames (CJK laptops).
	safe := strings.Map(func(r rune) rune {
		if r < 0x21 || r > 0x7e {
			return '-'
		}
		return r
	}, trimmed)
	safe = strings.ReplaceAll(safe, "--", "-")
	safe = strings.Trim(safe, "-")
	if len(safe) > 87 {
		safe = safe[:87]
	}
	if safe == "" {
		return ""
	}
	digest := shortHash(trimmed)
	out := safe + "-" + digest
	out = strings.TrimRight(out, "-")
	if !isPrintableASCII(out) {
		return ""
	}
	return out
}

func isPrintableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x21 || c > 0x7e {
			return false
		}
	}
	return len(s) > 0
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:8]
}
