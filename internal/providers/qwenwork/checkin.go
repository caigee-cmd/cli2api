package qwenwork

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// Daily reward paths. The desktop bundles (global 1.0.4, CN 1.4.0) call the
// growth-card endpoints below; production gateways may answer 404 while the
// campaign is closed, so Checkin falls back to the daily-grant probe.
const (
	pathDailyCheckinStatus = "/sash/api/v1/me/daily-check-in/status"
	pathDailyCheckinClaim  = "/sash/api/v1/me/daily-check-in/claim"
	pathWallets            = "/api/v1/adapter/user/wallets"
)

// dailyCheckinStatuses mirrors the desktop value set: CLAIMABLE, CLAIMED_TODAY,
// DISABLED, NOT_STARTED, ENDED. claimDailyCheckinResults mirrors the claim
// response: CLAIMED, ALREADY_CLAIMED.
const (
	dailyCheckinClaimable    = "CLAIMABLE"
	dailyCheckinClaimedToday = "CLAIMED_TODAY"
	dailyCheckinDisabled     = "DISABLED"
	dailyCheckinNotStarted   = "NOT_STARTED"
	dailyCheckinEnded        = "ENDED"

	dailyClaimDone    = "CLAIMED"
	dailyClaimAlready = "ALREADY_CLAIMED"

	// checkOK is the gateway envelope's success code.
	checkOK = "ok"

	// growthCardUserAgent is the UA the desktop growth card sends, distinct
	// from the gateway's qoderwork/<version> agent.
	growthCardUserAgent = "QoderWork"

	// checkinTimeout bounds the whole check-in round trip.
	checkinTimeout = 30 * time.Second
)

// dailyCheckinDocument is the growth-card status document. The desktop client
// reads the same fields from data.status; reward and streak values are kept as
// pointers because CLAIMED_TODAY may omit them.
type dailyCheckinDocument struct {
	Code string `json:"code"`
	Data struct {
		Status             string   `json:"status"`
		RewardCredits      *float64 `json:"rewardCredits"`
		NextClaimAt        *int64   `json:"nextClaimAt"`
		CurrentStreakDays  *int     `json:"currentStreakDays"`
		TotalClaimDays     *int     `json:"totalClaimDays"`
		TotalRewardCredits *float64 `json:"totalRewardCredits"`
		LastClaimedAt      *int64   `json:"lastClaimedAt"`
		RewardExpiresAt    *int64   `json:"rewardExpiresAt"`
	} `json:"data"`
}

// dailyClaimDocument is the claim response document.
type dailyClaimDocument struct {
	Code string `json:"code"`
	Data struct {
		Result        string   `json:"result"`
		RewardCredits *float64 `json:"rewardCredits"`
		ExpiresAt     *int64   `json:"expiresAt"`
	} `json:"data"`
}

// walletsDocument is the CN wallet summary. daily_credits holds the credits
// granted for the current day; the desktop notice renders the same number as
// "今日已获得 N 免费积分".
type walletsDocument struct {
	Code string `json:"code"`
	Data struct {
		DailyCredits struct {
			TotalBalance *float64 `json:"total_balance"`
		} `json:"daily_credits"`
		LongtermCredits struct {
			TotalBalance *float64 `json:"total_balance"`
		} `json:"longterm_credits"`
	} `json:"data"`
}

// Checkin confirms the account's daily free credits.
//
// The QwenWork desktop client ships a growth-card check-in (status + claim)
// gated by the remote `operations/announcement:credits-growth-card` config. The
// production gateways do not route that prefix, so the claim path is tried
// first and, when the route is absent, the daily grant is confirmed from the
// region's own data: CN exposes the daily wallet balance; global exposes only
// the account-context total.
func (c *Client) Checkin(ctx context.Context, accountID string) (providers.CheckinResult, error) {
	credential, st, err := c.resolvedCredential(ctx, accountID)
	if err != nil {
		return providers.CheckinResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, checkinTimeout)
	defer cancel()
	result, campaignHandled, err := c.claimDailyCampaign(ctx, accountID, st, credential)
	if err != nil {
		return providers.CheckinResult{}, err
	}
	if campaignHandled {
		return result, nil
	}
	return c.confirmDailyGrant(ctx, accountID, st, credential)
}

// claimDailyCampaign runs the growth-card status/claim flow. campaignHandled is
// false when the gateway does not route the check-in prefix, which is the
// current production state; the caller then confirms the server-side grant.
func (c *Client) claimDailyCampaign(ctx context.Context, accountID string, st site, credential Credential) (providers.CheckinResult, bool, error) {
	body, status, err := c.do(ctx, accountID, http.MethodGet, st.Gateway+pathDailyCheckinStatus, nil,
		func(h http.Header) { setGrowthCardHeaders(h, st, credential.AccessToken) })
	if err != nil {
		return providers.CheckinResult{}, false, fmt.Errorf("qwenwork check-in: status request: %w", err)
	}
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return providers.CheckinResult{}, false, fmt.Errorf("qwenwork check-in: authentication failed (HTTP %d)", status)
	case status == http.StatusNotFound || status == http.StatusMethodNotAllowed || status == http.StatusNotImplemented:
		// The prefix is not deployed; confirm today's automatic grant instead.
		return providers.CheckinResult{}, false, nil
	case status >= 300:
		return providers.CheckinResult{}, false, fmt.Errorf("qwenwork check-in: status HTTP %d: %s", status, truncateBody(body))
	}
	var document dailyCheckinDocument
	if err := json.Unmarshal(body, &document); err != nil {
		return providers.CheckinResult{}, false, fmt.Errorf("qwenwork check-in: invalid status document")
	}
	if document.Code != "" && document.Code != checkOK {
		return providers.CheckinResult{}, false, fmt.Errorf("qwenwork check-in: status code=%s", document.Code)
	}
	state := strings.ToUpper(strings.TrimSpace(document.Data.Status))
	switch state {
	case dailyCheckinClaimable:
		return c.claimCampaign(ctx, accountID, st, credential)
	case dailyCheckinClaimedToday:
		result := providers.CheckinResult{Status: "already", Message: checkinAlreadyMessage(document)}
		if document.Data.RewardCredits != nil {
			result.RewardCredits = *document.Data.RewardCredits
		}
		return result, true, nil
	case dailyCheckinDisabled:
		return providers.CheckinResult{Status: "skipped", Message: "签到活动未开放"}, true, nil
	case dailyCheckinNotStarted:
		return providers.CheckinResult{Status: "skipped", Message: "签到活动未开始"}, true, nil
	case dailyCheckinEnded:
		return providers.CheckinResult{Status: "skipped", Message: "签到活动已结束"}, true, nil
	default:
		message := "签到活动状态未知"
		if state != "" {
			message = fmt.Sprintf("签到活动状态 %s", state)
		}
		return providers.CheckinResult{Status: "skipped", Message: message}, true, nil
	}
}

// claimCampaign POSTs the claim for a CLAIMABLE campaign and maps the result.
func (c *Client) claimCampaign(ctx context.Context, accountID string, st site, credential Credential) (providers.CheckinResult, bool, error) {
	body, status, err := c.do(ctx, accountID, http.MethodPost, st.Gateway+pathDailyCheckinClaim, nil,
		func(h http.Header) { setGrowthCardHeaders(h, st, credential.AccessToken) })
	if err != nil {
		return providers.CheckinResult{}, false, fmt.Errorf("qwenwork check-in: claim request: %w", err)
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return providers.CheckinResult{}, false, fmt.Errorf("qwenwork check-in: authentication failed (HTTP %d)", status)
	}
	if status >= 300 {
		return providers.CheckinResult{}, false, fmt.Errorf("qwenwork check-in: claim HTTP %d: %s", status, truncateBody(body))
	}
	var document dailyClaimDocument
	if err := json.Unmarshal(body, &document); err != nil {
		return providers.CheckinResult{}, false, fmt.Errorf("qwenwork check-in: invalid claim document")
	}
	if document.Code != "" && document.Code != checkOK {
		return providers.CheckinResult{}, false, fmt.Errorf("qwenwork check-in: claim code=%s", document.Code)
	}
	reward := 0.0
	hasReward := document.Data.RewardCredits != nil
	if hasReward {
		reward = *document.Data.RewardCredits
	}
	switch strings.ToUpper(strings.TrimSpace(document.Data.Result)) {
	case dailyClaimDone:
		result := providers.CheckinResult{Status: "success", Message: "签到成功", RewardCredits: reward}
		if hasReward {
			result.Message = fmt.Sprintf("签到成功 +%g 积分", reward)
		}
		return result, true, nil
	case dailyClaimAlready:
		return providers.CheckinResult{Status: "already", Message: "今日已签到"}, true, nil
	default:
		return providers.CheckinResult{}, false, fmt.Errorf("qwenwork check-in: unexpected claim result %q", document.Data.Result)
	}
}

// confirmDailyGrant verifies the server-side daily free credits when the claim
// prefix is not deployed. CN exposes a per-day wallet; global exposes only the
// account total, so it reports the upstream gap instead of claiming success.
func (c *Client) confirmDailyGrant(ctx context.Context, accountID string, st site, credential Credential) (providers.CheckinResult, error) {
	if st.Region == "cn" {
		return c.confirmCNWallet(ctx, accountID, st, credential)
	}
	remaining, ok := c.dailyQuotaRemaining(ctx, accountID, st, credential)
	if !ok {
		return providers.CheckinResult{
			Status:  "skipped",
			Message: "签到接口未开放（上游 404）；每日登录奖励由服务端自动发放",
		}, nil
	}
	return providers.CheckinResult{
		Status:  "skipped",
		Message: fmt.Sprintf("签到接口未开放（上游 404）；每日登录奖励由服务端自动发放，当前剩余额度 %g", remaining),
	}, nil
}

// confirmCNWallet reads the CN daily wallet. A positive daily balance means
// today's free credits were granted (the client copy says they refresh at
// 00:00); a zero balance can mean not granted yet or already spent, so it is
// reported as skipped rather than a failure.
func (c *Client) confirmCNWallet(ctx context.Context, accountID string, st site, credential Credential) (providers.CheckinResult, error) {
	body, status, err := c.do(ctx, accountID, http.MethodGet, st.Gateway+pathWallets, nil,
		func(h http.Header) { setGrowthCardHeaders(h, st, credential.AccessToken) })
	if err != nil {
		return providers.CheckinResult{}, fmt.Errorf("qwenwork check-in: wallet request: %w", err)
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return providers.CheckinResult{}, fmt.Errorf("qwenwork check-in: authentication failed (HTTP %d)", status)
	}
	if status == http.StatusNotFound || status == http.StatusMethodNotAllowed {
		return providers.CheckinResult{
			Status:  "skipped",
			Message: "签到接口未开放（上游 404）；钱包接口不可用",
		}, nil
	}
	if status >= 300 {
		return providers.CheckinResult{}, fmt.Errorf("qwenwork check-in: wallet HTTP %d: %s", status, truncateBody(body))
	}
	var document walletsDocument
	if err := json.Unmarshal(body, &document); err != nil {
		return providers.CheckinResult{}, fmt.Errorf("qwenwork check-in: invalid wallet document")
	}
	if document.Code != "" && document.Code != checkOK {
		return providers.CheckinResult{}, fmt.Errorf("qwenwork check-in: wallet code=%s", document.Code)
	}
	daily := 0.0
	if document.Data.DailyCredits.TotalBalance != nil {
		daily = *document.Data.DailyCredits.TotalBalance
	}
	if daily <= 0 {
		return providers.CheckinResult{
			Status:  "skipped",
			Message: "今日每日免费积分尚未到账（每日 0 点刷新）",
		}, nil
	}
	return providers.CheckinResult{
		Status:        "already",
		Message:       fmt.Sprintf("每日免费积分已到账 +%g（每日 0 点刷新）", daily),
		RewardCredits: daily,
	}, nil
}

// dailyQuotaRemaining reads the account-context quota for the global region,
// where no per-day wallet is exposed.
func (c *Client) dailyQuotaRemaining(ctx context.Context, accountID string, st site, credential Credential) (float64, bool) {
	body, status, err := c.do(ctx, accountID, http.MethodGet, st.Gateway+pathAccountContext, nil,
		func(h http.Header) { setPlainHeaders(h, st, credential.AccessToken) })
	if err != nil || status >= 300 {
		return 0, false
	}
	var envelope struct {
		Data struct {
			Quota struct {
				Remaining *float64 `json:"remaining"`
			} `json:"quota"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Data.Quota.Remaining == nil {
		return 0, false
	}
	return *envelope.Data.Quota.Remaining, true
}

// checkinAlreadyMessage renders the streak detail the growth card shows next to
// "今日已签到", keeping the persisted message short.
func checkinAlreadyMessage(document dailyCheckinDocument) string {
	data := document.Data
	if data.RewardCredits != nil && *data.RewardCredits > 0 {
		return fmt.Sprintf("今日已签到 +%g 积分", *data.RewardCredits)
	}
	if data.CurrentStreakDays != nil && *data.CurrentStreakDays > 0 {
		return fmt.Sprintf("今日已签到（连签 %d 天）", *data.CurrentStreakDays)
	}
	return "今日已签到"
}

// setGrowthCardHeaders mirrors the desktop growth-card request identity: plain
// Bearer plus the X-QwenWork client headers and the QoderWork agent.
func setGrowthCardHeaders(h http.Header, st site, bearer string) {
	h.Set("Accept", "application/json")
	h.Set("Content-Type", "application/json")
	h.Set("User-Agent", growthCardUserAgent)
	h.Set("X-Qwenwork-Version", st.AppVersion)
	h.Set("X-Qwenwork-Release-Version", st.releaseVersion())
	h.Set("X-Qwenwork-Build", st.Build)
	h.Set("X-Qwenwork-Platform", "win32")
	h.Set("X-Qwenwork-Arch", "x64")
	h.Set("X-Qwenwork-Channel", "stable")
	if bearer != "" {
		h.Set("Authorization", "Bearer "+bearer)
	}
}
