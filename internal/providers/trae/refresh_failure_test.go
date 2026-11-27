package trae

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// A rejected refresh token is terminal: the account genuinely needs a new
// login, so the status must say so.
func TestRefreshCredentialMarksLoginRequiredOnAuthRejection(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"ResponseMetadata":{"Error":{"Code":"1001"}}}`)
	}))
	store.items = map[string][]byte{"acc1": mustCredential(t)}

	_, err := client.refreshCredential(context.Background(), "acc1", Credential{RefreshToken: "rt", UID: "u1"})
	if err == nil {
		t.Fatal("rejected refresh must fail")
	}
	if store.lastStatus != "login_required" || store.lastKind != accounts.KindAuth {
		t.Fatalf("status=%q kind=%q, want login_required/auth", store.lastStatus, store.lastKind)
	}
	var classified *providers.Error
	if !errors.As(err, &classified) || classified.Kind != accounts.KindAuth {
		t.Fatalf("err=%v classified=%+v", err, classified)
	}
}

// A transport failure while renewing is transient. It must not ask the operator
// for a re-login, because the refresh token itself was never rejected.
func TestRefreshCredentialKeepsAccountRoutableOnTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := NewClient(&memStore{})
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}
	server.Close() // every attempt now fails at the transport layer

	got, err := client.refreshCredential(context.Background(), "acc1", Credential{RefreshToken: "rt", UID: "u1"})
	if err == nil {
		t.Fatal("transport failure must surface")
	}
	if refreshFailureIsAuth(err) {
		t.Fatalf("transport failure classified as auth: %v", err)
	}
	if got.RefreshToken != "rt" {
		t.Fatalf("credential must be returned unchanged: %+v", got)
	}
}

// A successful renewal clears a stale login_required left by an earlier outage,
// so the card stops asking for a re-login while the account is serving again.
func TestRefreshCredentialObservesReadyOnSuccess(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathExchangeCode {
			t.Fatalf("path=%s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
			"Token": "fresh-at", "TokenExpireAt": 4102444800,
		}})
	}))
	store.items = map[string][]byte{"acc1": mustCredential(t)}

	credential, err := client.refreshCredential(context.Background(), "acc1", Credential{RefreshToken: "rt", UID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken != "fresh-at" {
		t.Fatalf("credential=%+v", credential)
	}
	if store.lastStatus != "ready" {
		t.Fatalf("status=%q, want ready", store.lastStatus)
	}
}

// A 401 on the chat call means the access token was rejected before its
// reported expiry. Renew once and retry on the same account instead of failing
// the request and demanding a re-login.
func TestChatStream401RenewsOnceAndRetries(t *testing.T) {
	var chatCalls int
	payload, _ := Credential{AccessToken: "stale-at", RefreshToken: "rt", UID: "u1", ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathModels:
			_ = json.NewEncoder(w).Encode(map[string]any{"config_info_list": []map[string]any{{
				"config_name": "glm-5.2", "display_config": map[string]any{"display_name": "GLM-5.2"},
			}}})
		case pathExchangeCode:
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
				"Token": "fresh-at", "TokenExpireAt": 4102444800,
			}})
		default:
			chatCalls++
			if strings.TrimSpace(r.Header.Get("Authorization")) == "Cloud-IDE-JWT fresh-at" {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(soloSSE))
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"code":1001,"message":"unauthorized"}`)
		}
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}

	resp, _, err := client.ChatStream(context.Background(), "acc1", translate.ChatRequest{Model: "glm-5.2"})
	if err != nil {
		t.Fatalf("401 must be renewed and retried, got %v", err)
	}
	defer resp.Body.Close()
	if chatCalls != 2 {
		t.Fatalf("chat attempts=%d, want 2", chatCalls)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "data: [DONE]") {
		t.Fatalf("stream=%s", body)
	}
	if store.lastStatus != "ready" {
		t.Fatalf("status=%q, want ready after a successful renewal", store.lastStatus)
	}
}

// A transport failure during the 401 retry must surface as unavailable so the
// pool cools the account down and fails over, not as an auth rejection.
func TestChatStream401RefreshTransportFailureIsUnavailable(t *testing.T) {
	payload, _ := Credential{AccessToken: "stale-at", RefreshToken: "rt", UID: "u1", ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathModels:
			_ = json.NewEncoder(w).Encode(map[string]any{"config_info_list": []map[string]any{{
				"config_name": "glm-5.2", "display_config": map[string]any{"display_name": "GLM-5.2"},
			}}})
		case pathExchangeCode, pathExchange:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"message":"upstream exploded"}`)
		default:
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"code":1001,"message":"unauthorized"}`)
		}
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}

	resp, _, err := client.ChatStream(context.Background(), "acc1", translate.ChatRequest{Model: "glm-5.2"})
	if resp != nil {
		resp.Body.Close()
	}
	var classified *providers.Error
	if !errors.As(err, &classified) {
		t.Fatalf("err=%v", err)
	}
	if classified.Kind == accounts.KindAuth {
		t.Fatalf("non-auth refresh failure must not demand a re-login: %+v", classified)
	}
	if store.lastStatus == "login_required" {
		t.Fatal("non-auth refresh failure must not observe login_required")
	}
}

func mustCredential(t *testing.T) []byte {
	t.Helper()
	payload, err := (Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", ExpiresAt: 4102444800}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	return payload
}
