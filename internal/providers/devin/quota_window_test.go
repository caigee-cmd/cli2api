package devin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	commonpb "github.com/caigee-cmd/cli2api/internal/providers/devin/devinpb/codeium_common_pb"
	seatpb "github.com/caigee-cmd/cli2api/internal/providers/devin/devinpb/seat_management_pb"
	"google.golang.org/protobuf/proto"
)

func TestQuotaExhaustionPeriod(t *testing.T) {
	cases := map[string]quotaPeriod{
		"Your daily usage quota has been exhausted":     quotaPeriodDaily,
		"weekly usage quota exhausted":                  quotaPeriodWeekly,
		"daily and weekly quota exhausted":              quotaPeriodWeekly,
		"monthly ACU quota exhausted":                   quotaPeriodNone,
		"resource_exhausted: model capacity, try again": quotaPeriodNone,
		"daily rate limit reached":                      quotaPeriodNone,
	}
	for msg, want := range cases {
		if got := quotaExhaustionPeriod(msg); got != want {
			t.Errorf("%q: got %v want %v", msg, got, want)
		}
	}
}

func TestClassifiedErrorTagsQuotaWindow(t *testing.T) {
	err := classifiedErrorWithHeaders(429, nil, `{"code":"resource_exhausted","message":"weekly usage quota exhausted"}`, "")
	var pe *providers.Error
	if !errors.As(err, &pe) || pe.Kind != accounts.KindQuota || pe.Code != CodeWeeklyQuotaExhausted {
		t.Fatalf("err=%+v", pe)
	}
	err = classifiedErrorWithHeaders(429, nil, `daily usage quota exhausted, reset in 3 hours`, "")
	if !errors.As(err, &pe) || pe.Code != CodeDailyQuotaExhausted || pe.RetryAfter != 3*time.Hour {
		t.Fatalf("err=%+v", pe)
	}
}

func TestQuotaResetDelayBounds(t *testing.T) {
	now := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	status := &UserStatus{DailyQuotaResetAt: now.Add(5 * time.Hour), WeeklyQuotaResetAt: now.Add(4 * 24 * time.Hour)}
	if got := quotaResetDelay(status, quotaPeriodDaily, now); got != 5*time.Hour {
		t.Fatalf("daily=%v", got)
	}
	if got := quotaResetDelay(status, quotaPeriodWeekly, now); got != 96*time.Hour {
		t.Fatalf("weekly=%v", got)
	}
	status.WeeklyQuotaResetAt = now.Add(30 * 24 * time.Hour)
	if got := quotaResetDelay(status, quotaPeriodWeekly, now); got != 0 {
		t.Fatalf("implausible reset must be ignored, got %v", got)
	}
	status.DailyQuotaResetAt = now.Add(-time.Hour)
	if got := quotaResetDelay(status, quotaPeriodDaily, now); got != 0 {
		t.Fatalf("past reset must be ignored, got %v", got)
	}
}

func TestWithQuotaResetUsesMatchingWindow(t *testing.T) {
	weeklyReset := time.Now().Add(72 * time.Hour).Unix()
	dailyReset := time.Now().Add(2 * time.Hour).Unix()
	body, _ := proto.Marshal(&seatpb.GetUserStatusResponse{UserStatus: &commonpb.UserStatus{PlanStatus: &commonpb.PlanStatus{
		DailyQuotaResetAtUnix: dailyReset, WeeklyQuotaResetAtUnix: weeklyReset,
	}}})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != PathGetUserStatus {
			t.Errorf("path=%s", r.URL.Path)
		}
		_, _ = w.Write(body)
	}))
	defer server.Close()
	c := &Client{serverBase: server.URL}
	cred := Credential{SessionToken: "devin-session-token$test"}

	weekly := &providers.Error{Kind: accounts.KindQuota, Code: CodeWeeklyQuotaExhausted}
	_ = c.withQuotaReset(context.Background(), server.Client(), cred, weekly)
	if weekly.RetryAfter < 71*time.Hour || weekly.RetryAfter > 72*time.Hour {
		t.Fatalf("weekly RetryAfter=%v", weekly.RetryAfter)
	}
	daily := &providers.Error{Kind: accounts.KindQuota, Code: CodeDailyQuotaExhausted}
	_ = c.withQuotaReset(context.Background(), server.Client(), cred, daily)
	if daily.RetryAfter < time.Hour || daily.RetryAfter > 2*time.Hour {
		t.Fatalf("daily RetryAfter=%v", daily.RetryAfter)
	}
	hinted := &providers.Error{Kind: accounts.KindQuota, Code: CodeWeeklyQuotaExhausted, RetryAfter: time.Minute}
	_ = c.withQuotaReset(context.Background(), server.Client(), cred, hinted)
	if hinted.RetryAfter != time.Minute {
		t.Fatalf("explicit hint must win, got %v", hinted.RetryAfter)
	}
}
