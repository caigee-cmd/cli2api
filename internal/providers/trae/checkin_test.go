package trae

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// seedCheckinCredential stores a long-lived Trae credential so Checkin does
// not try to refresh it against the test server.
func seedCheckinCredential(t *testing.T, store *memStore) {
	t.Helper()
	payload, err := json.Marshal(Credential{
		AccessToken:  "at",
		RefreshToken: "rt",
		ExpiresAt:    4102444800,
		Domain:       DomainCN,
		UID:          "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCredentialPayload(context.Background(), "acc1", CredentialFormat, payload); err != nil {
		t.Fatal(err)
	}
}

func TestCheckinClaimFlow(t *testing.T) {
	var paths []string
	var statusCalls int
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method=%s", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Cloud-IDE-JWT at" {
			t.Fatalf("authorization=%q", auth)
		}
		body, _ := io.ReadAll(r.Body)
		if strings.TrimSpace(string(body)) != "{}" {
			t.Fatalf("body=%q", body)
		}
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case pathCheckinStatus:
			statusCalls++
			// First probe: not yet checked in. Re-probe after claim: flipped.
			if statusCalls == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{"checked_in": false, "credits": 0, "enable": true})
			} else {
				_ = json.NewEncoder(w).Encode(map[string]any{"checked_in": true, "credits": 200})
			}
		case pathCheckinClaim:
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	seedCheckinCredential(t, store)

	result, err := client.Checkin(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "success" {
		t.Fatalf("result=%+v", result)
	}
	if result.RewardCredits != 200 {
		t.Fatalf("reward=%v", result.RewardCredits)
	}
	if len(paths) != 3 || paths[0] != pathCheckinStatus || paths[1] != pathCheckinClaim || paths[2] != pathCheckinStatus {
		t.Fatalf("paths=%v", paths)
	}
}

func TestCheckinAlreadyClaimedUpstream(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"checked_in": true, "credits": 100})
	}))
	seedCheckinCredential(t, store)

	result, err := client.Checkin(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "already" {
		t.Fatalf("result=%+v", result)
	}
}

func TestCheckinAlreadyByBusinessMessage(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 10001, "msg": "今日已签到，请明天再来"})
	}))
	seedCheckinCredential(t, store)

	result, err := client.Checkin(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "already" {
		t.Fatalf("result=%+v", result)
	}
}

func TestCheckinClaimAlreadyRace(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case pathCheckinStatus:
			_ = json.NewEncoder(w).Encode(map[string]any{"checked_in": false, "enable": true})
		case pathCheckinClaim:
			// Claim 与 status 之间出现 race: 状态查询时还已签到未发生,
			// 到 claim 时已经被其它通道签过,业务方返回 already 提示。
			_ = json.NewEncoder(w).Encode(map[string]any{"msg": "already checked in today"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	seedCheckinCredential(t, store)

	result, err := client.Checkin(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "already" {
		t.Fatalf("result=%+v", result)
	}
}

func TestCheckinDisabled(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"checked_in": false, "enable": false})
	}))
	seedCheckinCredential(t, store)

	result, err := client.Checkin(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "skipped" {
		t.Fatalf("result=%+v", result)
	}
}

func TestCheckinAuthDead(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "1001", "msg": "unauthorized"})
	}))
	seedCheckinCredential(t, store)

	_, err := client.Checkin(context.Background(), "acc1")
	if err == nil {
		t.Fatal("auth-dead must fail")
	}
	var already AlreadyCheckedInError
	if errors.As(err, &already) {
		t.Fatalf("auth-dead must not map to already: %v", err)
	}
}

func TestCheckinRegisteredOnAdapter(t *testing.T) {
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	adapter := client.Adapter()
	if adapter.Checkin == nil {
		t.Fatal("checkin must be registered on the trae adapter")
	}
}
