package kimi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// kimiServer is a stub of the Kimi OAuth host plus the managed coding API, so a
// test can drive the real HTTP paths of the adapter.
type kimiServer struct {
	mu sync.Mutex

	server *httptest.Server

	deviceRequests int
	tokenPolls     int
	pollSequence   []tokenPayload

	refreshPayload tokenPayload
	refreshStatus  int
	refreshPolls   int

	modelsBody string
	modelsHits int

	chatStatus       int
	chatErrorBody    string
	chatSSE          string
	chatRequests     []capturedChat
	unauthorizedOnce bool

	usageStatus int
	usageBody   string
	meStatus    int
	meBody      string

	denied  map[string]bool
	denyAll bool

	requests []capturedRequest
}

type capturedRequest struct {
	Method string
	Path   string
	Header http.Header
	Body   string
}

type capturedChat struct {
	Authorization string
	UserAgent     string
	Platform      string
	DeviceID      string
	Body          map[string]any
}

func newKimiServer(t *testing.T) *kimiServer {
	t.Helper()
	s := &kimiServer{usageStatus: http.StatusOK, meStatus: http.StatusOK, denied: map[string]bool{}}
	s.modelsBody = `{"object":"list","data":[
		{"id":"k3","display_name":"K3","limit":{"context":1048576},
		 "reasoning":{"type":"only","effort":{"valid":["low","high","max"],"default":"max"}},
		 "modalities":{"input":["text","image"]},"supports_tool_use":true},
		{"id":"kimi-for-coding","limit":{"context":262144,"output":32000},
		 "reasoning":{"type":"both","effort":{"valid":["low","high"],"default":"high"}}}]}`
	s.meBody = `{"user_id":"u_1","nickname":"moonwalker","user_level_name":"Vivace","region":"REGION_CN"}`
	s.usageBody = `{"usages":{"limit_5h":{"used_ratio":0.25,"reset_time":"2026-09-11T18:00:00Z"},
		"limit_7d":{"used_ratio":0.5}}}`
	s.chatSSE = strings.Join([]string{
		`data: {"id":"c1","choices":[{"index":0,"delta":{"reasoning_content":"think "},"finish_reason":null}]}`,
		``,
		`data: {"id":"c1","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`,
		``,
		`data: {"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"Read","arguments":"{\"path\":"}}]},"finish_reason":null}]}`,
		``,
		`data: {"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a.go\"}"}}]},"finish_reason":null}]}`,
		``,
		`data: {"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		``,
		`data: {"id":"c1","choices":[],"usage":{"prompt_tokens":11,"completion_tokens":7,"prompt_tokens_details":{"cached_tokens":3}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	s.refreshPayload = tokenPayload{AccessToken: "fresh-at", RefreshToken: "fresh-rt"}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.server.Close)
	return s
}

func (s *kimiServer) baseURL() string { return s.server.URL + "/coding/v1" }

func (s *kimiServer) capture(r *http.Request, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, capturedRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		Header: r.Header.Clone(),
		Body:   string(body),
	})
}

func (s *kimiServer) allow(r *http.Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.denyAll {
		return false
	}
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	return !s.denied[strings.TrimPrefix(header, "Bearer ")]
}

func (s *kimiServer) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	s.capture(r, body)

	switch {
	case r.URL.Path == DeviceAuthorizationPath:
		s.mu.Lock()
		s.deviceRequests++
		count := s.deviceRequests
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"user_code":"ABCD-EFGH","device_code":"dev-code-%d","verification_uri":"https://www.kimi.com/code/authorize_device","verification_uri_complete":"https://www.kimi.com/code/authorize_device?user_code=ABCD-EFGH","expires_in":1800,"interval":5}`, count)
	case r.URL.Path == TokenPath:
		s.handleToken(w, body)
	case r.URL.Path == "/coding/v1/models":
		s.mu.Lock()
		s.modelsHits++
		s.mu.Unlock()
		if !s.allow(r) {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error":{"message":"invalid token"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, s.modelsBody)
	case r.URL.Path == "/coding/v1/chat/completions":
		s.handleChat(w, r, body)
	case r.URL.Path == "/coding/v1/usages":
		if !s.allow(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if s.usageStatus != http.StatusOK {
			w.WriteHeader(s.usageStatus)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, s.usageBody)
	case r.URL.Path == "/coding/v1/me":
		if !s.allow(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if s.meStatus != http.StatusOK {
			w.WriteHeader(s.meStatus)
			return
		}
		io.WriteString(w, s.meBody)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *kimiServer) handleToken(w http.ResponseWriter, body []byte) {
	form, _ := url.ParseQuery(string(body))
	w.Header().Set("Content-Type", "application/json")
	switch form.Get("grant_type") {
	case DeviceGrantType:
		s.mu.Lock()
		s.tokenPolls++
		index := s.tokenPolls - 1
		var payload tokenPayload
		if index < len(s.pollSequence) {
			payload = s.pollSequence[index]
		} else {
			payload = tokenPayload{AccessToken: "access-from-device", RefreshToken: "refresh-from-device"}
		}
		if payload.AccessToken == "" && payload.Error == "" {
			payload.Error = "authorization_pending"
		}
		s.mu.Unlock()
		json.NewEncoder(w).Encode(payload)
	case "refresh_token":
		s.mu.Lock()
		s.refreshPolls++
		status := s.refreshStatus
		payload := s.refreshPayload
		s.mu.Unlock()
		if status != 0 && status != http.StatusOK {
			w.WriteHeader(status)
		}
		json.NewEncoder(w).Encode(payload)
	default:
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"unsupported_grant_type"}`)
	}
}

func (s *kimiServer) handleChat(w http.ResponseWriter, r *http.Request, body []byte) {
	var decoded map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	_ = decoder.Decode(&decoded)
	s.mu.Lock()
	s.chatRequests = append(s.chatRequests, capturedChat{
		Authorization: r.Header.Get("Authorization"),
		UserAgent:     r.Header.Get("User-Agent"),
		Platform:      r.Header.Get("X-Msh-Platform"),
		DeviceID:      r.Header.Get("X-Msh-Device-Id"),
		Body:          decoded,
	})
	status := s.chatStatus
	errorBody := s.chatErrorBody
	sse := s.chatSSE
	unauthorized := s.unauthorizedOnce
	if unauthorized {
		s.unauthorizedOnce = false
	}
	s.mu.Unlock()

	if unauthorized {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":{"message":"token expired"}}`)
		return
	}
	if status != 0 && status != http.StatusOK {
		w.WriteHeader(status)
		if errorBody != "" {
			io.WriteString(w, errorBody)
			return
		}
		io.WriteString(w, `{"error":{"message":"upstream error"}}`)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, sse)
}

func (s *kimiServer) lastRequest(path string) (capturedRequest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.requests) - 1; i >= 0; i-- {
		if s.requests[i].Path == path {
			return s.requests[i], true
		}
	}
	return capturedRequest{}, false
}

func (s *kimiServer) lastChat() capturedChat {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.chatRequests) == 0 {
		return capturedChat{}
	}
	return s.chatRequests[len(s.chatRequests)-1]
}

func (s *kimiServer) chatAttempts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.chatRequests)
}

func (s *kimiServer) refreshCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refreshPolls
}

func (s *kimiServer) devicePollCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokenPolls
}

// testRig wires a client whose account credentials point at the stub.
type testRig struct {
	store  *fakeStore
	client *Client
	server *kimiServer
}

func newRig(t *testing.T) *testRig {
	t.Helper()
	server := newKimiServer(t)
	store := newFakeStore()
	client := NewClient(store)
	client.nowFn = func() time.Time { return time.Unix(1_700_000_000, 0) }
	client.authHost = server.server.URL
	client.baseURL = server.baseURL()
	store.addAccount("acc-1", "cn")
	return &testRig{store: store, client: client, server: server}
}

func (r *testRig) seedCredential(t *testing.T, accountID, accessToken, refreshToken string, expiresAt int64) {
	t.Helper()
	r.store.setCredential(accountID, Credential{
		Format:       CredentialFormat,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
		DeviceID:     "device-" + accountID,
		OAuthHost:    r.server.server.URL,
		BaseURL:      r.server.baseURL(),
	})
}

func TestDeviceLoginFlowPersistsCredential(t *testing.T) {
	rig := newRig(t)
	rig.server.pollSequence = []tokenPayload{
		{Error: "authorization_pending"},
		{AccessToken: "device-at", RefreshToken: "device-rt", ExpiresIn: json.RawMessage(`3600`)},
	}
	ctx := context.Background()

	session, err := rig.client.StartLogin(ctx, "acc-1")
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	if !strings.Contains(session.AuthURL, "authorize_device") {
		t.Fatalf("auth url = %q", session.AuthURL)
	}
	if session.State != "ABCD-EFGH" {
		t.Fatalf("state = %q", session.State)
	}
	// Device authorization must not carry a bearer token, but does send the
	// device identity.
	request, ok := rig.server.lastRequest(DeviceAuthorizationPath)
	if !ok {
		t.Fatal("device authorization request missing")
	}
	if request.Header.Get("Authorization") != "" {
		t.Fatal("device authorization must not send an Authorization header")
	}
	if request.Header.Get("X-Msh-Device-Id") == "" {
		t.Fatal("device authorization must send the device identity")
	}

	done, message, err := rig.client.PollLogin(ctx, "acc-1")
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if done || !strings.Contains(message, "ABCD-EFGH") {
		t.Fatalf("pending poll = %v %q", done, message)
	}

	// Skip the politeness throttle for the next poll.
	rig.client.mu.Lock()
	rig.client.logins["acc-1"].interval = 0
	rig.client.mu.Unlock()

	done, message, err = rig.client.PollLogin(ctx, "acc-1")
	if err != nil {
		t.Fatalf("poll 2: %v", err)
	}
	if !done {
		t.Fatalf("poll 2 = %v %q", done, message)
	}
	credential, ok := rig.store.credential("acc-1")
	if !ok {
		t.Fatal("credential not persisted")
	}
	if credential.AccessToken != "device-at" || credential.RefreshToken != "device-rt" {
		t.Fatalf("tokens = %q/%q", credential.AccessToken, credential.RefreshToken)
	}
	if credential.ExpiresAt != time.Unix(1_700_000_000, 0).Unix()+3600 {
		t.Fatalf("expires_at = %d", credential.ExpiresAt)
	}
	if credential.UserID != "u_1" || credential.Plan != "Vivace" {
		t.Fatalf("profile = %+v", credential)
	}
	if credential.DeviceID == "" {
		t.Fatal("device id must be persisted")
	}
	if len(rig.store.observed) == 0 || !strings.Contains(rig.store.observed[len(rig.store.observed)-1], "ready") {
		t.Fatalf("observe calls = %v", rig.store.observed)
	}
}

func TestDeviceLoginDeniedIsAnError(t *testing.T) {
	rig := newRig(t)
	rig.server.pollSequence = []tokenPayload{{Error: "access_denied"}}
	ctx := context.Background()
	if _, err := rig.client.StartLogin(ctx, "acc-1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	rig.client.mu.Lock()
	rig.client.logins["acc-1"].interval = 0
	rig.client.mu.Unlock()
	if _, _, err := rig.client.PollLogin(ctx, "acc-1"); err == nil {
		t.Fatal("denied authorization must fail")
	}
}

func TestDeviceLoginExpiredCodeStartsANewRound(t *testing.T) {
	rig := newRig(t)
	rig.server.pollSequence = []tokenPayload{{Error: "expired_token"}}
	ctx := context.Background()
	if _, err := rig.client.StartLogin(ctx, "acc-1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	rig.client.mu.Lock()
	rig.client.logins["acc-1"].interval = 0
	rig.client.mu.Unlock()
	done, message, err := rig.client.PollLogin(ctx, "acc-1")
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if done {
		t.Fatal("expired code must not report done")
	}
	if !strings.Contains(message, "expired") {
		t.Fatalf("message = %q", message)
	}
	rig.server.mu.Lock()
	count := rig.server.deviceRequests
	rig.server.mu.Unlock()
	if count != 2 {
		t.Fatalf("device rounds = %d, want 2", count)
	}
}

func TestRefreshRotatesTokenAndPersists(t *testing.T) {
	rig := newRig(t)
	now := time.Unix(1_700_000_000, 0)
	rig.seedCredential(t, "acc-1", "old-at", "old-rt", now.Add(time.Minute).Unix())
	rig.server.refreshPayload = tokenPayload{AccessToken: "new-at", RefreshToken: "new-rt", ExpiresIn: json.RawMessage(`7200`)}

	credential, err := rig.client.credential(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("credential: %v", err)
	}
	if credential.AccessToken != "new-at" {
		t.Fatalf("access token = %q", credential.AccessToken)
	}
	stored, _ := rig.store.credential("acc-1")
	if stored.AccessToken != "new-at" || stored.RefreshToken != "new-rt" {
		t.Fatalf("persisted = %q/%q", stored.AccessToken, stored.RefreshToken)
	}
	if stored.ExpiresAt != rig.client.now().Unix()+7200 {
		t.Fatalf("expires_at = %d", stored.ExpiresAt)
	}
	request, _ := rig.server.lastRequest(TokenPath)
	if !strings.Contains(request.Body, "grant_type=refresh_token") {
		t.Fatalf("refresh body = %q", request.Body)
	}
	if !strings.Contains(request.Body, "client_id="+ClientID) {
		t.Fatalf("refresh body must carry the client id: %q", request.Body)
	}
}

func TestRefreshRejectedRefreshTokenIsAuthFailure(t *testing.T) {
	rig := newRig(t)
	now := time.Unix(1_700_000_000, 0)
	rig.seedCredential(t, "acc-1", "old-at", "revoked-rt", now.Add(time.Minute).Unix())
	rig.server.refreshStatus = http.StatusUnauthorized
	rig.server.refreshPayload = tokenPayload{Error: "invalid_grant"}

	_, err := rig.client.credential(context.Background(), "acc-1")
	if err == nil {
		t.Fatal("rejected refresh must fail")
	}
	providerErr, ok := err.(*providers.Error)
	if !ok || providerErr.Kind != accounts.KindAuth {
		t.Fatalf("error = %#v", err)
	}
}

func TestChatStreamSendsKimiBodyAndRelaysSSE(t *testing.T) {
	rig := newRig(t)
	now := time.Unix(1_700_000_000, 0)
	rig.seedCredential(t, "acc-1", "acct-1-at", "acct-1-rt", now.Add(2*time.Hour).Unix())

	req := translate.ChatRequest{
		Model:           "k3",
		Messages:        []translate.ChatMessage{{Role: "user", Content: "hello"}},
		Temperature:     json.RawMessage(`0.3`),
		ReasoningEffort: json.RawMessage(`"max"`),
		Tools:           json.RawMessage(`[{"type":"function","function":{"name":"Read","parameters":{"type":"object"}}}]`),
	}
	resp, resolved, err := rig.client.ChatStream(context.Background(), "acc-1", req)
	if err != nil {
		t.Fatalf("chat stream: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "hello") {
		t.Fatalf("stream body = %q", body)
	}
	if resolved.ReasoningLevel != "max" {
		t.Fatalf("reasoning level = %q", resolved.ReasoningLevel)
	}
	chat := rig.server.lastChat()
	if chat.Authorization != "Bearer acct-1-at" {
		t.Fatalf("authorization = %q", chat.Authorization)
	}
	if chat.Platform != Platform || chat.DeviceID != "device-acc-1" {
		t.Fatalf("identity headers = %q/%q", chat.Platform, chat.DeviceID)
	}
	if chat.UserAgent != UserAgent {
		t.Fatalf("user agent = %q", chat.UserAgent)
	}
	if chat.Body["model"] != "k3" {
		t.Fatalf("model = %#v", chat.Body["model"])
	}
	thinking, ok := chat.Body["thinking"].(map[string]any)
	if !ok || thinking["effort"] != "max" {
		t.Fatalf("thinking = %#v", chat.Body["thinking"])
	}
	if _, ok := chat.Body["temperature"]; ok {
		t.Fatal("rejected temperature must be omitted")
	}
	if tools, ok := chat.Body["tools"].([]any); !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v", chat.Body["tools"])
	}
}

func TestChatNonStreamCollectsToolCallsAndUsage(t *testing.T) {
	rig := newRig(t)
	now := time.Unix(1_700_000_000, 0)
	rig.seedCredential(t, "acc-1", "acct-1-at", "acct-1-rt", now.Add(2*time.Hour).Unix())

	req := translate.ChatRequest{
		Model:    "kimi-for-coding",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hello"}},
	}
	outcome, err := rig.client.ChatNonStream(context.Background(), "acc-1", req)
	if err != nil {
		t.Fatalf("chat non stream: %v", err)
	}
	if outcome.Content != "hello" {
		t.Fatalf("content = %q", outcome.Content)
	}
	if outcome.Reasoning != "think " {
		t.Fatalf("reasoning = %q", outcome.Reasoning)
	}
	if outcome.FinishReason != "tool_calls" {
		t.Fatalf("finish = %q", outcome.FinishReason)
	}
	if outcome.PromptTokens != 11 || outcome.CompletionTokens != 7 {
		t.Fatalf("usage = %d/%d", outcome.PromptTokens, outcome.CompletionTokens)
	}
	if outcome.CacheReadTokens == nil || *outcome.CacheReadTokens != 3 {
		t.Fatalf("cached tokens = %v", outcome.CacheReadTokens)
	}
	var calls []map[string]any
	if err := json.Unmarshal(outcome.ToolCalls, &calls); err != nil {
		t.Fatalf("tool calls: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("tool calls = %#v", calls)
	}
	function := calls[0]["function"].(map[string]any)
	if function["name"] != "Read" || function["arguments"] != `{"path":"a.go"}` {
		t.Fatalf("tool call = %#v", calls[0])
	}
}

func TestChatRefreshesOnceOnUnauthorized(t *testing.T) {
	rig := newRig(t)
	now := time.Unix(1_700_000_000, 0)
	rig.seedCredential(t, "acc-1", "stale-at", "old-rt", now.Add(2*time.Hour).Unix())
	rig.server.refreshPayload = tokenPayload{AccessToken: "rotated-at", RefreshToken: "rotated-rt"}
	rig.server.unauthorizedOnce = true

	req := translate.ChatRequest{Model: "k3", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}}
	resp, _, err := rig.client.ChatStream(context.Background(), "acc-1", req)
	if err != nil {
		t.Fatalf("chat stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	stored, _ := rig.store.credential("acc-1")
	if stored.AccessToken != "rotated-at" {
		t.Fatalf("persisted token = %q", stored.AccessToken)
	}
	if attempts := rig.server.chatAttempts(); attempts != 2 {
		t.Fatalf("chat attempts = %d, want 2", attempts)
	}
}

func TestChatEntitlementFailureSkipsRefresh(t *testing.T) {
	rig := newRig(t)
	now := time.Unix(1_700_000_000, 0)
	rig.seedCredential(t, "acc-1", "acct-1-at", "acct-1-rt", now.Add(2*time.Hour).Unix())
	rig.server.chatStatus = http.StatusUnauthorized
	rig.server.chatErrorBody = `{"error":{"message":"Your current subscription does not have access to k3. Upgrade to higher-tier Kimi Code plans.","type":"invalid_authentication_error"}}`

	req := translate.ChatRequest{Model: "k3", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}}
	_, _, err := rig.client.ChatStream(context.Background(), "acc-1", req)
	if err == nil {
		t.Fatal("entitlement failure must surface")
	}
	providerErr, ok := err.(*providers.Error)
	if !ok || providerErr.Kind != accounts.KindModelNotAvailable {
		t.Fatalf("error = %#v", err)
	}
	if attempts := rig.server.chatAttempts(); attempts != 1 {
		t.Fatalf("chat attempts = %d, want 1", attempts)
	}
	rig.server.mu.Lock()
	tokenRequests := 0
	for _, request := range rig.server.requests {
		if request.Path == TokenPath {
			tokenRequests++
		}
	}
	rig.server.mu.Unlock()
	if tokenRequests != 0 {
		t.Fatalf("token endpoint called %d times, want 0", tokenRequests)
	}
}

func TestChatClassifiesUpstreamError(t *testing.T) {
	rig := newRig(t)
	now := time.Unix(1_700_000_000, 0)
	rig.seedCredential(t, "acc-1", "acct-1-at", "acct-1-rt", now.Add(2*time.Hour).Unix())
	rig.server.chatStatus = http.StatusTooManyRequests

	req := translate.ChatRequest{Model: "k3", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}}
	_, _, err := rig.client.ChatStream(context.Background(), "acc-1", req)
	if err == nil {
		t.Fatal("upstream failure must surface")
	}
	providerErr, ok := err.(*providers.Error)
	if !ok || providerErr.Kind != accounts.KindRateLimit {
		t.Fatalf("error = %#v", err)
	}
}

func TestProbeUsesModelsAndRecordsProfile(t *testing.T) {
	rig := newRig(t)
	now := time.Unix(1_700_000_000, 0)
	rig.seedCredential(t, "acc-1", "acct-1-at", "acct-1-rt", now.Add(2*time.Hour).Unix())

	health, err := rig.client.Probe(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !health.Ready || health.UID != "u_1" {
		t.Fatalf("health = %+v", health)
	}
	stored, _ := rig.store.credential("acc-1")
	if stored.Plan != "Vivace" || stored.UserID != "u_1" {
		t.Fatalf("profile not persisted = %+v", stored)
	}
}

func TestProbeReportsAuthFailure(t *testing.T) {
	rig := newRig(t)
	now := time.Unix(1_700_000_000, 0)
	rig.seedCredential(t, "acc-1", "wrong-at", "acct-1-rt", now.Add(2*time.Hour).Unix())
	rig.server.denied["wrong-at"] = true

	health, err := rig.client.Probe(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if health.Ready {
		t.Fatalf("health = %+v, want not ready", health)
	}
	if health.LastError == "" {
		t.Fatal("probe must record the failure reason")
	}
}

func TestQuotaMapsSubscriptionWindows(t *testing.T) {
	rig := newRig(t)
	now := time.Unix(1_700_000_000, 0)
	rig.seedCredential(t, "acc-1", "acct-1-at", "acct-1-rt", now.Add(2*time.Hour).Unix())
	stored, _ := rig.store.credential("acc-1")
	stored.Plan = "Vivace"
	rig.store.setCredential("acc-1", stored)

	quota, err := rig.client.Quota(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("quota: %v", err)
	}
	if quota == nil {
		t.Fatal("quota must not be nil")
	}
	if len(quota.Windows) != 2 || quota.Percentage != 50 {
		t.Fatalf("quota = %+v", quota)
	}
	if quota.Plan != "Vivace" {
		t.Fatalf("plan = %q", quota.Plan)
	}
	request, _ := rig.server.lastRequest(usagesPath)
	if request.Header.Get("X-Msh-Device-Id") != "" {
		t.Fatal("/usages must not send device identity headers")
	}
}

func TestQuotaWithoutManagedApiReturnsNil(t *testing.T) {
	rig := newRig(t)
	now := time.Unix(1_700_000_000, 0)
	rig.seedCredential(t, "acc-1", "acct-1-at", "acct-1-rt", now.Add(2*time.Hour).Unix())
	rig.server.usageStatus = http.StatusNotFound

	quota, err := rig.client.Quota(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("quota: %v", err)
	}
	if quota != nil {
		t.Fatalf("quota = %+v, want nil", quota)
	}
}

func TestMultiAccountKeepsCredentialsAndCatalogsSeparate(t *testing.T) {
	rig := newRig(t)
	rig.store.addAccount("acc-2", "cn")
	now := time.Unix(1_700_000_000, 0)
	rig.seedCredential(t, "acc-1", "acct-1-at", "acct-1-rt", now.Add(2*time.Hour).Unix())
	rig.seedCredential(t, "acc-2", "acct-2-at", "acct-2-rt", now.Add(2*time.Hour).Unix())

	models, err := rig.client.Models(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("models acc-1: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %d", len(models))
	}
	// The second read must come from account 1's cache, not the network.
	rig.server.mu.Lock()
	hitsBefore := rig.server.modelsHits
	rig.server.mu.Unlock()
	if _, err := rig.client.Models(context.Background(), "acc-1"); err != nil {
		t.Fatalf("models acc-1 cached: %v", err)
	}
	rig.server.mu.Lock()
	hitsAfter := rig.server.modelsHits
	rig.server.mu.Unlock()
	if hitsAfter != hitsBefore {
		t.Fatal("catalog cache must serve the second read")
	}

	chatBody := translate.ChatRequest{Model: "k3", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}}
	if _, _, err := rig.client.ChatStream(context.Background(), "acc-2", chatBody); err != nil {
		t.Fatalf("chat acc-2: %v", err)
	}
	if got := rig.server.lastChat().Authorization; got != "Bearer acct-2-at" {
		t.Fatalf("account 2 used %q", got)
	}
	if _, _, err := rig.client.ChatStream(context.Background(), "acc-1", chatBody); err != nil {
		t.Fatalf("chat acc-1: %v", err)
	}
	if got := rig.server.lastChat().Authorization; got != "Bearer acct-1-at" {
		t.Fatalf("account 1 used %q", got)
	}
	// Refreshing account 2 must not touch account 1's stored token.
	rig.seedCredential(t, "acc-2", "acct-2-stale", "acct-2-rt", now.Add(time.Minute).Unix())
	rig.server.refreshPayload = tokenPayload{AccessToken: "acct-2-rotated", RefreshToken: "acct-2-rt-2"}
	credential, err := rig.client.credential(context.Background(), "acc-2")
	if err != nil {
		t.Fatalf("credential acc-2: %v", err)
	}
	if credential.AccessToken != "acct-2-rotated" {
		t.Fatalf("account 2 token = %q", credential.AccessToken)
	}
	first, _ := rig.store.credential("acc-1")
	if first.AccessToken != "acct-1-at" {
		t.Fatalf("account 1 token changed to %q", first.AccessToken)
	}
	second, _ := rig.store.credential("acc-2")
	if second.AccessToken != "acct-2-rotated" || second.RefreshToken != "acct-2-rt-2" {
		t.Fatalf("account 2 persisted = %q/%q", second.AccessToken, second.RefreshToken)
	}
}

// storedReasoningKey mirrors the key the adapter reads at chat time.
func storedReasoningKey(model string) string {
	return ProviderID + "|" + accounts.CanonicalModelID(model)
}

func (r *testRig) setStoredReasoning(model, effort string) {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	r.store.settings[storedReasoningKey(model)] = accounts.ProviderModelSetting{ReasoningEffort: effort}
}

func (r *testRig) loginState() *loginState {
	r.client.mu.Lock()
	defer r.client.mu.Unlock()
	state := r.client.logins["acc-1"]
	if state == nil {
		return nil
	}
	copied := *state
	return &copied
}

func (r *testRig) bypassPollThrottle() {
	r.client.mu.Lock()
	defer r.client.mu.Unlock()
	if state := r.client.logins["acc-1"]; state != nil {
		state.lastPolled = r.client.now().Add(-time.Hour)
	}
}

// TestReasoningPriority exercises the order the console relies on: the client
// value wins, then the saved console default, then the catalog default, and the
// result is always clamped to the model's declared ladder.
func TestReasoningPriority(t *testing.T) {
	cases := []struct {
		name      string
		stored    string
		requested string
		want      string
	}{
		{"catalog default when nothing is saved", "", "", "high"},
		{"saved console default beats the catalog default", "low", "", "low"},
		{"client value beats the saved default", "low", "high", "high"},
		{"saved default is clamped to the declared ladder", "medium", "", "high"},
		{"client value is clamped to the declared ladder", "", "xhigh", "high"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := newRig(t)
			now := time.Unix(1_700_000_000, 0)
			rig.seedCredential(t, "acc-1", "acct-1-at", "acct-1-rt", now.Add(2*time.Hour).Unix())
			if tc.stored != "" {
				rig.setStoredReasoning("kimi-for-coding", tc.stored)
			}
			req := translate.ChatRequest{
				Model:    "kimi-for-coding",
				Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
			}
			if tc.requested != "" {
				req.ReasoningEffort = json.RawMessage(`"` + tc.requested + `"`)
			}
			resp, resolved, err := rig.client.ChatStream(context.Background(), "acc-1", req)
			if err != nil {
				t.Fatalf("chat: %v", err)
			}
			resp.Body.Close()
			if resolved.ReasoningLevel != tc.want {
				t.Fatalf("resolved level = %q, want %q", resolved.ReasoningLevel, tc.want)
			}
			thinking, ok := rig.server.lastChat().Body["thinking"].(map[string]any)
			if !ok {
				t.Fatalf("thinking = %#v", rig.server.lastChat().Body["thinking"])
			}
			if thinking["effort"] != tc.want {
				t.Fatalf("sent effort = %#v, want %q", thinking["effort"], tc.want)
			}
		})
	}
}

func TestRefreshPersistFailureFailsTheRequest(t *testing.T) {
	rig := newRig(t)
	now := time.Unix(1_700_000_000, 0)
	rig.seedCredential(t, "acc-1", "old-at", "old-rt", now.Add(time.Minute).Unix())
	rig.server.refreshPayload = tokenPayload{AccessToken: "new-at", RefreshToken: "new-rt"}
	rig.store.saveErr = errors.New("disk full")

	req := translate.ChatRequest{Model: "kimi-for-coding", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}}
	_, _, err := rig.client.ChatStream(context.Background(), "acc-1", req)
	if err == nil {
		t.Fatal("an unpersisted rotation must fail the request")
	}
	providerErr, ok := err.(*providers.Error)
	if !ok || providerErr.Kind != accounts.KindUnavailable {
		t.Fatalf("error = %#v", err)
	}
	if !strings.Contains(providerErr.Message, "could not be saved") {
		t.Fatalf("message = %q", providerErr.Message)
	}
	// Nothing may be served from the unpersisted credential.
	if attempts := rig.server.chatAttempts(); attempts != 0 {
		t.Fatalf("chat attempts = %d, want 0", attempts)
	}
	stored, _ := rig.store.credential("acc-1")
	if stored.AccessToken != "old-at" || stored.RefreshToken != "old-rt" {
		t.Fatalf("stored credential changed to %q/%q", stored.AccessToken, stored.RefreshToken)
	}
}

func TestForceRefreshPersistFailureSurfaces(t *testing.T) {
	rig := newRig(t)
	now := time.Unix(1_700_000_000, 0)
	rig.seedCredential(t, "acc-1", "stale-at", "old-rt", now.Add(2*time.Hour).Unix())
	rig.server.refreshPayload = tokenPayload{AccessToken: "rotated-at", RefreshToken: "rotated-rt"}
	rig.server.unauthorizedOnce = true
	rig.store.saveErr = errors.New("read-only filesystem")

	req := translate.ChatRequest{Model: "kimi-for-coding", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}}
	_, _, err := rig.client.ChatStream(context.Background(), "acc-1", req)
	if err == nil {
		t.Fatal("an unpersisted rotation must fail the request")
	}
	providerErr, ok := err.(*providers.Error)
	if !ok || providerErr.Kind != accounts.KindUnavailable {
		t.Fatalf("error = %#v", err)
	}
	if got := rig.server.lastChat().Authorization; got != "Bearer stale-at" {
		t.Fatalf("authorization = %q", got)
	}
}

func TestConcurrentRefreshIsSingleFlight(t *testing.T) {
	rig := newRig(t)
	now := time.Unix(1_700_000_000, 0)
	rig.seedCredential(t, "acc-1", "old-at", "old-rt", now.Add(time.Minute).Unix())
	rig.server.refreshPayload = tokenPayload{
		AccessToken:  "new-at",
		RefreshToken: "new-rt",
		ExpiresIn:    json.RawMessage(`7200`),
	}

	const workers = 8
	tokens := make([]string, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			credential, err := rig.client.credential(context.Background(), "acc-1")
			errs[index] = err
			tokens[index] = credential.AccessToken
		}(i)
	}
	wg.Wait()

	for i := 0; i < workers; i++ {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		if tokens[i] != "new-at" {
			t.Fatalf("worker %d token = %q", i, tokens[i])
		}
	}
	if refreshes := rig.server.refreshCount(); refreshes != 1 {
		t.Fatalf("refresh calls = %d, want 1", refreshes)
	}
}

// TestDeviceLoginSlowDownBacksOff covers RFC 8628 §3.5: slow_down widens the
// interval, consecutive slow_downs keep widening it, and the normal completion
// still lands.
func TestDeviceLoginSlowDownBacksOff(t *testing.T) {
	rig := newRig(t)
	rig.server.pollSequence = []tokenPayload{
		{Error: "slow_down"},
		{Error: "slow_down"},
		{AccessToken: "device-at", RefreshToken: "device-rt", ExpiresIn: json.RawMessage(`3600`)},
	}
	ctx := context.Background()
	if _, err := rig.client.StartLogin(ctx, "acc-1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if state := rig.loginState(); state == nil || state.interval != pollFloor {
		t.Fatalf("initial interval = %v", state.interval)
	}

	done, _, err := rig.client.PollLogin(ctx, "acc-1")
	if err != nil || done {
		t.Fatalf("poll 1 = %v %v", done, err)
	}
	if got := rig.loginState().interval; got != pollFloor+pollSlowDownStep {
		t.Fatalf("interval after first slow_down = %v", got)
	}

	// The console polls on a fixed cadence; the throttle must swallow this call
	// instead of hitting the token endpoint again.
	pollsBefore := rig.server.devicePollCount()
	if _, _, err := rig.client.PollLogin(ctx, "acc-1"); err != nil {
		t.Fatalf("throttled poll: %v", err)
	}
	if polls := rig.server.devicePollCount(); polls != pollsBefore {
		t.Fatalf("throttled poll hit the token endpoint: %d -> %d", pollsBefore, polls)
	}

	rig.bypassPollThrottle()
	if _, _, err := rig.client.PollLogin(ctx, "acc-1"); err != nil {
		t.Fatalf("poll 2: %v", err)
	}
	if got := rig.loginState().interval; got != pollFloor+2*pollSlowDownStep {
		t.Fatalf("interval after second slow_down = %v", got)
	}

	rig.bypassPollThrottle()
	done, message, err := rig.client.PollLogin(ctx, "acc-1")
	if err != nil {
		t.Fatalf("poll 3: %v", err)
	}
	if !done {
		t.Fatalf("poll 3 = %v %q", done, message)
	}
	if _, ok := rig.store.credential("acc-1"); !ok {
		t.Fatal("credential not persisted after a slowed-down login")
	}
}

func TestDeviceLoginPendingDoesNotWidenInterval(t *testing.T) {
	rig := newRig(t)
	rig.server.pollSequence = []tokenPayload{{Error: "authorization_pending"}}
	ctx := context.Background()
	if _, err := rig.client.StartLogin(ctx, "acc-1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, _, err := rig.client.PollLogin(ctx, "acc-1"); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if got := rig.loginState().interval; got != pollFloor {
		t.Fatalf("pending widened the interval to %v", got)
	}
}
