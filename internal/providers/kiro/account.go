package kiro

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// usageLimits is getUsageLimits. CREDIT is the meter the console shows; the
// first breakdown is used when Kiro does not label one.
type usageLimits struct {
	UsageBreakdownList   []usageBreakdown `json:"usageBreakdownList"`
	NextDateReset        json.Number      `json:"nextDateReset"`
	OverageConfiguration *struct {
		OverageStatus string `json:"overageStatus"`
	} `json:"overageConfiguration"`
	SubscriptionInfo *struct {
		SubscriptionName  string `json:"subscriptionName"`
		SubscriptionTitle string `json:"subscriptionTitle"`
		SubscriptionType  string `json:"subscriptionType"`
		OverageCapability string `json:"overageCapability"`
	} `json:"subscriptionInfo"`
	UserInfo *struct {
		Email  string `json:"email"`
		UserID string `json:"userId"`
	} `json:"userInfo"`
}

type usageBreakdown struct {
	ResourceType              string         `json:"resourceType"`
	CurrentUsage              float64        `json:"currentUsage"`
	CurrentUsageWithPrecision *float64       `json:"currentUsageWithPrecision"`
	UsageLimit                float64        `json:"usageLimit"`
	UsageLimitWithPrecision   *float64       `json:"usageLimitWithPrecision"`
	Currency                  string         `json:"currency"`
	Unit                      string         `json:"unit"`
	OverageCap                float64        `json:"overageCap"`
	OverageRate               float64        `json:"overageRate"`
	CurrentOverages           float64        `json:"currentOverages"`
	FreeTrialInfo             *freeTrialInfo `json:"freeTrialInfo"`
	Bonuses                   []bonusInfo    `json:"bonuses"`
}

func (b usageBreakdown) used() float64 {
	if b.CurrentUsageWithPrecision != nil {
		return *b.CurrentUsageWithPrecision
	}
	return b.CurrentUsage
}

func (b usageBreakdown) limit() float64 {
	if b.UsageLimitWithPrecision != nil {
		return *b.UsageLimitWithPrecision
	}
	return b.UsageLimit
}

type freeTrialInfo struct {
	CurrentUsage              float64     `json:"currentUsage"`
	CurrentUsageWithPrecision *float64    `json:"currentUsageWithPrecision"`
	UsageLimit                float64     `json:"usageLimit"`
	UsageLimitWithPrecision   *float64    `json:"usageLimitWithPrecision"`
	FreeTrialStatus           string      `json:"freeTrialStatus"`
	FreeTrialExpiry           json.Number `json:"freeTrialExpiry"`
}

func (f freeTrialInfo) used() float64 {
	if f.CurrentUsageWithPrecision != nil {
		return *f.CurrentUsageWithPrecision
	}
	return f.CurrentUsage
}

func (f freeTrialInfo) limit() float64 {
	if f.UsageLimitWithPrecision != nil {
		return *f.UsageLimitWithPrecision
	}
	return f.UsageLimit
}

type bonusInfo struct {
	BonusCode    string      `json:"bonusCode"`
	DisplayName  string      `json:"displayName"`
	CurrentUsage float64     `json:"currentUsage"`
	UsageLimit   float64     `json:"usageLimit"`
	ExpiresAt    json.Number `json:"expiresAt"`
	Status       string      `json:"status"`
}

func (c *Client) fetchUsage(ctx context.Context, client *http.Client, cred Credential) (usageLimits, error) {
	endpoint := codeWhispererBase(cred) + "/getUsageLimits?origin=AI_EDITOR&resourceType=AGENTIC_REQUEST&isEmailRequired=true"
	endpoint = withProfileQuery(endpoint, cred.requestProfileArn())
	body, err := c.getJSON(ctx, client, cred, endpoint)
	if err != nil {
		return usageLimits{}, err
	}
	var parsed usageLimits
	if err := json.Unmarshal(body, &parsed); err != nil {
		return usageLimits{}, err
	}
	return parsed, nil
}

func (c *Client) getJSON(ctx context.Context, client *http.Client, cred Credential, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	applyRuntimeHeaders(req, cred, req.URL.Host)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	noteVersionGate(client, string(body))
	if resp.StatusCode != http.StatusOK {
		return nil, newProviderError(resp.StatusCode, string(body))
	}
	return body, nil
}

func (c *Client) Quota(ctx context.Context, accountID string) (*providers.QuotaInfo, error) {
	cred, err := c.credential(ctx, accountID)
	if err != nil {
		return nil, err
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return nil, err
	}
	ensureIDEVersion(client)
	usage, err := c.fetchUsage(ctx, client, cred)
	if err != nil {
		return nil, err
	}
	c.rememberIdentity(ctx, accountID, cred, usage)
	return quotaFromUsage(usage, time.Now().UTC()), nil
}

func (c *Client) rememberIdentity(ctx context.Context, accountID string, cred Credential, usage usageLimits) {
	if usage.UserInfo == nil {
		return
	}
	changed := false
	if usage.UserInfo.Email != "" && cred.Email == "" {
		cred.Email = usage.UserInfo.Email
		changed = true
	}
	if usage.UserInfo.UserID != "" && cred.UserID == "" {
		cred.UserID = usage.UserInfo.UserID
		changed = true
	}
	if !changed {
		_ = c.store.Observe(ctx, accountID, firstNonEmpty(cred.UserID, cred.Email), "", "", "")
		return
	}
	if encoded, err := cred.Encode(); err == nil {
		_ = c.store.SaveCredentialPayload(ctx, accountID, CredentialFormat, encoded)
	}
}

func quotaFromUsage(usage usageLimits, fetchedAt time.Time) *providers.QuotaInfo {
	breakdown := creditBreakdown(usage.UsageBreakdownList)
	unit := "credits"
	if breakdown != nil {
		unit = firstNonEmpty(strings.TrimSpace(breakdown.Unit), strings.TrimSpace(breakdown.Currency), "credits")
	}
	info := &providers.QuotaInfo{
		Unit: unit, FetchedAt: fetchedAt.Format(time.RFC3339), ProviderID: "kiro",
		Plan: subscriptionPlan(usage),
	}
	if breakdown != nil {
		info.Used = breakdown.used()
		info.Total = breakdown.limit()
		info.Remaining = math.Max(0, info.Total-info.Used)
		if info.Total > 0 {
			info.Percentage = math.Min(100, info.Used/info.Total*100)
		}
		planExceeded := info.Total > 0 && info.Remaining <= 0
		info.Windows = append(info.Windows, providers.QuotaWindow{
			ID: "credits", Label: "Credits", Used: info.Used, Total: info.Total,
			Remaining: info.Remaining, Percentage: info.Percentage, Unit: unit,
			ResetAt: unixNumber(usage.NextDateReset), Exceeded: planExceeded,
		})
		if trial := breakdown.FreeTrialInfo; trial != nil && trial.limit() > 0 {
			used, total := trial.used(), trial.limit()
			remaining := math.Max(0, total-used)
			info.Windows = append(info.Windows, providers.QuotaWindow{
				ID: "trial", Label: "Free trial", Used: used, Total: total, Remaining: remaining,
				Percentage: math.Min(100, used/total*100), Unit: unit,
				ResetAt: unixNumber(trial.FreeTrialExpiry), Exceeded: remaining <= 0 || strings.EqualFold(trial.FreeTrialStatus, "EXPIRED"),
			})
		}
		// An enabled overage allowance is spendable after the plan credits run
		// out. Routing reads the top-level Exceeded flag, so the account stays
		// selectable while that allowance still has room.
		overage := overageOf(usage, breakdown)
		if overage.enabled && overage.cap > 0 {
			remaining := math.Max(0, overage.cap-overage.used)
			pct := math.Min(100, overage.used/overage.cap*100)
			info.Windows = append(info.Windows, providers.QuotaWindow{
				ID: "overage", Label: "Overage", Used: overage.used, Total: overage.cap,
				Remaining: remaining, Percentage: pct, Unit: unit, Exceeded: remaining <= 0,
			})
			if planExceeded {
				info.Used = overage.used
				info.Total = overage.cap
				info.Remaining = remaining
				info.Percentage = pct
				info.Exceeded = remaining <= 0
			}
		} else {
			info.Exceeded = planExceeded
		}
		for _, bonus := range breakdown.Bonuses {
			if bonus.UsageLimit <= 0 || strings.EqualFold(bonus.Status, "EXPIRED") {
				continue
			}
			remaining := math.Max(0, bonus.UsageLimit-bonus.CurrentUsage)
			info.Packages = append(info.Packages, providers.QuotaPackage{
				Used: bonus.CurrentUsage, Size: bonus.UsageLimit, Remain: remaining, Unit: unit,
				EndsAt: unixSeconds(bonus.ExpiresAt), EndTime: unixNumber(bonus.ExpiresAt),
			})
		}
	}
	return info
}

type overageState struct {
	enabled bool
	cap     float64
	used    float64
}

// overageOf reads the allowance Kiro bills after plan credits. The status lives
// on the response; the cap and the amount already billed live on a breakdown.
func overageOf(usage usageLimits, credit *usageBreakdown) overageState {
	status := ""
	if usage.OverageConfiguration != nil {
		status = strings.ToUpper(strings.TrimSpace(usage.OverageConfiguration.OverageStatus))
	}
	state := overageState{enabled: status == "ENABLED"}
	if credit != nil && (credit.OverageCap > 0 || credit.CurrentOverages > 0) {
		state.cap, state.used = credit.OverageCap, credit.CurrentOverages
		return state
	}
	for i := range usage.UsageBreakdownList {
		item := &usage.UsageBreakdownList[i]
		if item.OverageCap > 0 || item.CurrentOverages > 0 {
			state.cap, state.used = item.OverageCap, item.CurrentOverages
			return state
		}
	}
	return state
}

func creditBreakdown(list []usageBreakdown) *usageBreakdown {
	for i := range list {
		if strings.EqualFold(list[i].ResourceType, "CREDIT") {
			return &list[i]
		}
	}
	if len(list) == 0 {
		return nil
	}
	return &list[0]
}

func subscriptionPlan(usage usageLimits) string {
	if usage.SubscriptionInfo == nil {
		return ""
	}
	raw := strings.ToUpper(firstNonEmpty(usage.SubscriptionInfo.SubscriptionTitle, usage.SubscriptionInfo.SubscriptionName, usage.SubscriptionInfo.SubscriptionType))
	switch {
	case strings.Contains(raw, "PRO_PLUS"), strings.Contains(raw, "PROPLUS"):
		return "PRO_PLUS"
	case strings.Contains(raw, "POWER"):
		return "POWER"
	case strings.Contains(raw, "PRO"):
		return "PRO"
	case raw == "":
		return ""
	default:
		return "FREE"
	}
}

func unixNumber(value json.Number) string {
	seconds := unixSeconds(value)
	if seconds <= 0 {
		return ""
	}
	return time.Unix(seconds, 0).UTC().Format(time.RFC3339)
}

func unixSeconds(value json.Number) int64 {
	if value == "" {
		return 0
	}
	if n, err := value.Int64(); err == nil {
		return normalizeEpoch(n)
	}
	parsed, err := value.Float64()
	if err != nil {
		return 0
	}
	return normalizeEpoch(int64(parsed))
}

func normalizeEpoch(n int64) int64 {
	if n > 10_000_000_000 {
		return n / 1000
	}
	return n
}

type upstreamModel struct {
	ModelID        string   `json:"modelId"`
	ModelName      string   `json:"modelName"`
	Description    string   `json:"description"`
	InputTypes     []string `json:"supportedInputTypes"`
	RateMultiplier float64  `json:"rateMultiplier"`
	TokenLimits    *struct {
		MaxInputTokens  int `json:"maxInputTokens"`
		MaxOutputTokens int `json:"maxOutputTokens"`
	} `json:"tokenLimits"`
}

func (c *Client) listModels(ctx context.Context, client *http.Client, cred Credential) ([]providers.ModelInfo, error) {
	base := modelBase(cred) + "/ListAvailableModels?origin=AI_EDITOR&maxResults=50"
	base = withProfileQuery(base, cred.requestProfileArn())
	var models []providers.ModelInfo
	next := ""
	for page := 0; page < 20; page++ {
		endpoint := base
		if next != "" {
			endpoint += "&nextToken=" + url.QueryEscape(next)
		}
		body, err := c.getJSON(ctx, client, cred, endpoint)
		if err != nil {
			return nil, err
		}
		var parsed struct {
			Models    []upstreamModel `json:"models"`
			NextToken string          `json:"nextToken"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, err
		}
		for _, model := range parsed.Models {
			if strings.TrimSpace(model.ModelID) == "" {
				continue
			}
			models = append(models, modelInfo(model))
		}
		next = strings.TrimSpace(parsed.NextToken)
		if next == "" {
			break
		}
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("kiro model list is empty")
	}
	return models, nil
}

func modelInfo(model upstreamModel) providers.ModelInfo {
	images := false
	for _, kind := range model.InputTypes {
		if strings.EqualFold(kind, "IMAGE") {
			images = true
		}
	}
	info := providers.ModelInfo{
		NativeModel: model.ModelID, PublicModel: model.ModelID,
		DisplayName: firstNonEmpty(model.ModelName, model.ModelID),
		Capabilities: providers.ModelCapabilities{
			Tools: true, Images: images, Reasoning: true,
			ReasoningOptions: []string{"low", "medium", "high"}, ReasoningDefault: "medium",
		},
	}
	if model.TokenLimits != nil {
		info.Capabilities.ContextWindow = model.TokenLimits.MaxInputTokens
		info.Capabilities.MaxOutput = model.TokenLimits.MaxOutputTokens
	}
	if model.RateMultiplier > 0 {
		info.Credits = strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", model.RateMultiplier), "0"), ".") + "x"
	}
	return info
}
