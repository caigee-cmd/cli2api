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
	// The provider check-in time setting is the operator's single source of
	// truth for every account of that provider, and it defaults to 09:00.
	// The stored per-account auto time is only a mirror of that setting kept
	// for display, so it must not outrank the setting after it changes.
	if store != nil {
		if value, err := CheckinTimeDefault(ctx, store, account.Provider); err == nil {
			return value, nil
		}
	}
	if account.CheckinAutoTime != "" {
		return NormalizeCheckinTime(account.CheckinAutoTime)
	}
	return DefaultCheckinTime, nil
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
