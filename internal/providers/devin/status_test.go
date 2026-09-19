package devin

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

func TestFetchUserStatusMapsPlanAndQuota(t *testing.T) {
	// Encode numeric fields independently of generated types so a schema or
	// adapter mapping error cannot also change the fixture's wire contract.
	bytesField := func(number protowire.Number, value []byte) []byte {
		return protowire.AppendBytes(protowire.AppendTag(nil, number, protowire.BytesType), value)
	}
	intField := func(number protowire.Number, value uint64) []byte {
		return protowire.AppendVarint(protowire.AppendTag(nil, number, protowire.VarintType), value)
	}
	timestamp := func(seconds uint64, nanos uint64) []byte {
		return append(intField(1, seconds), intField(2, nanos)...)
	}
	org := append(bytesField(4, []byte("org-1")), bytesField(8, []byte("Example Org"))...)
	plan := append(bytesField(2, []byte("Pro")), bytesField(33, org)...)
	planStatus := bytesField(1, plan)
	planStatus = append(planStatus, bytesField(2, timestamp(1700000000, 123000000))...)
	planStatus = append(planStatus, bytesField(3, timestamp(1702592000, 0))...)
	planStatus = append(planStatus, intField(14, 75)...)
	planStatus = append(planStatus, intField(15, 42)...)
	planStatus = append(planStatus, intField(17, 1700086400)...)
	planStatus = append(planStatus, intField(18, 1700604800)...)
	user := bytesField(3, []byte("Example User"))
	user = append(user, bytesField(5, []byte("team-1"))...)
	user = append(user, bytesField(7, []byte("user@example.invalid"))...)
	user = append(user, bytesField(13, planStatus)...)
	user = append(user, bytesField(36, []byte("user-1"))...)
	response := append(bytesField(1, user), intField(99, 1)...)

	requests := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != PathGetUserStatus {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Content-Type") != ContentTypeProto || r.Header.Get("Connect-Protocol-Version") != ConnectProtocolVersion {
			t.Error("missing protobuf Connect request headers")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- body
		w.Header().Set("Content-Type", ContentTypeProto)
		_, _ = w.Write(response)
	}))
	defer server.Close()

	got, err := FetchUserStatus(context.Background(), server.Client(), server.URL, "fixture-token", "fixture-device")
	if err != nil {
		t.Fatal(err)
	}
	metadata := wireMessage(t, <-requests, 1)
	if got := string(wireMessage(t, metadata, 1)); got != ClientName {
		t.Errorf("status client = %q", got)
	}
	if got := string(wireMessage(t, metadata, 31)); got != GenerateDeviceFingerprint("fixture-device") {
		t.Errorf("status fingerprint = %q", got)
	}
	want := &UserStatus{
		Email: "user@example.invalid", UserName: "Example User", UserID: "user-1", TeamID: "team-1",
		OrgID: "org-1", OrgName: "Example Org", Plan: "Pro",
		DailyQuotaRemainingPercent: 75, WeeklyQuotaRemainingPercent: 42,
		DailyQuotaResetAt: time.Unix(1700086400, 0).UTC(), WeeklyQuotaResetAt: time.Unix(1700604800, 0).UTC(),
		PlanStart: time.Unix(1700000000, 123000000).UTC(), PlanEnd: time.Unix(1702592000, 0).UTC(),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("status = %+v, want %+v", got, want)
	}
}

func TestQuotaFromStatusKeepsDailyAndWeeklyWindows(t *testing.T) {
	dailyReset := time.Unix(1700086400, 0).UTC()
	weeklyReset := time.Unix(1700604800, 0).UTC()
	got := quotaFromStatus(&UserStatus{
		DailyQuotaRemainingPercent:  100,
		WeeklyQuotaRemainingPercent: 33,
		DailyQuotaResetAt:           dailyReset,
		WeeklyQuotaResetAt:          weeklyReset,
	}, time.Unix(1700000000, 0).UTC())
	if got == nil {
		t.Fatal("quota info is nil")
	}
	if got.Remaining != 33 || got.Percentage != 67 || got.Exceeded {
		t.Fatalf("tightest quota = %+v", got)
	}
	if len(got.Windows) != 2 {
		t.Fatalf("windows = %+v", got.Windows)
	}
	if got.Windows[0].ID != "daily" || got.Windows[0].Remaining != 100 || got.Windows[0].Percentage != 0 || got.Windows[0].ResetAt != dailyReset.Format(time.RFC3339) {
		t.Fatalf("daily window = %+v", got.Windows[0])
	}
	if got.Windows[1].ID != "weekly" || got.Windows[1].Remaining != 33 || got.Windows[1].Percentage != 67 || got.Windows[1].ResetAt != weeklyReset.Format(time.RFC3339) {
		t.Fatalf("weekly window = %+v", got.Windows[1])
	}
}

func TestQuotaFromStatusHidesDailyWindow(t *testing.T) {
	got := quotaFromStatus(&UserStatus{
		DailyQuotaRemainingPercent:  0,
		WeeklyQuotaRemainingPercent: 42,
		HideDailyQuota:              true,
	}, time.Unix(1700000000, 0).UTC())
	if got == nil || got.Remaining != 42 || got.Exceeded || len(got.Windows) != 1 || got.Windows[0].ID != "weekly" {
		t.Fatalf("hidden daily quota = %+v", got)
	}
}
