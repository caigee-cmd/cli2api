package qoder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
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
// Campaign requests send the desktop identity headers the activity endpoint
// requires (User-Agent, Cosy-ClientType, Cosy-Version) and omit the derived
// cosy-machine* set. A full derived set is treated as a non-official client
// and the daily credit campaign is filtered out of an otherwise successful
// list, which is what made a later check-in look like "no campaign".
//
// Wire shape:
//   - GET  {openapi}/sash/api/v1/me/campaigns  → list of campaigns
//   - POST {openapi}/sash/api/v1/me/campaigns/{id}/claim  → claim one
//
// A campaign is claimable when actionType == "CLAIM_BENEFIT" and
// claimStatus == "CLAIMABLE". An empty or non-claimable list is retried,
// because the endpoint intermittently returns no daily campaign.
// SAME_PERSON_ALREADY_CLAIMED means this machine identity already collected
// today and is reported as already checked in, not as a failure.
// A CLAIMED credit grant inside its window is not treated as today's claim:
// the campaign id is reused across a multi-day batch, so the list stays
// CLAIMED after the first day. That grant is claimed again and the server
// decides — replayed means already claimed, a fresh CLAIMED means the list
// was stale and this call collected today's credits.
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

	// The campaign endpoint's bearer differs by region: CN accepts the COSY
	// securityOauthToken; Global rejects it (401) and needs a separately-issued
	// OpenAPI bearer. quotaBearer returns the right one and caches the global
	// token. Fall back to the COSY token if the exchange fails.
	bearer, err := b.quotaBearer(ctx)
	if err != nil || bearer == "" {
		bearer = b.CurrentSecurityOauth()
	}

	var doRequest checkinRequester
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
		if method != http.MethodGet {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("User-Agent", "Qoder")
		req.Header.Set("Cosy-ClientType", "10")
		req.Header.Set("Cosy-Version", qauth.DesktopCosyVersion)
		// Derived machine headers are deliberately absent. See the comment
		// on Client.Checkin: a full derived set hides the daily campaign.
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

	// 1) List campaigns. A successful response with nothing claimable is
	// retried: the endpoint intermittently omits the daily campaign, and
	// treating that single snapshot as final dropped the day's check-in.
	var items []campaign
	for attempt, wait := range checkinListRetryWaits {
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
		items = campaignsFrom(listRaw)
		if !campaignListNeedsRetry(items) || attempt == len(checkinListRetryWaits)-1 {
			break
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return providers.CheckinResult{}, ctx.Err()
		case <-timer.C:
		}
	}
	benefits := creditCampaigns(items)
	if len(benefits) == 0 {
		summary := campaignSummary(items)
		log.Printf("[qoder-checkin] region=%s no credit campaigns: count=%d summary=%s", regionName(b.Region), len(items), summary)
		return providers.CheckinResult{Status: "skipped", Message: skippedMessage("上游未返回可领取积分的签到活动", len(items), summary)}, nil
	}
	claimable := claimableCampaigns(benefits)
	if len(claimable) == 0 {
		// A CLAIMED row inside its window is a candidate, not proof that
		// today's grant was collected. Re-issue the claim and let the
		// server say whether it is a replay or a fresh credit.
		if grant, ok := claimedCreditGrant(benefits, time.Now()); ok {
			result, reclaimErr := b.reclaimClaimedGrant(doRequest, grant)
			if reclaimErr != nil {
				log.Printf("[qoder-checkin] region=%s claimed grant %s re-claim failed: %v", regionName(b.Region), grant.CampaignID, reclaimErr)
				return providers.CheckinResult{}, reclaimErr
			}
			return result, nil
		}
		summary := campaignSummary(items)
		log.Printf("[qoder-checkin] region=%s credit campaigns not claimable: count=%d summary=%s", regionName(b.Region), len(items), summary)
		return providers.CheckinResult{Status: "skipped", Message: skippedMessage("签到活动当前不可领取", len(items), summary)}, nil
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
		if failure, blocked := claimFailure(claimRaw); blocked {
			// Upstream dedupes by person (machine identity), not by account.
			// A single-account pool hits this too, after the official client
			// or another account on the same machine already collected.
			log.Printf("[qoder-checkin] region=%s person already claimed: code=%s", regionName(b.Region), failure)
			return providers.CheckinResult{Status: "already", Message: "今日已签到（同一身份已领取）"}, nil
		}
		if err != nil {
			// Re-list to see if the claim actually succeeded despite the error.
			relist, _, relistErr := doRequest(checkinCampaignsPath, http.MethodGet, true)
			if relistErr != nil {
				return providers.CheckinResult{}, err
			}
			if status := claimStatusOf(campaignsFrom(relist), campaign.CampaignID); claimConfirmed(status) {
				recovered++
			} else {
				return providers.CheckinResult{}, err
			}
		} else {
			status := unwrapClaimStatus(claimRaw)
			if !claimConfirmed(status) {
				// Fall back to re-listing; the claim may have been recorded server-side.
				relist, _, relistErr := doRequest(checkinCampaignsPath, http.MethodGet, true)
				if relistErr != nil {
					return providers.CheckinResult{}, fmt.Errorf("qoder check-in: claim unconfirmed")
				}
				if status := claimStatusOf(campaignsFrom(relist), campaign.CampaignID); claimConfirmed(status) {
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

// reclaimClaimedGrant re-issues POST /claim for a grant the list already
// marks CLAIMED. The claim is idempotent: replayed=true confirms the
// current period was collected (already); CLAIMED without replayed means
// the listed grant was stale and this call collected it (success). Anything
// else fails so the scheduler retries instead of recording a false already.
func (b *Bridge) reclaimClaimedGrant(doRequest checkinRequester, grant campaign) (providers.CheckinResult, error) {
	path := fmt.Sprintf("%s/%s/claim", checkinCampaignsPath, url.PathEscape(grant.CampaignID))
	claimRaw, status, err := doRequest(path, http.MethodPost, false)
	if err != nil {
		return providers.CheckinResult{}, err
	}
	if replayed, claimStatus, ok := claimVerdict(claimRaw); ok {
		if replayed {
			return providers.CheckinResult{Status: "already", Message: "今日已签到（幂等回放）"}, nil
		}
		if claimConfirmed(claimStatus) {
			log.Printf("[qoder-checkin] region=%s listed CLAIMED grant %s collected on re-claim (stale list)", regionName(b.Region), grant.CampaignID)
			out := providers.CheckinResult{Status: "success", Message: "签到成功"}
			if amount, has := rewardFrom(grant); has {
				out.Message = fmt.Sprintf("签到成功 +%g 积分", amount)
				out.RewardCredits = amount
			}
			return out, nil
		}
	}
	if status >= 200 && status < 300 {
		return providers.CheckinResult{}, fmt.Errorf("qoder check-in: claim unconfirmed")
	}
	return providers.CheckinResult{}, fmt.Errorf("qoder check-in: claim failed (HTTP %d)", status)
}

type checkinRequester func(path, method string, refreshed bool) (map[string]any, int, error)

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

// checkinListRetryWaits is the backoff before each extra campaign listing.
// The first attempt waits nothing; an empty or non-claimable list waits
// 800ms and then 2s. A list that already has something claimable stops.
// Tests replace the slice; production values match the observed jitter.
var checkinListRetryWaits = []time.Duration{0, 800 * time.Millisecond, 2 * time.Second}

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
	CampaignID  string  `json:"campaignId"`
	ActionType  string  `json:"actionType"`
	ClaimStatus string  `json:"claimStatus"`
	StartAt     float64 `json:"startAt"`
	EndAt       float64 `json:"endAt"`
	Benefit     struct {
		Kind   string  `json:"kind"`
		Amount float64 `json:"amount"`
	} `json:"benefit"`
}

// claimedCreditGrant returns the first CLAIMED credit grant whose window
// contains now. A missing window does not match: expired grants stay in the
// list unconditionally, and treating one as today's claim skips the real one.
func claimedCreditGrant(items []campaign, now time.Time) (campaign, bool) {
	nowSec := float64(now.Unix())
	for _, c := range items {
		if !strings.EqualFold(c.ClaimStatus, "CLAIMED") || !strings.EqualFold(c.Benefit.Kind, "CREDITS") {
			continue
		}
		if c.StartAt == 0 || c.EndAt == 0 || nowSec < c.StartAt || nowSec > c.EndAt {
			continue
		}
		return c, true
	}
	return campaign{}, false
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

// campaignListNeedsRetry reports whether a successful listing should be
// fetched again. An empty list, or a list with no CLAIMABLE credit campaign,
// is the intermittent shape that used to end the day's check-in.
func campaignListNeedsRetry(items []campaign) bool {
	if len(items) == 0 {
		return true
	}
	for _, c := range creditCampaigns(items) {
		if c.ClaimStatus == "CLAIMABLE" {
			return false
		}
	}
	return true
}

// claimConfirmed is a grant the campaigns endpoint counts as collected.
// CLAIMED is the list status; GRANTED and SUCCESS are the same outcome on
// the claim response.
func claimConfirmed(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "CLAIMED", "GRANTED", "SUCCESS":
		return true
	default:
		return false
	}
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

// claimFailure reads the upstream person-level dedupe. SAME_PERSON_ALREADY_CLAIMED
// (or status BLOCKED carrying that code) means this identity already collected
// today; it is not an account or credential failure.
func claimFailure(raw map[string]any) (string, bool) {
	if raw == nil {
		return "", false
	}
	body := raw
	if data, ok := raw["data"].(map[string]any); ok {
		if _, present := data["status"]; present || stringFieldOf(data, "failureCode") != "" {
			body = data
		}
	}
	status := strings.ToUpper(strings.TrimSpace(unwrapClaimStatus(raw)))
	code := strings.ToUpper(strings.TrimSpace(stringFieldOf(body, "failureCode")))
	if code == "SAME_PERSON_ALREADY_CLAIMED" || (status == "BLOCKED" && code != "") {
		if code == "" {
			code = status
		}
		return code, true
	}
	return "", false
}

func stringFieldOf(raw map[string]any, key string) string {
	value, _ := raw[key].(string)
	return value
}

func unwrapClaimStatus(raw map[string]any) string {
	status, _ := claimVerdictFields(raw)
	return status
}

// claimVerdict reports whether the claim body is a replay of an earlier claim
// and the status it carried. replayed and status are read from the top level
// or from data, matching both response shapes the endpoint has returned.
func claimVerdict(raw map[string]any) (replayed bool, status string, ok bool) {
	if raw == nil {
		return false, "", false
	}
	status, replayed = claimVerdictFields(raw)
	return replayed, status, status != "" || replayed
}

func claimVerdictFields(raw map[string]any) (status string, replayed bool) {
	status, replayed = verdictFrom(raw)
	if data, ok := raw["data"].(map[string]any); ok {
		nestedStatus, nestedReplay := verdictFrom(data)
		if status == "" {
			status = nestedStatus
		}
		replayed = replayed || nestedReplay
	}
	return status, replayed
}

func verdictFrom(raw map[string]any) (status string, replayed bool) {
	if s, ok := raw["status"].(string); ok {
		status = s
	}
	if v, ok := raw["replayed"].(bool); ok {
		replayed = v
	}
	return status, replayed
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

// campaignSummary renders "actionType:claimStatus" pairs (no ids or tokens) so
// a skipped check-in carries the upstream state that caused it instead of a
// generic "not open" message. Capped to keep the persisted message short.
func campaignSummary(items []campaign) string {
	if len(items) == 0 {
		return "none"
	}
	const maxItems = 8
	parts := make([]string, 0, maxItems+1)
	for i, c := range items {
		if i == maxItems {
			parts = append(parts, fmt.Sprintf("+%d", len(items)-maxItems))
			break
		}
		action := strings.TrimSpace(c.ActionType)
		if action == "" {
			action = "unknown"
		}
		status := strings.TrimSpace(c.ClaimStatus)
		if status == "" {
			status = "none"
		}
		parts = append(parts, action+":"+status)
	}
	return strings.Join(parts, ",")
}

func skippedMessage(reason string, count int, summary string) string {
	return fmt.Sprintf("%s（活动数 %d：%s）", reason, count, summary)
}

func regionName(r *qauth.RegionConfig) string {
	if r == nil {
		return ""
	}
	return r.Name
}
