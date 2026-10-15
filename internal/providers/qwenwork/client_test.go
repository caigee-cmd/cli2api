package qwenwork

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

var (
	_ providers.LoginSessionProvider = (*Client)(nil)
	_ providers.ProviderChat         = (*Client)(nil)
	_ providers.ModelCatalogProvider = (*Client)(nil)
	_ providers.AccountProber        = (*Client)(nil)
	_ providers.CredentialCodec      = credentialCodec{}
	_ providers.CredentialImporter   = credentialCodec{}
)

type memStore struct {
	mu         sync.Mutex
	items      map[string][]byte
	region     string
	observed   []string
	lastStatus string
	lastKind   string
}

func (s *memStore) Get(ctx context.Context, id string) (accounts.Account, error) {
	region := s.region
	if region == "" {
		region = "global"
	}
	return accounts.Account{ID: id, Provider: "qwenwork", ProviderRegion: region}, nil
}
func (s *memStore) LoadCredentialPayload(ctx context.Context, accountID string) (string, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, ok := s.items[accountID]
	if !ok {
		return "", nil, accounts.ErrAccountNotFound
	}
	return CredentialFormat, payload, nil
}
func (s *memStore) SaveCredentialPayload(ctx context.Context, accountID, format string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		s.items = map[string][]byte{}
	}
	s.items[accountID] = payload
	return nil
}
func (s *memStore) Observe(ctx context.Context, id, remoteUID, status, lastError, lastKind string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observed = append(s.observed, id)
	s.lastStatus = status
	s.lastKind = lastKind
	return nil
}

type rewriteTransport struct {
	server string
	round  http.RoundTripper
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(t.server, "http://")
	if t.round == nil {
		return http.DefaultTransport.RoundTrip(req)
	}
	return t.round.RoundTrip(req)
}

func newTestClient(t *testing.T, store *memStore, handler http.Handler) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}
	return client
}

// wrapChunk renders one QwenWork wrapper frame around an OpenAI chunk.
func wrapChunk(chunk string, status int) string {
	encoded, _ := json.Marshal(chunk)
	var frame strings.Builder
	frame.WriteString("data:{\"headers\":{},\"body\":")
	frame.Write(encoded)
	frame.WriteString(",\"statusCodeValue\":")
	frame.WriteString(jsonInt(status))
	frame.WriteString("}\n\n")
	return frame.String()
}

func jsonInt(n int) string {
	encoded, _ := json.Marshal(n)
	return string(encoded)
}

var chatSSE = wrapChunk(`{"choices":[{"delta":{"role":"assistant","content":"O"}}]}`, 200) +
	wrapChunk(`{"choices":[{"delta":{"content":"K","reasoning_content":"think","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_time","arguments":"{}"}}]}}]}`, 200) +
	wrapChunk(`{"choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`, 200) +
	"data: [DONE]\n\n"

func readyCredential() Credential {
	return Credential{
		AccessToken:  "at",
		RefreshToken: "rt",
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
		UID:          "u1",
		Nickname:     "Tester",
		Email:        "t@example.com",
		MachineID:    "11111111-2222-4333-8444-555555555555",
	}
}

func TestDecodeCredentialAcceptsCanonicalAndCamelCase(t *testing.T) {
	canonical, err := DecodeCredential([]byte(`{"access_token":"at","refresh_token":"rt","expires_at":123,"uid":"u1"}`))
	if err != nil || canonical.AccessToken != "at" || canonical.UID != "u1" {
		t.Fatalf("canonical=%+v err=%v", canonical, err)
	}
	camel, err := DecodeCredential([]byte(`{"accessToken":"at2","refreshToken":"rt2","expiresAt":456,"uid":"u2","nickname":"n","email":"e"}`))
	if err != nil || camel.AccessToken != "at2" || camel.RefreshToken != "rt2" || camel.ExpiresAt != 456 {
		t.Fatalf("camel=%+v err=%v", camel, err)
	}
	if _, err := DecodeCredential([]byte(`{"refresh_token":"rt"}`)); err == nil {
		t.Fatal("missing access_token must be rejected")
	}
}

func TestPrepareImportAcceptsTokenPairAndFillsMachineID(t *testing.T) {
	// The qwenwork2api account export shape.
	imported, err := credentialCodec{}.PrepareImport([]byte(`{"accessToken":"at","refreshToken":"rt","uid":"u1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !imported.Ready {
		t.Fatal("token pair plus uid must import ready")
	}
	credential, err := DecodeCredential(imported.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if credential.MachineID == "" {
		t.Fatal("import must fill machine_id when absent")
	}
	disabled, err := credentialCodec{}.PrepareImport([]byte(`{"access_token":"at","refresh_token":"rt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Ready {
		t.Fatal("missing uid imports not-ready")
	}
}

func TestLoginAuthURLCarriesSiteParams(t *testing.T) {
	tests := []struct {
		region      string
		gateway     string
		clientID    string
		redirectURI string
	}{
		{region: "global", gateway: "https://gateway.qwenwork.ai", clientID: "cc65e5fc-05bd-4f5d-a4e8-0df19aa3d75a", redirectURI: "qwenwork://"},
		{region: "cn", gateway: "https://gateway.qwenwork.cn", clientID: "e883ade2-e6e3-4d6d-adf7-f92ceff5fdcb", redirectURI: "qwenwork-cn://"},
	}
	for _, test := range tests {
		t.Run(test.region, func(t *testing.T) {
			client := NewClient(&memStore{region: test.region})
			session, err := client.StartLogin(context.Background(), "acc1")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(session.AuthURL, test.gateway+"/device/selectAccounts?") {
				t.Fatalf("authURL=%s", session.AuthURL)
			}
			for _, want := range []string{
				"challenge=", "challenge_method=S256", "nonce=",
				"machine_id=", "client_id=" + test.clientID,
			} {
				if !strings.Contains(session.AuthURL, want) {
					t.Fatalf("authURL missing %s: %s", want, session.AuthURL)
				}
			}
			if !strings.Contains(session.AuthURL, "redirect_uri="+url.QueryEscape(test.redirectURI)) {
				t.Fatalf("authURL missing redirect_uri: %s", session.AuthURL)
			}
			if session.State == "" {
				t.Fatal("state (nonce) must be returned for polling")
			}
		})
	}
}

func TestLoginUnknownRegionErrors(t *testing.T) {
	client := NewClient(&memStore{region: "mars"})
	if _, err := client.StartLogin(context.Background(), "acc1"); err == nil || !strings.Contains(err.Error(), "unknown region") {
		t.Fatalf("err=%v", err)
	}
}

func TestLoginPollPendingThenGrant(t *testing.T) {
	var polls int32
	client := newTestClient(t, &memStore{region: "global"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case pathDevicePoll:
			atomic.AddInt32(&polls, 1)
			for _, want := range []string{"nonce", "verifier", "challenge_method"} {
				if r.URL.Query().Get(want) == "" {
					t.Errorf("poll query missing %s: %s", want, r.URL.RawQuery)
				}
			}
			if r.URL.Query().Get("challenge_method") != "S256" {
				t.Errorf("challenge_method=%s", r.URL.Query().Get("challenge_method"))
			}
			if r.Header.Get("User-Agent") != "qoderwork/1.0.4" {
				t.Errorf("poll UA=%s", r.Header.Get("User-Agent"))
			}
			if atomic.LoadInt32(&polls) == 1 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token": "at", "refresh_token": "rt", "expires_in": 3600,
				"user_id": "u1", "user_name": "Tester",
			})
		case pathUserInfo:
			if r.Header.Get("Authorization") != "Bearer at" {
				t.Errorf("userinfo auth=%s", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "u1", "name": "Tester", "email": "t@example.com"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	if _, err := client.StartLogin(context.Background(), "acc1"); err != nil {
		t.Fatal(err)
	}
	done, message, err := client.PollLogin(context.Background(), "acc1")
	if err != nil || done || message == "" {
		t.Fatalf("pending done=%v message=%q err=%v", done, message, err)
	}
	done, message, err = client.PollLogin(context.Background(), "acc1")
	if err != nil || !done {
		t.Fatalf("done=%v message=%q err=%v", done, message, err)
	}
	store := client.store.(*memStore)
	if store.lastStatus != "ready" || len(store.observed) != 1 {
		t.Fatalf("observe status=%s calls=%v", store.lastStatus, store.observed)
	}
	_, payload, err := store.LoadCredentialPayload(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := DecodeCredential(payload)
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken != "at" || credential.RefreshToken != "rt" || credential.UID != "u1" ||
		credential.Email != "t@example.com" || !credential.Ready() {
		t.Fatalf("credential=%+v", credential)
	}
	if credential.ExpiresAt <= time.Now().Unix() {
		t.Fatalf("expires_at=%d", credential.ExpiresAt)
	}
	if _, err := validateUUID(credential.MachineID); err != nil {
		t.Fatalf("machine id: %v", err)
	}
}

func validateUUID(id string) (string, error) {
	if len(id) != 36 || strings.Count(id, "-") != 4 {
		return "", errFake{message: "not a uuid: " + id}
	}
	return id, nil
}

type errFake struct{ message string }

func (e errFake) Error() string { return e.message }

func TestRefreshRotatesTokenPairAndSaves(t *testing.T) {
	expiring := readyCredential()
	expiring.ExpiresAt = time.Now().Add(time.Minute).Unix()
	payload, _ := expiring.Encode()
	store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
	client := newTestClient(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathDeviceRefresh {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method=%s", r.Method)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["refresh_token"] != "rt" {
			t.Fatalf("refresh body=%v err=%v", body, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "at2", "refresh_token": "rt2", "expires_in": 7200})
	}))
	// Lazy path: near-expiry credential refreshes before use.
	credential, err := client.credential(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken != "at2" || credential.RefreshToken != "rt2" {
		t.Fatalf("refreshed=%+v", credential)
	}
	_, saved, err := store.LoadCredentialPayload(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	savedCredential, err := DecodeCredential(saved)
	if err != nil {
		t.Fatal(err)
	}
	if savedCredential.RefreshToken != "rt2" || savedCredential.AccessToken != "at2" {
		t.Fatalf("saved=%+v", savedCredential)
	}
}

// Regression: QwenWork rotates refresh tokens, so concurrent near-expiry
// callers must share one refresh round-trip. A second exchange with the same
// (already consumed) token reads as session-dead and disables the account.
func TestConcurrentCredentialRefreshSingleFlights(t *testing.T) {
	expiring := readyCredential()
	expiring.ExpiresAt = time.Now().Unix()
	payload, _ := expiring.Encode()
	store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
	var refreshCalls int32
	client := newTestClient(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathDeviceRefresh {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		atomic.AddInt32(&refreshCalls, 1)
		time.Sleep(25 * time.Millisecond) // widen the race window
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "at2", "refresh_token": "rt2", "expires_in": 7200})
	}))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			credential, err := client.credential(context.Background(), "acc1")
			if err != nil {
				t.Errorf("goroutine %d: %v", slot, err)
				return
			}
			if credential.AccessToken != "at2" || credential.RefreshToken != "rt2" {
				t.Errorf("goroutine %d got stale credential %+v", slot, credential)
			}
		}(i)
	}
	wg.Wait()
	if got := atomic.LoadInt32(&refreshCalls); got != 1 {
		t.Fatalf("refresh calls=%d, want 1: the rotated refresh token is single-use", got)
	}
}

func TestRefreshSessionDeadObservesLoginRequired(t *testing.T) {
	credential := readyCredential()
	payload, _ := credential.Encode()
	store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
	client := newTestClient(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"401","message":"token expired"}`))
	}))
	if _, err := client.Refresh(context.Background(), "acc1", credential); err == nil {
		t.Fatal("expected refresh error")
	}
	if store.lastStatus != "login_required" || store.lastKind != accounts.KindAuth {
		t.Fatalf("observe status=%s kind=%s", store.lastStatus, store.lastKind)
	}
}

// Regression: the upstream fragments one tool call across many deltas; the
// aggregate must merge them by index instead of keeping the last fragment.
func TestChatNonStreamMergesSplitToolCallDeltas(t *testing.T) {
	sse := wrapChunk(`{"choices":[{"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_time","arguments":"{\"city\":"}}]}}]}`, 200) +
		wrapChunk(`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Beijing\"}"}}]}}]}`, 200) +
		wrapChunk(`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"lookup","arguments":"{}"}}]}}]}`, 200) +
		wrapChunk(`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`, 200) +
		"data: [DONE]\n\n"
	payload, _ := readyCredential().Encode()
	store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
	client := newTestClient(t, store, chatHandlerFor(t, sse, nil))
	out, err := client.ChatNonStream(context.Background(), "acc1", translate.ChatRequest{
		Model: "qwork-advanced", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var calls []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	if err := json.Unmarshal(out.ToolCalls, &calls); err != nil {
		t.Fatalf("tool calls=%s err=%v", out.ToolCalls, err)
	}
	if len(calls) != 2 {
		t.Fatalf("tool calls=%d want 2: %s", len(calls), out.ToolCalls)
	}
	if calls[0].ID != "call_1" || calls[0].Function.Name != "get_time" || calls[0].Function.Arguments != `{"city":"Beijing"}` {
		t.Fatalf("call 0=%+v", calls[0])
	}
	if calls[1].ID != "call_2" || calls[1].Function.Name != "lookup" || calls[1].Function.Arguments != "{}" {
		t.Fatalf("call 1=%+v", calls[1])
	}
}

// Regression: quota code 14018 and the model-unavailable text must classify
// even when the wrapper statusCodeValue is 200.
func TestChatNonStreamClassifiesErrorMarkersInSuccessFrames(t *testing.T) {
	tests := []struct {
		name string
		sse  string
		kind string
	}{
		{"quota code 14018", wrapChunk(`{"code":14018,"message":"insufficient quota"}`, 200), accounts.KindQuota},
		{"model unavailable text", wrapChunk(`Model is not available for this user`, 200), accounts.KindModelNotAvailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload, _ := readyCredential().Encode()
			store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
			client := newTestClient(t, store, chatHandlerFor(t, test.sse, nil))
			_, err := client.ChatNonStream(context.Background(), "acc1", translate.ChatRequest{
				Model: "qwork-advanced", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
			})
			providerErr, ok := err.(*providers.Error)
			if !ok {
				t.Fatalf("err=%v type=%T", err, err)
			}
			if providerErr.Kind != test.kind {
				t.Fatalf("kind=%s want=%s", providerErr.Kind, test.kind)
			}
		})
	}
}

func chatHandlerFor(t *testing.T, sse string, verify func(r *http.Request, body []byte)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathChatPath {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if verify != nil {
			verify(r, body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sse)
	})
}

// verifyCosySignature recomputes md5(payload\ncosyKey\ndate\nbody\npath) from
// the request headers and confirms the signed path drops /algo.
func verifyCosySignature(t *testing.T, r *http.Request, body []byte, wantPath string) {
	t.Helper()
	authorization := r.Header.Get("Authorization")
	parts := strings.Split(authorization, " ")
	if len(parts) != 2 || parts[0] != "Bearer" || !strings.HasPrefix(parts[1], "COSY.") {
		t.Fatalf("authorization=%s", authorization)
	}
	segments := strings.Split(strings.TrimPrefix(parts[1], "COSY."), ".")
	if len(segments) != 2 {
		t.Fatalf("authorization=%s", authorization)
	}
	payloadB64, signature := segments[0], segments[1]
	cosyKey := r.Header.Get("Cosy-Key")
	date := r.Header.Get("Cosy-Date")
	if cosyKey == "" || date == "" {
		t.Fatalf("missing cosy headers: %+v", r.Header)
	}
	stripped := md5.Sum([]byte(payloadB64 + "\n" + cosyKey + "\n" + date + "\n" + string(body) + "\n" + wantPath))
	if signature != hex.EncodeToString(stripped[:]) {
		t.Fatalf("signature mismatch: got %s", signature)
	}
	// The /algo-prefixed path must NOT verify: the gateway signs without it.
	fullPath := "/algo" + wantPath
	prefixed := md5.Sum([]byte(payloadB64 + "\n" + cosyKey + "\n" + date + "\n" + string(body) + "\n" + fullPath))
	if signature == hex.EncodeToString(prefixed[:]) {
		t.Fatal("signature must not verify against the /algo-prefixed path")
	}
	// The payload itself carries the desktop identity fields.
	decoded, err := base64.StdEncoding.DecodeString(payloadB64)
	if err != nil {
		t.Fatalf("payload b64: %v", err)
	}
	var payload struct {
		CosyVersion string `json:"cosyVersion"`
		IdeVersion  string `json:"ideVersion"`
		Info        string `json:"info"`
		RequestID   string `json:"requestId"`
		Version     string `json:"version"`
	}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		t.Fatalf("payload json: %v", err)
	}
	if payload.CosyVersion != "1.1.18" || payload.Version != "v1" || payload.RequestID == "" || payload.Info == "" {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestChatSendsSignedRequestWithQwenWorkHeaders(t *testing.T) {
	payload, _ := readyCredential().Encode()
	store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
	client := newTestClient(t, store, chatHandlerFor(t, chatSSE, func(r *http.Request, body []byte) {
		verifyCosySignature(t, r, body, "/api/v2/service/pro/sse/agent_chat_generation")
		for header, want := range map[string]string{
			"User-Agent":                 "qoderwork/1.0.4",
			"X-Qwenwork-Version":         "1.0.4",
			"X-Qwenwork-Release-Version": "1.0.4-26090501",
			"X-Qwenwork-Build":           "26090501",
			"X-Qwenwork-Platform":        "win32",
			"X-Qwenwork-Arch":            "x64",
			"X-Qwenwork-Channel":         "stable",
			"Cosy-Business-Product":      "qoder_work",
			"Cosy-Business-Type":         "agent",
			"Cosy-Scene":                 "qwork",
			"Cosy-ClientType":            "6",
			"Cosy-MachineOS":             "x86_64_win32",
			"Login-Version":              "v2",
			"Cosy-User":                  "u1",
			"X-Model-Key":                "qwork-advanced",
			"X-Model-Source":             "system",
		} {
			if got := r.Header.Get(header); got != want {
				t.Errorf("%s=%q want %q", header, got, want)
			}
		}
		var sent map[string]any
		if err := json.Unmarshal(body, &sent); err != nil {
			t.Fatalf("body: %v", err)
		}
		if sent["session_type"] != businessProduct || sent["source"] != float64(1) || sent["version"] != "3" {
			t.Fatalf("body=%v", sent)
		}
	}))
	out, err := client.ChatNonStream(context.Background(), "acc1", translate.ChatRequest{
		Model: "qwork-advanced", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Content != "OK" {
		t.Fatalf("content=%q", out.Content)
	}
}

func TestChatHeadersFollowRegion(t *testing.T) {
	tests := []struct {
		region         string
		userAgent      string
		version        string
		releaseVersion string
		build          string
		defaultModel   string
	}{
		{region: "global", userAgent: "qoderwork/1.0.4", version: "1.0.4", releaseVersion: "1.0.4-26090501", build: "26090501", defaultModel: "qwork-advanced"},
		{region: "cn", userAgent: "qoderwork/1.0.5", version: "1.0.5", releaseVersion: "1.0.5-26090901", build: "26090901", defaultModel: "pro"},
	}
	for _, test := range tests {
		t.Run(test.region, func(t *testing.T) {
			payload, _ := readyCredential().Encode()
			store := &memStore{region: test.region, items: map[string][]byte{"acc1": payload}}
			client := newTestClient(t, store, chatHandlerFor(t, chatSSE, func(r *http.Request, body []byte) {
				for header, want := range map[string]string{
					"User-Agent":                 test.userAgent,
					"X-Qwenwork-Version":         test.version,
					"X-Qwenwork-Release-Version": test.releaseVersion,
					"X-Qwenwork-Build":           test.build,
				} {
					if got := r.Header.Get(header); got != want {
						t.Errorf("%s=%q want %q", header, got, want)
					}
				}
				var sent map[string]any
				_ = json.Unmarshal(body, &sent)
				model, _ := sent["model_config"].(map[string]any)
				if model["key"] != test.defaultModel {
					t.Errorf("empty request model must fall back to %s, got %v", test.defaultModel, model["key"])
				}
			}))
			// No model in the request: the region default is sent.
			if _, err := client.ChatNonStream(context.Background(), "acc1", translate.ChatRequest{
				Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestChatStreamRewritesWrappedFrames(t *testing.T) {
	payload, _ := readyCredential().Encode()
	store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
	client := newTestClient(t, store, chatHandlerFor(t, chatSSE, nil))
	resp, _, err := client.ChatStream(context.Background(), "acc1", translate.ChatRequest{
		Model: "qwork-advanced", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("resp status=%d type=%s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	for _, want := range []string{
		`"id":"chatcmpl-`, `"created":`, `"content":"O"`, `"content":"K"`,
		`"reasoning_content":"think"`, `"finish_reason":"tool_calls"`,
		`"prompt_tokens":3`, "data: [DONE]",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("stream missing %s:\n%s", want, text)
		}
	}
	if strings.Contains(text, "statusCodeValue") || strings.Contains(text, `"headers":{}`) {
		t.Fatalf("wrapper frame leaked: %s", text)
	}
}

func TestChatNonStreamAggregatesContentToolsReasoningUsage(t *testing.T) {
	payload, _ := readyCredential().Encode()
	store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
	client := newTestClient(t, store, chatHandlerFor(t, chatSSE, nil))
	out, err := client.ChatNonStream(context.Background(), "acc1", translate.ChatRequest{
		Model: "qwork-advanced", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Content != "OK" || out.Reasoning != "think" || out.FinishReason != "tool_calls" {
		t.Fatalf("outcome=%+v", out)
	}
	if !strings.Contains(string(out.ToolCalls), "get_time") {
		t.Fatalf("tool calls=%s", out.ToolCalls)
	}
	if out.PromptTokens != 3 || out.CompletionTokens != 2 || out.UsageSource != "upstream" {
		t.Fatalf("usage=%+v", out)
	}
	if out.Model != "qwork-advanced" {
		t.Fatalf("model=%s", out.Model)
	}
}

func TestChatNonStreamClassifiesErrorFrames(t *testing.T) {
	tests := []struct {
		name   string
		sse    string
		kind   string
		status int
	}{
		{
			name:   "quota code 14018",
			sse:    wrapChunk(`{"code":14018,"message":"insufficient quota"}`, 402),
			kind:   accounts.KindQuota,
			status: 402,
		},
		{
			name:   "model not available",
			sse:    wrapChunk(`Model is not available for this user`, 400),
			kind:   accounts.KindModelNotAvailable,
			status: 400,
		},
		{
			name:   "auth code in body",
			sse:    wrapChunk(`{"code":"401","message":"token expired"}`, 200),
			kind:   accounts.KindAuth,
			status: 401,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload, _ := readyCredential().Encode()
			store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
			client := newTestClient(t, store, chatHandlerFor(t, test.sse, nil))
			_, err := client.ChatNonStream(context.Background(), "acc1", translate.ChatRequest{
				Model: "qwork-advanced", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
			})
			providerErr, ok := err.(*providers.Error)
			if !ok {
				t.Fatalf("err=%v type=%T", err, err)
			}
			if providerErr.Kind != test.kind {
				t.Fatalf("kind=%s want=%s", providerErr.Kind, test.kind)
			}
			if providerErr.Status != test.status {
				t.Fatalf("status=%d want=%d", providerErr.Status, test.status)
			}
		})
	}
}

func TestChatHTTPStatusErrorsAreClassified(t *testing.T) {
	tests := []struct {
		status int
		body   string
		kind   string
	}{
		{http.StatusUnauthorized, `{"code":"401","message":"token expired"}`, accounts.KindAuth},
		{http.StatusPaymentRequired, `{"code":14018,"message":"quota exceeded"}`, accounts.KindQuota},
		{http.StatusTooManyRequests, "too many requests", accounts.KindRateLimit},
		{http.StatusBadRequest, "bad request", accounts.KindInvalidRequest},
		{http.StatusInternalServerError, "boom", accounts.KindUnavailable},
		{http.StatusNotFound, "", accounts.KindUnavailable},
	}
	for _, test := range tests {
		t.Run(test.body+jsonInt(test.status), func(t *testing.T) {
			payload, _ := readyCredential().Encode()
			store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			})
			client := newTestClient(t, store, handler)
			_, _, err := client.ChatStream(context.Background(), "acc1", translate.ChatRequest{
				Model: "qwork-advanced", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
			})
			providerErr, ok := err.(*providers.Error)
			if !ok {
				t.Fatalf("err=%v type=%T", err, err)
			}
			if providerErr.Kind != test.kind {
				t.Fatalf("kind=%s want=%s (status=%d body=%s)", providerErr.Kind, test.kind, test.status, test.body)
			}
		})
	}
}

func TestClassifyKnownErrorTexts(t *testing.T) {
	cases := []struct {
		status int
		body   string
		kind   string
	}{
		{401, `{"code":"401","message":"token expired"}`, accounts.KindAuth},
		{402, `{"code":14018,"message":"quota exceeded"}`, accounts.KindQuota},
		{200, `{"code":14018,"message":"quota exceeded"}`, accounts.KindQuota},
		{429, "too many requests", accounts.KindRateLimit},
		{400, "Model is not available for this user", accounts.KindModelNotAvailable},
		{400, `{"message":"invalid messages"}`, accounts.KindInvalidRequest},
		{413, "payload too large", accounts.KindInvalidRequest},
		{500, "boom", accounts.KindUnavailable},
		{404, "", accounts.KindUnavailable},
		{400, `{"code":"401","message":"token expired"}`, accounts.KindAuth},
	}
	for _, c := range cases {
		got := Classify(c.status, c.body)
		if got.Kind != c.kind {
			t.Fatalf("Classify(%d,%s)=%+v want %s", c.status, c.body, got, c.kind)
		}
	}
}

func TestModelsParsesQworkKey(t *testing.T) {
	payload, _ := readyCredential().Encode()
	store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
	client := newTestClient(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathModelList {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("X-Qwenwork-Version") != "1.0.4" {
			t.Fatalf("models must carry site headers, got %+v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"qwork": []map[string]any{
			{
				"key": "qwork-advanced", "display_name": "Advanced", "is_vl": true,
				"is_reasoning": false, "max_input_tokens": 180000, "max_output_tokens": 32768,
				"credits": "x1.0",
			},
			{"key": "hidden", "enable": false},
			{"key": "qwork-auto", "display_name": "Auto", "enable": true},
			{"display_name": "no key"},
		}})
	}))
	models, err := client.Models(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("models=%+v", models)
	}
	advanced := models[0]
	if advanced.NativeModel != "qwork-advanced" || advanced.PublicModel != "qwork-advanced" ||
		advanced.DisplayName != "Advanced" || advanced.Scene != scene ||
		!advanced.Capabilities.Images || advanced.Capabilities.Reasoning ||
		advanced.Capabilities.ContextWindow != 180000 || advanced.Capabilities.MaxOutput != 32768 {
		t.Fatalf("model=%+v", advanced)
	}
	// Catalog memory resolves the model for later chat requests.
	global, _ := siteFor("global")
	if info, ok := client.catalogModel(global, "qwork-auto"); !ok || info.NativeModel != "qwork-auto" {
		t.Fatalf("catalog lookup=%+v ok=%v", info, ok)
	}
	cn, _ := siteFor("cn")
	if _, ok := client.catalogModel(cn, "qwork-auto"); ok {
		t.Fatal("global catalog leaked into the cn site")
	}
}

func TestQuotaMapsAccountContext(t *testing.T) {
	payload, _ := readyCredential().Encode()
	store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
	client := newTestClient(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathAccountContextPath {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer at" {
			t.Fatalf("auth=%s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": "ok",
			"data": map[string]any{
				"user":  map[string]any{"name": "Tester", "email": "t@example.com"},
				"plan":  map[string]any{"name": "Pro", "user_type": "personal", "is_personal_version": true},
				"quota": map[string]any{"total": 100, "used": 25, "remaining": 75, "exceeded": false},
			},
		})
	}))
	quota, err := client.Quota(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if quota.ProviderID != "qwenwork" || quota.Used != 25 || quota.Total != 100 ||
		quota.Remaining != 75 || quota.Percentage != 25 || quota.Plan != "Pro" || quota.Exceeded {
		t.Fatalf("quota=%+v", quota)
	}
	if quota.FetchedAt == "" {
		t.Fatal("fetched_at missing")
	}
}

func TestQuotaAbsentFieldsError(t *testing.T) {
	payload, _ := readyCredential().Encode()
	store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
	client := newTestClient(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "ok", "data": map[string]any{}})
	}))
	if _, err := client.Quota(context.Background(), "acc1"); err == nil {
		t.Fatal("missing quota must be an error, not fake zeros")
	}
}

func TestProbeReportsCredentialReadiness(t *testing.T) {
	payload, _ := readyCredential().Encode()
	store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
	client := NewClient(store)
	health, err := client.Probe(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if !health.Ready || !health.Hot || health.UID != "u1" {
		t.Fatalf("health=%+v", health)
	}
	missing, _ := Credential{AccessToken: "at"}.Encode()
	store.items = map[string][]byte{"acc2": missing}
	health, err = client.Probe(context.Background(), "acc2")
	if err != nil {
		t.Fatal(err)
	}
	if health.Ready || health.LastError == "" {
		t.Fatalf("health=%+v", health)
	}
}

func TestGrantExpiryParsing(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name  string
		raw   string
		check func(t *testing.T, got int64)
	}{
		{"rfc3339", `{"token":"t","expires_at":"2026-01-02T03:04:05Z"}`, func(t *testing.T, got int64) {
			if got != time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC).Unix() {
				t.Fatalf("got=%d", got)
			}
		}},
		{"unix seconds", `{"token":"t","expires_at":1800000000}`, func(t *testing.T, got int64) {
			if got != 1800000000 {
				t.Fatalf("got=%d", got)
			}
		}},
		{"unix millis", `{"token":"t","expires_at":1800000000000}`, func(t *testing.T, got int64) {
			if got != 1800000000 {
				t.Fatalf("got=%d", got)
			}
		}},
		{"expires_in seconds", `{"token":"t","expires_in":3600}`, func(t *testing.T, got int64) {
			if got != now.Add(time.Hour).Unix() {
				t.Fatalf("got=%d", got)
			}
		}},
		{"default lifetime", `{"token":"t"}`, func(t *testing.T, got int64) {
			if got != now.Add(defaultTokenLifetime).Unix() {
				t.Fatalf("got=%d", got)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var grant grant
			if err := json.Unmarshal([]byte(test.raw), &grant); err != nil {
				t.Fatal(err)
			}
			test.check(t, grant.expiry(now))
		})
	}
}

func TestChatStreamEmptyUpstreamFailsOver(t *testing.T) {
	payload, _ := readyCredential().Encode()
	store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
	client := newTestClient(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	_, _, err := client.ChatStream(context.Background(), "acc1", translate.ChatRequest{
		Model: "qwork-advanced", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	})
	providerErr, ok := err.(*providers.Error)
	if !ok {
		t.Fatalf("err=%v type=%T", err, err)
	}
	if providerErr.Kind != accounts.KindUnavailable || providerErr.Code != "upstream_empty" {
		t.Fatalf("err=%+v", providerErr)
	}
}

func TestChatRequestSendsSystemAndMessageHistory(t *testing.T) {
	payload, _ := readyCredential().Encode()
	store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
	client := newTestClient(t, store, chatHandlerFor(t, chatSSE, func(r *http.Request, body []byte) {
		var sent map[string]any
		if err := json.Unmarshal(body, &sent); err != nil {
			t.Fatalf("body: %v", err)
		}
		if sent["system"] != "be brief" {
			t.Fatalf("system=%v", sent["system"])
		}
		messages, _ := sent["messages"].([]any)
		if len(messages) != 2 {
			t.Fatalf("messages=%v", messages)
		}
		first, _ := messages[0].(map[string]any)
		second, _ := messages[1].(map[string]any)
		if first["role"] != "user" || second["role"] != "assistant" {
			t.Fatalf("roles=%v %v", first["role"], second["role"])
		}
	}))
	_, err := client.ChatNonStream(context.Background(), "acc1", translate.ChatRequest{
		Model:    "qwork-advanced",
		Messages: []translate.ChatMessage{{Role: "system", Content: "be brief"}, {Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Regression: the first relayed chunk must carry delta.role so strict OpenAI
// SDKs recognize the assistant turn.
func TestChatStreamKeepsFirstDeltaRole(t *testing.T) {
	sse := wrapChunk(`{"choices":[{"delta":{"role":"assistant","content":"O"}}]}`, 200) +
		wrapChunk(`{"choices":[{"delta":{},"finish_reason":"stop"}]}`, 200) +
		"data: [DONE]\n\n"
	payload, _ := readyCredential().Encode()
	store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
	client := newTestClient(t, store, chatHandlerFor(t, sse, nil))
	resp, _, err := client.ChatStream(context.Background(), "acc1", translate.ChatRequest{
		Model: "qwork-advanced", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if !strings.Contains(text, `"role":"assistant"`) {
		t.Fatalf("first chunk missing delta.role:\n%s", text)
	}
}

// Regression: a trailing error frame after partial content must surface the
// classified error, not a truncated success — matching the JS reference.
func TestChatNonStreamSurfacesTrailingErrorAfterContent(t *testing.T) {
	sse := wrapChunk(`{"choices":[{"delta":{"content":"partial answer"}}]}`, 200) +
		wrapChunk(`{"code":14018,"message":"insufficient quota"}`, 402) +
		"data: [DONE]\n\n"
	payload, _ := readyCredential().Encode()
	store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
	client := newTestClient(t, store, chatHandlerFor(t, sse, nil))
	out, err := client.ChatNonStream(context.Background(), "acc1", translate.ChatRequest{
		Model: "qwork-advanced", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	})
	providerErr, ok := err.(*providers.Error)
	if !ok {
		t.Fatalf("expected classified error, got outcome=%+v err=%v type=%T", out, err, err)
	}
	if providerErr.Kind != accounts.KindQuota {
		t.Fatalf("kind=%s want=quota", providerErr.Kind)
	}
}

// Streaming counterpart: the error chunk reaches the client and no synthetic
// finish_reason is appended after it.
func TestChatStreamSurfacesTrailingErrorAfterContent(t *testing.T) {
	sse := wrapChunk(`{"choices":[{"delta":{"content":"partial answer"}}]}`, 200) +
		wrapChunk(`{"code":14018,"message":"insufficient quota"}`, 402) +
		"data: [DONE]\n\n"
	payload, _ := readyCredential().Encode()
	store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
	client := newTestClient(t, store, chatHandlerFor(t, sse, nil))
	resp, _, err := client.ChatStream(context.Background(), "acc1", translate.ChatRequest{
		Model: "qwork-advanced", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if !strings.Contains(text, `"content":"partial answer"`) {
		t.Fatalf("stream missing content:\n%s", text)
	}
	if !strings.Contains(text, "insufficient quota") {
		t.Fatalf("stream missing error object:\n%s", text)
	}
	if strings.Contains(text, `"finish_reason":"stop"`) {
		t.Fatalf("synthetic finish_reason appended after the error chunk:\n%s", text)
	}
	if !strings.Contains(text, "data: [DONE]") {
		t.Fatalf("stream missing terminal [DONE]:\n%s", text)
	}
}

func TestAdapterBundle(t *testing.T) {
	adapter := NewClient(&memStore{}).Adapter()
	if adapter.ID != "qwenwork" {
		t.Fatalf("id=%s", adapter.ID)
	}
	for _, capability := range []string{"credential", "login", "chat", "models", "classifier", "prober"} {
		if !adapter.Supports(capability) {
			t.Fatalf("adapter missing %s", capability)
		}
	}
	if adapter.Supports("checkin") {
		t.Fatal("qwenwork has no check-in")
	}
}

// Regression: a caller that loaded the credential before an earlier refresh
// finished still holds the consumed refresh token. It must pick up the stored
// rotated pair instead of replaying the old token.
func TestStaleCallerReusesRotatedCredential(t *testing.T) {
	expiring := readyCredential()
	expiring.ExpiresAt = time.Now().Unix()
	payload, _ := expiring.Encode()
	store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
	var refreshCalls int32
	client := newTestClient(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&refreshCalls, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "at2", "refresh_token": "rt2", "expires_in": 7200})
	}))
	stale, _ := DecodeCredential(payload)
	if _, err := client.singleFlightRefresh(context.Background(), "acc1", stale); err != nil {
		t.Fatal(err)
	}
	second, err := client.singleFlightRefresh(context.Background(), "acc1", stale)
	if err != nil {
		t.Fatal(err)
	}
	if second.AccessToken != "at2" || second.RefreshToken != "rt2" {
		t.Fatalf("stale caller got %+v", second)
	}
	if got := atomic.LoadInt32(&refreshCalls); got != 1 {
		t.Fatalf("refresh calls=%d, want 1", got)
	}
}

// A cancelled leader must not fail the refresh: the rotated pair is saved.
func TestRefreshSurvivesCancelledCaller(t *testing.T) {
	expiring := readyCredential()
	expiring.ExpiresAt = time.Now().Unix()
	payload, _ := expiring.Encode()
	store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
	ctx, cancel := context.WithCancel(context.Background())
	client := newTestClient(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel()
		time.Sleep(10 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "at2", "refresh_token": "rt2", "expires_in": 7200})
	}))
	if _, err := client.singleFlightRefresh(ctx, "acc1", expiring); err != nil {
		t.Fatal(err)
	}
	saved, _ := client.loadCredential(context.Background(), "acc1")
	if saved.RefreshToken != "rt2" {
		t.Fatalf("rotated pair not saved: %+v", saved)
	}
}

func TestRefreshWithoutRefreshTokenSkipsExchange(t *testing.T) {
	credential := readyCredential()
	credential.RefreshToken = ""
	credential.ExpiresAt = time.Now().Unix()
	payload, _ := credential.Encode()
	store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
	client := newTestClient(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected upstream call %s", r.URL.Path)
	}))
	got, err := client.credential(context.Background(), "acc1")
	if err != nil || got.AccessToken != "at" {
		t.Fatalf("credential=%+v err=%v", got, err)
	}
	if store.lastStatus == "login_required" {
		t.Fatal("missing refresh token must not mark the account login_required")
	}
}

func TestProbeFillsMissingUIDFromUserInfo(t *testing.T) {
	credential := readyCredential()
	credential.UID = ""
	payload, _ := credential.Encode()
	store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
	client := newTestClient(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathUserInfo {
			t.Fatalf("path=%s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "u9", "name": "Nine", "email": "n@example.com"})
	}))
	health, err := client.Probe(context.Background(), "acc1")
	if err != nil || !health.Ready || health.UID != "u9" {
		t.Fatalf("health=%+v err=%v", health, err)
	}
	saved, _ := client.loadCredential(context.Background(), "acc1")
	if saved.UID != "u9" {
		t.Fatalf("uid not saved: %+v", saved)
	}
}

func TestDecodeCredentialNormalizesMillisecondExpiry(t *testing.T) {
	ms := time.Now().Add(time.Hour).UnixMilli()
	credential, err := DecodeCredential([]byte(`{"accessToken":"at","refreshToken":"rt","expiresAt":` + jsonInt(int(ms)) + `}`))
	if err != nil {
		t.Fatal(err)
	}
	if credential.ExpiresAt != ms/1000 {
		t.Fatalf("expiresAt=%d want %d", credential.ExpiresAt, ms/1000)
	}
}

// Regression: an error-only first frame must fail over before any byte
// reaches the client, carrying the classified kind.
func TestChatStreamErrorFirstFrameFailsOver(t *testing.T) {
	cases := []struct {
		name string
		sse  string
		kind string
	}{
		{"quota 14018", wrapChunk(`{"code":14018,"message":"quota exceeded"}`, 200), accounts.KindQuota},
		{"model unavailable", wrapChunk(`Model is not available for this user`, 400), accounts.KindModelNotAvailable},
		{"auth", wrapChunk(`{"code":"401","message":"token expired"}`, 200), accounts.KindAuth},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			payload, _ := readyCredential().Encode()
			store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
			client := newTestClient(t, store, chatHandlerFor(t, c.sse+"data: [DONE]\n\n", nil))
			resp, _, err := client.ChatStream(context.Background(), "acc1", translate.ChatRequest{
				Model: "qwork-advanced", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
			})
			if resp != nil {
				resp.Body.Close()
				t.Fatal("error-only stream was relayed instead of failing over")
			}
			providerErr, ok := err.(*providers.Error)
			if !ok || providerErr.Kind != c.kind {
				t.Fatalf("err=%v (%T) want kind %s", err, err, c.kind)
			}
		})
	}
}

// The relayed mid-stream error object keeps code, kind, and status so the
// gateway classifier does not re-guess from the message.
func TestChatStreamErrorChunkCarriesClassification(t *testing.T) {
	sse := wrapChunk(`{"choices":[{"delta":{"content":"partial"}}]}`, 200) +
		wrapChunk(`{"code":14018,"message":"insufficient credits"}`, 200) +
		"data: [DONE]\n\n"
	payload, _ := readyCredential().Encode()
	store := &memStore{region: "global", items: map[string][]byte{"acc1": payload}}
	client := newTestClient(t, store, chatHandlerFor(t, sse, nil))
	resp, _, err := client.ChatStream(context.Background(), "acc1", translate.ChatRequest{
		Model: "qwork-advanced", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	for _, want := range []string{`"code":"14018"`, `"kind":"quota"`, `"status":402`} {
		if !strings.Contains(text, want) {
			t.Fatalf("error chunk missing %s:\n%s", want, text)
		}
	}
}

func TestChatBodyClampsMaxTokensToCatalog(t *testing.T) {
	body := buildChatBody(translate.ChatRequest{
		Messages:  []translate.ChatMessage{{Role: "user", Content: "hi"}},
		MaxTokens: json.RawMessage(`100000`),
	}, "qwork-advanced", 32768)
	parameters, _ := body["parameters"].(map[string]any)
	if parameters["max_tokens"] != 32768 {
		t.Fatalf("max_tokens=%v", parameters["max_tokens"])
	}
}

func TestClassifyQuotaCodeOnlyFromCodeField(t *testing.T) {
	if got := Classify(500, `{"message":"request 140181 failed"}`); got.Kind != accounts.KindUnavailable {
		t.Fatalf("kind=%s: a 14018 substring must not read as quota", got.Kind)
	}
}
