package codearts

import (
	"context"
	"encoding/json"
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

func writeSSE(t *testing.T, w io.Writer, payload any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", raw); err != nil {
		t.Fatal(err)
	}
}

type memoryStore struct {
	mu      sync.Mutex
	payload []byte
	account accounts.Account
	saved   []byte
}

func (s *memoryStore) Get(context.Context, string) (accounts.Account, error) {
	return s.account, nil
}

func (s *memoryStore) LoadCredentialPayload(context.Context, string) (string, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return CredentialFormat, append([]byte(nil), s.payload...), nil
}

func (s *memoryStore) SaveCredentialPayload(_ context.Context, _ string, _ string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.payload = append([]byte(nil), payload...)
	s.saved = append([]byte(nil), payload...)
	return nil
}

func (s *memoryStore) Observe(context.Context, string, string, string, string, string) error {
	return nil
}

func testCredential() Credential {
	return Credential{
		Format:          CredentialFormat,
		AccessKeyID:     "AK",
		SecretAccessKey: "SK",
		SecurityToken:   "STS",
		ExpiresAt:       time.Now().Add(24 * time.Hour).Format(time.RFC3339Nano),
		UserID:          "user-1",
		UserName:        "tester",
		DomainID:        "domain-1",
	}
}

func newTestClient(t *testing.T, server *httptest.Server) (*Client, *memoryStore) {
	t.Helper()
	credential := testCredential()
	payload, err := credential.Encode()
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{
		payload: payload,
		account: accounts.Account{ID: "acct-1", Provider: ProviderID, ProviderRegion: "cn-north-4", ProxyURL: ""},
	}
	client := NewClient(store)
	client.baseURL = server.URL
	client.benefitBase = server.URL
	client.stsHost = server.URL
	client.portalHost = server.URL
	client.nowFn = func() time.Time { return time.Now() }
	return client, store
}

func TestValidateCredential(t *testing.T) {
	valid, _ := testCredential().Encode()
	if err := ValidateCredential(valid); err != nil {
		t.Fatalf("valid credential rejected: %v", err)
	}
	if err := ValidateCredential([]byte(`{"access_key_id":"AK"}`)); err == nil {
		t.Fatal("credential without secrets accepted")
	}
	if err := ValidateCredential([]byte(`{"refresh_token":"r","code_verifier":"v"}`)); err != nil {
		t.Fatalf("refresh-only credential rejected: %v", err)
	}
}

func TestChatNonStreamAndToolMapping(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case chatPath:
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &gotBody)
			if r.Header.Get("x-auth-token") != "STS" || r.Header.Get("Authorization") == "" {
				t.Errorf("missing auth headers: %+v", r.Header)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			writeSSE(t, w, map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant"}}}})
			writeSSE(t, w, map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"reasoning_content": "think"}}}})
			writeSSE(t, w, map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "hello"}}}})
			writeSSE(t, w, map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": "call_1", "type": "function",
				"function": map[string]any{"name": "lookup", "arguments": `{"q":"x"}`},
			}}}}}})
			writeSSE(t, w, map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}}})
			writeSSE(t, w, map[string]any{"choices": []any{}, "usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8}})
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, _ := newTestClient(t, server)

	tools := json.RawMessage(`[{"type":"function","function":{"name":"lookup","description":"look up","parameters":{"type":"object","properties":{"q":{"type":"string"}}}}}]`)
	req := translate.ChatRequest{
		Model:      "GLM-5.2",
		Messages:   []translate.ChatMessage{{Role: "user", Content: "hi"}},
		Tools:      tools,
		ToolChoice: json.RawMessage(`{"type":"function","function":{"name":"lookup"}}`),
	}
	outcome, err := client.ChatNonStream(context.Background(), "acct-1", req)
	if err != nil {
		t.Fatalf("ChatNonStream: %v", err)
	}
	if outcome.Content != "hello" || outcome.Reasoning != "think" {
		t.Fatalf("content/reasoning = %q/%q", outcome.Content, outcome.Reasoning)
	}
	if outcome.FinishReason != "tool_calls" || len(outcome.ToolCalls) == 0 {
		t.Fatalf("tool outcome = %+v", outcome)
	}
	if outcome.PromptTokens != 5 || outcome.CompletionTokens != 3 {
		t.Fatalf("usage = %d/%d", outcome.PromptTokens, outcome.CompletionTokens)
	}
	if gotBody["tool_choice"] != "required" {
		t.Fatalf("tool_choice = %v", gotBody["tool_choice"])
	}
	if gotBody["model"] != "GLM-5.2" {
		t.Fatalf("model = %v", gotBody["model"])
	}
}

func TestChatStreamEmitsDeltasAndDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != chatPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, w, map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "one"}}}})
		writeSSE(t, w, map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": " two"}, "finish_reason": "stop"}}})
		writeSSE(t, w, map[string]any{"choices": []any{}, "usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 2}})
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	client, _ := newTestClient(t, server)
	resp, resolved, err := client.ChatStream(context.Background(), "acct-1", translate.ChatRequest{
		Model:    "GLM-5.2",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	body := string(raw)
	if !strings.Contains(body, `"content":"one"`) || !strings.Contains(body, `"content":" two"`) {
		t.Fatalf("stream body missing content: %s", body)
	}
	if !strings.Contains(body, `"usage":{`) || !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("stream body missing usage/done: %s", body)
	}
	if resolved.ReasoningLevel != "" {
		t.Fatalf("unexpected reasoning level %q", resolved.ReasoningLevel)
	}
}

func TestModelsAndQuota(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/model/builtin":
			_, _ = io.WriteString(w, `{"builtinModels":[{"model_id":"GLM-5.2","model_name":"GLM-5.2","context_window":200000,"max_tokens":131072}]}`)
		case "/v1/agent-center/agents/useragents":
			http.NotFound(w, r)
		case "/api/v1/benefit/claim":
			_, _ = io.WriteString(w, `{"error_code":"0000"}`)
		case "/api/v1/gateway/config":
			_, _ = io.WriteString(w, `{"error_code":"0000","result":{"models":[{"model_id":"glm-5.3-flash","context_window":1048576,"max_tokens":393216}]}}`)
		case statisticsPath:
			_, _ = io.WriteString(w, `{"end_date":"2026-12-31","package":{"spec_code":"free","package_name_en":"Free"},"metrics":[{"name":"usageTotalPackageCredit","show":true,"package_credit_amount":500,"package_credit_used":100,"package_credit_remain":400,"value":20},{"name":"usageTokenChatMessages","show":true,"usage_token_num":1000,"package_token_amount":10000,"value":10}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, _ := newTestClient(t, server)

	models, err := client.Models(context.Background(), "acct-1")
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %+v", models)
	}
	found := false
	for _, model := range models {
		if model.NativeModel == "GLM-5.2" {
			found = true
			if model.Capabilities.ContextWindow != 200000 || model.Capabilities.MaxOutput != 65536 {
				t.Fatalf("capabilities = %+v", model.Capabilities)
			}
		}
	}
	if !found {
		t.Fatal("GLM-5.2 missing")
	}
	if !client.isBenefitModel("acct-1", "glm-5.3-flash") {
		t.Fatal("benefit model not marked")
	}
	quota, err := client.Quota(context.Background(), "acct-1")
	if err != nil {
		t.Fatalf("Quota: %v", err)
	}
	if quota.Total != 500 || quota.Remaining != 400 || quota.Unit != "积分" {
		t.Fatalf("quota = %+v", quota)
	}
	if len(quota.Windows) != 2 {
		t.Fatalf("windows = %+v", quota.Windows)
	}
}

func TestLoginCallbackAndRefresh(t *testing.T) {
	var refreshed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case oauthTokensPath:
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse token form: %v", err)
			}
			if r.Form.Get("grant_type") == "refresh_token" {
				refreshed = true
			}
			if r.Header.Get("DPoP") == "" {
				t.Error("token request missing DPoP proof")
			}
			_, _ = io.WriteString(w, `{"user_id":"user-2","user_name":"login-user","domain_id":"domain-2","refresh_token":"refresh-2","credentials":{"access_key_id":"AK2","secret_access_key":"SK2","security_token":"STS2","expiration":"`+time.Now().Add(24*time.Hour).Format(time.RFC3339Nano)+`"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, store := newTestClient(t, server)
	client.stsHost = server.URL
	client.portalHost = server.URL

	session, err := client.StartLogin(context.Background(), "acct-1")
	if err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	parsed, err := url.Parse(session.AuthURL)
	if err != nil {
		t.Fatal(err)
	}
	port := parsed.Query().Get("port")
	if port == "" {
		t.Fatalf("auth URL has no port: %s", session.AuthURL)
	}
	response, err := http.Get("http://127.0.0.1:" + port + OAuthRedirect + "?code=test-code")
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	_ = response.Body.Close()
	done, message, err := client.PollLogin(context.Background(), "acct-1")
	if err != nil || !done {
		t.Fatalf("PollLogin = %v, %q, %v", done, message, err)
	}
	credential, err := DecodeCredential(store.payload)
	if err != nil {
		t.Fatal(err)
	}
	if credential.UserID != "user-2" || credential.SecurityToken != "STS2" || !credential.CanRefresh() {
		t.Fatalf("saved credential = %+v", credential)
	}

	expired := credential
	expired.ExpiresAt = time.Now().Add(-time.Hour).Format(time.RFC3339Nano)
	payload, _ := expired.Encode()
	store.payload = payload
	rotated, err := client.credential(context.Background(), "acct-1")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if !refreshed || rotated.SecurityToken != "STS2" || rotated.RefreshToken != "refresh-2" {
		t.Fatalf("rotated = %+v refreshed=%v", rotated, refreshed)
	}
}

func TestProbeAndCheckin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case currentUserPath:
			_, _ = io.WriteString(w, `{"user_id":"probe-user","user_name":"probe","domain_id":"probe-domain"}`)
		case creditDeliveryPath:
			_, _ = io.WriteString(w, `{"code":0,"data":{"items":[{"campaignId":1,"title":"Daily","status":"ELIGIBLE","claimable":true,"benefitUnit":"CREDIT","benefitAmount":100,"pendingTotalAmount":100,"extra":{"startTime":"2026-01-01T00:00:00Z","endTime":"2027-01-01T00:00:00Z"}}]}}`)
		case creditClaimPath, creditConfirmPath:
			_, _ = io.WriteString(w, `{"code":0,"data":{}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, _ := newTestClient(t, server)

	health, err := client.Probe(context.Background(), "acct-1")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if !health.Ready || health.UID != "probe-user" {
		t.Fatalf("health = %+v", health)
	}
	result, err := client.Checkin(context.Background(), "acct-1")
	if err != nil {
		t.Fatalf("Checkin: %v", err)
	}
	if result.Status != "success" || result.RewardCredits != 100 {
		t.Fatalf("checkin = %+v", result)
	}
}

func TestImporterAndClassifier(t *testing.T) {
	credential, _ := testCredential().Encode()
	imported, err := PrepareImport(credential)
	if err != nil || !imported.Ready {
		t.Fatalf("PrepareImport = %+v, %v", imported, err)
	}
	if _, err := PrepareImport([]byte(`{"access_key_id":"AK"}`)); err == nil {
		t.Fatal("incomplete import accepted")
	}
	if got := Classify(401, `{"error_msg":"x-auth-token not found"}`); got.Kind != accounts.KindAuth {
		t.Fatalf("401 kind = %q", got.Kind)
	}
	if got := Classify(400, `InferHub.002002009.404 model is not registered`); got.Kind != accounts.KindModelNotAvailable {
		t.Fatalf("model kind = %q", got.Kind)
	}
	if got := Classify(429, `InferHub.4001.429 TPM limit`); got.Kind != accounts.KindRateLimit {
		t.Fatalf("rate kind = %q", got.Kind)
	}
	if got := Classify(400, `InferHub.001001005 request param is invalid`); got.Kind != accounts.KindInvalidRequest {
		t.Fatalf("param kind = %q", got.Kind)
	}
}

func TestQuotaPlaceholderTokenMeterIsNotExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != statisticsPath {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"end_date":"2026-11-30","package":{"package_name_en":"Trial"},"metrics":[{"name":"usageTokenChatMessages","show":true,"usage_token_num":193,"package_token_amount":1,"value":0},{"name":"usageTotalPackageCredit","show":true,"package_credit_amount":4500,"package_credit_used":0.27,"package_credit_remain":4499.73,"value":0}]}`)
	}))
	defer server.Close()
	client, _ := newTestClient(t, server)
	quota, err := client.Quota(context.Background(), "acct-1")
	if err != nil {
		t.Fatalf("Quota: %v", err)
	}
	if quota.Total != 4500 || quota.Remaining != 4499.73 {
		t.Fatalf("top-level quota = %+v", quota)
	}
	for _, window := range quota.Windows {
		if window.ID == "usageTokenChatMessages" {
			if window.Total != 0 || window.Exceeded {
				t.Fatalf("placeholder token meter = %+v", window)
			}
		}
	}
}

func TestModelsFiltersLearnedUnavailable(t *testing.T) {
	credential, _ := testCredential().Encode()
	client := NewClient(&memoryStore{payload: credential})
	client.rememberCatalog("acct-1", []providers.ModelInfo{
		{NativeModel: "good", PublicModel: "good"},
		{NativeModel: "bad", PublicModel: "bad"},
	}, nil)
	client.markModelUnusable("acct-1", "bad")
	models, err := client.Models(context.Background(), "acct-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].NativeModel != "good" {
		t.Fatalf("filtered models = %+v", models)
	}
}

func TestBenefitModelsHiddenWhenBenefitUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case agentListPath:
			_, _ = io.WriteString(w, `{"agents":[{"agent_id":"agent-1","agent_name":"CodeAgent","is_primary_agent":true,"show_in_ide":true,"alias":{"alias_zh_cn":"智能体"}}]}`)
		case agentDetailPath:
			_, _ = io.WriteString(w, `{"gpts":{"models":[{"model_name":"Normal","model_parameters":{"context_window":1000,"max_tokens":100}},{"model_name":"Trial","model_parameters":{"context_window":1000,"max_tokens":100,"model_desc":"限时体验"}}]}}`)
		case modelBuiltinPath:
			_, _ = io.WriteString(w, `{"builtinModels":[]}`)
		case benefitClaim:
			_, _ = io.WriteString(w, `{"error_code":"4000","error_msg":"invalid channel, group not found"}`)
		case benefitConfig:
			_, _ = io.WriteString(w, `{"error_code":"4000","error_msg":"invalid channel"}`)
		case statisticsPath:
			_, _ = io.WriteString(w, `{"metrics":[],"model_metrics":[{"model_id":"Trial","model_name":"Trial","model_desc":"限时体验","enable":true}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, _ := newTestClient(t, server)
	models, err := client.Models(context.Background(), "acct-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].NativeModel != "Normal" {
		t.Fatalf("models = %+v", models)
	}
}

func TestAdapterSurface(t *testing.T) {
	credential, _ := testCredential().Encode()
	store := &memoryStore{payload: credential}
	client := NewClient(store)
	adapter := client.Adapter()
	for _, capability := range []string{"credential", "login", "chat", "models", "classifier", "import_export", "prober", "checkin"} {
		if !adapter.Supports(capability) {
			t.Fatalf("adapter missing %s", capability)
		}
	}
	var _ providers.CredentialImporter = importer{}
	var _ providers.ModelCatalogProvider = client
	var _ providers.ProviderChat = client
	var _ providers.AccountProber = client
}
