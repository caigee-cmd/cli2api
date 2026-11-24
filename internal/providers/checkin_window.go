package providers

import (
	"strings"
	"time"
)

// CheckinClock formats a provider daily window start as HH:MM. Providers that
// return an absolute timestamp use the same +08:00 wall clock their desktop
// clients use for the daily reset, even when the host runs elsewhere.
func CheckinClock(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.In(time.FixedZone("UTC+8", 8*3600)).Format("15:04")
}

// CheckinClockFromUnix converts Unix seconds (Qoder campaign windows) to HH:MM.
func CheckinClockFromUnix(seconds int64) string {
	if seconds <= 0 {
		return ""
	}
	return CheckinClock(time.Unix(seconds, 0))
}

// CheckinClockFromUnixMillis converts Unix milliseconds (QwenWork nextClaimAt)
// to HH:MM.
func CheckinClockFromUnixMillis(milliseconds int64) string {
	if milliseconds <= 0 {
		return ""
	}
	return CheckinClock(time.UnixMilli(milliseconds))
}

// CheckinClockFromText extracts the wall-clock time from the campaign timestamps
// providers return as plain strings ("2026-09-30 00:00:00" or RFC3339).
//
// Timestamps that carry a zone are converted to the provider's UTC+8 calendar;
// timestamps without a zone are already the server's wall clock, so they are
// formatted as-is. This keeps a UTC "00:00Z" from being mistaken for a Beijing
// midnight window.
func CheckinClockFromText(value string) string {
	value = strings.TrimSpace(value)
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return CheckinClock(parsed)
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
	} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Format("15:04")
		}
	}
	return ""
}
