package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

type testStore struct {
	items   map[string][]byte
	secrets map[string]string
}

func (s testStore) Get(context.Context, string) (accounts.Account, error) {
	return accounts.Account{Provider: "codex", ProviderRegion: "global"}, nil
}
func (s testStore) LoadCredentialPayload(_ context.Context, accountID string) (string, []byte, error) {
	payload, ok := s.items[accountID]
	if !ok {
		return "", nil, accounts.ErrAccountNotFound
	}
	return CredentialFormat, payload, nil
}
func (s testStore) SaveCredentialPayload(_ context.Context, accountID, _ string, payload []byte) error {
	if s.items == nil {
		return nil
	}
	s.items[accountID] = payload
	return nil
}
func (testStore) Observe(context.Context, string, string, string, string, string) error {
	return nil
}

func (s testStore) GetSecret(_ context.Context, name string) (string, bool, error) {
	if s.secrets == nil {
		return "", false, nil
	}
	value, ok := s.secrets[name]
	return value, ok, nil
}

func credentialPayload() []byte {
	payload, _ := Credential{
		AccessToken:  "at",
		RefreshToken: "rt",
		AccountID:    "acct_123",
		Email:        "u@example.com",
		ExpiresAt:    time.Now().Add(10 * 24 * time.Hour).Unix(),
	}.Encode()
	return payload
}

func TestAdapterCapabilities(t *testing.T) {
	adapter := NewClient(testStore{}).Adapter()
	if adapter.ID != "codex" {
		t.Fatalf("adapter id %q", adapter.ID)
	}
	for _, capability := range []string{"credential", "login", "chat", "models", "classifier", "import_export", "prober", "native_responses"} {
		if !adapter.Supports(capability) {
			t.Fatalf("missing capability %q", capability)
		}
	}
	if adapter.Supports("checkin") {
		t.Fatalf("codex must not support checkin")
	}
}

func TestCredentialRoundTrip(t *testing.T) {
	cred := Credential{
		IDToken:      "id",
		AccessToken:  "at",
		RefreshToken: "rt",
		AccountID:    "acct",
		Email:        "u@example.com",
		ExpiresAt:    1893456000,
	}
	payload, err := cred.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCredential(payload); err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCredential(payload)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.AccessToken != "at" || decoded.AccountID != "acct" || !decoded.Ready() {
		t.Fatalf("decoded %+v", decoded)
	}
}

func unsignedJWT(claims map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, _ := json.Marshal(claims)
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func TestParseTokenResponseReadsNestedChatGPTAccount(t *testing.T) {
	idToken := unsignedJWT(map[string]any{
		"email": "free@example.com",
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acct_free",
			"chatgpt_plan_type":  "free",
		},
	})
	body, _ := json.Marshal(map[string]any{
		"access_token":  "at",
		"refresh_token": "rt",
		"id_token":      idToken,
		"expires_in":    3600,
	})
	cred, err := parseTokenResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if cred.AccountID != "acct_free" || cred.Email != "free@example.com" || !cred.Ready() {
		t.Fatalf("credential %+v", cred)
	}
}

func TestProbeRecoversMissingAccountIDFromIDToken(t *testing.T) {
	idToken := unsignedJWT(map[string]any{
		"email": "free@example.com",
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acct_free",
			"chatgpt_plan_type":  "free",
		},
	})
	payload, _ := json.Marshal(Credential{
		IDToken:      idToken,
		AccessToken:  "at",
		RefreshToken: "rt",
		ExpiresAt:    time.Now().Add(10 * 24 * time.Hour).Unix(),
	})
	store := testStore{items: map[string][]byte{"acc-1": payload}}
	health, err := NewClient(store).Probe(context.Background(), "acc-1")
	if err != nil {
		t.Fatal(err)
	}
	if !health.Ready || health.LastError != "" || health.UID != "free@example.com" {
		t.Fatalf("health %+v", health)
	}
	recovered, err := DecodeCredential(store.items["acc-1"])
	if err != nil {
		t.Fatal(err)
	}
	if recovered.AccountID != "acct_free" {
		t.Fatalf("account id not persisted: %+v", recovered)
	}
}

func TestDecodeCredentialNestedTokenData(t *testing.T) {
	payload := []byte(`{"token_data":{"access_token":"at","refresh_token":"rt","account_id":"acct","email":"e@x","expired":"2027-01-01T00:00:00Z"}}`)
	cred, err := DecodeCredential(payload)
	if err != nil {
		t.Fatal(err)
	}
	if cred.AccessToken != "at" || cred.AccountID != "acct" || cred.ExpiresAt == 0 {
		t.Fatalf("nested decode %+v", cred)
	}
	if cred.UsesChatGPT() {
		t.Fatalf("cli nested credential reported chatgpt: %+v", cred)
	}
}

func TestDecodeCredentialNestedSIWC(t *testing.T) {
	payload := []byte(`{"use_siwc":true,"token_data":{"access_token":"outer-at","refresh_token":"outer-rt","account_id":"acct","email":"e@x","expired":"2027-01-01T00:00:00Z"},"siwc":{"access_token":"sat","refresh_token":"srt","client_id":"issued-client","device_id":"dev-1","expired":"2027-01-01T00:00:00Z"}}`)
	cred, err := DecodeCredential(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !cred.UsesChatGPT() || cred.AccessToken != "sat" || cred.ClientID != "issued-client" || cred.DeviceID != "dev-1" || !cred.Ready() || !cred.UseSIWC {
		t.Fatalf("siwc nested decode %+v", cred)
	}
	if cred.Outer == nil || cred.Outer.AccessToken != "outer-at" || cred.Outer.RefreshToken != "outer-rt" {
		t.Fatalf("outer partition dropped: %+v", cred.Outer)
	}
}

func chatgptCredentialPayload() []byte {
	payload, _ := Credential{
		AccessToken:  "sat",
		RefreshToken: "srt",
		AccountID:    "acct_123",
		Email:        "u@example.com",
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
		AuthMethod:   AuthMethodChatGPT,
		ClientID:     "issued-client",
		DeviceID:     "dev-1",
	}.Encode()
	return payload
}

func TestStartLoginUsesChatGPTAuthorizeURL(t *testing.T) {
	client := NewClient(testStore{secrets: map[string]string{codexLoginMethodSecret("a1"): "chatgpt"}})
	session, err := client.startLogin(context.Background(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(session.AuthURL, SIWCAuthorizeURL+"?") {
		t.Fatalf("auth url = %s", session.AuthURL)
	}
	parsed, err := url.Parse(session.AuthURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if query.Get("client_id") != SIWCDynamicClient || query.Get("scope") != SIWCScope || query.Get("resource") != SIWCResource {
		t.Fatalf("query = %v", query)
	}
}

func TestChatRequestRoutesChatGPTToAPIOpenAI(t *testing.T) {
	client := NewClient(testStore{items: map[string][]byte{"a1": chatgptCredentialPayload()}})
	credential, err := client.credential(context.Background(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	req := translate.ChatRequest{Model: "gpt-5.5", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}}
	httpReq, _, err := client.chatRequest(context.Background(), credential, req, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if httpReq.URL.String() != SIWCBaseURL+pathResponses {
		t.Fatalf("url = %s", httpReq.URL)
	}
	if httpReq.Header.Get("Originator") != "" || httpReq.Header.Get("Chatgpt-Account-Id") != "" {
		t.Fatalf("cli headers leaked: %v", httpReq.Header)
	}
	if httpReq.Header.Get("User-Agent") != SIWCAgentName+" (darwin; arm64)" {
		t.Fatalf("user-agent = %q", httpReq.Header.Get("User-Agent"))
	}
	body, _ := io.ReadAll(httpReq.Body)
	var obj map[string]any
	if json.Unmarshal(body, &obj) != nil {
		t.Fatal("body not json")
	}
	if _, ok := obj["prompt_cache_key"]; ok {
		t.Fatalf("prompt_cache_key forwarded: %v", obj)
	}
	if _, ok := obj["instructions"]; ok {
		t.Fatalf("empty instructions forwarded: %v", obj)
	}
}

func TestQuotaSkipsWhamUsageForChatGPT(t *testing.T) {
	client := NewClient(testStore{items: map[string][]byte{"a1": chatgptCredentialPayload()}})
	cached := &providers.QuotaInfo{Remaining: 42, ProviderID: "codex"}
	client.rememberQuota("a1", cached)
	got, err := client.Quota(context.Background(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	if got != cached {
		t.Fatalf("chatgpt quota = %+v, want cached", got)
	}
}

func TestScanCodexSSEJoinsSplitCompletedEvent(t *testing.T) {
	sse := strings.Join([]string{
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":"hi"}`,
		`event: response.completed`,
		`data: {"type":"response.completed","response":{"status":"completed",`,
		`data: "usage":{"input_tokens":10,"output_tokens":5},"output":[]}}`,
	}, "\n")
	outcome, err := aggregate(strings.NewReader(sse), "gpt-5.5")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Content != "hi" || outcome.PromptTokens != 10 || outcome.CompletionTokens != 5 {
		t.Fatalf("split completed lost usage: %+v", outcome)
	}
}

func TestNormalizeToolParametersStripsInt64OverflowBounds(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer","minimum":0,"maximum":9223372036854775808,"exclusiveMaximum":18446744073709551615}}}`)
	got, changed := normalizeToolParameters(raw)
	if !changed {
		t.Fatal("expected overflow bounds to be stripped")
	}
	if strings.Contains(string(got), "9223372036854775808") || strings.Contains(string(got), "exclusiveMaximum") {
		t.Fatalf("overflow bounds remain: %s", got)
	}
	if !strings.Contains(string(got), `"minimum":0`) {
		t.Fatalf("in-range bound dropped: %s", got)
	}
}

func TestBuildBodyReasoningClamp(t *testing.T) {
	caps := capsFor("gpt-5.5")
	req := translate.ChatRequest{
		Model:           "gpt-5.5",
		Messages:        []translate.ChatMessage{{Role: "user", Content: "hi"}},
		ReasoningEffort: json.RawMessage(`"max"`),
	}
	body, resolved, err := buildBody(req, caps)
	if err != nil {
		t.Fatal(err)
	}
	// gpt-5.5 declares low/medium/high/xhigh; "max" clamps to the default.
	if resolved.ReasoningLevel != "medium" {
		t.Fatalf("reasoning level %q", resolved.ReasoningLevel)
	}
	var obj map[string]any
	if json.Unmarshal(body, &obj) != nil {
		t.Fatal("body not json")
	}
	if obj["model"] != "gpt-5.5" || obj["stream"] != true || obj["instructions"] != "" || obj["store"] != false {
		t.Fatalf("body %v", obj)
	}
	if _, ok := obj["temperature"]; ok {
		t.Fatalf("temperature forwarded: %v", obj["temperature"])
	}
	include, _ := obj["include"].([]any)
	if len(include) != 1 || include[0] != "reasoning.encrypted_content" {
		t.Fatalf("include %v", obj["include"])
	}
	reasoning, _ := obj["reasoning"].(map[string]any)
	if reasoning["effort"] != "medium" {
		t.Fatalf("reasoning %v", obj["reasoning"])
	}
}

func TestBuildBodyToolCallItems(t *testing.T) {
	req := translate.ChatRequest{
		Model: "gpt-5.5",
		Messages: []translate.ChatMessage{
			{Role: "user", Content: "call it"},
			{Role: "assistant", Content: "", ToolCalls: json.RawMessage(`[{"id":"call_1","type":"function","function":{"name":"run","arguments":"{\"x\":1}"}}]`)},
			{Role: "tool", ToolCallID: "call_1", Content: "done"},
		},
	}
	body, _, err := buildBody(req, capsFor("gpt-5.5"))
	if err != nil {
		t.Fatal(err)
	}
	var obj struct {
		Input []map[string]any `json:"input"`
	}
	if json.Unmarshal(body, &obj) != nil {
		t.Fatal("bad json")
	}
	if len(obj.Input) != 4 {
		t.Fatalf("input items %d", len(obj.Input))
	}
	if obj.Input[2]["type"] != "function_call" || obj.Input[3]["type"] != "function_call_output" {
		t.Fatalf("input %v", obj.Input)
	}
}

func TestBuildBodyDropsCodexRejectedChatFields(t *testing.T) {
	req := translate.ChatRequest{
		Model: "gpt-5.5",
		Messages: []translate.ChatMessage{
			{Role: "system", Content: "rules"},
			{Role: "user", Content: "hi"},
		},
		Temperature: json.RawMessage(`0.2`),
		MaxTokens:   json.RawMessage(`128`),
		TopP:        json.RawMessage(`0.9`),
		Tools:       json.RawMessage(`[{"type":"function","function":{"name":"run","parameters":{"type":"object","properties":{}}}}]`),
	}
	body, _, err := buildBody(req, capsFor("gpt-5.5"))
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if json.Unmarshal(body, &obj) != nil {
		t.Fatal("body not json")
	}
	for _, field := range []string{"temperature", "top_p", "max_output_tokens", "max_tokens"} {
		if _, ok := obj[field]; ok {
			t.Errorf("rejected field %q forwarded", field)
		}
	}
	if obj["store"] != false || obj["parallel_tool_calls"] != true {
		t.Fatalf("store=%v parallel=%v", obj["store"], obj["parallel_tool_calls"])
	}
	input, _ := json.Marshal(obj["input"])
	if strings.Contains(string(input), `"role":"system"`) || !strings.Contains(string(input), `"role":"developer"`) {
		t.Fatalf("system role was not rewritten: %s", input)
	}
}

func TestAggregateTerminal(t *testing.T) {
	sse := strings.Join([]string{
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		``,
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":" world"}`,
		``,
		`event: response.completed`,
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":10,"output_tokens":5},"output":[{"type":"message","content":[{"type":"output_text","text":"hello world"}]}]}}`,
		``,
	}, "\n")
	outcome, err := aggregate(strings.NewReader(sse), "gpt-5.5")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Content != "hello world" {
		t.Fatalf("content %q", outcome.Content)
	}
	if outcome.PromptTokens != 10 || outcome.CompletionTokens != 5 {
		t.Fatalf("usage %+v", outcome)
	}
}

func TestChatStreamRewritesToChatCompletions(t *testing.T) {
	sse := strings.Join([]string{
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":"hi"}`,
		``,
		`event: response.completed`,
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":1,"output_tokens":1},"output":[]}}`,
		``,
	}, "\n")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Chatgpt-Account-Id") != "acct_123" {
			t.Errorf("missing account id header")
		}
		if r.Header.Get("Originator") != Originator {
			t.Errorf("missing originator")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Codex-Primary-Used-Percent", "42")
		_, _ = w.Write([]byte(sse))
	}))
	defer upstream.Close()

	client := NewClient(testStore{items: map[string][]byte{"a1": credentialPayload()}})
	// Point the chat base at the test server.
	old := ChatBase
	_ = old
	resp, resolved, err := func() (*http.Response, providers.ResolvedChat, error) {
		credential, err := client.credential(context.Background(), "a1")
		if err != nil {
			return nil, providers.ResolvedChat{}, err
		}
		req := translate.ChatRequest{Model: "gpt-5.5", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}}
		httpReq, resolved, err := client.chatRequest(context.Background(), credential, req, "a1")
		if err != nil {
			return nil, resolved, err
		}
		httpReq.URL.Host = strings.TrimPrefix(upstream.URL, "http://")
		httpReq.URL.Scheme = "http"
		httpReq.Host = httpReq.URL.Host
		out, err := http.DefaultClient.Do(httpReq)
		return out, resolved, err
	}()
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	stream, err := rewriteStream(resp.Body, "gpt-5.5")
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 8192)
	n, _ := stream.Reader.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, `"content":"hi"`) {
		t.Fatalf("expected content delta, got %s", out)
	}
	if !strings.Contains(out, "[DONE]") && !strings.Contains(out, "data:") {
		t.Fatalf("expected SSE output, got %s", out)
	}
	_ = resolved
}

func TestClassifyAuth(t *testing.T) {
	if got := Classify(401, `{"error":{"code":"invalid_api_key","message":"bad"}}`); got.Kind != accounts.KindAuth {
		t.Fatalf("kind %q", got.Kind)
	}
	if got := Classify(429, `{"error":{"code":"rate_limit_exceeded","message":"slow"}}`); got.Kind != accounts.KindRateLimit {
		t.Fatalf("kind %q", got.Kind)
	}
	if got := Classify(403, `<!doctype html>cloudflare`); got.Kind != accounts.KindUnavailable {
		t.Fatalf("cloudflare kind %q", got.Kind)
	}
}
