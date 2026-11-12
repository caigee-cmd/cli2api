package command

import (
	"context"
	"math"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// Quota reports Command Code usage. /alpha/billing/credits returns the credit
// balance and the rolling 5-hour/weekly windows (root-level `windowLimits`).
// The headline meter and every window use only those upstream values — no
// local plan table and no synthesized total. Command Code does not expose a
// monthly window here, so we do not invent one.
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
	windows := make([]providers.QuotaWindow, 0, 2)
	if window := rollingWindow("fiveHour", "5-Hour Limit", balance.FiveHour); window != nil {
		windows = append(windows, *window)
	}
	if window := rollingWindow("weeklyLimit", "Weekly Limit", balance.Weekly); window != nil {
		windows = append(windows, *window)
	}

	// Headline meter follows the first real upstream window we have. No local
	// plan table and no reverse-derived usage: if the upstream returned nothing,
	// the card shows 0/0 rather than a fabricated number.
	var used, total, remaining, percentage float64
	if len(windows) > 0 {
		used = windows[0].Used
		total = windows[0].Total
		remaining = windows[0].Remaining
		percentage = windows[0].Percentage
	}
	info := &providers.QuotaInfo{
		Used:       used,
		Total:      total,
		Remaining:  remaining,
		Percentage: percentage,
		Unit:       QuotaUnit,
		Exceeded:   remaining <= 0,
		FetchedAt:  fetchedAt.Format(time.RFC3339),
		ProviderID: "command",
		Windows:    windows,
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

func msToRFC3339(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}
