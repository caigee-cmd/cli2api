package devin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	seatpb "github.com/caigee-cmd/cli2api/internal/providers/devin/devinpb/seat_management_pb"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// UserStatus contains plan, quota, and account metadata from GetUserStatus.
type UserStatus struct {
	Email                       string
	UserName                    string
	UserID                      string
	TeamID                      string
	OrgID                       string
	OrgName                     string
	Plan                        string
	DailyQuotaRemainingPercent  int64
	WeeklyQuotaRemainingPercent int64
	DailyQuotaResetAt           time.Time
	WeeklyQuotaResetAt          time.Time
	HideDailyQuota              bool
	HideWeeklyQuota             bool
	PlanStart                   time.Time
	PlanEnd                     time.Time
}

func BuildGetUserStatusRequest(sessionToken, deviceFingerprint string) ([]byte, error) {
	if deviceFingerprint == "" {
		deviceFingerprint = GenerateDeviceFingerprint(sessionToken)
	}
	return proto.Marshal(&seatpb.GetUserStatusRequest{Metadata: statusMetadata(sessionToken, deviceFingerprint)})
}

func ParseGetUserStatusResponse(data []byte) (*UserStatus, error) {
	if len(data) == 0 {
		return nil, errors.New("empty response data")
	}
	var response seatpb.GetUserStatusResponse
	if err := proto.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode Devin user status: %w", err)
	}
	user := response.GetUserStatus()
	if user == nil {
		return &UserStatus{}, nil
	}
	status := &UserStatus{Email: user.GetEmail(), UserName: user.GetName(), UserID: user.GetUserId(), TeamID: user.GetTeamId()}
	planStatus := user.GetPlanStatus()
	if planStatus == nil {
		return status, nil
	}
	status.DailyQuotaRemainingPercent = int64(planStatus.GetDailyQuotaRemainingPercent())
	status.WeeklyQuotaRemainingPercent = int64(planStatus.GetWeeklyQuotaRemainingPercent())
	status.DailyQuotaResetAt = timeFromTimestamp(planStatus.GetDailyQuotaResetAtUnix())
	status.WeeklyQuotaResetAt = timeFromTimestamp(planStatus.GetWeeklyQuotaResetAtUnix())
	status.PlanStart = timestampTime(planStatus.GetPlanStart())
	status.PlanEnd = timestampTime(planStatus.GetPlanEnd())
	if plan := planStatus.GetPlanInfo(); plan != nil {
		status.Plan = plan.GetPlanName()
		status.HideDailyQuota = plan.GetHideDailyQuota()
		status.HideWeeklyQuota = plan.GetHideWeeklyQuota()
		if devin := plan.GetDevinInfo(); devin != nil {
			status.OrgID, status.OrgName = devin.GetOrgId(), devin.GetAccountDisplayName()
		}
	}
	return status, nil
}

func timeFromTimestamp(seconds int64) time.Time {
	if seconds <= 0 {
		return time.Time{}
	}
	return time.Unix(seconds, 0).UTC()
}

func timestampTime(timestamp *timestamppb.Timestamp) time.Time {
	if timestamp == nil || timestamp.GetSeconds() <= 0 {
		return time.Time{}
	}
	return time.Unix(timestamp.GetSeconds(), int64(timestamp.GetNanos())).UTC()
}

func FetchUserStatus(ctx context.Context, client *http.Client, serverBase, sessionToken, deviceSeed string) (*UserStatus, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	sessionToken = strings.TrimSpace(sessionToken)
	if sessionToken == "" {
		return nil, errors.New("devin session token is required")
	}
	reqBody, err := BuildGetUserStatusRequest(sessionToken, GenerateDeviceFingerprint(deviceSeed))
	if err != nil {
		return nil, fmt.Errorf("encode Devin user-status request: %w", err)
	}
	endpoint := strings.TrimRight(firstNonEmpty(serverBase, ServerBase), "/") + PathGetUserStatus
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", BasicAuthHeader(sessionToken))
	req.Header.Set("Connect-Protocol-Version", ConnectProtocolVersion)
	req.Header.Set("Content-Type", ContentTypeProto)
	req.Header.Set("Accept", "*/*")
	req.Header["User-Agent"] = []string{""}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("devin seat management error (status %d): %s", resp.StatusCode, string(respBytes))
	}
	return ParseGetUserStatusResponse(respBytes)
}
