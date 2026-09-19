package trae

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

// AlreadyCheckedInError marks the upstream "already checked in" business state.
// The runtime records it as CheckinResult{Status: "already"} without erroring.
type AlreadyCheckedInError struct {
	Msg string
}

func (e AlreadyCheckedInError) Error() string {
	if strings.TrimSpace(e.Msg) == "" {
		return "trae already checked in"
	}
	return e.Msg
}

func (AlreadyCheckedInError) AlreadyCheckedIn() bool { return true }

// Checkin claims the Trae daily credit grant via the ug checkin_credits API.
// Flow: refresh credential if needed → status probe → claim when eligible.
// "今日已签到" maps to Status="already"; session-dead surfaces KindAuth so the
// manager can flag the account for re-login.
func (client *Client) Checkin(ctx context.Context, accountID string) (providers.CheckinResult, error) {
	credential, err := client.credential(ctx, accountID)
	if err != nil {
		return providers.CheckinResult{}, err
	}
	checkedIn, credits, enable, err := client.checkinStatus(ctx, accountID, credential)
	if err != nil {
		var already AlreadyCheckedInError
		if errors.As(err, &already) {
			return providers.CheckinResult{Status: "already", Message: already.Msg}, nil
		}
		return providers.CheckinResult{}, err
	}
	if checkedIn {
		return providers.CheckinResult{
			Status:        "already",
			Message:       "already checked in",
			RewardCredits: float64(credits),
		}, nil
	}
	if !enable {
		return providers.CheckinResult{Status: "skipped", Message: "checkin disabled"}, nil
	}
	reward, err := client.checkinClaim(ctx, accountID, credential)
	if err != nil {
		var already AlreadyCheckedInError
		if errors.As(err, &already) {
			return providers.CheckinResult{Status: "already", Message: already.Msg}, nil
		}
		return providers.CheckinResult{}, err
	}
	return providers.CheckinResult{Status: "success", Message: "checkin claimed", RewardCredits: reward}, nil
}

// checkinStatus decodes the ug checkin_credits/status response. Business
// "already checked in" yields AlreadyCheckedInError so the caller can return
// Status="already" without treating it as a failure.
func (client *Client) checkinStatus(ctx context.Context, accountID string, credential Credential) (checkedIn bool, credits int64, enable bool, err error) {
	body, err := client.CheckinStatus(ctx, accountID, credential)
	if err != nil {
		return false, 0, false, err
	}
	text := strings.TrimSpace(string(body))
	classified := Classify(200, text)
	if classified.Kind == accounts.KindAuth {
		return false, 0, false, fmt.Errorf("trae checkin session dead: re-login required")
	}
	if msg, ok := alreadyCheckedInMessage(text); ok {
		return false, 0, false, AlreadyCheckedInError{Msg: msg}
	}
	var env struct {
		CheckedIn bool  `json:"checked_in"`
		Credits   int64 `json:"credits"`
		Enable    bool  `json:"enable"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return false, 0, false, fmt.Errorf("checkin status parse: %w", err)
	}
	return env.CheckedIn, env.Credits, env.Enable, nil
}

// checkinClaim decodes the ug checkin_credits/claim response. The endpoint
// returns 200 with empty body on success in practice; reward credits come
// from the trailing status probe when claim does not echo them.
func (client *Client) checkinClaim(ctx context.Context, accountID string, credential Credential) (float64, error) {
	body, err := client.CheckinClaim(ctx, accountID, credential)
	if err != nil {
		return 0, err
	}
	text := strings.TrimSpace(string(body))
	classified := Classify(200, text)
	if classified.Kind == accounts.KindAuth {
		return 0, fmt.Errorf("trae checkin session dead: re-login required")
	}
	if msg, ok := alreadyCheckedInMessage(text); ok {
		return 0, AlreadyCheckedInError{Msg: msg}
	}
	var env struct {
		Credits float64 `json:"credits"`
	}
	if text == "" {
		return 0, nil
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return 0, fmt.Errorf("checkin claim parse: %w", err)
	}
	return env.Credits, nil
}

// alreadyCheckedInMessage matches the upstream "今日已签到" business error.
// Matches the reference implementation at connectedGraph/trae2api-web
// (cmd/signin): only unambiguous markers so 429/5xx bodies that merely
// contain "checkin" are not misclassified.
func alreadyCheckedInMessage(text string) (string, bool) {
	s := strings.ToLower(text)
	if strings.Contains(s, "已签到") ||
		strings.Contains(s, "already check") ||
		strings.Contains(s, "already checked") {
		return "already checked in", true
	}
	return "", false
}
