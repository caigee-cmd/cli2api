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
	items  map[string][]byte
	region string
}

func (s *memStore) Get(ctx context.Context, id string) (accounts.Account, error) {
	region := s.region
	if region == "" {
		region = "cn"
	}
	return accounts.Account{ID: id, Provider: "workbuddy", ProviderRegion: region}, nil
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
		if r.URL.Path == pathModelsCN {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"models": []map[string]any{{"id": "glm-5.2", "name": "GLM", "maxInputTokens": 128000}},
				"agents": []map[string]any{{"name": "cli", "models": []string{"glm-5.2"}}},
			}})
			return
		}
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
		messages, _ := body["messages"].([]any)
		if len(messages) < 2 {
			t.Fatalf("messages=%v", body["messages"])
		}
		first, _ := messages[0].(map[string]any)
		if first["role"] != "system" {
			t.Fatalf("global/CN chat requires a leading system slot, got %v", body["messages"])
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
		if r.URL.Path != pathModelsCN {
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

func TestModelsParsesReasoningOptions(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", UID: "u1", Domain: "codebuddy.cn", ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	disable := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"models": []map[string]any{
				{
					"id": "glm-5.3", "name": "GLM-5.3", "maxInputTokens": 1000000, "maxOutputTokens": 48000,
					"onlyReasoning": false, "supportsReasoning": true,
					"reasoning": map[string]any{
						"canDisableThinking": disable, "defaultEffort": "high", "supportedEfforts": []string{"low", "high", "xhigh"},
					},
				},
				{
					"id": "glm-5.2", "name": "GLM-5.2", "maxInputTokens": 1000000,
					"onlyReasoning": true, "supportsReasoning": true,
					"reasoning": map[string]any{"effort": "medium"},
				},
			},
			"agents": []map[string]any{{"name": "cli", "models": []string{"glm-5.3", "glm-5.2"}}},
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
	if len(models) != 2 {
		t.Fatalf("models=%+v", models)
	}
	glm53 := models[0]
	if !glm53.Capabilities.CanDisableThinking || glm53.Capabilities.ReasoningDefault != "high" {
		t.Fatalf("glm-5.3 caps=%+v", glm53.Capabilities)
	}
	if got := strings.Join(glm53.Capabilities.ReasoningOptions, ","); got != "none,low,high,xhigh" {
		t.Fatalf("glm-5.3 options=%q", got)
	}
	glm52 := models[1]
	if glm52.Capabilities.CanDisableThinking || glm52.Capabilities.ReasoningDefault != "medium" || strings.Join(glm52.Capabilities.ReasoningOptions, ",") != "medium" {
		t.Fatalf("glm-5.2 caps=%+v", glm52.Capabilities)
	}
}

func TestChatRequestSendsCatalogReasoningEffort(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", UID: "u1", Domain: "codebuddy.cn", ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pathModelsCN {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"models": []map[string]any{{
					"id": "glm-5.3", "name": "GLM-5.3", "supportsReasoning": true,
					"reasoning": map[string]any{"defaultEffort": "high", "supportedEfforts": []string{"low", "high", "xhigh"}},
				}},
				"agents": []map[string]any{{"name": "cli", "models": []string{"glm-5.3"}}},
			}})
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(chatSSE))
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}
	if _, err := client.ChatNonStream(context.Background(), "acc1", translate.ChatRequest{
		Model: "glm-5.3", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
		ReasoningEffort: json.RawMessage(`"xhigh"`),
	}); err != nil {
		t.Fatal(err)
	}
	reasoning, _ := got["reasoning"].(map[string]any)
	if reasoning["effort"] != "xhigh" {
		t.Fatalf("reasoning=%v", got["reasoning"])
	}
}

func TestModelsAcceptsGlobalCLIAgentNamesAndUsesAccountRegion(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", UID: "u1", Domain: "codebuddy.cn", ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}, region: "global"}
	var origin, requestHost, ideType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathModelsGlobal {
			t.Fatalf("path=%s", r.URL.Path)
		}
		origin = r.Header.Get("Origin")
		requestHost = r.Host
		ideType = r.Header.Get("X-IDE-Type")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"models": []map[string]any{
				{"id": "glm-5.2", "name": "GLM", "maxInputTokens": 128000, "maxOutputTokens": 16384},
				{"id": "web-model"},
			},
			"agents": []map[string]any{
				{"name": "web", "models": []string{"web-model"}},
				{"name": "CLI", "models": []string{"glm-5.2"}},
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
	if len(models) != 1 || models[0].NativeModel != "glm-5.2" {
		t.Fatalf("models=%+v", models)
	}
	if origin != "https://www.workbuddy.ai" {
		t.Fatalf("origin=%s", origin)
	}
	if ideType != "" {
		t.Fatalf("catalog must not send chat-only CLI headers, got X-IDE-Type=%q host=%s", ideType, requestHost)
	}
}

func TestModelsWithoutAgentsReturnsEnabledModels(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", UID: "u1", Domain: "www.workbuddy.ai", ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}, region: "global"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathModelsGlobal {
			t.Fatalf("path=%s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"models": []map[string]any{
				{"id": "glm-5.2", "name": "GLM"},
				{"id": "secret-model", "disabled": true},
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
	if len(models) != 1 || models[0].NativeModel != "glm-5.2" {
		t.Fatalf("models=%+v", models)
	}
}

func TestIsGlobalRecognizesWorkBuddyDomains(t *testing.T) {
	if (Credential{Domain: "www.workbuddy.ai"}).IsGlobal() != true {
		t.Fatal("workbuddy.ai should be global")
	}
	if (Credential{Domain: "workbuddy.com"}).IsGlobal() != true {
		t.Fatal("workbuddy.com should be global")
	}
	if (Credential{Domain: "codebuddy.cn"}).IsGlobal() {
		t.Fatal("codebuddy.cn should stay CN")
	}
	if (Credential{}).IsGlobal() {
		t.Fatal("empty domain should not be global")
	}
	if (Credential{Domain: "www.workbuddy.ai"}).catalogPath() != pathModelsGlobal {
		t.Fatal("global catalog must use the plugin JSON path")
	}
	if (Credential{Domain: "codebuddy.cn"}).catalogPath() != pathModelsCN {
		t.Fatal("CN catalog must keep the console path")
	}
}

func TestModelsGlobalHTMLErrorDoesNotLeakPage(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", UID: "u1", Domain: "www.workbuddy.ai", ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}, region: "global"}
	var sawConsolePath bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pathModelsCN {
			sawConsolePath = true
		}
		if r.URL.Path != pathModelsGlobal {
			t.Fatalf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><title>500 Internal Server Error</title></head><body>openresty</body></html>`))
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}

	_, err := client.Models(context.Background(), "acc1")
	if err == nil {
		t.Fatal("expected catalog error")
	}
	if sawConsolePath {
		t.Fatal("global catalog must not hit the console OIDC path")
	}
	if !strings.Contains(err.Error(), "models status=500") || strings.Contains(err.Error(), "<html") {
		t.Fatalf("error leaked html: %v", err)
	}
	if !strings.Contains(err.Error(), "upstream html error page") {
		t.Fatalf("error=%v", err)
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
		{400, `{"code":11101,"msg":"bad request"}`, "invalid_request"},
		{200, `{"code":11128,"msg":"first message is not system prompt"}`, "invalid_request"},
		{200, `{"code":12001,"msg":"内容包含敏感信息"}`, "invalid_request"},
		{200, `{"code":12002,"msg":"sensitive content detected"}`, "invalid_request"},
	}
	for _, c := range cases {
		got := Classify(c.status, c.body)
		if got.Kind != c.kind {
			t.Fatalf("Classify(%d,%s)=%+v want %s", c.status, c.body, got, c.kind)
		}
		if c.kind == "invalid_request" && got.Status != 400 && got.Status != c.status {
			t.Fatalf("Classify(%d,%s) status=%d want request-level 400", c.status, c.body, got.Status)
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

func TestPrepareBodyInsertsEmptyLeadingSystem(t *testing.T) {
	out := PrepareBody([]byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatal(err)
	}
	messages, _ := body["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("messages=%v", body["messages"])
	}
	first, _ := messages[0].(map[string]any)
	second, _ := messages[1].(map[string]any)
	if first["role"] != "system" || first["content"] != "" || second["role"] != "user" {
		t.Fatalf("messages=%v", body["messages"])
	}

	kept := PrepareBody([]byte(`{"model":"m","messages":[{"role":"system","content":"keep me"},{"role":"user","content":"hi"}]}`))
	var keptBody map[string]any
	if err := json.Unmarshal(kept, &keptBody); err != nil {
		t.Fatal(err)
	}
	keptMessages, _ := keptBody["messages"].([]any)
	if len(keptMessages) != 2 {
		t.Fatalf("existing system should not be duplicated: %v", keptBody["messages"])
	}
	firstKept, _ := keptMessages[0].(map[string]any)
	if firstKept["content"] != "keep me" {
		t.Fatalf("existing system rewritten: %v", keptBody["messages"])
	}
}

func TestPrepareBodyDropsNullAndEmptyTools(t *testing.T) {
	nullOut := PrepareBody([]byte(`{"model":"m","tools":null}`))
	var nullBody map[string]any
	if err := json.Unmarshal(nullOut, &nullBody); err != nil {
		t.Fatal(err)
	}
	if _, ok := nullBody["tools"]; ok {
		t.Fatalf("null tools should be dropped: %v", nullBody)
	}
	emptyOut := PrepareBody([]byte(`{"model":"m","tools":[],"tool_choice":"auto"}`))
	var emptyBody map[string]any
	if err := json.Unmarshal(emptyOut, &emptyBody); err != nil {
		t.Fatal(err)
	}
	if _, ok := emptyBody["tools"]; ok {
		t.Fatalf("empty tools should be dropped: %v", emptyBody)
	}
	if _, ok := emptyBody["tool_choice"]; ok {
		t.Fatalf("tool_choice without tools should be dropped: %v", emptyBody)
	}
}

func TestChatHeadersStayRegionSpecificAndIncludeCLIChannel(t *testing.T) {
	cn := http.Header{}
	SetChatHeaders(cn, Credential{AccessToken: "at", UID: "u1", Domain: "www.codebuddy.cn"})
	if cn.Get("Origin") != "https://www.codebuddy.cn" || cn.Get("Referer") != "https://www.codebuddy.cn/" {
		t.Fatalf("CN origin/referer mixed: %+v", cn)
	}
	if got := cn.Get("User-Agent"); got != UserAgent || !strings.Contains(got, "2.139.0") {
		t.Fatalf("CN UA=%q", got)
	}
	if cn.Get("X-Product") != "SaaS" || cn.Get("X-IDE-Type") != "CLI" || cn.Get("X-Agent-Intent") != "craft" ||
		cn.Get("X-Agent-Type") != "main" || cn.Get("X-Private-Data") != "false" || cn.Get("X-Request-ID") == "" {
		t.Fatalf("CN missing CLI channel headers: %+v", cn)
	}
	if cn.Get("X-Refresh-Token") != "" || cn.Get("X-API-Key") != "" {
		t.Fatal("chat must not carry refresh token or API key")
	}
	if strings.Contains(cn.Get("Origin"), "workbuddy.ai") {
		t.Fatal("CN chat must not use Global origin")
	}

	global := http.Header{}
	SetChatHeaders(global, Credential{AccessToken: "at", UID: "u2", Domain: "www.workbuddy.ai"})
	if global.Get("Origin") != "https://www.workbuddy.ai" || global.Get("Referer") != "https://www.workbuddy.ai/" {
		t.Fatalf("Global origin/referer mixed: %+v", global)
	}
	if global.Get("X-IDE-Type") != "CLI" || global.Get("X-Product") != "SaaS" {
		t.Fatalf("Global missing CLI channel headers: %+v", global)
	}
	if strings.Contains(global.Get("Origin"), "codebuddy.cn") {
		t.Fatal("Global chat must not use CN origin")
	}
	globalCred := Credential{Domain: "www.workbuddy.ai"}
	if globalCred.ChatBase() != ChatBaseGlobal || globalCred.BillingBase() != ChatBaseGlobal {
		t.Fatal("Global billing host must equal chat host")
	}
	cnCred := Credential{Domain: "www.codebuddy.cn"}
	if cnCred.ChatBase() != ChatBaseCN || cnCred.BillingBase() != BillingBaseCN {
		t.Fatal("CN chat and billing hosts must stay split")
	}

	refresh := http.Header{}
	SetRefreshHeaders(refresh, Credential{RefreshToken: "rt", Domain: "www.codebuddy.cn"})
	if refresh.Get("X-Refresh-Token") != "rt" {
		t.Fatal("refresh must carry X-Refresh-Token")
	}
	if refresh.Get("X-IDE-Type") != "" || refresh.Get("X-Agent-Intent") != "" || refresh.Get("X-Request-ID") != "" {
		t.Fatalf("refresh must not carry CLI channel headers: %+v", refresh)
	}
}

func TestProbeReadyWithCredential(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("probe should not hit network when credential is fresh: %s", r.URL.Path)
	}))
	payload, _ := json.Marshal(Credential{
		AccessToken: "at", RefreshToken: "rt", ExpiresAt: 4102444800, Domain: DomainCN, UID: "u1",
	})
	_ = store.SaveCredentialPayload(context.Background(), "acc1", CredentialFormat, payload)
	health, err := client.Probe(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if !health.Ready || !health.Hot || health.UID != "u1" || health.LastError != "" {
		t.Fatalf("health=%+v", health)
	}
}

func TestUserResourceAggregation(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, pathUserResource) {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer at" || r.Header.Get("X-User-Id") != "u1" {
			t.Fatalf("billing headers missing: %+v", r.Header)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["ProductCode"] != "p_tcaca" {
			t.Fatalf("body=%v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"Response": map[string]any{
					"Data": map[string]any{
						"Accounts": []map[string]any{
							{"CycleCapacitySize": 2000, "CycleCapacityRemain": 1200, "CycleCapacityUsed": 800},
							{"CycleCapacitySize": 500, "CycleCapacityRemain": 300, "CycleCapacityUsed": 200},
						},
					},
				},
			},
		})
	}))
	payload, _ := json.Marshal(Credential{
		AccessToken: "at", RefreshToken: "rt", ExpiresAt: 4102444800, Domain: DomainCN, UID: "u1",
	})
	_ = store.SaveCredentialPayload(context.Background(), "acc1", CredentialFormat, payload)
	info, err := client.Quota(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if info.Remaining != 1500 || info.Total != 2500 || info.Used != 1000 || info.Unit != "credits" {
		t.Fatalf("quota=%+v", info)
	}
}

func TestAggregateUserResourceNegativeClamped(t *testing.T) {
	remain, used, size := aggregateUserResource([]resourcePackage{{
		CycleCapacitySize: 100, CycleCapacityRemain: -50, CycleCapacityUsed: 0,
	}}, 0)
	if remain != 0 || size != 100 || used != 100 {
		t.Fatalf("remain=%d used=%d size=%d", remain, used, size)
	}
}

func TestAdapterWiresProber(t *testing.T) {
	client := NewClient(&memStore{})
	adapter := client.Adapter()
	if !adapter.Supports("prober") || adapter.Prober == nil {
		t.Fatalf("adapter missing prober: %+v", adapter)
	}
}
