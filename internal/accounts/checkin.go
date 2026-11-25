package accounts

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

const (
	DefaultCheckinTime            = "09:00"
	DefaultEarliestCheckinClock   = "00:00"
	CheckinDisabledAccountsSecret = "checkin_disabled_accounts"
)

type CheckinSettingsReader interface {
	GetSecret(context.Context, string) (string, bool, error)
}

func CheckinTimeSecret(providerID string) string {
	if providerID == "workbuddy" {
		return WorkBuddyCheckinTimeSecret
	}
	return "checkin_time." + providerID
}

func NormalizeCheckinTime(value string) (string, error) {
	value = strings.TrimSpace(value)
	parsed, err := time.Parse("15:04", value)
	if err != nil || parsed.Format("15:04") != value {
		return "", fmt.Errorf("checkin_time must use HH:mm")
	}
	return value, nil
}

func CheckinTimeDefault(ctx context.Context, store CheckinSettingsReader, providerID string) (string, error) {
	value, found, err := store.GetSecret(ctx, CheckinTimeSecret(providerID))
	if err != nil {
		return "", err
	}
	if !found {
		return DefaultCheckinTime, nil
	}
	return NormalizeCheckinTime(value)
}

func ResolveCheckinTime(ctx context.Context, store CheckinSettingsReader, account Account) (string, error) {
	if account.CheckinTime != "" {
		return NormalizeCheckinTime(account.CheckinTime)
	}
	if account.CheckinAutoTime != "" {
		return NormalizeCheckinTime(account.CheckinAutoTime)
	}
	// Before the first upstream status sync, keep the legacy provider default
	// as a fallback. The first sync writes CheckinAutoTime and takes over.
	fallback, err := CheckinTimeDefault(ctx, store, account.Provider)
	if err != nil {
		return "", err
	}
	return fallback, nil
}

// EarliestCheckinClock is the provider's fallback daily window start when the
// upstream status response does not carry one. Regions differ: Qoder Global
// rotates at 10:00 Asia/Shanghai, Qoder CN (QoderWork) at local midnight.
func EarliestCheckinClock(provider string) string {
	descriptor, ok := providers.Get(provider)
	if !ok {
		return DefaultEarliestCheckinClock
	}
	return EarliestCheckinClockFor(provider, descriptor.DefaultRegion)
}

// EarliestCheckinClockFor resolves the fallback window for one provider region.
// The registry policy is the single source of truth, so a region that rolls
// over at a different hour cannot inherit its sibling's schedule.
func EarliestCheckinClockFor(provider, region string) string {
	if policy, ok := providers.CheckinFor(provider, region); ok {
		if trimmed := strings.TrimSpace(policy.Earliest); trimmed != "" {
			return trimmed
		}
	}
	return DefaultEarliestCheckinClock
}

// CheckinWindowFromUpstream reports whether the provider exposes its daily
// window start in the status payload, so the runtime learns it per account
// instead of trusting the fallback table.
func CheckinWindowFromUpstream(provider, region string) bool {
	policy, ok := providers.CheckinFor(provider, region)
	return ok && policy.WindowFromUpstream
}

// AutoCheckinTimeFromEarliest delays the upstream window by one hour so the
// scheduler never races the reset. An invalid or empty clock returns empty.
// A window that starts at 23:00 or later clamps to 23:59 instead of wrapping
// to 00:xx, which would fire before the window it is meant to follow.
func AutoCheckinTimeFromEarliest(earliest string) string {
	parsed, err := time.Parse("15:04", strings.TrimSpace(earliest))
	if err != nil {
		return ""
	}
	minutes := parsed.Hour()*60 + parsed.Minute() + 60
	if minutes >= 24*60 {
		minutes = 24*60 - 1
	}
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

// DefaultCheckinAutoTime is the provider fallback window + 1 hour, used before
// the first upstream sync has produced an account-specific value.
func DefaultCheckinAutoTime(provider string) string {
	descriptor, ok := providers.Get(provider)
	if !ok {
		return AutoCheckinTimeFromEarliest(DefaultEarliestCheckinClock)
	}
	return DefaultCheckinAutoTimeFor(provider, descriptor.DefaultRegion)
}

// DefaultCheckinAutoTimeFor is DefaultCheckinAutoTime for one provider region.
func DefaultCheckinAutoTimeFor(provider, region string) string {
	return AutoCheckinTimeFromEarliest(EarliestCheckinClockFor(provider, region))
}

func ValidateCheckinSettings(providerID, regionID string, enabled bool, override string) error {
	if override != "" {
		if _, err := NormalizeCheckinTime(override); err != nil {
			return err
		}
	}
	if _, supported := providers.CheckinFor(providerID, regionID); !supported && (enabled || override != "") {
		return fmt.Errorf("check-in is not available for this provider and region")
	}
	return nil
}
