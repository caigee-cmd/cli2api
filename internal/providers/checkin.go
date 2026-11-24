package providers

import (
	"context"
	"time"
)

// CheckinSureHour is the first hour of the provider's UTC+8 day from which a
// campaign-level status without a per-day flag can be trusted. WorkBuddy
// (today_checked_in), Trae (checked_in), MiniMax (is_today) and QwenWork
// (CLAIMED_TODAY) carry a server-computed day flag and are read directly;
// CodeArts only exposes a campaign status that stays CONFIRMED across days.
// Two hours after the 00:00 rollover leaves room for cache and settlement
// lag before that campaign status is treated as today's.
const CheckinSureHour = 2

// CheckinClaimSettledHour is the first hour from which a claim endpoint's
// "already claimed" answer is trusted. The automatic schedule runs at 01:00
// (earliest window + 1h); a vendor that has not rolled its day yet can still
// answer with yesterday's "already" at that hour. Treat that answer as
// unconfirmed for one more hour and let the retry pick it up, rather than
// recording a false success and skipping the whole day.
const CheckinClaimSettledHour = 2

var checkinCalendarZone = time.FixedZone("UTC+8", 8*3600)

// CheckinCalendarNow returns t in the provider check-in calendar (UTC+8).
func CheckinCalendarNow(t time.Time) time.Time {
	return t.In(checkinCalendarZone)
}

// CheckinAnswerProvisional reports whether a campaign-level "already" answer
// at t may still describe the previous provider day. A fresh local success is
// always authoritative and is handled by the runtime separately.
func CheckinAnswerProvisional(t time.Time) bool {
	return CheckinCalendarNow(t).Hour() < CheckinSureHour
}

// CheckinClaimProvisional reports whether an "already claimed" answer from the
// claim endpoint is still early enough to be yesterday's stale state.
func CheckinClaimProvisional(t time.Time) bool {
	return CheckinCalendarNow(t).Hour() < CheckinClaimSettledHour
}

type CheckinPolicy struct {
	// Timezone is the provider's server-side check-in calendar zone, not the
	// operator PC's zone. Use an IANA location such as Asia/Shanghai; never
	// "Local", so schedules stay stable when the host or browser moves.
	Timezone string `json:"timezone"`
}

type CheckinResult struct {
	Status        string  `json:"status"`
	Message       string  `json:"message"`
	RewardCredits float64 `json:"reward_credits,omitempty"`
	Streak        int     `json:"streak,omitempty"`
	// EarliestCheckin is the upstream daily window start as HH:MM in the
	// provider's configured timezone. Empty means the provider did not expose it.
	EarliestCheckin string `json:"earliest_checkin,omitempty"`
}

func (result CheckinResult) Valid() bool {
	return result.Status == "success" || result.Status == "already" || result.Status == "skipped" ||
		result.Status == "pending" || result.Status == "unknown" || result.Status == "error"
}

type AccountCheckiner interface {
	Checkin(ctx context.Context, accountID string) (CheckinResult, error)
}

// AccountCheckinStatusReader reads the upstream check-in state without claiming
// anything. Providers that share one endpoint for read and claim only implement
// this when the read path can be separated safely.
type AccountCheckinStatusReader interface {
	CheckinStatus(ctx context.Context, accountID string) (CheckinResult, error)
}

func CheckinFor(providerID, regionID string) (*CheckinPolicy, bool) {
	_, region, err := Resolve(providerID, regionID)
	return region.Checkin, err == nil && region.Checkin != nil
}

func (descriptor ProviderDescriptor) SupportsCheckin() bool {
	for _, region := range descriptor.Regions {
		if region.Checkin != nil {
			return true
		}
	}
	return false
}
