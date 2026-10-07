package zhipu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// Quota probes a Coding Plan key. Pay-as-you-go has no public balance
// endpoint, so it returns a nil snapshot and the console shows unknown.
//
// The upstream returns used percentages, not token totals. Each TOKENS_LIMIT
// row becomes one window. Unit 3 is the 5-hour window and unit 6 is the
// weekly window; anything else is labeled from its unit number.
func (c *Client) Quota(ctx context.Context, accountID string) (*providers.QuotaInfo, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if NormalizeMode(credential.Mode) != ModeCoding {
		return nil, nil
	}
	base, err := credential.ChatBase()
	if err != nil {
		return nil, err
	}
	target, err := quotaURL(base, credential.Organization)
	if err != nil {
		return nil, err
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	// The quota endpoint wants the raw key, not a Bearer prefix.
	req.Header.Set("Authorization", credential.APIKey)
	req.Header.Set("Accept", "application/json")
	if org := strings.TrimSpace(credential.Organization); org != "" {
		req.Header.Set("bigmodel-organization", org)
		if project := strings.TrimSpace(credential.Project); project != "" {
			req.Header.Set("bigmodel-project", project)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, newProviderError(resp.StatusCode, string(body))
	}
	return parseQuota(body, time.Now().UTC())
}

func quotaURL(chatBase, organization string) (string, error) {
	parsed, err := url.Parse(chatBase)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("zhipu quota: invalid chat base")
	}
	host := CNQuotaHost
	if strings.Contains(strings.ToLower(parsed.Host), "z.ai") {
		host = GlobalQuotaHost
	}
	target := host + QuotaPath
	if strings.TrimSpace(organization) != "" {
		target += "?type=2"
	}
	return target, nil
}

type quotaPayload struct {
	Success *bool  `json:"success"`
	Msg     string `json:"msg"`
	Data    struct {
		Level  string       `json:"level"`
		Limits []quotaLimit `json:"limits"`
	} `json:"data"`
}

type quotaLimit struct {
	Type          string          `json:"type"`
	Unit          int             `json:"unit"`
	Percentage    float64         `json:"percentage"`
	NextResetTime json.RawMessage `json:"nextResetTime"`
}

func parseQuota(body []byte, fetchedAt time.Time) (*providers.QuotaInfo, error) {
	var payload quotaPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("zhipu quota: %w", err)
	}
	if payload.Success != nil && !*payload.Success {
		message := strings.TrimSpace(payload.Msg)
		if message == "" {
			message = "zhipu quota probe failed"
		}
		return nil, fmt.Errorf("%s", message)
	}
	windows := make([]providers.QuotaWindow, 0, len(payload.Data.Limits))
	tightest := -1.0
	for _, limit := range payload.Data.Limits {
		kind := strings.ToUpper(strings.TrimSpace(limit.Type))
		if kind != "" && kind != "TOKENS_LIMIT" {
			continue
		}
		window := providers.QuotaWindow{
			ID:         quotaWindowID(limit.Unit),
			Label:      quotaWindowLabel(limit.Unit),
			Used:       clampPercent(limit.Percentage),
			Total:      100,
			Remaining:  clampPercent(100 - limit.Percentage),
			Percentage: clampPercent(limit.Percentage),
			Unit:       QuotaUnit,
			ResetAt:    resetTime(limit.NextResetTime),
			Exceeded:   limit.Percentage >= 100,
		}
		windows = append(windows, window)
		if tightest < 0 || window.Percentage > tightest {
			tightest = window.Percentage
		}
	}
	if tightest < 0 {
		tightest = 0
	}
	return &providers.QuotaInfo{
		Used:       tightest,
		Total:      100,
		Remaining:  clampPercent(100 - tightest),
		Percentage: tightest,
		Unit:       QuotaUnit,
		Exceeded:   tightest >= 100,
		FetchedAt:  fetchedAt.Format(time.RFC3339),
		Windows:    windows,
		ProviderID: "zhipu",
		Plan:       strings.TrimSpace(payload.Data.Level),
	}, nil
}

func quotaWindowID(unit int) string {
	switch unit {
	case 3:
		return "5h"
	case 6:
		return "weekly"
	default:
		if unit <= 0 {
			return "usage"
		}
		return fmt.Sprintf("unit-%d", unit)
	}
}

func quotaWindowLabel(unit int) string {
	switch unit {
	case 3:
		return "5-Hour Limit"
	case 6:
		return "Weekly Limit"
	default:
		return "Usage"
	}
}

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func resetTime(raw json.RawMessage) string {
	raw = bytesTrim(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if raw[0] == '"' {
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return ""
		}
		return strings.TrimSpace(text)
	}
	var millis int64
	if json.Unmarshal(raw, &millis) != nil || millis <= 0 {
		return ""
	}
	return time.UnixMilli(millis).UTC().Format(time.RFC3339)
}

func bytesTrim(raw []byte) []byte {
	return []byte(strings.TrimSpace(string(raw)))
}
