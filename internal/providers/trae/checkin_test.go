package trae

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

func checkinTestCredential(t *testing.T) []byte {
	t.Helper()
	payload, err := Credential{
		AccessToken: "at", RefreshToken: "rt", UID: "u1",
		ExpiresAt: 4102444800, MachineID: "m1", DeviceID: "d1",
	}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestDailyCheckinClaimsCredits(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathCheckinStatus:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"checked_in": false, "code": 0, "credits": 150,
				"did_checked_in": false, "enable": true, "extra_credits": 50, "message": "success",
			})
		case pathCheckinClaim:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"checked_in": true, "code": 0, "credits": 200, "did_checked_in": true, "message": "success",
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	store.items = map[string][]byte{"acc1": checkinTestCredential(t)}
	msg, err := client.DailyCheckin(context.Background(), "acc1")
	if err != nil || msg != "checked in +200 credits" {
		t.Fatalf("msg=%q err=%v", msg, err)
	}
}

func TestDailyCheckinAlreadyCheckedInSkipsClaim(t *testing.T) {
	claimed := false
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathCheckinStatus {
			claimed = true
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"checked_in": true, "code": 0, "credits": 150, "enable": true, "message": "success",
		})
	}))
	store.items = map[string][]byte{"acc1": checkinTestCredential(t)}
	_, err := client.DailyCheckin(context.Background(), "acc1")
	var already AlreadyCheckedInError
	if !errors.As(err, &already) {
		t.Fatalf("err=%v", err)
	}
	if claimed {
		t.Fatal("claim must not run when the day is already claimed")
	}
}

func TestDailyCheckinClaimReportsAlready(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathCheckinStatus:
			_ = json.NewEncoder(w).Encode(map[string]any{"checked_in": false, "code": 0, "enable": true, "message": "success"})
		case pathCheckinClaim:
			_ = json.NewEncoder(w).Encode(map[string]any{"checked_in": true, "code": 1, "message": "今日已签到"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	store.items = map[string][]byte{"acc1": checkinTestCredential(t)}
	msg, err := client.DailyCheckin(context.Background(), "acc1")
	var already AlreadyCheckedInError
	if !errors.As(err, &already) || msg != "今日已签到" {
		t.Fatalf("msg=%q err=%v", msg, err)
	}
}

func TestDailyCheckinClaimBusinessError(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathCheckinStatus:
			_ = json.NewEncoder(w).Encode(map[string]any{"checked_in": false, "code": 0, "enable": true, "message": "success"})
		case pathCheckinClaim:
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 1005, "message": "plan limit"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	store.items = map[string][]byte{"acc1": checkinTestCredential(t)}
	msg, err := client.DailyCheckin(context.Background(), "acc1")
	if err == nil || msg != "plan limit" {
		t.Fatalf("msg=%q err=%v", msg, err)
	}
	var already AlreadyCheckedInError
	if errors.As(err, &already) {
		t.Fatalf("business failure must not be reported as already checked in: %v", err)
	}
}

func TestDailyCheckinDisabledAccount(t *testing.T) {
	claimed := false
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathCheckinStatus {
			claimed = true
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"checked_in": false, "code": 0, "enable": false, "message": "check-in disabled"})
	}))
	store.items = map[string][]byte{"acc1": checkinTestCredential(t)}
	msg, err := client.DailyCheckin(context.Background(), "acc1")
	if err == nil || msg != "check-in disabled" {
		t.Fatalf("msg=%q err=%v", msg, err)
	}
	if claimed {
		t.Fatal("a disabled check-in must not claim")
	}
}

func TestKeepaliveRefreshesCredential(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathExchange {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
			"Token": "at2", "RefreshToken": "rt2", "TokenExpireAt": time.Now().Add(time.Hour).Unix(),
		}})
	}))
	store.items = map[string][]byte{"acc1": checkinTestCredential(t)}
	if err := client.Keepalive(context.Background(), "acc1"); err != nil {
		t.Fatal(err)
	}
	_, payload, err := store.LoadCredentialPayload(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := DecodeCredential(payload)
	if err != nil || credential.AccessToken != "at2" || credential.RefreshToken != "rt2" {
		t.Fatalf("credential=%+v err=%v", credential, err)
	}
}

// The UG checkin backend keys its per-device daily limit on X-Device-Id and
// answers a bare hex id with code 9074, so every stored id must be sent in the
// aha-<hex> shape the Trae client uses.
func TestSetUgHeadersNormalizesDeviceID(t *testing.T) {
	cases := []struct {
		name     string
		deviceID string
		want     string
	}{
		{"bare hex gets the aha prefix", "3ee0250fe3e0e6e03cdd8054e3a01a7b", "aha-3ee0250fe3e0e6e03cdd8054e3a01a7b"},
		{"prefixed id is left alone", "aha-3ee0250fe3e0e6e03cdd8054e3a01a7b", "aha-3ee0250fe3e0e6e03cdd8054e3a01a7b"},
		{"whitespace is trimmed", "  cfac6e546e2386ec29e13335393d79dc \n", "aha-cfac6e546e2386ec29e13335393d79dc"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			header := http.Header{}
			SetUgHeaders(header, Credential{AccessToken: "at", DeviceID: testCase.deviceID})
			if got := header.Get("X-Device-Id"); got != testCase.want {
				t.Fatalf("X-Device-Id=%q want %q", got, testCase.want)
			}
			if got := header.Get("Authorization"); got != "Cloud-IDE-JWT at" {
				t.Fatalf("Authorization=%q", got)
			}
		})
	}

	header := http.Header{}
	SetUgHeaders(header, Credential{AccessToken: "at"})
	if got := header.Get("X-Device-Id"); got != "" {
		t.Fatalf("missing device id must not be sent: %q", got)
	}
}