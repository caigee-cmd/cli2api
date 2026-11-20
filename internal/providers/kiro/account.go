package kiro

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

const (
	maxProfilePages       = 20
	maxProfileResponse    = 1 << 20
	maxProfileError       = 64 << 10
	profileRetryAttempts  = 3
	profileRetryBaseDelay = 200 * time.Millisecond
)

// ensureProfileArn resolves the profile used by REST/model requests. Kiro's
// social and IDC exports often omit it even though ListAvailableProfiles can
// return one for the same bearer.
func (c *Client) ensureProfileArn(ctx context.Context, accountID string, client *http.Client, cred Credential) (Credential, error) {
	if cred.isAPIKey() || cred.isBuilderID() {
		return cred, nil
	}
	if strings.TrimSpace(cred.ProfileArn) != "" {
		canonical, _, ok := parseProfileArn(cred.ProfileArn)
		if !ok {
			return cred, fmt.Errorf("kiro credential contains an invalid profile arn")
		}
		if canonical == cred.ProfileArn {
			return cred, nil
		}
		cred.ProfileArn = canonical
		encoded, err := cred.Encode()
		if err != nil {
			return cred, err
		}
		if err := c.store.SaveCredentialPayload(ctx, accountID, CredentialFormat, encoded); err != nil {
			return cred, err
		}
		return cred, nil
	}
	profiles, err := c.listProfilesAcrossRegions(ctx, client, cred)
	if err != nil {
		return cred, err
	}
	if len(profiles) == 0 {
		return cred, nil
	}
	canonical, _, ok := parseProfileArn(profiles[0])
	if !ok {
		return cred, fmt.Errorf("kiro profile response contained an invalid arn")
	}
	cred.ProfileArn = canonical
	encoded, err := cred.Encode()
	if err != nil {
		return cred, err
	}
	if err := c.store.SaveCredentialPayload(ctx, accountID, CredentialFormat, encoded); err != nil {
		return cred, err
	}
	return cred, nil
}

func profileRegions(cred Credential) []string {
	seen := map[string]bool{}
	regions := make([]string, 0, 2)
	add := func(region string) {
		region = strings.ToLower(strings.TrimSpace(region))
		if !profileRegionPattern.MatchString(region) || seen[region] {
			return
		}
		seen[region] = true
		regions = append(regions, region)
	}
	add(regionFromProfile(cred.ProfileArn))
	add(DefaultRegion)
	add("eu-central-1")
	return regions
}

func (c *Client) listProfilesAcrossRegions(ctx context.Context, client *http.Client, cred Credential) ([]string, error) {
	var errs []string
	seen := map[string]bool{}
	var profiles []string
	for _, region := range profileRegions(cred) {
		items, err := c.listProfilesInRegion(ctx, client, cred, region)
		if err != nil {
			errs = append(errs, region+": "+err.Error())
			continue
		}
		for _, item := range items {
			if !seen[item] {
				seen[item] = true
				profiles = append(profiles, item)
			}
		}
		if len(profiles) > 0 {
			return profiles, nil
		}
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("kiro profile lookup failed: %s", strings.Join(errs, "; "))
	}
	return nil, nil
}

func (c *Client) listProfilesInRegion(ctx context.Context, client *http.Client, cred Credential, region string) ([]string, error) {
	endpoint := profileBase(region) + "/ListAvailableProfiles"
	var profiles []string
	next := ""
	for page := 0; page < maxProfilePages; page++ {
		// ListAvailableProfiles currently rejects values above 10, even though the
		// model endpoint accepts 50. Keep this request at the service limit.
		body := map[string]any{"maxResults": 10}
		if next != "" {
			body["nextToken"] = next
		}
		encoded, _ := json.Marshal(body)
		var raw []byte
		var err error
		for attempt := 0; attempt < profileRetryAttempts; attempt++ {
			raw, err = c.postProfilePage(ctx, client, cred, endpoint, encoded)
			if err == nil {
				break
			}
			if ctx.Err() != nil || attempt == profileRetryAttempts-1 || !retryProfileError(err) {
				return nil, err
			}
			select {
			case <-time.After(profileRetryBaseDelay * time.Duration(1<<attempt)):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		var parsed struct {
			Profiles []struct {
				ARN string `json:"arn"`
			} `json:"profiles"`
			NextToken string `json:"nextToken"`
		}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, err
		}
		for _, profile := range parsed.Profiles {
			canonical, _, ok := parseProfileArn(profile.ARN)
			if ok {
				profiles = append(profiles, canonical)
			}
		}
		next = strings.TrimSpace(parsed.NextToken)
		if next == "" {
			return profiles, nil
		}
	}
	return nil, fmt.Errorf("kiro profile pagination exceeded %d pages", maxProfilePages)
}

func retryProfileError(err error) bool {
	var providerErr *providers.Error
	if !errors.As(err, &providerErr) {
		return true
	}
	return providerErr.Status == http.StatusTooManyRequests || providerErr.Status >= 500
}

func (c *Client) postProfilePage(ctx context.Context, client *http.Client, cred Credential, endpoint string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	applyRuntimeHeaders(req, cred, req.URL.Host)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	limit := int64(maxProfileResponse)
	if resp.StatusCode != http.StatusOK {
		limit = maxProfileError
	}
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if readErr != nil {
		return nil, readErr
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("profile response exceeds %d bytes", limit)
	}
	if resp.StatusCode != http.StatusOK {
		noteVersionGate(client, string(raw))
		return nil, newProviderError(resp.StatusCode, string(raw))
	}
	return raw, nil
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
	cred, err = c.ensureProfileArn(ctx, accountID, client, cred)
	if err != nil {
		return nil, err
	}
	_ = waitForIDEVersion(ctx, client)
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
	// The CREDIT breakdown is metered in credits even though Kiro labels its
	// unit INVOCATIONS; other resource types keep the unit Kiro states.
	if breakdown != nil && !strings.EqualFold(breakdown.ResourceType, "CREDIT") {
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
	case strings.Contains(raw, "PRO_PLUS"), strings.Contains(raw, "PROPLUS"), strings.Contains(raw, "PRO+"):
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

type listModelsPage struct {
	Models    []upstreamModel `json:"models"`
	NextToken string          `json:"nextToken"`
}

func (c *Client) listModels(ctx context.Context, client *http.Client, cred Credential) ([]providers.ModelInfo, error) {
	base := modelBase(cred) + "/ListAvailableModels?origin=AI_EDITOR&maxResults=50"
	base = withProfileQuery(base, cred.requestProfileArn())
	var models []providers.ModelInfo
	seenModels := map[string]struct{}{}
	seenTokens := map[string]struct{}{}
	next := ""
	for page := 0; page < maxModelPages; page++ {
		endpoint := base
		if next != "" {
			endpoint += "&nextToken=" + url.QueryEscape(next)
		}
		body, err := c.getJSON(ctx, client, cred, endpoint)
		if err != nil {
			return nil, err
		}
		var parsed listModelsPage
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, err
		}
		for _, model := range parsed.Models {
			modelID := strings.TrimSpace(model.ModelID)
			if modelID == "" {
				continue
			}
			key := strings.ToLower(modelID)
			if _, duplicate := seenModels[key]; duplicate {
				continue
			}
			seenModels[key] = struct{}{}
			models = append(models, modelInfo(model))
		}
		next = strings.TrimSpace(parsed.NextToken)
		if next == "" {
			break
		}
		if _, repeated := seenTokens[next]; repeated {
			return nil, fmt.Errorf("kiro model pagination repeated next token")
		}
		seenTokens[next] = struct{}{}
		if page == maxModelPages-1 {
			return nil, fmt.Errorf("kiro model pagination exceeded %d pages", maxModelPages)
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
