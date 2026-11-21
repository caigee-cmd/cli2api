package minimaxcode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

type memoryStore struct {
	mu         sync.Mutex
	payload    []byte
	account    accounts.Account
	observedID string
}

func (s *memoryStore) Get(context.Context, string) (accounts.Account, error) { return s.account, nil }
func (s *memoryStore) LoadCredentialPayload(context.Context, string) (string, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return CredentialFormat, append([]byte(nil), s.payload...), nil
}
func (s *memoryStore) SaveCredentialPayload(_ context.Context, _, _ string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.payload = append([]byte(nil), payload...)
	return nil
}
func (s *memoryStore) Observe(_ context.Context, _ string, remoteID, _, _, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observedID = remoteID
	return nil
}

func newTestClient(t *testing.T, accountBase, agentBase string) (*Client, *memoryStore) {
	t.Helper()
	store := &memoryStore{account: accounts.Account{Enabled: true}}
	cred := Credential{Format: CredentialFormat, AccessToken: "tok", Site: "cn", AccountBase: accountBase, AgentBase: agentBase, Region: "cn", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	payload, _ := cred.Encode()
	store.payload = payload
	client := NewClient(store)
	client.http = &http.Client{}
	acceptTestEndpoint = true
	return client, store
}

func TestCredentialRoundTrip(t *testing.T) {
	cred := Credential{AccessToken: "access", RefreshToken: "refresh", Site: "global", ExpiresIn: 3600}
	payload, err := cred.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCredential(payload)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.AccessToken != "access" || decoded.Site != "global" {
		t.Fatalf("decoded = %+v", decoded)
	}
}

func TestDeviceLoginFlow(t *testing.T) {
	deviceCodeIssued := false
	tokenApproved := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case deviceCodePath:
			deviceCodeIssued = true
			_, _ = io.WriteString(w, `{"device_code":"dev-1","user_code":"ABCD-1234","verification_uri_complete":"https://example.com/device","expires_in":600,"interval":1}`)
		case tokenPath:
			if !tokenApproved {
				_, _ = io.WriteString(w, `{"status":"pending"}`)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, _ := newTestClient(t, server.URL, server.URL)
	// Login starts against the credential's account base.
	session, err := client.StartLogin(context.Background(), "acct-1")
	if err != nil {
		t.Fatal(err)
	}
	if session.AuthURL != "https://example.com/device" || session.State != "ABCD-1234" {
		t.Fatalf("session = %+v", session)
	}
	if !deviceCodeIssued {
		t.Fatal("device code endpoint not called")
	}
	done, msg, err := client.PollLogin(context.Background(), "acct-1")
	if err != nil || done {
		t.Fatalf("pending poll: done=%v msg=%q err=%v", done, msg, err)
	}
	if !strings.Contains(msg, "ABCD-1234") {
		t.Fatalf("user code not surfaced: %q", msg)
	}
	tokenApproved = true
	// Force the next poll past the interval gate.
	client.mu.Lock()
	client.logins["acct-1"].lastPolled = time.Time{}
	client.mu.Unlock()
	done, _, err = client.PollLogin(context.Background(), "acct-1")
	if err != nil || !done {
		t.Fatalf("approval poll: done=%v err=%v", done, err)
	}
}

func TestChatNonStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == modelsPath {
			_, _ = io.WriteString(w, `{"models":[{"id":"MiniMax-M3","name":"MiniMax M3"}]}`)
			return
		}
		if r.URL.Path == messagesPath {
			if r.Header.Get("Authorization") != "Bearer tok" {
				t.Error("missing bearer")
			}
			if r.Header.Get("X-Mavis-Agent-Id") != AgentID {
				t.Error("missing mavis agent header")
			}
			body, _ := io.ReadAll(r.Body)
			var anthropic map[string]any
			if json.Unmarshal(body, &anthropic) != nil {
				t.Error("invalid anthropic body")
			}
			if anthropic["model"] != "MiniMax-M3" {
				t.Errorf("model = %v", anthropic["model"])
			}
			if anthropic["system"] != "be brief" {
				t.Errorf("system = %v", anthropic["system"])
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"msg-1","model":"MiniMax-M3","stop_reason":"end_turn","content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":3,"output_tokens":2}}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	client, _ := newTestClient(t, server.URL, server.URL)
	req := translate.ChatRequest{
		Model: "minimaxcode/MiniMax-M3",
		Messages: []translate.ChatMessage{
			{Role: "system", Content: "be brief"},
			{Role: "user", Content: "hi"},
		},
	}
	outcome, err := client.ChatNonStream(context.Background(), "acct-1", req)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Content != "hello" {
		t.Fatalf("content = %q", outcome.Content)
	}
	if outcome.PromptTokens != 3 || outcome.CompletionTokens != 2 {
		t.Fatalf("usage = %+v", outcome)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		status int
		body   string
		kind   string
	}{
		{401, `{"type":"error","error":{"message":"unauthorized"}}`, accounts.KindAuth},
		{402, `{"error":{"message":"insufficient balance"}}`, accounts.KindQuota},
		{429, `{"error":{"message":"rate limit"}}`, accounts.KindRateLimit},
		{500, `boom`, accounts.KindUnavailable},
	}
	for _, tc := range cases {
		got := Classify(tc.status, tc.body)
		if got.Kind != tc.kind {
			t.Errorf("status %d: kind = %q want %q", tc.status, got.Kind, tc.kind)
		}
	}
}

func TestModelsFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"unauthorized"}}`)
	}))
	defer server.Close()
	client, _ := newTestClient(t, server.URL, server.URL)
	models, err := client.Models(context.Background(), "acct-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != len(knownModels) {
		t.Fatalf("fallback models = %d", len(models))
	}
}

var _ = fmt.Sprintf

func newCheckinTestClient(t *testing.T, agentBase, userID string) (*Client, *memoryStore) {
	t.Helper()
	store := &memoryStore{account: accounts.Account{Enabled: true, RemoteUID: userID}}
	credential := Credential{
		Format:      CredentialFormat,
		AccessToken: "tok",
		Site:        "cn",
		AccountBase: agentBase,
		AgentBase:   agentBase,
		Region:      "cn",
		UserID:      userID,
		ExpiresAt:   time.Now().Add(time.Hour).Unix(),
	}
	payload, err := credential.Encode()
	if err != nil {
		t.Fatal(err)
	}
	store.payload = payload
	client := NewClient(store)
	client.http = &http.Client{}
	acceptTestEndpoint = true
	return client, store
}

func TestCheckinAlreadyToday(t *testing.T) {
	claimCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == signinClaimPath {
			claimCalled = true
			http.Error(w, "claim must not run when today is already signed", http.StatusTeapot)
			return
		}
		if r.URL.Path != signinStatusPath {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Error("missing bearer token")
		}
		if r.Header.Get("yy") == "" || len(r.Header.Get("x-signature")) != 32 || r.Header.Get("x-timestamp") == "" {
			t.Error("missing gateway signature headers")
		}
		if r.URL.Query().Get("user_id") != "u-1" || r.URL.Query().Get("device_platform") != "web" || r.URL.Query().Get("is_desktop") != "1" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"base_resp":{"status_code":0},"data":{"scene":2,"days":[
			{"day_no":1,"points":100,"status":3,"is_today":false},
			{"day_no":2,"points":200,"status":3,"is_today":false},
			{"day_no":3,"points":400,"status":3,"is_today":true},
			{"day_no":4,"points":500,"status":1,"is_today":false},
			{"day_no":5,"points":500,"status":1,"is_today":false},
			{"day_no":6,"points":500,"status":1,"is_today":false},
			{"day_no":7,"points":500,"status":1,"is_today":false}]}}`)
	}))
	defer server.Close()
	client, _ := newCheckinTestClient(t, server.URL, "u-1")
	result, err := client.Checkin(context.Background(), "acct-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "already" || result.RewardCredits != 400 {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(result.Message, "连签 3 天") {
		t.Fatalf("message = %q", result.Message)
	}
	if claimCalled {
		t.Fatal("claim endpoint must not run for an already signed day")
	}
}

func TestCheckinClaimsToday(t *testing.T) {
	claimBody := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case signinStatusPath:
			_, _ = io.WriteString(w, `{"base_resp":{"status_code":0},"data":{"scene":2,"days":[
				{"day_no":1,"points":100,"status":3,"is_today":false},
				{"day_no":2,"points":200,"status":3,"is_today":false},
				{"day_no":3,"points":400,"status":2,"is_today":true},
				{"day_no":4,"points":500,"status":1,"is_today":false},
				{"day_no":5,"points":500,"status":1,"is_today":false},
				{"day_no":6,"points":500,"status":1,"is_today":false},
				{"day_no":7,"points":500,"status":1,"is_today":false}]}}`)
		case signinClaimPath:
			if r.Method != http.MethodPost {
				t.Errorf("claim method = %s", r.Method)
			}
			raw, _ := io.ReadAll(r.Body)
			claimBody = string(raw)
			if len(r.Header.Get("x-signature")) != 32 {
				t.Error("claim missing signature")
			}
			_, _ = io.WriteString(w, `{"base_resp":{"status_code":0},"data":{"claim_id":"c-1","claim_result":1,"day_no":3,"points":400,"expire_at_ms":1795858082691}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, _ := newCheckinTestClient(t, server.URL, "u-1")
	result, err := client.Checkin(context.Background(), "acct-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "success" || result.RewardCredits != 400 {
		t.Fatalf("result = %+v", result)
	}
	if claimBody != "{}" {
		t.Fatalf("claim body = %q", claimBody)
	}
}

func TestCheckinSkipsWhenDisabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != signinStatusPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"base_resp":{"status_code":0},"data":{"scene":2,"days":[
			{"day_no":1,"points":100,"status":3,"is_today":false},
			{"day_no":2,"points":200,"status":3,"is_today":false},
			{"day_no":3,"points":400,"status":4,"is_today":true},
			{"day_no":4,"points":500,"status":1,"is_today":false},
			{"day_no":5,"points":500,"status":1,"is_today":false},
			{"day_no":6,"points":500,"status":1,"is_today":false},
			{"day_no":7,"points":500,"status":1,"is_today":false}]}}`)
	}))
	defer server.Close()
	client, _ := newCheckinTestClient(t, server.URL, "u-1")
	result, err := client.Checkin(context.Background(), "acct-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "skipped" || !strings.Contains(result.Message, "未开放") {
		t.Fatalf("result = %+v", result)
	}
}

func TestCheckinResolvesUserIDFromIdentity(t *testing.T) {
	statusUserID := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case userInfoPath:
			if r.URL.Query().Get("user_id") != "0" {
				t.Errorf("identity user_id = %q", r.URL.Query().Get("user_id"))
			}
			_, _ = io.WriteString(w, `{"base_resp":{"status_code":0},"data":{"userInfo":{"realUserID":"u-9"}}}`)
		case signinStatusPath:
			statusUserID = r.URL.Query().Get("user_id")
			_, _ = io.WriteString(w, `{"base_resp":{"status_code":0},"data":{"scene":2,"days":[
				{"day_no":3,"points":400,"status":3,"is_today":true}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, store := newCheckinTestClient(t, server.URL, "")
	result, err := client.Checkin(context.Background(), "acct-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "already" {
		t.Fatalf("result = %+v", result)
	}
	if statusUserID != "u-9" {
		t.Fatalf("status user_id = %q", statusUserID)
	}
	if store.observedID != "u-9" {
		t.Fatalf("observed id = %q", store.observedID)
	}
}

func TestCheckinRefreshesUnauthorizedToken(t *testing.T) {
	statusCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case signinStatusPath:
			statusCalls++
			if statusCalls == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if r.Header.Get("Authorization") != "Bearer new-token" {
				t.Errorf("retry authorization = %q", r.Header.Get("Authorization"))
			}
			_, _ = io.WriteString(w, `{"base_resp":{"status_code":0},"data":{"scene":2,"days":[
				{"day_no":3,"points":400,"status":3,"is_today":true}]}}`)
		case tokenPath:
			_, _ = io.WriteString(w, `{"access_token":"new-token","refresh_token":"new-refresh","expires_in":3600}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, _ := newCheckinTestClient(t, server.URL, "u-1")
	result, err := client.Checkin(context.Background(), "acct-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "already" || statusCalls != 2 {
		t.Fatalf("result = %+v, status calls = %d", result, statusCalls)
	}
}

func TestEncodeURIComponentMatchesJavaScript(t *testing.T) {
	cases := map[string]string{
		"/minimax-cloud/api/v1/signin/status?device_platform=web&user_id=u-1&lang=zh": "%2Fminimax-cloud%2Fapi%2Fv1%2Fsignin%2Fstatus%3Fdevice_platform%3Dweb%26user_id%3Du-1%26lang%3Dzh",
		"a b!~*'()":   "a%20b!~*'()",
		"timezone=+8": "timezone%3D%2B8",
	}
	for input, want := range cases {
		if got := encodeURIComponent(input); got != want {
			t.Errorf("encodeURIComponent(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestAdapterExposesCheckin(t *testing.T) {
	client, _ := newCheckinTestClient(t, "https://agent.minimax.cn", "u-1")
	if client.Adapter().Checkin == nil {
		t.Fatal("minimaxcode adapter must expose Checkin")
	}
}
