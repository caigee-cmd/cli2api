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
	"sync/atomic"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	apipb "github.com/caigee-cmd/cli2api/internal/providers/devin/devinpb/api_server_pb"
	commonpb "github.com/caigee-cmd/cli2api/internal/providers/devin/devinpb/codeium_common_pb"
	"github.com/caigee-cmd/cli2api/internal/translate"
	"google.golang.org/protobuf/proto"
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
	req, err := BuildGetUserStatusRequest(token, strings.Repeat("ab", FingerprintHexLen/2))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(req, []byte(token)) {
		t.Fatal("request missing session token")
	}
	if !bytes.Contains(req, []byte(ClientName)) {
		t.Fatal("request missing client name")
	}
	if !bytes.Contains(req, append([]byte{0x0a, byte(len(ClientName))}, ClientName...)) {
		t.Fatal("request missing status client name field")
	}
	if bytes.Contains(req, append([]byte{0xe2, 0x01, byte(len(ClientName))}, ClientName...)) {
		t.Fatal("status request unexpectedly contains chat-only metadata field 28")
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
	textFrame := []byte("\x1a\x10hello from devin")
	stopFrame := []byte{0x28, 0x02}
	// GetChatMessageResponse.usage with independent upstream input, cache write,
	// and cache read fields. This captured-shape wire fixture is intentionally
	// not produced by the local request encoder.
	usageFrame := []byte{0x3a, 0x08, 0x10, 0x09, 0x18, 0x02, 0x20, 0x05, 0x28, 0x07}

	var buf bytes.Buffer
	buf.Write(WrapConnectEnvelope(textFrame))
	buf.Write(WrapConnectEnvelope(stopFrame))
	buf.Write(WrapConnectEnvelope(usageFrame))
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
	if out.PromptTokens != 21 || out.CompletionTokens != 2 {
		t.Fatalf("token totals = %d/%d", out.PromptTokens, out.CompletionTokens)
	}
	if out.CacheReadTokens == nil || *out.CacheReadTokens != 7 || out.CacheWriteTokens == nil || *out.CacheWriteTokens != 5 {
		t.Fatalf("cache usage = read:%v write:%v", out.CacheReadTokens, out.CacheWriteTokens)
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

func TestChatStreamTextToolAndDone(t *testing.T) {
	textFrame := []byte("\x1a\x08partial ")
	textFrame2 := []byte("\x1a\x06answer")
	toolFrame := []byte("\x32\x1b\x0a\x06call_1\x12\x06lookup\x1a\x09{\"q\":\"x\"}\x28\x0a")
	usageFrame := []byte{0x3a, 0x08, 0x10, 0x09, 0x18, 0x02, 0x20, 0x05, 0x28, 0x07}

	var buf bytes.Buffer
	buf.Write(WrapConnectEnvelope(textFrame))
	buf.Write(WrapConnectEnvelope(textFrame2))
	buf.Write(WrapConnectEnvelope(toolFrame))
	buf.Write(WrapConnectEnvelope(usageFrame))
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
	if !strings.Contains(text, `"prompt_tokens":21`) || !strings.Contains(text, `"cache_read_tokens":7`) || !strings.Contains(text, `"cache_write_tokens":5`) {
		t.Fatalf("missing upstream usage: %s", text)
	}
}

func TestAggregateConnectStreamMissingEOS(t *testing.T) {
	framed := WrapConnectEnvelope([]byte("\x1a\x06orphan"))
	_, err := aggregateConnectStream(bytes.NewReader(framed), nil, "")
	if err == nil {
		t.Fatal("expected missing EOS error")
	}
	if !strings.Contains(err.Error(), "missing EOS") {
		t.Fatalf("err=%v", err)
	}
}

func TestAggregateConnectStreamKeepsDistinctToolIDs(t *testing.T) {
	frames := [][]byte{
		[]byte("\x32\x12\x0a\x06call_a\x12\x05alpha\x1a\x01{"),
		[]byte("\x32\x12\x0a\x06call_b\x12\x04beta\x1a\x02[]"),
		[]byte("\x32\x10\x0a\x06call_a\x1a\x06\"a\":1}"),
	}
	var stream bytes.Buffer
	for _, frame := range frames {
		stream.Write(WrapConnectEnvelope(frame))
	}
	stream.Write(WrapConnectEnvelopeWithFlag(ConnectFlagEndStream, []byte(`{}`)))
	aggregate, err := aggregateConnectStream(&stream, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(aggregate.ToolCalls) != 2 {
		t.Fatalf("tool calls = %#v", aggregate.ToolCalls)
	}
	if aggregate.ToolCalls[0]["id"] != "call_a" || aggregate.ToolCalls[0]["function"].(map[string]any)["arguments"] != `{"a":1}` {
		t.Fatalf("first tool = %#v", aggregate.ToolCalls[0])
	}
	if aggregate.ToolCalls[1]["id"] != "call_b" {
		t.Fatalf("second tool = %#v", aggregate.ToolCalls[1])
	}
}

func TestChatStreamKeepsDistinctToolIDs(t *testing.T) {
	frames := [][]byte{
		[]byte("\x32\x12\x0a\x06call_a\x12\x05alpha\x1a\x01{"),
		[]byte("\x32\x12\x0a\x06call_b\x12\x04beta\x1a\x02[]"),
		[]byte("\x32\x10\x0a\x06call_a\x1a\x06\"a\":1}"),
	}
	var stream bytes.Buffer
	for _, frame := range frames {
		stream.Write(WrapConnectEnvelope(frame))
	}
	stream.Write(WrapConnectEnvelopeWithFlag(ConnectFlagEndStream, []byte(`{}`)))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", ContentTypeConnectProto)
		_, _ = w.Write(stream.Bytes())
	}))
	defer server.Close()
	store := newMemStore()
	store.accounts["acc1"] = accounts.Account{ID: "acc1", Provider: "devin", ProviderRegion: "global"}
	credential := Credential{SessionToken: FormatSessionToken("eyJabc.def.ghi"), DeviceSeed: "seed", BaseURL: server.URL}
	encoded, err := credential.Encode()
	if err != nil {
		t.Fatal(err)
	}
	store.creds["acc1"] = encoded
	client := NewClient(store)
	client.SetBases(AppBase, APIBase, server.URL)
	response, err := client.ChatStream(context.Background(), "acc1", translate.ChatRequest{Model: "swe-2-high", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{`"id":"call_a"`, `"id":"call_b"`, `"index":0`, `"index":1`} {
		if !strings.Contains(text, want) {
			t.Fatalf("stream missing %s: %s", want, text)
		}
	}
}

func TestChatStreamCancelClosesBody(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		textFrame := []byte("\x1a\x04late")
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
	err := classifiedErrorWithToolsDiag(403, body, "")
	var providerErr *providers.Error
	if !errors.As(err, &providerErr) {
		t.Fatalf("classifiedError type=%T", err)
	}
	if providerErr.Kind != accounts.KindInvalidRequest {
		t.Fatalf("kind=%s", providerErr.Kind)
	}
	if providerErr.RetryAfter != 0 {
		t.Fatalf("retry_after=%s want 0", providerErr.RetryAfter)
	}

	diag := "in=[namespace:mcp__computer-use,ns.function:left_click] out(1)=[mcp_computer_use_left_click]"
	err = classifiedErrorWithToolsDiag(403, body, diag)
	if !errors.As(err, &providerErr) {
		t.Fatalf("classifiedErrorWithToolsDiag type=%T", err)
	}
	if !strings.Contains(providerErr.Message, "tools_diag="+diag) {
		t.Fatalf("message missing tools_diag: %s", providerErr.Message)
	}
	if providerErr.Kind != accounts.KindInvalidRequest || providerErr.RetryAfter != 0 {
		t.Fatalf("diag classify=%+v", providerErr)
	}
}

func TestBuildToolsDiagSummarizesInboundAndOutbound(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"namespace","name":"mcp__computer-use","tools":[
			{"type":"function","name":"left_click","parameters":{"type":"object"}},
			{"type":"function","function":{"name":"mcp__computer-use__type","parameters":{"type":"object"}}}
		]},
		{"type":"mcp","server_label":"browser"},
		{"type":"web_search"},
		{"type":"custom","name":"weird_shell"},
		{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}
	]`)
	payload := BuildChatPayload(translate.ChatRequest{
		Model:    "swe-2",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
		Tools:    raw,
	}, nil)
	if !strings.Contains(payload.ToolsDiag, "in=[") {
		t.Fatalf("missing inbound summary: %s", payload.ToolsDiag)
	}
	if !strings.Contains(payload.ToolsDiag, "namespace:mcp__computer-use") {
		t.Fatalf("missing namespace entry: %s", payload.ToolsDiag)
	}
	if !strings.Contains(payload.ToolsDiag, "ns.function:left_click") {
		t.Fatalf("missing nested function: %s", payload.ToolsDiag)
	}
	if !strings.Contains(payload.ToolsDiag, "mcp:browser") {
		t.Fatalf("missing mcp shell: %s", payload.ToolsDiag)
	}
	if !strings.Contains(payload.ToolsDiag, "web_search") {
		t.Fatalf("missing web_search: %s", payload.ToolsDiag)
	}
	if !strings.Contains(payload.ToolsDiag, "custom:weird_shell") {
		t.Fatalf("missing custom type: %s", payload.ToolsDiag)
	}
	if !strings.Contains(payload.ToolsDiag, "out(") || !strings.Contains(payload.ToolsDiag, "lookup") {
		t.Fatalf("missing outbound summary: %s", payload.ToolsDiag)
	}
	if strings.Contains(payload.ToolsDiag, `"parameters"`) || strings.Contains(payload.ToolsDiag, "description") {
		t.Fatalf("diag leaked schema/description: %s", payload.ToolsDiag)
	}
}

func TestParseToolsAliasesMCPNamespace(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"function","function":{"name":"exec_command","description":"run","parameters":{"type":"object"}}},
		{"type":"function","function":{"name":"mcp__computer-use__left_click","description":"click","parameters":{"type":"object"}}},
		{"type":"function","function":{"name":"MCP__plugin_chrome__click","description":"click","parameters":{"type":"object"}}},
		{"type":"function","function":{"name":"list_mcp_resources","description":"list","parameters":{"type":"object"}}},
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
	if len(payload.Tools) != 5 {
		t.Fatalf("tools=%d want 5: %+v", len(payload.Tools), payload.Tools)
	}
	wantAlias := makeDevinToolAlias("mcp__computer-use__left_click")
	foundAlias := false
	for _, tool := range payload.Tools {
		if strings.Contains(strings.ToLower(tool.Name), "mcp") {
			t.Fatalf("mcp semantics leaked into payload: %s", tool.Name)
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
	listAlias := makeDevinToolAlias("list_mcp_resources")
	if payload.OriginalByAlias[listAlias] != "list_mcp_resources" {
		t.Fatalf("list_mcp_resources not aliased: %v", payload.OriginalByAlias)
	}
}

func TestParseToolsExpandsNamespaceAndDropsHostedShells(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"function","function":{"name":"lookup","description":"lookup","parameters":{"type":"object"}}},
		{"type":"namespace","name":"mcp__computer-use","tools":[
			{"type":"function","name":"left_click","description":"click","parameters":{"type":"object","properties":{"x":{"type":"number"}}}},
			{"type":"function","function":{"name":"mcp__computer-use__type","description":"type","parameters":{"type":"object"}}}
		]},
		{"type":"mcp","server_label":"computer-use"},
		{"type":"web_search"},
		{"type":"namespace","name":"mcp__empty","tools":[]}
	]`)
	payload := BuildChatPayload(translate.ChatRequest{
		Model:    "swe-2",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
		Tools:    raw,
	}, nil)
	names := make([]string, 0, len(payload.Tools))
	for _, tool := range payload.Tools {
		names = append(names, tool.Name)
		if tool.Name == "mcp__computer-use" || tool.Name == "mcp_computer_use" {
			t.Fatalf("namespace shell leaked as tool: %s", tool.Name)
		}
		if tool.Name == "web_search" || tool.Name == "computer-use" {
			t.Fatalf("hosted shell leaked as tool: %s", tool.Name)
		}
		if tool.Name != "lookup" && strings.Contains(strings.ToLower(tool.Name), "mcp") {
			t.Fatalf("mcp semantics leaked into payload: %s", tool.Name)
		}
	}
	want := map[string]bool{
		"lookup": true,
		makeDevinToolAlias("mcp__computer-use__left_click"): true,
		makeDevinToolAlias("mcp__computer-use__type"):       true,
	}
	if len(payload.Tools) != 3 {
		t.Fatalf("tools=%v want 3", names)
	}
	for _, tool := range payload.Tools {
		if !want[tool.Name] {
			t.Fatalf("unexpected tool %q in %v", tool.Name, names)
		}
	}
	leftAlias := makeDevinToolAlias("mcp__computer-use__left_click")
	if payload.OriginalByAlias[leftAlias] != "mcp__computer-use__left_click" {
		t.Fatalf("reverse map=%v", payload.OriginalByAlias)
	}
}

func TestCodexToolsetAliasesHaveNoMCPSemantics(t *testing.T) {
	names := []string{
		"exec_command", "write_stdin", "list_mcp_resources", "list_mcp_resource_templates", "read_mcp_resource",
		"request_user_input", "view_image",
		"multi_agent_v1__close_agent", "multi_agent_v1__resume_agent", "multi_agent_v1__send_input",
		"multi_agent_v1__spawn_agent", "multi_agent_v1__wait_agent",
		"mcp__codex_app__automation_update", "mcp__codex_app__create_thread", "mcp__node_repl__js",
		"get_goal", "create_goal", "update_goal",
	}
	tools := make([]map[string]any, 0, len(names))
	for _, name := range names {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":       name,
				"parameters": map[string]any{"type": "object"},
			},
		})
	}
	raw, err := json.Marshal(tools)
	if err != nil {
		t.Fatal(err)
	}
	payload := BuildChatPayload(translate.ChatRequest{
		Model:    "swe-2",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
		Tools:    raw,
	}, nil)
	if len(payload.Tools) != len(names) {
		t.Fatalf("tools=%d want %d", len(payload.Tools), len(names))
	}
	keptPlain := map[string]bool{}
	for _, tool := range payload.Tools {
		if strings.Contains(strings.ToLower(tool.Name), "mcp") {
			t.Fatalf("outbound still has mcp semantics: %s", tool.Name)
		}
		keptPlain[tool.Name] = true
	}
	for _, plain := range []string{"exec_command", "write_stdin", "view_image", "get_goal", "multi_agent_v1__close_agent"} {
		if !keptPlain[plain] {
			t.Fatalf("plain tool %q was renamed unexpectedly: %+v", plain, payload.Tools)
		}
	}
	if restoreToolName(makeDevinToolAlias("list_mcp_resources"), payload.OriginalByAlias) != "list_mcp_resources" {
		t.Fatalf("list_mcp_resources restore failed: %v", payload.OriginalByAlias)
	}
	stripped := stripMCPSemanticTools(payload.Tools, payload.OriginalByAlias)
	if countMCPSemanticTools(stripped, payload.OriginalByAlias) != 0 {
		t.Fatalf("strip left mcp tools: %+v", stripped)
	}
	if len(stripped) == 0 || len(stripped) >= len(payload.Tools) {
		t.Fatalf("strip count=%d from %d", len(stripped), len(payload.Tools))
	}
}

func TestChatStreamRestoresMCPToolName(t *testing.T) {
	original := "mcp__computer-use__left_click"
	alias := makeDevinToolAlias(original)
	toolFrame, err := proto.Marshal(&apipb.GetChatMessageResponse{
		DeltaToolCalls: []*commonpb.ChatToolCall{{
			Id:            "call_1",
			Name:          alias,
			ArgumentsJson: `{"x":2}`,
		}},
		StopReason: commonpb.StopReason_STOP_REASON_FUNCTION_CALL,
	})
	if err != nil {
		t.Fatal(err)
	}

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
		if bytes.Contains(body, []byte("mcp_computer_use_left_click")) {
			t.Errorf("upstream request still uses soft mcp_ alias")
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

func TestChatNonStreamFallsBackAfterMCPConfigDenial(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != PathGetChatMessage {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		n := calls.Add(1)
		w.Header().Set("Content-Type", ContentTypeConnectProto)
		if n == 1 {
			if !bytes.Contains(body, []byte(makeDevinToolAlias("list_mcp_resources"))) {
				t.Errorf("first request missing aliased list_mcp_resources")
			}
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`permission_denied: Unable to process request due to an MCP configuration issue.`))
			return
		}
		if bytes.Contains(body, []byte(makeDevinToolAlias("list_mcp_resources"))) || bytes.Contains(body, []byte("list_mcp_resources")) {
			t.Errorf("fallback request still contains mcp tool")
		}
		if !bytes.Contains(body, []byte("exec_command")) {
			t.Errorf("fallback request dropped plain exec_command")
		}
		var buf bytes.Buffer
		buf.Write(WrapConnectEnvelope([]byte("\x1a\x02OK")))
		buf.Write(WrapConnectEnvelopeWithFlag(ConnectFlagEndStream, []byte(`{}`)))
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

	tools := json.RawMessage(`[
		{"type":"function","function":{"name":"exec_command","parameters":{"type":"object"}}},
		{"type":"function","function":{"name":"list_mcp_resources","parameters":{"type":"object"}}},
		{"type":"function","function":{"name":"mcp__codex_app__create_thread","parameters":{"type":"object"}}}
	]`)
	out, err := client.ChatNonStream(context.Background(), "acc1", translate.ChatRequest{
		Model:    "swe-2",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
		Tools:    tools,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Content != "OK" {
		t.Fatalf("content=%q", out.Content)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls=%d want 2", calls.Load())
	}
}

func TestChatStreamFallsBackAfterImmediateMCPTrailer(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != PathGetChatMessage {
			http.NotFound(w, r)
			return
		}
		n := calls.Add(1)
		w.Header().Set("Content-Type", ContentTypeConnectProto)
		var buf bytes.Buffer
		if n == 1 {
			buf.Write(WrapConnectEnvelopeWithFlag(ConnectFlagEndStream, []byte(`{"error":{"code":"permission_denied","message":"Unable to process request due to an MCP configuration issue."}}`)))
			_, _ = w.Write(buf.Bytes())
			return
		}
		buf.Write(WrapConnectEnvelope([]byte("\x1a\x02OK")))
		buf.Write(WrapConnectEnvelopeWithFlag(ConnectFlagEndStream, []byte(`{}`)))
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

	tools := json.RawMessage(`[
		{"type":"function","function":{"name":"exec_command","parameters":{"type":"object"}}},
		{"type":"function","function":{"name":"mcp__node_repl__js","parameters":{"type":"object"}}}
	]`)
	resp, err := client.ChatStream(context.Background(), "acc1", translate.ChatRequest{
		Model:    "swe-2",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
		Tools:    tools,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "OK") {
		t.Fatalf("body=%s", body)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls=%d want 2", calls.Load())
	}
}

func TestChatStreamKeepsCoreToolsAfterStripStillDenied(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != PathGetChatMessage {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		n := calls.Add(1)
		w.Header().Set("Content-Type", ContentTypeConnectProto)
		var buf bytes.Buffer
		switch n {
		case 1:
			if !bytes.Contains(body, []byte(makeDevinToolAlias("list_mcp_resources"))) {
				t.Errorf("initial request missing aliased list_mcp_resources")
			}
			buf.Write(WrapConnectEnvelopeWithFlag(ConnectFlagEndStream, []byte(`{"error":{"code":"permission_denied","message":"Unable to process request due to an MCP configuration issue."}}`)))
			_, _ = w.Write(buf.Bytes())
			return
		case 2:
			if !bytes.Contains(body, []byte("exec_command")) {
				t.Errorf("strip_mcp fallback missing exec_command")
			}
			if bytes.Contains(body, []byte("list_mcp_resources")) || bytes.Contains(body, []byte(makeDevinToolAlias("list_mcp_resources"))) {
				t.Errorf("strip_mcp fallback still has mcp tool")
			}
			buf.Write(WrapConnectEnvelopeWithFlag(ConnectFlagEndStream, []byte(`{"error":{"code":"permission_denied","message":"Unable to process request due to an MCP configuration issue."}}`)))
			_, _ = w.Write(buf.Bytes())
			return
		default:
			if !bytes.Contains(body, []byte("exec_command")) {
				t.Errorf("core_tools fallback missing exec_command")
			}
			if !bytes.Contains(body, []byte("write_stdin")) || !bytes.Contains(body, []byte("view_image")) || !bytes.Contains(body, []byte("request_user_input")) {
				t.Errorf("core_tools fallback missing one of the core tools")
			}
			if bytes.Contains(body, []byte("MCP configuration")) || bytes.Contains(body, []byte("mcp_server")) {
				t.Errorf("core_tools fallback still carries MCP wording")
			}
			if bytes.Contains(body, []byte("get_goal")) || bytes.Contains(body, []byte("multi_agent_v1__")) {
				t.Errorf("core_tools fallback kept non-core tools")
			}
			buf.Write(WrapConnectEnvelope([]byte("\x1a\x02OK")))
			buf.Write(WrapConnectEnvelopeWithFlag(ConnectFlagEndStream, []byte(`{}`)))
			_, _ = w.Write(buf.Bytes())
		}
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

	tools := json.RawMessage(`[
		{"type":"function","function":{"name":"exec_command","description":"Runs a command alongside MCP configuration","parameters":{"type":"object","properties":{"mcp_server":{"type":"string"}}}}},
		{"type":"function","function":{"name":"write_stdin","description":"write","parameters":{"type":"object"}}},
		{"type":"function","function":{"name":"get_goal","description":"goal","parameters":{"type":"object"}}},
		{"type":"function","function":{"name":"list_mcp_resources","parameters":{"type":"object"}}}
	]`)
	resp, err := client.ChatStream(context.Background(), "acc1", translate.ChatRequest{
		Model:    "swe-2",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
		Tools:    tools,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "OK") {
		t.Fatalf("body=%s", body)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls=%d want 3", calls.Load())
	}
}

func TestCoreLocalToolsUsesMinimalSchemas(t *testing.T) {
	got := coreLocalTools()
	want := []string{"exec_command", "write_stdin", "view_image", "request_user_input"}
	if len(got) != len(want) {
		t.Fatalf("tools=%+v want %d", got, len(want))
	}
	for i, tool := range got {
		if tool.Name != want[i] {
			t.Fatalf("tool[%d]=%q want %q", i, tool.Name, want[i])
		}
		if strings.Contains(strings.ToLower(tool.Description), "tool") || strings.Contains(strings.ToLower(string(tool.Parameters)), "tool") {
			t.Fatalf("unsanitized schema: %+v", tool)
		}
		if !json.Valid(tool.Parameters) {
			t.Fatalf("invalid schema: %s", tool.Parameters)
		}
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

	apiErr := classifiedErrorWithToolsDiag(401, "session dead; token="+FormatSessionToken(secret), "")
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

// Count semantic tools for test assertions only.
func countMCPSemanticTools(tools []Tool, originalByAlias map[string]string) int {
	count := 0
	for _, tool := range tools {
		original := tool.Name
		if mapped, ok := originalByAlias[tool.Name]; ok && mapped != "" {
			original = mapped
		}
		if needsDevinToolAlias(original) || needsDevinToolAlias(tool.Name) {
			count++
		}
	}
	return count
}
