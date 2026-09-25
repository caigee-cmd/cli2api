package codex

import (
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
	primary := windowFromHeaders(h, "X-Codex-Primary-", "primary")
	secondary := windowFromHeaders(h, "X-Codex-Secondary-", "secondary")
	if primary == nil && secondary == nil {
		return nil
	}
	windows := []providers.QuotaWindow{}
	var usedPct float64
	exceeded := false
	if primary != nil {
		windows = append(windows, *primary)
		usedPct = primary.Percentage
		exceeded = exceeded || primary.Exceeded
	}
	if secondary != nil {
		windows = append(windows, *secondary)
		// Route on the tighter window.
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
	}
}

func windowFromHeaders(h http.Header, prefix, label string) *providers.QuotaWindow {
	used := headerFloat(h, prefix+"Used-Percent")
	resetSecs := headerFloat(h, prefix+"Reset-After-Seconds")
	windowMinutes := headerFloat(h, prefix+"Window-Minutes")
	if used <= 0 && resetSecs <= 0 {
		return nil
	}
	window := &providers.QuotaWindow{
		ID:         label,
		Label:      label + " window",
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
