package workbuddy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// checkinActivityStatus mirrors the official desktop flow: query the campaign
// switch before claiming, so an inactive campaign and a same-day repeat never
// produce a false failure. A failed status probe falls through to the claim,
// matching the desktop's "status unavailable, still check in" behaviour.
type checkinActivityStatus struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Active         *bool    `json:"active"`
		TodayCheckedIn *bool    `json:"today_checked_in"`
		StartTime      string   `json:"start_time"`
		EndTime        string   `json:"end_time"`
		CurrentStreak  *int     `json:"current_streak"`
		Points         *float64 `json:"points"`
	} `json:"data"`
}

func (client *Client) checkinActivity(ctx context.Context, accountID string, credential Credential) (checkinActivityStatus, bool) {
	body, status, err := client.do(ctx, accountID, http.MethodPost,
		credential.BillingBase()+pathCheckinStatus, []byte("{}"),
		func(h http.Header) { SetBillingHeaders(h, credential) })
	if err != nil || status >= 300 {
		return checkinActivityStatus{}, false
	}
	var envelope checkinActivityStatus
	if json.Unmarshal(body, &envelope) != nil || envelope.Code != 0 {
		return checkinActivityStatus{}, false
	}
	if envelope.Data.Active == nil && envelope.Data.TodayCheckedIn == nil {
		return checkinActivityStatus{}, false
	}
	// A response that only carries today_checked_in still describes a live
	// campaign; treat a missing active flag as active.
	if envelope.Data.Active == nil {
		envelope.Data.Active = new(bool)
		*envelope.Data.Active = true
	}
	if envelope.Data.TodayCheckedIn == nil {
		envelope.Data.TodayCheckedIn = new(bool)
	}
	return envelope, true
}

func (client *Client) checkinStatus(ctx context.Context, accountID string, credential Credential) (active, checked, known bool) {
	activity, known := client.checkinActivity(ctx, accountID, credential)
	if !known {
		return false, false, false
	}
	return *activity.Data.Active, *activity.Data.TodayCheckedIn, true
}

// CheckinStatus reads the campaign switch and today's flag without claiming.
func (client *Client) CheckinStatus(ctx context.Context, accountID string) (providers.CheckinResult, error) {
	credential, err := client.resolvedCredential(ctx, accountID)
	if err != nil {
		return providers.CheckinResult{}, err
	}
	if credential.IsGlobal() {
		return providers.CheckinResult{Status: "skipped", Message: "WorkBuddy Global has no daily check-in"}, nil
	}
	activity, known := client.checkinActivity(ctx, accountID, credential)
	if !known {
		return providers.CheckinResult{Status: "unknown", Message: "上游签到状态不可用"}, nil
	}
	earliest := providers.CheckinClockFromText(activity.Data.StartTime)
	result := providers.CheckinResult{EarliestCheckin: earliest}
	if activity.Data.CurrentStreak != nil {
		result.Streak = *activity.Data.CurrentStreak
	}
	if activity.Data.Points != nil {
		result.RewardCredits = *activity.Data.Points
	}
	if !*activity.Data.Active {
		result.Status = "skipped"
		result.Message = "签到活动未开放"
		return result, nil
	}
	if *activity.Data.TodayCheckedIn {
		// today_checked_in is computed by the provider for its own day, so it is
		// trusted directly. Re-issuing the claim merely to prove the date would
		// turn a valid answer into a false "unknown" every morning.
		result.Status = "already"
		result.Message = "今日已签到"
		if result.Streak > 0 {
			result.Message = "今日已签到"
		}
		return result, nil
	}
	result.Status = "pending"
	result.Message = "上游显示今日未签到"
	return result, nil
}

func (client *Client) Checkin(ctx context.Context, accountID string) (providers.CheckinResult, error) {
	credential, err := client.resolvedCredential(ctx, accountID)
	if err != nil {
		return providers.CheckinResult{}, err
	}
	// The international desktop disables its check-in feature; there is no
	// endpoint to emulate. Report a clean skip instead of a red failure.
	if credential.IsGlobal() {
		return providers.CheckinResult{Status: "skipped", Message: "WorkBuddy Global has no daily check-in"}, nil
	}
	active, checked, known := client.checkinStatus(ctx, accountID, credential)
	if known {
		if !active {
			return providers.CheckinResult{Status: "skipped", Message: "check-in activity is not active"}, nil
		}
		// today_checked_in is the provider's own day flag; trust it and skip the
		// extra claim. A claim endpoint "already" answer at 01:00 is still
		// guarded separately because the campaign can replay yesterday's reward.
		if checked {
			return providers.CheckinResult{Status: "already", Message: "今日已签到"}, nil
		}
	}
	message, err := client.DailyCheckin(ctx, accountID)
	var already AlreadyCheckedInError
	if errors.As(err, &already) {
		if providers.CheckinClaimProvisional(time.Now()) {
			return providers.CheckinResult{Status: "unknown", Message: "上游日界尚未确认，稍后复核"}, nil
		}
		return providers.CheckinResult{Status: "already", Message: message}, nil
	}
	if err != nil {
		return providers.CheckinResult{}, err
	}
	return providers.CheckinResult{Status: "success", Message: message}, nil
}
