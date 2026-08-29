package trae

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
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
	return accounts.Account{ID: id, Provider: "trae", ProviderRegion: region}, nil
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
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}
	return client, store
}

type rewriteTransport struct {
	server string
	round  http.RoundTripper
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(strings.TrimPrefix(t.server, "http://"), "https://")
	if t.round == nil {
		return http.DefaultTransport.RoundTrip(req)
	}
	return t.round.RoundTrip(req)
}

const soloSSE = "event: metadata\ndata: {\"model\":\"glm-5.2\"}\n\n" +
	"event: output\ndata: {\"reasoning_content\":\"think\"}\n\n" +
	"event: output\ndata: {\"response\":\"O\"}\n\n" +
	"event: output\ndata: {\"response\":\"K\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function_call\":{\"name\":\"get_time\",\"arguments\":\"{}\"}}]}\n\n" +
	"event: token_usage\ndata: {\"prompt_tokens\":3,\"completion_tokens\":2}\n\n" +
	"event: done\ndata: {\"finish_reason\":\"tool_calls\"}\n\n"

func TestDecodeCredentialNestedAndFlat(t *testing.T) {
	nested := []byte(`{"account":{"uid":"u1","enterpriseId":"e1","nickname":"N"},"auth":{"accessToken":"at","refreshToken":"rt","expiresAt":4102444800000,"domain":"trae.cn","machineId":"m1","deviceId":"d1"}}`)
	credential, err := DecodeCredential(nested)
	if err != nil || credential.UID != "u1" || credential.RefreshToken != "rt" || credential.ExpiresAt != 4102444800 {
		t.Fatalf("nested=%+v err=%v", credential, err)
	}
	flat, err := DecodeCredential([]byte(`{"access_token":"at","refresh_token":"rt","uid":"u1"}`))
	if err != nil || !flat.Ready() {
		t.Fatalf("flat=%+v err=%v", flat, err)
	}
}

func TestLoginCallbackStoresDeviceAndToken(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathExchange:
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
				"Token": "at", "RefreshToken": "rt2", "TokenExpireAt": time.Now().Add(time.Hour).Unix(),
			}})
		case pathUserInfo:
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
				"UserID": "u1", "ScreenName": "Tester", "EnterpriseID": "e1",
			}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	session, err := client.StartLogin(context.Background(), "acc1")
	if err != nil || session.AuthURL == "" || !strings.Contains(session.AuthURL, "auth_from=solo") {
		t.Fatalf("session=%+v err=%v", session, err)
	}
	if !strings.Contains(session.AuthURL, "127.0.0.1") {
		t.Fatalf("callback must be loopback: %s", session.AuthURL)
	}
	client.mu.Lock()
	pending := client.pending["acc1"]
	pending.done = true
	pending.credential = Credential{RefreshToken: "rt", MachineID: pending.machineID, DeviceID: pending.deviceID}
	client.mu.Unlock()
	done, _, err := client.PollLogin(context.Background(), "acc1")
	if err != nil || !done {
		t.Fatalf("done=%v err=%v", done, err)
	}
	_, payload, err := store.LoadCredentialPayload(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := DecodeCredential(payload)
	if err != nil || credential.UID != "u1" || credential.AccessToken != "at" || credential.MachineID == "" || credential.DeviceID == "" {
		t.Fatalf("credential=%+v err=%v", credential, err)
	}
}

func TestCompleteLoginAcceptsPastedCallbackURL(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathExchange:
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
				"Token": "at", "RefreshToken": "rt2", "TokenExpireAt": time.Now().Add(time.Hour).Unix(),
			}})
		case pathUserInfo:
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
				"UserID": "u1", "ScreenName": "Tester", "EnterpriseID": "e1",
			}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	if _, err := client.StartLogin(context.Background(), "acc1"); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	machine, device := client.pending["acc1"].machineID, client.pending["acc1"].deviceID
	client.mu.Unlock()
	err := client.CompleteLogin(context.Background(), "acc1",
		`http://127.0.0.1:9/authorize?refreshToken=rt&userInfo={"UserID":"u1","ScreenName":"N"}`)
	if err != nil {
		t.Fatal(err)
	}
	_, payload, err := store.LoadCredentialPayload(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := DecodeCredential(payload)
	if err != nil || credential.UID != "u1" || credential.AccessToken != "at" || credential.MachineID != machine || credential.DeviceID != device {
		t.Fatalf("credential=%+v err=%v want machine=%s device=%s", credential, err, machine, device)
	}
}

func TestCompleteLoginIsIdempotentWhenAlreadyReady(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", ExpiresAt: 4102444800, MachineID: "m1", DeviceID: "d1"}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	client := NewClient(store)
	client.http = (&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("already-ready complete login must not hit upstream")
		return nil, nil
	})})
	if err := client.CompleteLogin(context.Background(), "acc1", `http://127.0.0.1:9/authorize?refreshToken=rt`); err != nil {
		t.Fatal(err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestChatNonStreamAggregatesToolsAndReasoning(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", Domain: DomainCN, ExpiresAt: 4102444800, MachineID: "m1", DeviceID: "d1"}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathChat {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Cloud-IDE-JWT at" || r.Header.Get("X-Uid") != "u1" ||
			r.Header.Get("X-Ide-Version") != IdeVersion || r.Header.Get("X-Machine-Id") != "m1" {
			t.Fatalf("headers missing: %+v", r.Header)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] != true || body["function"] != Function || body["config_name"] != "glm-5.2" {
			t.Fatalf("body=%v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(soloSSE))
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
	if out.Content != "OK" || out.Reasoning != "think" || out.FinishReason != "tool_calls" || out.PromptTokens != 3 {
		t.Fatalf("outcome=%+v", out)
	}
	if !strings.Contains(string(out.ToolCalls), "get_time") {
		t.Fatalf("tool calls=%s", out.ToolCalls)
	}
}

func TestChatStreamRewritesOpenAIChunks(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(soloSSE))
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}
	resp, err := client.ChatStream(context.Background(), "acc1", translate.ChatRequest{Model: "glm-5.2"})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if !strings.Contains(text, `"object":"chat.completion.chunk"`) || !strings.Contains(text, "data: [DONE]") {
		t.Fatalf("stream=%s", text)
	}
	if strings.Contains(text, "event: output") {
		t.Fatalf("must rewrite solo events, got %s", text)
	}
}

func TestChatStreamFirstErrorFailsBeforeOpenAIChunks(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: metadata\ndata: {\"model\":\"Doubao-Seed-Evolving\"}\n\nevent: error\ndata: {\"code\":1005,\"message\":\"\"}\n\n"))
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}
	resp, err := client.ChatStream(context.Background(), "acc1", translate.ChatRequest{Model: "Doubao-Seed-Evolving"})
	if resp != nil {
		resp.Body.Close()
	}
	var classified *providers.Error
	if err == nil || !errors.As(err, &classified) || classified.Kind != accounts.KindQuota {
		t.Fatalf("err=%v classified=%+v", err, classified)
	}
}

func TestModelsFromConfigInfoList(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathModels {
			t.Fatalf("path=%s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"config_info_list": []map[string]any{
				{"config_name": "glm-5.2", "display_config": map[string]any{"display_name": "GLM 5.2"}},
				{"config_name": "glm-5.3", "display_config": map[string]any{"display_name": "GLM 5.3"}},
			},
		})
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}
	models, err := client.Models(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].NativeModel != "glm-5.2" || models[1].DisplayName != "GLM 5.3" {
		t.Fatalf("models=%+v", models)
	}
}

func TestChatRequestMapsOpenAIReasoningAndMaxMode(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", Domain: DomainCN, ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pathModels {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"config_info_list": []map[string]any{{
					"config_name":           "DeepSeek-V4-Pro-Official",
					"context_window_tokens": map[string]any{"dev": 200000, "max": 1000000},
					"display_config":        map[string]any{"display_name": "DeepSeek-V4-Pro 正式版"},
					"reasoning_effort_config": map[string]any{
						"support_thinking": true,
						"options":          []string{"low", "medium", "high", "xhigh"},
						"default_level":    "medium",
					},
				}},
			})
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(soloSSE))
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}
	if _, err := client.Models(context.Background(), "acc1"); err != nil {
		t.Fatal(err)
	}
	max := true
	_, err := client.ChatNonStream(context.Background(), "acc1", translate.ChatRequest{
		Model:           "DeepSeek-V4-Pro-Official",
		IsMaxMode:       &max,
		ReasoningEffort: json.RawMessage(`"extra_high"`),
		ContextLength:   json.RawMessage(`500000`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["is_max_mode"] != float64(1) || got["reasoning_effort_level"] != "xhigh" {
		t.Fatalf("body=%v", got)
	}
	if _, ok := got["context_length"]; ok {
		t.Fatalf("qoder context leaked: %v", got)
	}
}

func TestModelsEmptyIsError(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"config_info_list": []any{}})
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}
	if _, err := client.Models(context.Background(), "acc1"); err == nil {
		t.Fatal("expected empty catalog error")
	}
}

func TestErrorMappingAndCooldown(t *testing.T) {
	cases := []struct {
		status   int
		body     string
		kind     string
		cooldown time.Duration
	}{
		{401, `{"code":1001}`, "auth", 30 * time.Minute},
		{200, `{"code":1005,"message":"plan"}`, "quota", quotaCooldownPlan},
		{200, `{"code":4008}`, "quota", quotaCooldownSolo},
		{200, `{"code":4001}`, "invalid_request", 0},
		{200, `{"code":4011}`, "rate_limit", hardRateCooldown},
		{429, "too many requests", "rate_limit", time.Minute},
		{500, "boom", "unavailable", 0},
	}
	for _, c := range cases {
		got := Classify(c.status, c.body)
		if got.Kind != c.kind {
			t.Fatalf("Classify(%d,%s)=%+v want %s", c.status, c.body, got, c.kind)
		}
		if cooldown := classifiedCooldown(got.Kind, extractCode(c.body)); cooldown != c.cooldown {
			t.Fatalf("cooldown(%s)=%s want %s", c.body, cooldown, c.cooldown)
		}
		err := wrapClassified(got, extractCode(c.body))
		var classified *providers.Error
		if !errors.As(err, &classified) {
			t.Fatalf("wrapClassified type %T", err)
		}
		wantFailover := c.kind != "invalid_request"
		if classified.Failover == nil || *classified.Failover != wantFailover {
			t.Fatalf("failover(%s)=%v want %v", c.body, classified.Failover, wantFailover)
		}
	}
}

func TestPrepareBodyForcesSoloShape(t *testing.T) {
	out := PrepareBody([]byte(`{"model":"glm-5.3","stream":false,"messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"t","parameters":{"type":"object"}}}],"tool_choice":{"type":"function","function":{"name":"t"}}}`))
	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatal(err)
	}
	if body["stream"] != true || body["function"] != Function || body["config_name"] != "glm-5.3" {
		t.Fatalf("body=%v", body)
	}
	messages, _ := body["messages"].([]any)
	first, _ := messages[0].(map[string]any)
	content, _ := first["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content=%v", first["content"])
	}
	if body["tool_choice"] != "t" {
		t.Fatalf("tool_choice=%v", body["tool_choice"])
	}
	tools, _ := body["tools"].([]any)
	tool, _ := tools[0].(map[string]any)
	fn, _ := tool["function"].(map[string]any)
	if _, ok := fn["parameters"].(string); !ok {
		t.Fatalf("parameters should be JSON string: %v", fn["parameters"])
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
	if !health.Ready || !health.Hot || health.UID != "u1" {
		t.Fatalf("health=%+v", health)
	}
}

func TestQuotaUsesEntitlementPackUnit(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathEntUsage {
			t.Fatalf("path=%s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"user_entitlement_pack_list": []map[string]any{
				{"entitlement_base_info": map[string]any{"quota": map[string]any{"credits_limit": 2000}}, "usage": map[string]any{"credits_amount": 500}},
			},
		})
	}))
	payload, _ := json.Marshal(Credential{AccessToken: "at", RefreshToken: "rt", ExpiresAt: 4102444800, UID: "u1"})
	_ = store.SaveCredentialPayload(context.Background(), "acc1", CredentialFormat, payload)
	info, err := client.Quota(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if info.Remaining != 1500 || info.Total != 2000 || info.Used != 500 || info.Unit != QuotaUnit {
		t.Fatalf("quota=%+v", info)
	}
}

func TestParseEntitlementUsageUnwrapsResultEnvelope(t *testing.T) {
	remain, used, total := parseEntitlementUsage([]byte(`{"Result":{"user_entitlement_pack_list":[{"entitlement_base_info":{"quota":{"credits_limit":100}},"usage":{"credits_amount":25}}]}}`))
	if remain != 75 || used != 25 || total != 100 {
		t.Fatalf("remain=%d used=%d total=%d", remain, used, total)
	}
}

func TestAdapterWiresCapabilities(t *testing.T) {
	adapter := NewClient(&memStore{}).Adapter()
	for _, cap := range []string{"credential", "login", "chat", "models", "classifier", "prober", "import_export"} {
		if !adapter.Supports(cap) {
			t.Fatalf("missing %s", cap)
		}
	}
}

func TestBuildLoginURLIsLoopbackSolo(t *testing.T) {
	u := BuildLoginURL("m", "d", "http://127.0.0.1:9/authorize", "trace")
	if strings.Contains(u, "0.0.0.0") || !strings.Contains(u, "127.0.0.1") || !strings.Contains(u, "auth_from=solo") {
		t.Fatalf("url=%s", u)
	}
}

func TestParseCallbackReadsRefreshAndUserInfo(t *testing.T) {
	info, err := ParseCallback(`http://127.0.0.1:18080/authorize?refreshToken=rt&userInfo={"UserID":"u1","ScreenName":"N"}`)
	if err != nil || info.RefreshToken != "rt" || info.UID != "u1" {
		t.Fatalf("info=%+v err=%v", info, err)
	}
}
