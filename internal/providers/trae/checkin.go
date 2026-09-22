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
// Flow: refresh credential if needed → status probe → claim when eligible →
// re-probe so success is only reported when the account actually flipped.
// "今日已签到" maps to Status="already"; session-dead surfaces KindAuth so the
// manager can flag the account for re-login.
func (client *Client) Checkin(ctx context.Context, accountID string) (providers.CheckinResult, error) {
	credential, err := client.credential(ctx, accountID)
	if err != nil {
		return providers.CheckinResult{}, err
	}
	st, err := client.checkinStatus(ctx, accountID, credential)
	if err != nil {
		var already AlreadyCheckedInError
		if errors.As(err, &already) {
			return providers.CheckinResult{Status: "already", Message: already.Msg}, nil
		}
		return providers.CheckinResult{}, err
	}
	if st.checkedIn {
		return providers.CheckinResult{
			Status:        "already",
			Message:       "already checked in",
			RewardCredits: float64(st.credits + st.extraCredits),
		}, nil
	}
	if !st.enable {
		return providers.CheckinResult{Status: "skipped", Message: "checkin disabled"}, nil
	}
	if err := client.checkinClaim(ctx, accountID, credential); err != nil {
		var already AlreadyCheckedInError
		if errors.As(err, &already) {
			return providers.CheckinResult{Status: "already", Message: already.Msg}, nil
		}
		return providers.CheckinResult{}, err
	}
	// The claim endpoint answers code 0 even when the device identity is
	// refused (9074) or the daily grant was already taken, so success is
	// decided by re-probing: a real claim flips checked_in to true.
	after, err := client.checkinStatus(ctx, accountID, credential)
	if err != nil {
		return providers.CheckinResult{}, err
	}
	if !after.checkedIn {
		return providers.CheckinResult{}, fmt.Errorf("trae checkin did not register (device may be rejected); retry later")
	}
	return providers.CheckinResult{
		Status:        "success",
		Message:       "checkin claimed",
		RewardCredits: float64(after.credits + after.extraCredits),
	}, nil
}

type checkinState struct {
	checkedIn    bool
	credits      int64
	extraCredits int64
	enable       bool
}

// checkinStatus decodes the ug checkin_credits/status response. Business
// "already checked in" yields AlreadyCheckedInError so the caller can return
// Status="already" without treating it as a failure.
func (client *Client) checkinStatus(ctx context.Context, accountID string, credential Credential) (checkinState, error) {
	body, err := client.CheckinStatus(ctx, accountID, credential)
	if err != nil {
		return checkinState{}, err
	}
	text := strings.TrimSpace(string(body))
	classified := Classify(200, text)
	if classified.Kind == accounts.KindAuth {
		return checkinState{}, fmt.Errorf("trae checkin session dead: re-login required")
	}
	if msg, ok := alreadyCheckedInMessage(text); ok {
		return checkinState{}, AlreadyCheckedInError{Msg: msg}
	}
	var env struct {
		CheckedIn    bool  `json:"checked_in"`
		Credits      int64 `json:"credits"`
		ExtraCredits int64 `json:"extra_credits"`
		Enable       bool  `json:"enable"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return checkinState{}, fmt.Errorf("checkin status parse: %w", err)
	}
	return checkinState{checkedIn: env.CheckedIn, credits: env.Credits, extraCredits: env.ExtraCredits, enable: env.Enable}, nil
}

// checkinClaim posts the ug checkin_credits/claim request. The response is
// HTTP 200 with a business body {"code":0} on success (idempotent), or a
// non-zero code such as 9074 when the device identity is refused. Only an
// "already checked in" marker is treated as a soft success; other non-zero
// codes are surfaced as errors.
func (client *Client) checkinClaim(ctx context.Context, accountID string, credential Credential) error {
	body, err := client.CheckinClaim(ctx, accountID, credential)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(string(body))
	classified := Classify(200, text)
	if classified.Kind == accounts.KindAuth {
		return fmt.Errorf("trae checkin session dead: re-login required")
	}
	if msg, ok := alreadyCheckedInMessage(text); ok {
		return AlreadyCheckedInError{Msg: msg}
	}
	var env struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("checkin claim parse: %w", err)
	}
	if env.Code != 0 {
		return fmt.Errorf("trae checkin claim rejected (code %d): %s", env.Code, strings.TrimSpace(env.Message))
	}
	return nil
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
