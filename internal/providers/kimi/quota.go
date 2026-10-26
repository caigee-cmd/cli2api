package kimi

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

// QuotaUnit labels the subscription windows the managed endpoint reports. Kimi
// returns a used fraction per window, not a token balance.
const QuotaUnit = "percent"

// quotaWindowKeys are the four windows the managed /usages endpoint reports,
// in the order the console should show them.
var quotaWindowKeys = []struct {
	field string
	id    string
	label string
}{
	{"limit_5h", "5h", "5-Hour Limit"},
	{"limit_7d", "7d", "7-Day Limit"},
	{"limit_month_total", "month", "Monthly Limit"},
	{"limit_month_code", "month-code", "Monthly Code Limit"},
}

// Quota implements providers.AccountProber. Kimi's managed endpoint reports four
// rolling subscription windows as used fractions. Accounts without the managed
// API (404) have no usage surface and return a nil snapshot, which the console
// renders as unknown instead of zero.
func (c *Client) Quota(ctx context.Context, accountID string) (*providers.QuotaInfo, error) {
	credential, token, err := c.accessToken(ctx, accountID)
	if err != nil {
		return nil, err
	}
	base := c.apiBase(credential)
	requestCtx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, base+usagesPath, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return nil, err
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
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode >= 300 {
		return nil, newProviderError(resp.StatusCode, string(body))
	}
	return ParseQuotaJSON(body, credential.Plan, c.now())
}

// ParseQuotaJSON maps the /usages payload onto the gateway quota contract. A
// window whose used_ratio is missing or non-numeric is dropped rather than
// shown as zero usage.
func ParseQuotaJSON(raw []byte, plan string, fetchedAt time.Time) (*providers.QuotaInfo, error) {
	var payload struct {
		Usages map[string]struct {
			UsedRatio json.RawMessage `json:"used_ratio"`
			ResetTime string          `json:"reset_time"`
		} `json:"usages"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("kimi quota: %w", err)
	}
	windows := make([]providers.QuotaWindow, 0, len(quotaWindowKeys))
	tightest := 0.0
	for _, key := range quotaWindowKeys {
		entry, ok := payload.Usages[key.field]
		if !ok {
			continue
		}
		ratio, ok := parseRatio(entry.UsedRatio)
		if !ok {
			continue
		}
		used := clampPercent(ratio * 100)
		window := providers.QuotaWindow{
			ID:         key.id,
			Label:      key.label,
			Used:       used,
			Total:      100,
			Remaining:  clampPercent(100 - used),
			Percentage: used,
			Unit:       QuotaUnit,
			ResetAt:    strings.TrimSpace(entry.ResetTime),
			Exceeded:   used >= 100,
		}
		windows = append(windows, window)
		if window.Percentage > tightest {
			tightest = window.Percentage
		}
	}
	if len(windows) == 0 {
		return nil, nil
	}
	return &providers.QuotaInfo{
		Used:       tightest,
		Total:      100,
		Remaining:  clampPercent(100 - tightest),
		Percentage: tightest,
		Unit:       QuotaUnit,
		Exceeded:   tightest >= 100,
		FetchedAt:  fetchedAt.UTC().Format(time.RFC3339),
		Windows:    windows,
		ProviderID: ProviderID,
		Plan:       strings.TrimSpace(plan),
	}, nil
}

func parseRatio(raw json.RawMessage) (float64, bool) {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return 0, false
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err == nil {
		return value, true
	}
	var quoted string
	if err := json.Unmarshal(raw, &quoted); err == nil {
		var parsed float64
		if _, err := fmt.Sscanf(strings.TrimSpace(quoted), "%f", &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
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
