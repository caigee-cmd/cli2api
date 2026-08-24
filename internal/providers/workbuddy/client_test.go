package workbuddy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

type memStore struct {
	items map[string][]byte
}

func (s *memStore) Get(ctx context.Context, id string) (accounts.Account, error) {
	return accounts.Account{ID: id, Provider: "workbuddy", ProviderRegion: "cn"}, nil
}
func (s *memStore) LoadCredentialPayload(ctx context.Context, accountID string) (string, []byte, error) {
	payload, ok := s.items[accountID]
	if !ok {
		return "", nil, accounts.ErrAccountNotFound
	}
	return CredentialFormat, payload, nil
}
func (s *memStore) SaveCredentialPayload(ctx context.Context, accountID, format string, payload []byte) error {
	if s.items == nil {
		s.items = map[string][]byte{}
	}
	s.items[accountID] = payload
	return nil
}
func (s *memStore) Observe(ctx context.Context, id, remoteUID, status, lastError, lastKind string) error {
	return nil
}

func newTestClient(t *testing.T, handler http.Handler) (*Client, *memStore) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	store := &memStore{}
	client := NewClient(store)
	client.http = server.Client()
	// Point every base at the test server by overriding constants per request:
	// the client derives bases from credential domain, so use a transport rewriter.
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}
	return client, store
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

const (
	chatSSE = "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"O\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"K\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"get_time\",\"arguments\":\"{}\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2}}\n\n" +
		"data: [DONE]\n\n"
)

func TestLoginStatePollAndStore(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case pathAuthState:
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"state": "s1", "authUrl": "https://auth.example"}})
		case pathAuthToken:
			if r.URL.Query().Get("state") != "s1" {
				t.Fatalf("state=%s", r.URL.Query().Get("state"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"accessToken": "at", "refreshToken": "rt", "expiresIn": 3600, "domain": "codebuddy.cn",
			}})
		case pathAuthAccount:
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"uid": "u1", "enterpriseId": "e1", "nickname": "Tester",
			}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	session, err := client.StartLogin(context.Background(), "acc1")
	if err != nil || session.State != "s1" || session.AuthURL == "" {
		t.Fatalf("session=%+v err=%v", session, err)
	}
	done, _, err := client.PollLogin(context.Background(), "acc1")
	if err != nil || !done {
		t.Fatalf("done=%v err=%v", done, err)
	}
	format, payload, err := store.LoadCredentialPayload(context.Background(), "acc1")
	if err != nil || format != CredentialFormat {
		t.Fatalf("format=%s err=%v", format, err)
	}
	credential, err := DecodeCredential(payload)
	if err != nil || credential.UID != "u1" || credential.EnterpriseID != "e1" || credential.AccessToken != "at" {
		t.Fatalf("credential=%+v err=%v", credential, err)
	}
	if !credential.Ready() {
		t.Fatal("credential with uid and token must be ready")
	}
}

func TestChatNonStreamAggregatesToolsAndReasoning(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", UID: "u1", Domain: "codebuddy.cn", ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathChat {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("X-Product") != "SaaS" || r.Header.Get("Authorization") != "Bearer at" ||
			r.Header.Get("X-User-Id") != "u1" || r.Header.Get("User-Agent") != UserAgent {
			t.Fatalf("headers missing required identity: %+v", r.Header)
		}
		if r.Header.Get("X-Refresh-Token") != "" {
			t.Fatal("chat request must never carry the refresh token")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] != true {
			t.Fatalf("stream=%v, upstream requires true", body["stream"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(chatSSE))
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}

	out, err := client.ChatNonStream(context.Background(), "acc1", translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Content != "OK" || out.FinishReason != "tool_calls" || out.PromptTokens != 3 {
		t.Fatalf("outcome=%+v", out)
	}
	if !strings.Contains(string(out.ToolCalls), "get_time") {
		t.Fatalf("tool calls=%s", out.ToolCalls)
	}
}

func TestModelsFiltersCliAgentAndDisabled(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", UID: "u1", Domain: "codebuddy.cn", ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathModels {
			t.Fatalf("path=%s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"models": []map[string]any{
				{"id": "glm-5.2", "name": "GLM", "maxInputTokens": 128000, "maxOutputTokens": 16384},
				{"id": "secret-model", "disabled": true},
				{"id": "web-model"},
			},
			"agents": []map[string]any{
				{"name": "web", "models": []string{"web-model"}},
				{"name": "cli", "models": []string{"glm-5.2", "secret-model"}},
			},
		}})
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}

	models, err := client.Models(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].NativeModel != "glm-5.2" || models[0].Capabilities.ContextWindow != 128000 {
		t.Fatalf("models=%+v", models)
	}
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		status int
		body   string
		kind   string
	}{
		{402, `{"code":402}`, "quota"},
		{429, "too many requests", "rate_limit"},
		{401, `{"code":12153,"msg":"Offline user session not found"}`, "auth"},
		{404, "", "unavailable"},
		{500, "boom", "unavailable"},
	}
	for _, c := range cases {
		if got := Classify(c.status, c.body); got.Kind != c.kind {
			t.Fatalf("Classify(%d,%s)=%+v want %s", c.status, c.body, got, c.kind)
		}
	}
}

func TestPrepareBodyForcesStreamAndStringToolChoice(t *testing.T) {
	out := PrepareBody([]byte(`{"model":"m","stream":false,"tool_choice":{"type":"function","function":{"name":"get_time"}}}`))
	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatal(err)
	}
	if body["stream"] != true || body["tool_choice"] != "get_time" {
		t.Fatalf("body=%v", body)
	}
}
