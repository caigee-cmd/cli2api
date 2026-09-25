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

// quotaFromUsage decodes the /wham/usage body. The shape matches the websocket
// codex.rate_limits event: rate_limits.primary / secondary each carry
// used_percent, window_minutes, and reset_after_seconds or reset_at.
func quotaFromUsage(body []byte) *providers.QuotaInfo {
	var payload struct {
		RateLimits struct {
			Primary   codexRateWindow `json:"primary"`
			Secondary codexRateWindow `json:"secondary"`
		} `json:"rate_limits"`
		PlanType string `json:"plan_type"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return nil
	}
	primary := payload.RateLimits.Primary.window("fiveHour", "5-hour limit")
	secondary := payload.RateLimits.Secondary.window("weeklyLimit", "weekly limit")
	return quotaFromWindows(primary, secondary, payload.PlanType)
}

type codexRateWindow struct {
	UsedPercent       float64 `json:"used_percent"`
	WindowMinutes     int64   `json:"window_minutes"`
	ResetAfterSeconds int64   `json:"reset_after_seconds"`
	ResetAt           int64   `json:"reset_at"`
}

func (w codexRateWindow) window(id, label string) *providers.QuotaWindow {
	if w.UsedPercent < 0 || w.UsedPercent > 100 || w.WindowMinutes <= 0 {
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
	_ = planType
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
