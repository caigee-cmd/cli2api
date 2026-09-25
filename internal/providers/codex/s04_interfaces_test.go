package codex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

type testStore struct {
	items map[string][]byte
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
func (testStore) SaveCredentialPayload(context.Context, string, string, []byte) error { return nil }
func (testStore) Observe(context.Context, string, string, string, string, string) error {
	return nil
}

func credentialPayload() []byte {
	payload, _ := Credential{
		AccessToken:  "at",
		RefreshToken: "rt",
		AccountID:    "acct_123",
		Email:        "u@example.com",
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
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

func TestDecodeCredentialNestedTokenData(t *testing.T) {
	payload := []byte(`{"token_data":{"access_token":"at","refresh_token":"rt","account_id":"acct","email":"e@x","expired":"2027-01-01T00:00:00Z"}}`)
	cred, err := DecodeCredential(payload)
	if err != nil {
		t.Fatal(err)
	}
	if cred.AccessToken != "at" || cred.AccountID != "acct" || cred.ExpiresAt == 0 {
		t.Fatalf("nested decode %+v", cred)
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
	if obj["model"] != "gpt-5.5" || obj["stream"] != true || obj["instructions"] != "" {
		t.Fatalf("body %v", obj)
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
