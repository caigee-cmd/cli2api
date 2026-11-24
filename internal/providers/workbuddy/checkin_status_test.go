package workbuddy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCheckinStatusReadsUpstreamWithoutClaiming(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathCheckinStatus {
			t.Fatalf("status must never claim: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method=%s", r.Method)
		}
		raw, _ := io.ReadAll(r.Body)
		if strings.TrimSpace(string(raw)) != "{}" {
			t.Fatalf("body=%q", raw)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "", "data": map[string]any{
			"active": true, "today_checked_in": true, "start_time": "2026-09-30 00:00:00",
			"end_time": "2026-12-31 23:59:59", "current_streak": 5, "points": 12.5,
		}})
	}))
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", ExpiresAt: 4102444800, Domain: DomainCN, UID: "u1"}.Encode()
	_ = store.SaveCredentialPayload(context.Background(), "acc1", CredentialFormat, payload)

	result, err := client.CheckinStatus(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "already" || result.EarliestCheckin != "00:00" || result.Streak != 5 || result.RewardCredits != 12.5 {
		t.Fatalf("result=%+v", result)
	}
}

func TestCheckinStatusPendingWhenTodayMissing(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"active": true, "today_checked_in": false, "start_time": "2026-09-30 00:00:00", "current_streak": 4,
		}})
	}))
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", ExpiresAt: 4102444800, Domain: DomainCN, UID: "u1"}.Encode()
	_ = store.SaveCredentialPayload(context.Background(), "acc1", CredentialFormat, payload)
	result, err := client.CheckinStatus(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "pending" || result.EarliestCheckin != "00:00" || result.Streak != 4 {
		t.Fatalf("result=%+v", result)
	}
}

func saveCheckinCredential(t *testing.T, store *memStore, domain string) {
	t.Helper()
	payload, _ := json.Marshal(Credential{
		AccessToken: "at", RefreshToken: "rt", ExpiresAt: 4102444800, Domain: domain, UID: "u1",
	})
	if err := store.SaveCredentialPayload(context.Background(), "acc1", CredentialFormat, payload); err != nil {
		t.Fatal(err)
	}
}

func TestCheckinQueriesActivityStatusBeforeClaim(t *testing.T) {
	var statusCalls, claimCalls atomic.Int32
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathCheckinStatus:
			statusCalls.Add(1)
			if r.Method != http.MethodPost {
				t.Fatalf("status method=%s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"active": true, "today_checked_in": false},
			})
		case pathDailyCheckin:
			claimCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "签到成功"})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	saveCheckinCredential(t, store, DomainCN)
	result, err := client.Checkin(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "success" || result.Message != "签到成功" {
		t.Fatalf("result=%+v", result)
	}
	if statusCalls.Load() != 1 || claimCalls.Load() != 1 {
		t.Fatalf("status=%d claim=%d", statusCalls.Load(), claimCalls.Load())
	}
}

func TestCheckinInactiveActivitySkipsWithoutClaim(t *testing.T) {
	var claimCalls atomic.Int32
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pathCheckinStatus {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"active": false, "today_checked_in": false},
			})
			return
		}
		if r.URL.Path == pathDailyCheckin {
			claimCalls.Add(1)
		}
	}))
	saveCheckinCredential(t, store, DomainCN)
	result, err := client.Checkin(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "skipped" || claimCalls.Load() != 0 {
		t.Fatalf("result=%+v claim=%d", result, claimCalls.Load())
	}
}

func TestCheckinStatusAlreadyCheckedInSkipsClaim(t *testing.T) {
	var claimCalls atomic.Int32
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pathCheckinStatus {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"active": true, "today_checked_in": true},
			})
			return
		}
		if r.URL.Path == pathDailyCheckin {
			claimCalls.Add(1)
		}
	}))
	saveCheckinCredential(t, store, DomainCN)
	result, err := client.Checkin(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "already" || claimCalls.Load() != 0 {
		t.Fatalf("result=%+v claim=%d", result, claimCalls.Load())
	}
}

func TestCheckinGlobalSkipsBeforeAnyRequest(t *testing.T) {
	var calls atomic.Int32
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	}))
	saveCheckinCredential(t, store, DomainGlobal)
	store.region = "global"
	result, err := client.Checkin(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "skipped" || calls.Load() != 0 {
		t.Fatalf("result=%+v calls=%d", result, calls.Load())
	}
}
