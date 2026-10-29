package codearts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// quotaMeter is one row of the official subscription statistics document.
type quotaMeter struct {
	Name         string
	Label        string
	UsedTokens   int64
	Allowance    int64
	CreditTotal  float64
	CreditUsed   float64
	CreditRemain float64
	Percent      float64
	HasPercent   bool
}

// statistics is the parsed /snap-manager/v1/statistics/plugin response.
type statistics struct {
	ResetDate string
	Plan      string
	PlanName  string
	Meters    []quotaMeter
	Models    []statisticsModel
}

type statisticsModel struct {
	ID     string
	Name   string
	Desc   string
	Enable bool
}

func (s statistics) empty() bool { return len(s.Meters) == 0 }

func (m quotaMeter) unit() string {
	if m.CreditTotal > 0 || m.CreditUsed > 0 || m.CreditRemain > 0 {
		return "积分"
	}
	if m.UsedTokens > 0 || m.Allowance > 0 {
		return "tokens"
	}
	return ""
}

func (m quotaMeter) used() float64 {
	if m.CreditTotal > 0 || m.CreditUsed > 0 || m.CreditRemain > 0 {
		return m.CreditUsed
	}
	return float64(m.UsedTokens)
}

func (m quotaMeter) total() float64 {
	if m.CreditTotal > 0 {
		return m.CreditTotal
	}
	// package_token_amount=1 is the upstream's placeholder when the trial
	// account has no chat-token package; it is a counter, not a 1-token quota.
	if m.Allowance > 1 {
		return float64(m.Allowance)
	}
	return 0
}

func (m quotaMeter) remaining() float64 {
	if m.CreditRemain > 0 || m.CreditTotal > 0 {
		if m.CreditRemain > 0 {
			return m.CreditRemain
		}
		return m.CreditTotal - m.CreditUsed
	}
	if m.Allowance > 1 {
		return float64(m.Allowance - m.UsedTokens)
	}
	return 0
}

func (m quotaMeter) percentage() float64 {
	if m.total() <= 0 {
		return 0
	}
	if m.HasPercent {
		return m.Percent
	}
	return m.used() / m.total() * 100
}

// Quota reads the package meters. The activity/benefit balance is intentionally
// not merged: it is a different accounting pool and mixing it would report a
// fake total.
func (c *Client) Quota(ctx context.Context, accountID string) (*providers.QuotaInfo, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return nil, err
	}
	stats, err := c.fetchStatistics(ctx, accountID, credential)
	if err != nil {
		return nil, err
	}
	if stats.empty() {
		return nil, fmt.Errorf("codearts statistics returned no visible quota meters")
	}
	info := &providers.QuotaInfo{
		ProviderID: ProviderID,
		Plan:       stats.PlanName,
		FetchedAt:  c.now().Format(time.RFC3339),
	}
	for _, meter := range stats.Meters {
		window := providers.QuotaWindow{
			ID:         meter.Name,
			Label:      meter.Label,
			Used:       meter.used(),
			Total:      meter.total(),
			Remaining:  meter.remaining(),
			Percentage: meter.percentage(),
			Unit:       meter.unit(),
			ResetAt:    stats.ResetDate,
			Exceeded:   meter.remaining() <= 0 && meter.total() > 0,
		}
		info.Windows = append(info.Windows, window)
	}
	// The package-credit meter is the plan-level number; fall back to the first
	// visible meter when a plan does not expose it.
	primary := info.Windows[0]
	for _, window := range info.Windows {
		if window.ID == "usageTotalPackageCredit" {
			primary = window
			break
		}
	}
	info.Used = primary.Used
	info.Total = primary.Total
	info.Remaining = primary.Remaining
	info.Percentage = primary.Percentage
	info.Unit = primary.Unit
	info.Exceeded = primary.Exceeded
	return info, nil
}

func (c *Client) fetchStatistics(ctx context.Context, accountID string, credential Credential) (statistics, error) {
	requestCtx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, c.apiBase(credential)+statisticsPath, nil)
	if err != nil {
		return statistics{}, err
	}
	// The statistics endpoint signs this exact header set; do not add
	// Content-Type or Agent-Type, which would change the signature.
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Language", Language)
	req.Header.Set("plugin-name", PluginName)
	req.Header.Set("plugin-version", PluginVersion)
	signRequest(req, nil, credential.Sign())
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return statistics{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return statistics{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode >= 400 {
		return statistics{}, newProviderError(resp.StatusCode, string(raw))
	}
	return parseStatistics(raw)
}

func parseStatistics(raw []byte) (statistics, error) {
	var parsed struct {
		EndDate string `json:"end_date"`
		Metrics []struct {
			Name         string   `json:"name"`
			Show         bool     `json:"show"`
			UsedTokens   int64    `json:"usage_token_num"`
			Allowance    int64    `json:"package_token_amount"`
			CreditTotal  float64  `json:"package_credit_amount"`
			CreditUsed   float64  `json:"package_credit_used"`
			CreditRemain float64  `json:"package_credit_remain"`
			Value        *float64 `json:"value"`
		} `json:"metrics"`
		Package *struct {
			SpecCode      string `json:"spec_code"`
			PackageNameEN string `json:"package_name_en"`
			PackageNameCN string `json:"package_name_cn"`
		} `json:"package"`
		ModelMetrics []struct {
			ModelID   string `json:"model_id"`
			ModelName string `json:"model_name"`
			ModelDesc string `json:"model_desc"`
			Enable    bool   `json:"enable"`
		} `json:"model_metrics"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return statistics{}, fmt.Errorf("parse codearts statistics: %w", err)
	}
	out := statistics{ResetDate: strings.TrimSpace(parsed.EndDate)}
	if parsed.Package != nil {
		out.Plan = strings.TrimSpace(parsed.Package.SpecCode)
		out.PlanName = firstNonEmpty(parsed.Package.PackageNameEN, parsed.Package.PackageNameCN)
	}
	for _, model := range parsed.ModelMetrics {
		id := strings.TrimSpace(firstNonEmpty(model.ModelID, model.ModelName))
		if id == "" {
			continue
		}
		out.Models = append(out.Models, statisticsModel{
			ID: id, Name: strings.TrimSpace(model.ModelName),
			Desc: strings.TrimSpace(model.ModelDesc), Enable: model.Enable,
		})
	}
	for _, metric := range parsed.Metrics {
		name := strings.TrimSpace(metric.Name)
		if name == "" || !metric.Show {
			continue
		}
		meter := quotaMeter{
			Name:         name,
			Label:        quotaMeterLabel(name),
			UsedTokens:   metric.UsedTokens,
			Allowance:    metric.Allowance,
			CreditTotal:  metric.CreditTotal,
			CreditUsed:   metric.CreditUsed,
			CreditRemain: metric.CreditRemain,
		}
		if metric.Value != nil && *metric.Value >= 0 {
			meter.Percent = *metric.Value
			meter.HasPercent = true
		}
		if meter.Label == "" {
			meter.Label = name
		}
		if meter.UsedTokens == 0 && meter.Allowance <= 1 &&
			meter.CreditTotal == 0 && meter.CreditUsed == 0 && meter.CreditRemain == 0 {
			// A zero-credit row or the placeholder 1-token counter is noise.
			continue
		}
		out.Meters = append(out.Meters, meter)
	}
	return out, nil
}

// quotaMeterLabel is copied from the verified upstream names; unknown names
// keep their raw id rather than being hidden.
func quotaMeterLabel(name string) string {
	switch name {
	case "usageDataCodeCompletions":
		return "代码补全额度"
	case "usageDataChatMessages":
		return "对话消息额度"
	case "usageTokenChatMessages":
		return "对话 token 额度"
	case "usageTotalPackageCredit":
		return "套餐积分"
	case "usageBasicPackageCredit":
		return "基础包积分"
	case "usageOnDemandPackageCredit":
		return "按需付费积分"
	case "usageBonusPackageCredit":
		return "赠送积分"
	default:
		return ""
	}
}
