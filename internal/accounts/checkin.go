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
// upstream status response does not carry one. Qoder's activity refreshes at
// 10:00 Asia/Shanghai; the other currently supported check-in providers roll
// over at local midnight.
func EarliestCheckinClock(provider string) string {
	if provider == "qoder" {
		return "10:00"
	}
	return "00:00"
}

// AutoCheckinTimeFromEarliest delays the upstream window by one hour so the
// scheduler never races the reset. An invalid or empty clock returns empty.
func AutoCheckinTimeFromEarliest(earliest string) string {
	parsed, err := time.Parse("15:04", strings.TrimSpace(earliest))
	if err != nil {
		return ""
	}
	return parsed.Add(time.Hour).Format("15:04")
}

// DefaultCheckinAutoTime is the provider fallback window + 1 hour, used before
// the first upstream sync has produced an account-specific value.
func DefaultCheckinAutoTime(provider string) string {
	return AutoCheckinTimeFromEarliest(EarliestCheckinClock(provider))
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
