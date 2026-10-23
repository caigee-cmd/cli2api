package qwenwork

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// newCheckinClient seeds one ready credential and points the adapter at the
// handler. Region selects the CN wallet fallback versus the global path.
func newCheckinClient(t *testing.T, region string, handler http.Handler) *Client {
	t.Helper()
	encoded, err := readyCredential().Encode()
	if err != nil {
		t.Fatalf("encode credential: %v", err)
	}
	store := &memStore{region: region, items: map[string][]byte{"acc1": encoded}}
	return newTestClient(t, store, handler)
}

func TestCheckinClaimsCampaign(t *testing.T) {
	var claimed bool
	client := newCheckinClient(t, "global", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == pathDailyCheckinStatus:
			if got := r.Header.Get("Authorization"); got != "Bearer at" {
				t.Errorf("status auth=%q", got)
			}
			if got := r.Header.Get("User-Agent"); got != growthCardUserAgent {
				t.Errorf("status ua=%q", got)
			}
			if got := r.Header.Get("X-Qwenwork-Version"); got == "" {
				t.Error("status missing client version header")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "ok", "data": map[string]any{
				"status": dailyCheckinClaimable, "rewardCredits": 200,
			}})
		case r.Method == http.MethodPost && r.URL.Path == pathDailyCheckinClaim:
			claimed = true
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "ok", "data": map[string]any{
				"result": dailyClaimDone, "rewardCredits": 200,
			}})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	result, err := client.Checkin(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if !claimed {
		t.Fatal("claim was not posted")
	}
	if result.Status != "success" || result.RewardCredits != 200 || !strings.Contains(result.Message, "+200") {
		t.Fatalf("result=%+v", result)
	}
}

func TestCheckinCampaignAlreadyClaimed(t *testing.T) {
	client := newCheckinClient(t, "global", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != pathDailyCheckinStatus {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"status": dailyCheckinClaimedToday, "rewardCredits": 100, "currentStreakDays": 3,
		}})
	}))
	result, err := client.Checkin(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "already" || result.RewardCredits != 100 {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(result.Message, "今日已签到") {
		t.Fatalf("message=%q", result.Message)
	}
}

func TestCheckinCampaignAlreadyClaimedWithoutReward(t *testing.T) {
	client := newCheckinClient(t, "global", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"status": dailyCheckinClaimedToday, "currentStreakDays": 4,
		}})
	}))
	result, err := client.Checkin(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "already" || !strings.Contains(result.Message, "连签 4 天") {
		t.Fatalf("result=%+v", result)
	}
}

func TestCheckinClaimResultAlreadyClaimed(t *testing.T) {
	client := newCheckinClient(t, "global", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case pathDailyCheckinStatus:
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"status": dailyCheckinClaimable}})
		case pathDailyCheckinClaim:
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"result": dailyClaimAlready}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	result, err := client.Checkin(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "already" || result.Message != "今日已签到" {
		t.Fatalf("result=%+v", result)
	}
}

func TestCheckinCampaignClosedStates(t *testing.T) {
	for _, test := range []struct {
		status  string
		message string
	}{
		{dailyCheckinDisabled, "签到活动未开放"},
		{dailyCheckinNotStarted, "签到活动未开始"},
		{dailyCheckinEnded, "签到活动已结束"},
	} {
		t.Run(test.status, func(t *testing.T) {
			client := newCheckinClient(t, "global", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"status": test.status}})
			}))
			result, err := client.Checkin(context.Background(), "acc1")
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != "skipped" || result.Message != test.message {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}

func TestCheckinFallsBackToCNWallet(t *testing.T) {
	client := newCheckinClient(t, "cn", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case pathDailyCheckinStatus:
			w.WriteHeader(http.StatusNotFound)
		case pathWallets:
			if r.Header.Get("Authorization") != "Bearer at" {
				t.Errorf("wallet auth=%q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "ok", "data": map[string]any{
				"daily_credits": map[string]any{"total_balance": 100},
			}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	result, err := client.Checkin(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "already" || result.RewardCredits != 100 {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(result.Message, "每日免费积分已到账") {
		t.Fatalf("message=%q", result.Message)
	}
}

func TestCheckinCNWalletZeroIsSkipped(t *testing.T) {
	client := newCheckinClient(t, "cn", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case pathDailyCheckinStatus:
			w.WriteHeader(http.StatusNotFound)
		case pathWallets:
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"daily_credits": map[string]any{"total_balance": 0},
			}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	result, err := client.Checkin(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "skipped" || !strings.Contains(result.Message, "尚未到账") {
		t.Fatalf("result=%+v", result)
	}
}

func TestCheckinGlobalReportsClosedCampaignWithQuota(t *testing.T) {
	client := newCheckinClient(t, "global", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case pathDailyCheckinStatus:
			w.WriteHeader(http.StatusNotFound)
		case pathAccountContextPath:
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "ok", "data": map[string]any{
				"quota": map[string]any{"remaining": 700},
			}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	result, err := client.Checkin(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "skipped" || !strings.Contains(result.Message, "700") {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(result.Message, "服务端自动发放") {
		t.Fatalf("message=%q", result.Message)
	}
}

func TestCheckinAuthenticationFailure(t *testing.T) {
	client := newCheckinClient(t, "global", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"401","message":"invalid token"}`))
	}))
	if _, err := client.Checkin(context.Background(), "acc1"); err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("err=%v", err)
	}
}

func TestCheckinClaimHTTPError(t *testing.T) {
	client := newCheckinClient(t, "global", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case pathDailyCheckinStatus:
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"status": dailyCheckinClaimable}})
		case pathDailyCheckinClaim:
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"code":"429"}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	if _, err := client.Checkin(context.Background(), "acc1"); err == nil || !strings.Contains(err.Error(), "claim HTTP 429") {
		t.Fatalf("err=%v", err)
	}
}

func TestCheckinRejectsUnknownClaimResult(t *testing.T) {
	client := newCheckinClient(t, "global", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case pathDailyCheckinStatus:
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"status": dailyCheckinClaimable}})
		case pathDailyCheckinClaim:
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"result": "WAT"}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	if _, err := client.Checkin(context.Background(), "acc1"); err == nil || !strings.Contains(err.Error(), "unexpected claim result") {
		t.Fatalf("err=%v", err)
	}
}

func TestCheckinWalletAuthFailure(t *testing.T) {
	client := newCheckinClient(t, "cn", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pathDailyCheckinStatus {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"403"}`))
	}))
	if _, err := client.Checkin(context.Background(), "acc1"); err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("err=%v", err)
	}
}
