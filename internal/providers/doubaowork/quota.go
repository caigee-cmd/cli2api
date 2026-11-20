package doubaowork

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// Quota aggregates the subscription summary, activity gift cards and the
// read-only check-in status into provider-neutral windows. Each source degrades
// independently: a transient failure on one does not blank the whole quota, and
// a quota error never flips account readiness.
func (c *Client) Quota(ctx context.Context, accountID string) (*providers.QuotaInfo, error) {
	sess := c.sessionFor(accountID)
	if err := sess.ready(ctx); err != nil {
		return nil, err
	}

	summary := c.tryJSON(ctx, sess, "POST", "/alice/commerce/sale/subscription/quota/summary/", map[string]any{"product_line": "membership"})
	if summary == nil {
		return &providers.QuotaInfo{
			ProviderID: "doubaowork",
			FetchedAt:  time.Now().UTC().Format(time.RFC3339),
		}, nil
	}
	overview := c.tryJSON(ctx, sess, "POST", "/alice/commerce/sale/subscription/overview/", map[string]any{"product_line": "membership"})
	cards := c.tryJSON(ctx, sess, "POST", "/alice/commerce/marketing/card/list/", map[string]any{
		"status_in":         []int{1, 2, 3, 4},
		"with_limit_status": true,
	})

	windows := make([]providers.QuotaWindow, 0)
	tightest := -1.0
	for _, section := range subscriptionSections(summary) {
		sectionData, _ := summary[section.key].(map[string]any)
		if sectionData == nil {
			continue
		}
		exhausted := asBool(sectionData["usage_exhausted"])
		for _, groupRaw := range asSlice(sectionData["window_limit_groups"]) {
			group, ok := groupRaw.(map[string]any)
			if !ok {
				continue
			}
			featureName := firstNonEmpty(asString(group["feature_group_name"]), section.label)
			limits := asSlice(group["window_limits"])
			for index, limitRaw := range limits {
				limit, ok := limitRaw.(map[string]any)
				if !ok {
					continue
				}
				amount, hasAmount := numOf(limit["used_amount"])
				limitTotal, hasLimit := numOf(limit["total_amount"])
				percent, hasPercent := numOf(limit["used_percent"])
				label := featureName
				if len(limits) > 1 {
					label = featureName + " · 窗口" + itoa(index+1)
				}
				window := providers.QuotaWindow{
					ID:       section.key + "-" + itoa(index),
					Label:    label,
					Unit:     "积分",
					ResetAt:  isoTime(limit["end_time"]),
					Exceeded: exhausted,
				}
				if hasAmount && hasLimit {
					window.Used = amount
					window.Total = limitTotal
					window.Remaining = math.Max(0, limitTotal-amount)
				}
				if hasPercent {
					window.Percentage = percent
					if tightest < 0 || percent > tightest {
						tightest = percent
					}
				}
				if asBool(limit["less_than_one_percent"]) {
					window.Percentage = math.Max(window.Percentage, 1)
				}
				windows = append(windows, window)
			}
		}
	}
	windows = append(windows, cardWindows(cards)...)

	plan := ""
	if subscription, ok := overview["current_subscription"].(map[string]any); ok {
		if display, ok := subscription["display"].(map[string]any); ok {
			plan = firstNonEmpty(asString(display["short_name"]), asString(display["product_name"]))
		}
	}
	if plan == "" {
		plan = "豆包工作"
	}
	if asBool(overview["is_membership_gift"]) && !strings.Contains(plan, "赠送") {
		plan += "（赠送）"
	}

	if tightest < 0 {
		tightest = 0
	}

	return &providers.QuotaInfo{
		Used:       tightest,
		Total:      100,
		Remaining:  math.Max(0, 100-tightest),
		Percentage: tightest,
		Unit:       "积分",
		Exceeded:   tightest >= 100,
		FetchedAt:  time.Now().UTC().Format(time.RFC3339),
		Windows:    windows,
		ProviderID: "doubaowork",
		Plan:       plan,
	}, nil
}

type quotaSection struct {
	key   string
	label string
}

func subscriptionSections(summary map[string]any) []quotaSection {
	sections := []quotaSection{
		{key: "window_limit_section", label: "订阅额度"},
		{key: "incentive_quota_section", label: "活动赠送"},
		{key: "enterprise_window_limit_section", label: "企业额度"},
	}
	present := make([]quotaSection, 0, len(sections))
	for _, section := range sections {
		if data, ok := summary[section.key].(map[string]any); ok {
			if len(asSlice(data["window_limit_groups"])) > 0 {
				present = append(present, section)
			}
		}
	}
	return present
}

// cardWindows renders the activity gift cards, which often hold the only quota
// a free account has.
func cardWindows(cards map[string]any) []providers.QuotaWindow {
	if cards == nil {
		return nil
	}
	list := asSlice(digSlice(cards, "data", "cards"))
	if list == nil {
		list = asSlice(cards["cards"])
	}
	windows := make([]providers.QuotaWindow, 0, len(list))
	for index, cardRaw := range list {
		card, ok := cardRaw.(map[string]any)
		if !ok {
			continue
		}
		track, _ := card["track_params"].(map[string]any)
		remain, hasRemain := numOf(track["card_balance"])
		window := providers.QuotaWindow{
			ID:    "card-" + itoa(index),
			Label: firstNonEmpty(asString(card["source_display_text"]), "活动赠送"),
			Unit:  "次",
		}
		if hasRemain {
			window.Remaining = remain
			window.Total = remain
		}
		windows = append(windows, window)
	}
	return windows
}

// checkinWindow renders the read-only check-in status as a quota window.
func checkinWindow(state checkinState) []providers.QuotaWindow {
	if !state.Found {
		return nil
	}
	note := "今日未签"
	if state.Claimed {
		note = "今日已签"
		if state.Streak > 0 {
			note += " · 连续 " + itoa(state.Streak) + " 天"
		}
	} else if state.Points > 0 {
		note += " · 可领 " + itoa(state.Points) + " 额度"
	}
	return []providers.QuotaWindow{{
		ID:        "checkin",
		Label:     "每日签到（" + note + "）",
		Unit:      "积分",
		Remaining: float64(state.Points),
	}}
}

func (c *Client) tryJSON(ctx context.Context, sess *session, method, path string, body any) map[string]any {
	data, err := sess.json(ctx, method, path, body, nil)
	if err != nil {
		return nil
	}
	return data
}

func numOf(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case string:
		cleaned := strings.ReplaceAll(strings.TrimSpace(typed), ",", "")
		if cleaned == "" {
			return 0, false
		}
		parsed, err := strconv.ParseFloat(cleaned, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func isoTime(value any) string {
	seconds, ok := numOf(value)
	if !ok || seconds <= 0 {
		return ""
	}
	if seconds < 1e12 {
		return time.Unix(int64(seconds), 0).UTC().Format(time.RFC3339)
	}
	return time.UnixMilli(int64(seconds)).UTC().Format(time.RFC3339)
}
