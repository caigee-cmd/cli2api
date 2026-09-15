package devin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func TestFormatSessionToken(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "devin-session-token$eyJ123", want: "devin-session-token$eyJ123"},
		{input: "eyJ123.456.789", want: "devin-session-token$eyJ123.456.789"},
		{input: "custom-token-xyz", want: "custom-token-xyz"},
	}
	for _, tt := range tests {
		if got := FormatSessionToken(tt.input); got != tt.want {
			t.Errorf("FormatSessionToken(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCredentialValidateAndEncode(t *testing.T) {
	raw := []byte(`{"session_token":"eyJabc.def.ghi"}`)
	if err := ValidateCredential(raw); err != nil {
		t.Fatal(err)
	}
	cred, err := DecodeCredential(raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := cred.Encode()
	if err != nil {
		t.Fatal(err)
	}
	var round Credential
	if err := json.Unmarshal(encoded, &round); err != nil {
		t.Fatal(err)
	}
	if round.Format != CredentialFormat {
		t.Fatalf("format=%q", round.Format)
	}
	if !strings.HasPrefix(round.SessionToken, TokenPrefix) {
		t.Fatalf("token=%q", round.SessionToken)
	}
	if round.DeviceSeed == "" {
		t.Fatal("expected device seed")
	}
	if !round.Ready() {
		t.Fatal("expected ready")
	}
}

func TestConnectEnvelopeRoundtrip(t *testing.T) {
	payload := []byte("hello-devin")
	framed := WrapConnectEnvelope(payload)
	flag, got, err := ReadConnectFrame(bytes.NewReader(framed))
	if err != nil {
		t.Fatal(err)
	}
	if flag != ConnectFlagData {
		t.Fatalf("flag=%x", flag)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("got=%q want=%q", got, payload)
	}
	eos := WrapConnectEnvelopeWithFlag(ConnectFlagEndStream, []byte(`{}`))
	flag, got, err = ReadConnectFrame(bytes.NewReader(eos))
	if err != nil {
		t.Fatal(err)
	}
	if flag != ConnectFlagEndStream || string(got) != "{}" {
		t.Fatalf("eos flag=%x payload=%q", flag, got)
	}
}

func TestParseTrailerErrorMapping(t *testing.T) {
	status, err := ParseTrailerError([]byte(`{"error":{"code":"unauthenticated","message":"bad token"}}`))
	if err == nil || status != 401 {
		t.Fatalf("status=%d err=%v", status, err)
	}
	status, err = ParseTrailerError([]byte(`{"error":{"code":"resource_exhausted","message":"slow down"}}`))
	if err == nil || status != 429 {
		t.Fatalf("status=%d err=%v", status, err)
	}
	status, err = ParseTrailerError([]byte(`{}`))
	if err != nil || status != 0 {
		t.Fatalf("empty trailer status=%d err=%v", status, err)
	}
}

func TestBuildGetUserStatusRequestContainsToken(t *testing.T) {
	token := "devin-session-token$eyJtest"
	req := BuildGetUserStatusRequest(token, strings.Repeat("ab", FingerprintHexLen/2))
	if !bytes.Contains(req, []byte(token)) {
		t.Fatal("request missing session token")
	}
	if !bytes.Contains(req, []byte(ClientName)) {
		t.Fatal("request missing client name")
	}
}

func TestBuildAuthorizationURLQueryOrder(t *testing.T) {
	u := BuildAuthorizationURL(AppBase, "http://127.0.0.1:1234/callback", "test-challenge", "state-abc")
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatal(err)
	}
	want := "redirect_uri=http%3A%2F%2F127.0.0.1%3A1234%2Fcallback&state=state-abc&prompt=select_account&code_challenge=test-challenge&code_challenge_method=S256"
	if parsed.RawQuery != want {
		t.Fatalf("raw query = %q, want %q", parsed.RawQuery, want)
	}
	headless := BuildAuthorizationURL(AppBase, "", "test-challenge-headless", "state-xyz")
	parsed, err = url.Parse(headless)
	if err != nil {
		t.Fatal(err)
	}
	wantHeadless := "state=state-xyz&prompt=select_account&code_challenge=test-challenge-headless&code_challenge_method=S256&cli_pkce_marker=1"
	if parsed.RawQuery != wantHeadless {
		t.Fatalf("headless raw query = %q, want %q", parsed.RawQuery, wantHeadless)
	}
}

func TestClassify401And429(t *testing.T) {
	got := Classify(401, "unauthorized")
	if got.Kind != accounts.KindAuth || got.Status != 401 {
		t.Fatalf("401 classify=%+v", got)
	}
	got = Classify(429, "too many requests")
	if got.Kind != accounts.KindRateLimit || got.Status != 429 {
		t.Fatalf("429 classify=%+v", got)
	}
	got = Classify(429, "quota exhausted")
	if got.Kind != accounts.KindQuota {
		t.Fatalf("quota classify=%+v", got)
	}
}

type memStore struct {
	accounts map[string]accounts.Account
	creds    map[string][]byte
}

func newMemStore() *memStore {
	return &memStore{accounts: map[string]accounts.Account{}, creds: map[string][]byte{}}
}

func (m *memStore) Get(_ context.Context, id string) (accounts.Account, error) {
	acc, ok := m.accounts[id]
	if !ok {
		return accounts.Account{}, accounts.ErrAccountNotFound
	}
	return acc, nil
}

func (m *memStore) LoadCredentialPayload(_ context.Context, accountID string) (string, []byte, error) {
	payload, ok := m.creds[accountID]
	if !ok {
		return "", nil, accounts.ErrAccountNotFound
	}
	return CredentialFormat, payload, nil
}

func (m *memStore) SaveCredentialPayload(_ context.Context, accountID, format string, payload []byte) error {
	_ = format
	m.creds[accountID] = append([]byte(nil), payload...)
	return nil
}

func (m *memStore) Observe(_ context.Context, id, remoteUID, status, lastError, lastKind string) error {
	acc := m.accounts[id]
	acc.ID = id
	acc.RemoteUID = remoteUID
	acc.Status = status
	acc.LastError = lastError
	acc.LastErrorKind = lastKind
	m.accounts[id] = acc
	return nil
}

func TestChatNonStreamHTTPtest(t *testing.T) {
	var textFrame []byte
	textFrame = AppendTag(textFrame, 3, BytesType)
	textFrame = AppendString(textFrame, "hello from devin")
	var stopFrame []byte
	stopFrame = AppendTag(stopFrame, 5, VarintType)
	stopFrame = AppendVarint(stopFrame, 2)

	var buf bytes.Buffer
	buf.Write(WrapConnectEnvelope(textFrame))
	buf.Write(WrapConnectEnvelope(stopFrame))
	buf.Write(WrapConnectEnvelopeWithFlag(ConnectFlagEndStream, []byte(`{}`)))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != PathGetChatMessage {
			http.NotFound(w, r)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Basic ") {
			http.Error(w, "missing auth", 401)
			return
		}
		if r.Header.Get("Content-Type") != ContentTypeConnectProto {
			http.Error(w, "bad content type", 400)
			return
		}
		if r.Header.Get("Connect-Protocol-Version") != ConnectProtocolVersion {
			http.Error(w, "bad protocol", 400)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if len(body) < 5 {
			http.Error(w, "empty body", 400)
			return
		}
		w.Header().Set("Content-Type", ContentTypeConnectProto)
		_, _ = w.Write(buf.Bytes())
	}))
	defer server.Close()

	store := newMemStore()
	store.accounts["acc1"] = accounts.Account{ID: "acc1", Provider: "devin", ProviderRegion: "global"}
	cred := Credential{SessionToken: FormatSessionToken("eyJabc.def.ghi"), DeviceSeed: "seed", BaseURL: server.URL}
	payload, err := cred.Encode()
	if err != nil {
		t.Fatal(err)
	}
	store.creds["acc1"] = payload

	client := NewClient(store)
	client.SetBases(AppBase, APIBase, server.URL)
	out, err := client.ChatNonStream(context.Background(), "acc1", translate.ChatRequest{
		Model: "swe-2-high",
		Messages: []translate.ChatMessage{
			{Role: "user", Content: "hi"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Content != "hello from devin" {
		t.Fatalf("content=%q", out.Content)
	}
	if out.FinishReason != "stop" {
		t.Fatalf("finish=%q", out.FinishReason)
	}
}

func TestResolveChatModelUIDWithFixture(t *testing.T) {
	path := filepath.Join("testdata", "devin_models.sample.json")
	_, levels, err := LoadCatalogFixture(path)
	if err != nil {
		t.Fatal(err)
	}
	got := ResolveChatModelUID("devin/swe-2", "", 0, levels)
	if got != "swe-2-high" {
		t.Fatalf("got=%q", got)
	}
	got = ResolveChatModelUID("claude-haiku-4-5", "", 0, levels)
	if got != "MODEL_PRIVATE_11" {
		t.Fatalf("alias=%q", got)
	}
}

func TestFingerprintLength(t *testing.T) {
	fp := GenerateDeviceFingerprint("seed")
	if len(fp) != FingerprintHexLen {
		t.Fatalf("len=%d", len(fp))
	}
}

func buildToolCallDeltaFrame(id, name, args string, index int) []byte {
	var tc []byte
	tc = AppendTag(tc, 1, BytesType)
	tc = AppendBytes(tc, []byte(id))
	tc = AppendTag(tc, 2, BytesType)
	tc = AppendBytes(tc, []byte(name))
	tc = AppendTag(tc, 3, BytesType)
	tc = AppendBytes(tc, []byte(args))
	tc = AppendTag(tc, 4, VarintType)
	tc = AppendVarint(tc, uint64(index))

	var frame []byte
	frame = AppendTag(frame, 6, BytesType)
	frame = AppendBytes(frame, tc)
	frame = AppendTag(frame, 5, VarintType)
	frame = AppendVarint(frame, 10)
	return frame
}

func TestChatStreamTextToolAndDone(t *testing.T) {
	var textFrame []byte
	textFrame = AppendTag(textFrame, 3, BytesType)
	textFrame = AppendString(textFrame, "partial ")
	textFrame2 := AppendTag(nil, 3, BytesType)
	textFrame2 = AppendString(textFrame2, "answer")
	toolFrame := buildToolCallDeltaFrame("call_1", "lookup", `{"q":"x"}`, 0)

	var buf bytes.Buffer
	buf.Write(WrapConnectEnvelope(textFrame))
	buf.Write(WrapConnectEnvelope(textFrame2))
	buf.Write(WrapConnectEnvelope(toolFrame))
	buf.Write(WrapConnectEnvelopeWithFlag(ConnectFlagEndStream, []byte(`{}`)))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != PathGetChatMessage {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", ContentTypeConnectProto)
		_, _ = w.Write(buf.Bytes())
	}))
	defer server.Close()

	store := newMemStore()
	store.accounts["acc1"] = accounts.Account{ID: "acc1", Provider: "devin", ProviderRegion: "global"}
	cred := Credential{SessionToken: FormatSessionToken("eyJabc.def.ghi"), DeviceSeed: "seed", BaseURL: server.URL}
	payload, err := cred.Encode()
	if err != nil {
		t.Fatal(err)
	}
	store.creds["acc1"] = payload

	client := NewClient(store)
	client.SetBases(AppBase, APIBase, server.URL)
	resp, err := client.ChatStream(context.Background(), "acc1", translate.ChatRequest{
		Model: "swe-2-high",
		Messages: []translate.ChatMessage{
			{Role: "user", Content: "hi"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, `"content":"partial "`) && !strings.Contains(text, `"content":"partial`) {
		t.Fatalf("missing text delta: %s", text)
	}
	if !strings.Contains(text, `"tool_calls"`) || !strings.Contains(text, `"lookup"`) {
		t.Fatalf("missing tool call delta: %s", text)
	}
	if !strings.Contains(text, `"finish_reason":"tool_calls"`) {
		t.Fatalf("expected tool_calls finish_reason: %s", text)
	}
	if !strings.Contains(text, "data: [DONE]") {
		t.Fatalf("missing DONE marker: %s", text)
	}
}

func TestAggregateConnectStreamMissingEOS(t *testing.T) {
	var textFrame []byte
	textFrame = AppendTag(textFrame, 3, BytesType)
	textFrame = AppendString(textFrame, "orphan")
	framed := WrapConnectEnvelope(textFrame)
	_, err := aggregateConnectStream(bytes.NewReader(framed), nil)
	if err == nil {
		t.Fatal("expected missing EOS error")
	}
	if !strings.Contains(err.Error(), "missing EOS") {
		t.Fatalf("err=%v", err)
	}
}

func TestChatStreamCancelClosesBody(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		var textFrame []byte
		textFrame = AppendTag(textFrame, 3, BytesType)
		textFrame = AppendString(textFrame, "late")
		var buf bytes.Buffer
		buf.Write(WrapConnectEnvelope(textFrame))
		buf.Write(WrapConnectEnvelopeWithFlag(ConnectFlagEndStream, []byte(`{}`)))
		w.Header().Set("Content-Type", ContentTypeConnectProto)
		_, _ = w.Write(buf.Bytes())
	}))
	defer server.Close()
	defer close(release)

	store := newMemStore()
	store.accounts["acc1"] = accounts.Account{ID: "acc1", Provider: "devin", ProviderRegion: "global"}
	cred := Credential{SessionToken: FormatSessionToken("eyJabc.def.ghi"), DeviceSeed: "seed", BaseURL: server.URL}
	payload, err := cred.Encode()
	if err != nil {
		t.Fatal(err)
	}
	store.creds["acc1"] = payload

	client := NewClient(store)
	client.SetBases(AppBase, APIBase, server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := client.ChatStream(ctx, "acc1", translate.ChatRequest{
			Model: "swe-2-high",
			Messages: []translate.ChatMessage{
				{Role: "user", Content: "hi"},
			},
		})
		errCh <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream never started")
	}
	cancel()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected cancel error")
		}
		if !errors.Is(err, context.Canceled) && !strings.Contains(strings.ToLower(err.Error()), "cancel") {
			t.Fatalf("unexpected err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ChatStream did not return after cancel")
	}
}

func TestRemoteCatalogHTTPtestSuccessAndFailure(t *testing.T) {
	ClearCatalog()
	t.Cleanup(ClearCatalog)

	sample, err := os.ReadFile(filepath.Join("testdata", "devin_models.sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	okServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(sample)
	}))
	defer okServer.Close()

	prevClient := catalogHTTPClient
	prevURLs := catalogURLs
	t.Cleanup(func() {
		catalogHTTPClient = prevClient
		catalogURLs = prevURLs
	})
	catalogHTTPClient = okServer.Client()
	catalogURLs = []string{okServer.URL + "/devin_models.json"}

	models, levels, err := FetchRemoteCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) == 0 || len(levels) == 0 {
		t.Fatalf("models=%d levels=%d", len(models), len(levels))
	}

	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer failServer.Close()
	ClearCatalog()
	catalogURLs = []string{failServer.URL + "/missing.json"}
	if _, _, err := FetchRemoteCatalog(context.Background()); err == nil {
		t.Fatal("expected remote catalog failure")
	}

	store := newMemStore()
	store.accounts["acc1"] = accounts.Account{ID: "acc1", Provider: "devin", ProviderRegion: "global"}
	cred := Credential{SessionToken: FormatSessionToken("eyJabc.def.ghi"), DeviceSeed: "seed"}
	payload, err := cred.Encode()
	if err != nil {
		t.Fatal(err)
	}
	store.creds["acc1"] = payload
	client := NewClient(store)
	if _, err := client.Models(context.Background(), "acc1"); err == nil {
		t.Fatal("Models must fail closed when remote catalog is empty")
	}
}

func TestClassifyAuthForbiddenQuotaTrailer(t *testing.T) {
	got := Classify(403, "permission_denied")
	if got.Kind != accounts.KindInvalidRequest {
		t.Fatalf("bare permission_denied classify=%+v want invalid_request", got)
	}
	if got.Status != 400 && got.Status != 403 {
		t.Fatalf("bare permission_denied status=%d want 400 or 403", got.Status)
	}
	got = Classify(401, "")
	if got.Kind != accounts.KindAuth {
		t.Fatalf("401 classify=%+v", got)
	}
	got = Classify(429, `{"error":{"code":"resource_exhausted","message":"quota acu exhausted"}}`)
	if got.Kind != accounts.KindQuota {
		t.Fatalf("quota trailer classify=%+v", got)
	}
	status, err := ParseTrailerError([]byte(`{"error":{"code":"resource_exhausted","message":"acu exhausted"}}`))
	if err == nil || status != 429 {
		t.Fatalf("trailer status=%d err=%v", status, err)
	}
	got = Classify(status, err.Error())
	if got.Kind != accounts.KindQuota && got.Kind != accounts.KindRateLimit {
		t.Fatalf("classified trailer=%+v", got)
	}
}

func TestClassifyMCPConfigPermissionDenied(t *testing.T) {
	body := "devin upstream error (permission_denied): Unable to process request due to an MCP configuration issue. (trace ID: 3de0a0f4c7f0e2cd3bd8e001a46749bd)"
	got := Classify(403, body)
	if got.Kind != accounts.KindInvalidRequest || got.Status != 400 {
		t.Fatalf("MCP permission_denied classify=%+v", got)
	}
	err := classifiedError(403, body)
	var providerErr *providers.Error
	if !errors.As(err, &providerErr) {
		t.Fatalf("classifiedError type=%T", err)
	}
	if providerErr.Kind != accounts.KindInvalidRequest {
		t.Fatalf("kind=%s", providerErr.Kind)
	}
	if providerErr.Cooldown != 0 {
		t.Fatalf("cooldown=%s want 0", providerErr.Cooldown)
	}
	if providerErr.Failover == nil || *providerErr.Failover {
		t.Fatalf("failover=%v want false", providerErr.Failover)
	}
}

func TestParseToolsAliasesMCPNamespace(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"function","function":{"name":"exec_command","description":"run","parameters":{"type":"object"}}},
		{"type":"function","function":{"name":"mcp__computer-use__left_click","description":"click","parameters":{"type":"object"}}},
		{"type":"function","function":{"name":"MCP__plugin_chrome__click","description":"click","parameters":{"type":"object"}}},
		{"type":"function","function":{"name":"web_search","description":"search","parameters":{"type":"object"}}}
	]`)
	historyCalls, _ := json.Marshal([]map[string]any{{
		"id":   "call_1",
		"type": "function",
		"function": map[string]any{
			"name":      "mcp__computer-use__left_click",
			"arguments": `{"x":1}`,
		},
	}})
	payload := BuildChatPayload(translate.ChatRequest{
		Model: "swe-2",
		Messages: []translate.ChatMessage{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "", ToolCalls: historyCalls},
			{Role: "tool", ToolCallID: "call_1", Content: "ok"},
		},
		Tools: raw,
	}, nil)
	if len(payload.Tools) != 4 {
		t.Fatalf("tools=%d want 4: %+v", len(payload.Tools), payload.Tools)
	}
	wantAlias := "mcp_computer_use_left_click"
	foundAlias := false
	for _, tool := range payload.Tools {
		if strings.HasPrefix(strings.ToLower(tool.Name), "mcp__") {
			t.Fatalf("mcp tool leaked into payload: %s", tool.Name)
		}
		if tool.Name == wantAlias {
			foundAlias = true
		}
	}
	if !foundAlias {
		t.Fatalf("missing alias %q in %+v", wantAlias, payload.Tools)
	}
	if payload.OriginalByAlias[wantAlias] != "mcp__computer-use__left_click" {
		t.Fatalf("reverse map=%v", payload.OriginalByAlias)
	}
	if len(payload.Prompts) < 2 || len(payload.Prompts[1].ToolCalls) != 1 {
		t.Fatalf("history prompts=%+v", payload.Prompts)
	}
	if payload.Prompts[1].ToolCalls[0].Name != wantAlias {
		t.Fatalf("history tool call name=%q", payload.Prompts[1].ToolCalls[0].Name)
	}
	if restoreToolName(wantAlias, payload.OriginalByAlias) != "mcp__computer-use__left_click" {
		t.Fatalf("restore failed")
	}
}

func TestChatStreamRestoresMCPToolName(t *testing.T) {
	original := "mcp__computer-use__left_click"
	alias := "mcp_computer_use_left_click"
	toolFrame := buildToolCallDeltaFrame("call_1", alias, `{"x":2}`, 0)

	var buf bytes.Buffer
	buf.Write(WrapConnectEnvelope(toolFrame))
	buf.Write(WrapConnectEnvelopeWithFlag(ConnectFlagEndStream, []byte(`{}`)))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != PathGetChatMessage {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if bytes.Contains(body, []byte(original)) {
			t.Errorf("upstream request still contains original mcp name")
		}
		if !bytes.Contains(body, []byte(alias)) {
			t.Errorf("upstream request missing aliased mcp name")
		}
		w.Header().Set("Content-Type", ContentTypeConnectProto)
		_, _ = w.Write(buf.Bytes())
	}))
	defer server.Close()

	store := newMemStore()
	store.accounts["acc1"] = accounts.Account{ID: "acc1", Provider: "devin", ProviderRegion: "global"}
	cred := Credential{SessionToken: FormatSessionToken("eyJabc.def.ghi"), DeviceSeed: "seed", BaseURL: server.URL}
	payload, err := cred.Encode()
	if err != nil {
		t.Fatal(err)
	}
	store.creds["acc1"] = payload

	tools := json.RawMessage(`[{"type":"function","function":{"name":"mcp__computer-use__left_click","description":"click","parameters":{"type":"object"}}}]`)
	client := NewClient(store)
	client.SetBases(AppBase, APIBase, server.URL)
	resp, err := client.ChatStream(context.Background(), "acc1", translate.ChatRequest{
		Model: "swe-2-high",
		Messages: []translate.ChatMessage{
			{Role: "user", Content: "click"},
		},
		Tools: tools,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, original) {
		t.Fatalf("client stream missing restored mcp name: %s", text)
	}
	if strings.Contains(text, `"`+alias+`"`) {
		t.Fatalf("client stream still exposes alias: %s", text)
	}
}

func TestCredentialErrorsDoNotLeakToken(t *testing.T) {
	secret := "eyJsuper.secret.token.value"
	raw := []byte(`{"session_token":"` + secret + `"}`)
	if err := ValidateCredential(raw); err != nil {
		t.Fatal(err)
	}
	cred, err := DecodeCredential(raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := cred.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(FormatSessionToken(secret))) {
		t.Fatal("encoded payload should keep formatted token for storage")
	}

	apiErr := classifiedError(401, "session dead; token="+FormatSessionToken(secret))
	if apiErr == nil {
		t.Fatal("expected classified error")
	}
	if strings.Contains(apiErr.Error(), secret) || strings.Contains(apiErr.Error(), FormatSessionToken(secret)) {
		t.Fatalf("secret leaked in classified error: %v", apiErr)
	}

	out, err := (importer{}).Export(context.Background(), "acc1")
	if out != nil {
		t.Fatalf("export payload=%v", out)
	}
	if !errors.Is(err, providers.ErrUnsupported) {
		t.Fatalf("export err=%v", err)
	}
	if err != nil && strings.Contains(err.Error(), secret) {
		t.Fatalf("export error leaked secret: %v", err)
	}
}

func TestModelsRequiresCredential(t *testing.T) {
	ClearCatalog()
	t.Cleanup(ClearCatalog)
	store := newMemStore()
	client := NewClient(store)
	if _, err := client.Models(context.Background(), "missing"); err == nil {
		t.Fatal("expected credential load failure")
	}
}
