package codex

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// quotaFromHeaders converts the x-codex-* rate-limit headers the codex backend
// attaches to every response into QuotaInfo windows. Header names observed:
// x-codex-primary-used-percent, x-codex-primary-reset-after-seconds (5h window),
// x-codex-secondary-used-percent, x-codex-secondary-reset-after-seconds (7d),
// plus x-codex-*-window-minutes variants and x-codex-plan-type.
func quotaFromHeaders(h http.Header) *providers.QuotaInfo {
	primary := windowFromHeaders(h, "X-Codex-Primary-", "fiveHour", "5-hour limit")
	secondary := windowFromHeaders(h, "X-Codex-Secondary-", "weeklyLimit", "weekly limit")
	return quotaFromWindows(primary, secondary, h.Get("X-Codex-Plan-Type"))
}

// quotaFromUsage decodes GET /wham/usage. The current ChatGPT payload is
// rate_limit.primary_window / secondary_window, each with used_percent,
// limit_window_seconds, reset_after_seconds, and reset_at. Older responses
// and the websocket codex.rate_limits event nest the same windows under
// rate_limits.primary / secondary with window_minutes instead.
func quotaFromUsage(body []byte) *providers.QuotaInfo {
	var payload usagePayload
	if json.Unmarshal(body, &payload) != nil {
		return nil
	}
	primary, secondary := payload.windows()
	return quotaFromWindows(primary, secondary, payload.PlanType)
}

type usagePayload struct {
	PlanType   string             `json:"plan_type"`
	RateLimit  *codexLimitDetails `json:"rate_limit"`
	RateLimits struct {
		Primary   codexRateWindow `json:"primary"`
		Secondary codexRateWindow `json:"secondary"`
	} `json:"rate_limits"`
}

func (p usagePayload) windows() (*providers.QuotaWindow, *providers.QuotaWindow) {
	if p.RateLimit != nil {
		primary := p.RateLimit.PrimaryWindow.window("fiveHour", "5-hour limit")
		secondary := p.RateLimit.SecondaryWindow.window("weeklyLimit", "weekly limit")
		if primary != nil || secondary != nil {
			return primary, secondary
		}
	}
	return p.RateLimits.Primary.window("fiveHour", "5-hour limit"),
		p.RateLimits.Secondary.window("weeklyLimit", "weekly limit")
}

type codexLimitDetails struct {
	PrimaryWindow   codexRateWindow `json:"primary_window"`
	SecondaryWindow codexRateWindow `json:"secondary_window"`
}

type codexRateWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	WindowMinutes      int64   `json:"window_minutes"`
	LimitWindowSeconds int64   `json:"limit_window_seconds"`
	ResetAfterSeconds  int64   `json:"reset_after_seconds"`
	ResetAt            int64   `json:"reset_at"`
}

func (w codexRateWindow) window(id, label string) *providers.QuotaWindow {
	minutes := w.WindowMinutes
	if minutes <= 0 && w.LimitWindowSeconds > 0 {
		minutes = w.LimitWindowSeconds / 60
	}
	if w.UsedPercent < 0 || w.UsedPercent > 100 || minutes <= 0 {
		return nil
	}
	if w.ResetAfterSeconds < 0 && w.ResetAt <= 0 {
		return nil
	}
	window := &providers.QuotaWindow{
		ID:         id,
		Label:      label,
		Used:       w.UsedPercent,
		Total:      100,
		Remaining:  100 - w.UsedPercent,
		Percentage: w.UsedPercent,
		Unit:       "percent",
		Exceeded:   w.UsedPercent >= 100,
	}
	switch {
	case w.ResetAt > 0:
		window.ResetAt = time.Unix(w.ResetAt, 0).UTC().Format(time.RFC3339)
	case w.ResetAfterSeconds >= 0:
		window.ResetAt = time.Now().Add(time.Duration(w.ResetAfterSeconds) * time.Second).UTC().Format(time.RFC3339)
	}
	return window
}

func quotaFromWindows(primary, secondary *providers.QuotaWindow, planType string) *providers.QuotaInfo {
	if primary == nil && secondary == nil {
		return nil
	}
	windows := []providers.QuotaWindow{}
	var usedPct float64
	exceeded := false
	if primary != nil {
		windows = append(windows, *primary)
		usedPct = primary.Percentage
		exceeded = primary.Exceeded
	}
	if secondary != nil {
		windows = append(windows, *secondary)
		if secondary.Percentage > usedPct {
			usedPct = secondary.Percentage
		}
		exceeded = exceeded || secondary.Exceeded
	}
	return &providers.QuotaInfo{
		Used:       usedPct,
		Total:      100,
		Remaining:  100 - usedPct,
		Percentage: usedPct,
		Unit:       QuotaUnit,
		Exceeded:   exceeded,
		FetchedAt:  time.Now().UTC().Format(time.RFC3339),
		Windows:    windows,
		ProviderID: "codex",
		Plan:       strings.TrimSpace(planType),
	}
}

func windowFromHeaders(h http.Header, prefix, id, label string) *providers.QuotaWindow {
	used := headerFloat(h, prefix+"Used-Percent")
	resetSecs := headerFloat(h, prefix+"Reset-After-Seconds")
	windowMinutes := headerFloat(h, prefix+"Window-Minutes")
	if used < 0 || (used == 0 && resetSecs <= 0 && windowMinutes <= 0) {
		return nil
	}
	window := &providers.QuotaWindow{
		ID:         id,
		Label:      label,
		Used:       used,
		Total:      100,
		Remaining:  100 - used,
		Percentage: used,
		Unit:       "percent",
		Exceeded:   used >= 100,
	}
	if resetSecs > 0 {
		window.ResetAt = time.Now().Add(time.Duration(resetSecs) * time.Second).UTC().Format(time.RFC3339)
	}
	_ = windowMinutes
	return window
}

func headerFloat(h http.Header, name string) float64 {
	raw := strings.TrimSpace(h.Get(name))
	if raw == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(raw, 64)
	return f
}
