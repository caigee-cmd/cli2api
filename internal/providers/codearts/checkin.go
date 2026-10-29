package codearts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

const creditChannel = "IDE"

type creditCampaign struct {
	ID                 int     `json:"campaignId"`
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

type creditClaim struct {
	CampaignID int
	Title      string
	OK         bool
	Status     string
	Credits    float64
	Message    string
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
	for _, campaign := range campaigns {
		if campaign.ID != 1 && campaign.ID != 4 {
			continue
		}
		if campaign.BenefitUnit != "" && campaign.BenefitUnit != "CREDIT" {
			continue
		}
		claim := c.claimCampaign(ctx, accountID, credential, campaign)
		claims = append(claims, claim)
		allOK = allOK && claim.OK
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

func (c *Client) claimCampaign(ctx context.Context, accountID string, credential Credential, campaign creditCampaign) creditClaim {
	claim := creditClaim{CampaignID: campaign.ID, Title: campaign.Title, Status: campaign.Status, OK: true}
	if claim.Title == "" {
		claim.Title = fmt.Sprintf("credit campaign %d", campaign.ID)
	}
	now := c.now()
	if end, err := time.Parse(time.RFC3339Nano, campaign.Extra.EndTime); err == nil && !now.Before(end) {
		claim.Message = "campaign ended"
		return claim
	}
	if start, err := time.Parse(time.RFC3339Nano, campaign.Extra.StartTime); err == nil && now.Before(start) {
		claim.Message = "campaign has not started"
		return claim
	}
	switch campaign.Status {
	case "CONFIRMED", "CONSUMED":
		claim.Message = "already claimed"
		return claim
	case "CLAIMED":
		// Already delivered; only the confirm step may be missing.
	case "ELIGIBLE":
		if !campaign.Claimable {
			claim.OK = false
			claim.Message = "not claimable yet"
			return claim
		}
		if _, err := c.creditRequest(ctx, accountID, credential, http.MethodPost, creditClaimPath, map[string]any{
			"campaignId": campaign.ID, "channel": creditChannel,
			"idempotentKey": fmt.Sprintf("claim_%d_%d", campaign.ID, now.UnixMilli()),
		}); err != nil {
			claim.OK = false
			claim.Message = err.Error()
			return claim
		}
		claim.Status = "CLAIMED"
		claim.Credits = campaign.PendingTotalAmount
		if claim.Credits <= 0 {
			claim.Credits = campaign.BenefitAmount
		}
	case "":
		claim.Message = "not eligible"
		return claim
	default:
		claim.OK = false
		claim.Message = "unknown campaign status: " + campaign.Status
		return claim
	}
	if _, err := c.creditRequest(ctx, accountID, credential, http.MethodPost, creditConfirmPath, map[string]any{"campaignId": campaign.ID}); err != nil {
		claim.OK = false
		claim.Message = "claimed but confirmation failed: " + err.Error()
		return claim
	}
	claim.Status = "CONFIRMED"
	claim.Message = "confirmed"
	if claim.Credits > 0 {
		claim.Message = fmt.Sprintf("claimed %g credits", claim.Credits)
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
		return nil, &providers.Error{Kind: accounts.KindInvalidRequest, Status: http.StatusBadRequest, Message: envelope.Message}
	}
	return envelope.Data, nil
}
