package codearts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

var errCreditAlreadyClaimed = errors.New("codearts credit already claimed")

const (
	creditChannel = "IDE"
	// CodeArts publishes the daily check-in as campaign 1 and the one-time
	// new-user reward as campaign 4. Titles are matched too so a regional
	// rename does not silently stop the automatic path.
	dailyCreditCampaignID   = 1
	newUserCreditCampaignID = 4
)

type creditCampaign struct {
	ID                 int     `json:"campaignId"`
	Type               string  `json:"type"`
	Title              string  `json:"title"`
	Status             string  `json:"status"`
	Claimable          bool    `json:"claimable"`
	BenefitUnit        string  `json:"benefitUnit"`
	BenefitAmount      float64 `json:"benefitAmount"`
	PendingTotalAmount float64 `json:"pendingTotalAmount"`
	Extra              struct {
		StartTime string `json:"startTime"`
		EndTime   string `json:"endTime"`
	} `json:"extra"`
}

func isDailyCreditCampaign(campaign creditCampaign) bool {
	if strings.EqualFold(strings.TrimSpace(campaign.Type), "USER_LOGIN") {
		return true
	}
	if campaign.ID == dailyCreditCampaignID {
		return true
	}
	title := strings.ToLower(strings.TrimSpace(campaign.Title))
	return strings.Contains(title, "daily") || strings.Contains(title, "每日") || strings.Contains(title, "签到")
}

func isNewUserCreditCampaign(campaign creditCampaign) bool {
	return strings.EqualFold(strings.TrimSpace(campaign.Type), "NEW_USER_REGISTER") ||
		campaign.ID == newUserCreditCampaignID
}

// isAutomaticCreditCampaign limits unattended claims to the daily check-in and
// the new-user campaign. Every claimable credit campaign is not safe to claim:
// student verification, invite rewards, and other manual benefit campaigns can
// also be "claimable" and must stay an explicit user action.
func isAutomaticCreditCampaign(campaign creditCampaign) bool {
	return isDailyCreditCampaign(campaign) || isNewUserCreditCampaign(campaign)
}

// creditCampaignTime parses both RFC3339 timestamps and the plain Asia/Shanghai
// wall-clock strings the ops API may return.
func creditCampaignTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, true
	}
	zone := time.FixedZone("UTC+8", 8*3600)
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if parsed, err := time.ParseInLocation(layout, value, zone); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func creditCampaignOpen(campaign creditCampaign, now time.Time) bool {
	if start, ok := creditCampaignTime(campaign.Extra.StartTime); ok && now.Before(start) {
		return false
	}
	if end, ok := creditCampaignTime(campaign.Extra.EndTime); ok && !now.Before(end) {
		return false
	}
	return true
}

type creditClaim struct {
	CampaignID  int
	Title       string
	OK          bool
	Status      string
	Credits     float64
	Provisional bool
	Message     string
}

// Checkin claims the daily check-in (campaign 1) and the new-user campaign
// (campaign 4). The upstream decides eligibility and the daily reset; already
// claimed campaigns only get their missing confirmation.
func (c *Client) Checkin(ctx context.Context, accountID string) (providers.CheckinResult, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return providers.CheckinResult{}, err
	}
	campaigns, err := c.fetchCampaigns(ctx, accountID, credential)
	if err != nil {
		return providers.CheckinResult{}, err
	}
	var claims []creditClaim
	total := 0.0
	allOK := true
	provisional := false
	for _, campaign := range campaigns {
		// The server marks the activities an account may actually claim. Only
		// the daily check-in and the new-user reward are automatic: student
		// verification and invite campaigns are never claimed on behalf of the
		// user even when the server marks them claimable. The daily campaign is
		// read even when it is already claimed so the result says "already"
		// instead of "no campaign".
		if campaign.BenefitUnit != "" && campaign.BenefitUnit != "CREDIT" {
			continue
		}
		if !isAutomaticCreditCampaign(campaign) {
			continue
		}
		if !campaign.Claimable && !isDailyCreditCampaign(campaign) {
			continue
		}
		claim := c.claimCampaign(ctx, accountID, credential, campaign)
		claims = append(claims, claim)
		allOK = allOK && claim.OK
		provisional = provisional || claim.Provisional
		total += claim.Credits
	}
	if len(claims) == 0 {
		return providers.CheckinResult{Status: "skipped", Message: "no check-in or new-user campaign"}, nil
	}
	status := "success"
	if !allOK {
		status = "skipped"
	} else {
		already := true
		for _, claim := range claims {
			if claim.Credits > 0 {
				already = false
			}
		}
		if already {
			status = "already"
			if provisional {
				status = "unknown"
			}
		}
	}
	parts := make([]string, 0, len(claims))
	for _, claim := range claims {
		parts = append(parts, claim.Title+": "+claim.Message)
	}
	return providers.CheckinResult{
		Status:        status,
		Message:       strings.Join(parts, "; "),
		RewardCredits: total,
	}, nil
}

func (c *Client) CheckinStatus(ctx context.Context, accountID string) (providers.CheckinResult, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return providers.CheckinResult{}, err
	}
	campaigns, err := c.fetchCampaigns(ctx, accountID, credential)
	if err != nil {
		return providers.CheckinResult{}, err
	}
	now := c.now()
	result := providers.CheckinResult{}
	pending := false
	dailyClaimed := false
	for _, campaign := range campaigns {
		if campaign.BenefitUnit != "" && campaign.BenefitUnit != "CREDIT" {
			continue
		}
		if !isAutomaticCreditCampaign(campaign) {
			continue
		}
		if isDailyCreditCampaign(campaign) && result.EarliestCheckin == "" {
			// extra.startTime is the campaign's launch date, not the daily reset.
			// The CodeArts daily panel rolls over at 00:00 Asia/Shanghai.
			result.EarliestCheckin = "00:00"
		}
		if !creditCampaignOpen(campaign, now) {
			continue
		}
		if campaign.Claimable {
			pending = true
			continue
		}
		if isDailyCreditCampaign(campaign) {
			switch campaign.Status {
			case "CONFIRMED", "CONSUMED", "CLAIMED":
				dailyClaimed = true
			}
		}
	}
	switch {
	case pending:
		result.Status = "pending"
		result.Message = "上游显示可签到"
	case dailyClaimed:
		// The new-user campaign stays CONFIRMED forever. Only the daily
		// campaign may make the account look checked in, or a used-up new-user
		// reward would report "already" every day.
		if providers.CheckinAnswerProvisional(now) {
			result.Status = "unknown"
			result.Message = "上游日界尚未确认，稍后复核"
		} else {
			result.Status = "already"
			result.Message = "今日已签到"
		}
	default:
		result.Status = "skipped"
		result.Message = "当前无可签到活动"
	}
	if result.EarliestCheckin == "" {
		result.EarliestCheckin = "00:00"
	}
	return result, nil
}

func (c *Client) claimCampaign(ctx context.Context, accountID string, credential Credential, campaign creditCampaign) creditClaim {
	claim := creditClaim{CampaignID: campaign.ID, Title: campaign.Title, Status: campaign.Status, OK: true}
	if claim.Title == "" {
		claim.Title = fmt.Sprintf("credit campaign %d", campaign.ID)
	}
	now := c.now()
	if end, ok := creditCampaignTime(campaign.Extra.EndTime); ok && !now.Before(end) {
		claim.OK = false
		claim.Message = "campaign ended"
		return claim
	}
	if start, ok := creditCampaignTime(campaign.Extra.StartTime); ok && now.Before(start) {
		claim.OK = false
		claim.Message = "campaign has not started"
		return claim
	}
	// Before the provider day is confirmed, a daily campaign marked CONFIRMED
	// may still be yesterday's. Re-issuing the idempotent claim tells the server
	// to replay yesterday's result or deliver today's, instead of suppressing
	// today's check-in.
	provisionalDaily := isDailyCreditCampaign(campaign) && providers.CheckinAnswerProvisional(now)
	claimedBefore := false
	switch campaign.Status {
	case "CONFIRMED", "CONSUMED":
		claimedBefore = true
		if !provisionalDaily {
			claim.Message = "already claimed"
			return claim
		}
	case "CLAIMED":
		claimedBefore = true
		// Already delivered; only the confirm step may be missing.
	case "ELIGIBLE":
	default:
		claim.OK = false
		if campaign.Status == "" {
			claim.Message = "not eligible"
		} else {
			claim.Message = "unknown campaign status: " + campaign.Status
		}
		return claim
	}
	// The ops claim body is only {campaignId, channel}; the protocol has no
	// client-supplied idempotency key. Repeated claims are deduplicated by the
	// server with code 40001 instead.
	raw, err := c.creditRequest(ctx, accountID, credential, http.MethodPost, creditClaimPath, map[string]any{
		"campaignId": campaign.ID, "channel": creditChannel,
	})
	if err != nil {
		if errors.Is(err, errCreditAlreadyClaimed) {
			claim.Status = "CONFIRMED"
			claim.Message = "今天已领取"
			claim.Credits = 0
			claim.Provisional = providers.CheckinClaimProvisional(now)
			return claim
		}
		claim.OK = false
		claim.Message = err.Error()
		return claim
	}
	var benefit struct {
		ID            json.RawMessage `json:"id"`
		BenefitAmount float64         `json:"benefitAmount"`
		Credits       float64         `json:"credits"`
		Credit        float64         `json:"credit"`
		Amount        float64         `json:"amount"`
		CreditAmount  float64         `json:"creditAmount"`
	}
	_ = json.Unmarshal(raw, &benefit)
	if len(benefit.ID) > 0 && string(benefit.ID) != "null" {
		// The IDE posts /v1/ops/confirm whenever the claim returns a benefit id;
		// without it the credits can stay pending. This step is best effort:
		// older builds answer 404 here and the claim is still successful.
		_, _ = c.creditRequest(ctx, accountID, credential, http.MethodPost, creditConfirmPath, map[string]any{
			"campaignId": campaign.ID,
		})
	}
	claim.Status = "CONFIRMED"
	if provisionalDaily && claimedBefore {
		// The claim was a replay/verification of a state that may have been
		// yesterday's. Do not report the old campaign amount as new credits.
		claim.Credits = 0
		claim.Message = "已签到（复查确认）"
		claim.Provisional = providers.CheckinClaimProvisional(now)
		return claim
	}
	claim.Status = "CLAIMED"
	claim.Credits = campaign.PendingTotalAmount
	for _, value := range []float64{benefit.BenefitAmount, benefit.Credits, benefit.Credit, benefit.CreditAmount, benefit.Amount} {
		if value > 0 {
			claim.Credits = value
			break
		}
	}
	if claim.Credits <= 0 {
		claim.Credits = campaign.BenefitAmount
	}
	// The claims endpoint is the source of truth. When it returned a benefit id
	// this function already posted the IDE's best-effort /v1/ops/confirm above;
	// a missing confirm response is not a failed claim.
	claim.Status = "CONFIRMED"
	if claim.Credits > 0 {
		claim.Message = fmt.Sprintf("claimed %g credits", claim.Credits)
	} else {
		claim.Message = "claimed"
	}
	return claim
}

func (c *Client) fetchCampaigns(ctx context.Context, accountID string, credential Credential) ([]creditCampaign, error) {
	raw, err := c.creditRequest(ctx, accountID, credential, http.MethodGet, creditDeliveryPath+"?channel="+creditChannel, nil)
	if err != nil {
		return nil, err
	}
	var data struct {
		Items *[]creditCampaign `json:"items"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("parse codearts credit campaigns: %w", err)
	}
	if data.Items == nil {
		return nil, fmt.Errorf("codearts credit response missing data.items")
	}
	return *data.Items, nil
}

func (c *Client) creditRequest(ctx context.Context, accountID string, credential Credential, method, path string, body any) (json.RawMessage, error) {
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	requestCtx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, method, c.apiBase(credential)+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Agent-Type", "PromptCenter")
	req.Header.Set("X-Language", Language)
	signRequest(req, raw, credential.Sign())
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	response, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, newProviderError(resp.StatusCode, string(response))
	}
	var envelope struct {
		Code    *int            `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(response, &envelope); err != nil {
		return nil, fmt.Errorf("parse codearts credit response: %w", err)
	}
	if envelope.Code == nil {
		return nil, fmt.Errorf("codearts credit response missing business code")
	}
	if *envelope.Code != 0 {
		if *envelope.Code == 40001 {
			return nil, fmt.Errorf("%w: %s", errCreditAlreadyClaimed, envelope.Message)
		}
		return nil, &providers.Error{Kind: accounts.KindInvalidRequest, Status: http.StatusBadRequest, Message: envelope.Message}
	}
	return envelope.Data, nil
}
