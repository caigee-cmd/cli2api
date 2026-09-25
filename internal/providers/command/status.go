package command

import (
	"context"
	"math"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// Quota reports Command Code usage. /alpha/billing/credits returns the credit
// balance, the plan id, and the rolling 5-hour/weekly windows
// (root-level `windowLimits`); the monthly window is derived from the plan's
// credit allowance minus the remaining monthly credits. Command Code does not
// expose a monthly reset time here, so the monthly window carries no resetAt.
//
// A failure returns a nil snapshot so the console card shows "unknown" rather
// than a wrong number.
func (c *Client) Quota(ctx context.Context, accountID string) (*providers.QuotaInfo, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return nil, err
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return nil, err
	}
	balance, err := c.fetchCredits(ctx, client, credential)
	if err != nil {
		return nil, err
	}
	return quotaFromCredits(balance, time.Now().UTC()), nil
}

func quotaFromCredits(balance credits, fetchedAt time.Time) *providers.QuotaInfo {
	windows := make([]providers.QuotaWindow, 0, 3)
	if window := rollingWindow("fiveHour", "5-Hour Limit", balance.FiveHour); window != nil {
		windows = append(windows, *window)
	}
	if window := rollingWindow("weeklyLimit", "Weekly Limit", balance.Weekly); window != nil {
		windows = append(windows, *window)
	}
	if window := monthlyWindow(balance); window != nil {
		windows = append(windows, *window)
	}

	totalRemaining := balance.total()
	info := &providers.QuotaInfo{
		Used:       0,
		Total:      0,
		Remaining:  totalRemaining,
		Percentage: 0,
		Unit:       QuotaUnit,
		Exceeded:   totalRemaining <= 0,
		FetchedAt:  fetchedAt.Format(time.RFC3339),
		ProviderID: "command",
		Windows:    windows,
	}
	// The card's headline meter follows the monthly plan window when the plan is
	// known, matching the CLI's own usage percentage.
	if monthly := monthlyWindow(balance); monthly != nil {
		info.Used = monthly.Used
		info.Total = monthly.Total
		info.Remaining = monthly.Remaining
		info.Percentage = monthly.Percentage
	}
	return info
}

// rollingWindow converts a windowLimits entry into a QuotaWindow. used/cap are
// credit-denominated; resetAt is a millisecond epoch.
func rollingWindow(id, label string, limit *windowLimit) *providers.QuotaWindow {
	if limit == nil || limit.Cap <= 0 {
		return nil
	}
	used := math.Max(0, limit.Used)
	remaining := math.Max(0, limit.Cap-used)
	window := &providers.QuotaWindow{
		ID:         id,
		Label:      label,
		Used:       used,
		Total:      limit.Cap,
		Remaining:  remaining,
		Percentage: math.Min(100, used/limit.Cap*100),
		Unit:       QuotaUnit,
		Exceeded:   remaining <= 0,
		ResetAt:    msToRFC3339(limit.ResetAt),
	}
	return window
}

// monthlyWindow derives the monthly allowance window from the plan's total
// credits and the remaining monthly credits. It is nil when the plan is unknown.
func monthlyWindow(balance credits) *providers.QuotaWindow {
	total, ok := planTotalCredits(balance.PlanID)
	if !ok || total <= 0 {
		return nil
	}
	remaining := math.Max(0, math.Min(balance.Monthly, total))
	used := math.Max(0, total-remaining)
	return &providers.QuotaWindow{
		ID:         "monthlyLimit",
		Label:      "Monthly Limit",
		Used:       used,
		Total:      total,
		Remaining:  remaining,
		Percentage: math.Min(100, used/total*100),
		Unit:       QuotaUnit,
		Exceeded:   remaining <= 0,
	}
}

func msToRFC3339(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}
