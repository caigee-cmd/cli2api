package zhipu

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func init() {
	acceptChatBase = func(parsed *url.URL) bool {
		if parsed == nil || parsed.Host == "" {
			return false
		}
		if strings.EqualFold(parsed.Scheme, "https") {
			return true
		}
		return parsed.Scheme == "http" && (parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost")
	}
}

func TestCredentialDefaultsToPayGAndRejectsBadBase(t *testing.T) {
	credential, err := DecodeCredential([]byte(`{"apiKey":" Bearer sk-test ","account_mode":"coding","zhipu_organization":"org-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if credential.APIKey != "sk-test" || credential.Mode != ModeCoding || credential.Organization != "org-1" {
		t.Fatalf("decoded credential = %+v", credential)
	}
	base, err := credential.ChatBase()
	if err != nil || base != CodingBaseURL {
		t.Fatalf("chat base = %q err=%v", base, err)
	}
	credential.BaseURL = "http://open.bigmodel.cn/api/paas/v4"
	if err := ValidateCredential(mustJSON(t, credential)); err == nil {
		t.Fatal("http base url must be rejected")
	}
}

func TestChatEndpointKeepsV4(t *testing.T) {
	got := chatEndpoint(CodingBaseURL, "/v1/chat/completions")
	want := CodingBaseURL + "/chat/completions"
	if got != want {
		t.Fatalf("chat endpoint = %q, want %q", got, want)
	}
	if got := chatEndpoint("https://example.test", "/v1/models"); got != "https://example.test/v1/models" {
		t.Fatalf("bare origin endpoint = %q", got)
	}
}

func TestGLMReasoningScale(t *testing.T) {
	cases := []struct {
		model string
		level string
		want  string
	}{
		{"glm-5.2", "low", "high"},
		{"glm-5.2", "medium", "high"},
		{"glm-5.2", "xhigh", "max"},
		{"glm-5.3", "low", "low"},
		{"glm-5.3", "max", "max"},
		{"glm-4.7", "none", ""},
	}
	for _, tc := range cases {
		body, err := buildChatBody(translate.ChatRequest{
			Model:           tc.model,
			Messages:        []translate.ChatMessage{{Role: "developer", Content: "rules"}, {Role: "user", Content: "hi"}},
			ReasoningEffort: json.RawMessage(`"` + tc.level + `"`),
		}, tc.model, zhipuEffort(tc.model, tc.level))
		if tc.level == "none" {
			body, err = buildChatBody(translate.ChatRequest{
				Model:    tc.model,
				Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
			}, tc.model, "")
		}
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if json.Unmarshal(body, &got) != nil {
			t.Fatalf("body is not json: %s", body)
		}
		if tc.want == "" {
			if _, ok := got["reasoning_effort"]; ok {
				t.Fatalf("%s %s kept reasoning_effort", tc.model, tc.level)
			}
		} else if got["reasoning_effort"] != tc.want {
			t.Fatalf("%s %s effort = %v, want %s", tc.model, tc.level, got["reasoning_effort"], tc.want)
		}
		messages := got["messages"].([]any)
		first := messages[0].(map[string]any)
		if tc.model == "glm-4.7" {
			continue
		}
		if first["role"] != "system" {
			t.Fatalf("developer role was not rewritten: %v", first["role"])
		}
	}
}

func TestCatalogDeclaresGLM53Low(t *testing.T) {
	models, err := ParseCatalogJSON([]byte(`{"data":[{"id":"glm-5.3"},{"id":"glm-5.2"},{"id":"glm-5.3"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %d, want 2 after dedup", len(models))
	}
	if got := strings.Join(models[0].Capabilities.ReasoningOptions, ","); got != "low,high,max" {
		t.Fatalf("glm-5.3 options = %s", got)
	}
	if got := strings.Join(models[1].Capabilities.ReasoningOptions, ","); got != "high,max" {
		t.Fatalf("glm-5.2 options = %s", got)
	}
}

func TestChatStreamPostsBearerAndRelaysSSE(t *testing.T) {
	var gotAuth, gotPath string
	var gotBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1}}\n\ndata: [DONE]\n")
	}))
	defer upstream.Close()

	store := &memoryStore{payload: mustJSON(t, Credential{APIKey: "sk-live", BaseURL: upstream.URL + "/v4"})}
	client := NewClient(store)
	resp, resolved, err := client.ChatStream(context.Background(), "acc", translate.ChatRequest{
		Model:           "glm-5.2",
		Messages:        []translate.ChatMessage{{Role: "user", Content: "hi"}},
		ReasoningEffort: json.RawMessage(`"medium"`),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if gotAuth != "Bearer sk-live" {
		t.Fatalf("authorization = %q", gotAuth)
	}
	if gotPath != "/v4/chat/completions" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotBody["reasoning_effort"] != "high" || gotBody["stream"] != true {
		t.Fatalf("outbound body = %#v", gotBody)
	}
	if resolved.ReasoningLevel != "high" {
		t.Fatalf("resolved level = %q", resolved.ReasoningLevel)
	}
	outcome, err := collectChatSSE(resp.Body, "glm-5.2")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Content != "ok" || outcome.PromptTokens != 3 || outcome.CompletionTokens != 1 {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestCodingQuotaURLAndWindows(t *testing.T) {
	target, err := quotaURL("https://open.bigmodel.cn/api/coding/paas/v4", "org-1")
	if err != nil {
		t.Fatal(err)
	}
	if target != CNQuotaHost+QuotaPath+"?type=2" {
		t.Fatalf("cn quota url = %s", target)
	}
	global, err := quotaURL("https://api.z.ai/api/coding/paas/v4", "")
	if err != nil {
		t.Fatal(err)
	}
	if global != GlobalQuotaHost+QuotaPath {
		t.Fatalf("global quota url = %s", global)
	}
	info, err := parseQuota([]byte(`{"success":true,"data":{"level":"pro","limits":[{"type":"TOKENS_LIMIT","unit":3,"percentage":20,"nextResetTime":1710000000000},{"type":"TOKENS_LIMIT","unit":6,"percentage":80},{"type":"CREDIT_LIMIT","unit":3,"percentage":99}]}}`), time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if info.Plan != "pro" || info.Percentage != 80 || len(info.Windows) != 2 {
		t.Fatalf("quota = %+v", info)
	}
	if info.Windows[0].ID != "5h" || info.Windows[0].ResetAt == "" || info.Windows[1].ID != "weekly" {
		t.Fatalf("windows = %+v", info.Windows)
	}
}

func TestClassifyHidesBearerAndMapsQuota(t *testing.T) {
	got := Classify(402, `{"error":"balance low","authorization":"Bearer sk-secret-value"}`)
	if got.Kind != accounts.KindQuota {
		t.Fatalf("kind = %s", got.Kind)
	}
	if strings.Contains(got.Message, "sk-secret-value") {
		t.Fatalf("secret leaked: %s", got.Message)
	}
}

type memoryStore struct {
	payload []byte
}

func (s *memoryStore) Get(context.Context, string) (accounts.Account, error) {
	return accounts.Account{}, nil
}
func (s *memoryStore) LoadCredentialPayload(context.Context, string) (string, []byte, error) {
	return CredentialFormat, append([]byte(nil), s.payload...), nil
}
func (s *memoryStore) SaveCredentialPayload(context.Context, string, string, []byte) error {
	return nil
}
func (s *memoryStore) Observe(context.Context, string, string, string, string, string) error {
	return nil
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
