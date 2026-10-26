package kimi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func TestMain(m *testing.M) {
	// Package tests drive the adapter against httptest http origins.
	acceptTestEndpoint = true
	os.Exit(m.Run())
}

type fakeStore struct {
	mu       sync.Mutex
	accounts map[string]accounts.Account
	creds    map[string][]byte
	secrets  map[string]string
	settings map[string]accounts.ProviderModelSetting
	observed []string
	saves    int
	// saveErr makes every credential write fail, so a test can prove the
	// caller surfaces a rotated token it could not persist.
	saveErr error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		accounts: map[string]accounts.Account{},
		creds:    map[string][]byte{},
		secrets:  map[string]string{},
		settings: map[string]accounts.ProviderModelSetting{},
	}
}

func (s *fakeStore) addAccount(id, region string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accounts[id] = accounts.Account{ID: id, Provider: ProviderID, ProviderRegion: region, Enabled: true}
}

func (s *fakeStore) setCredential(id string, credential Credential) {
	payload, err := credential.Encode()
	if err != nil {
		panic(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.creds[id] = payload
}

func (s *fakeStore) credential(id string) (Credential, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, ok := s.creds[id]
	if !ok {
		return Credential{}, false
	}
	credential, err := DecodeCredential(payload)
	if err != nil {
		return Credential{}, false
	}
	return credential, true
}

func (s *fakeStore) Get(_ context.Context, id string) (accounts.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.accounts[id]
	if !ok {
		return accounts.Account{}, fmt.Errorf("account %s not found", id)
	}
	return account, nil
}

func (s *fakeStore) LoadCredentialPayload(_ context.Context, accountID string) (string, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, ok := s.creds[accountID]
	if !ok {
		return "", nil, fmt.Errorf("no credential for %s", accountID)
	}
	return CredentialFormat, append([]byte(nil), payload...), nil
}

func (s *fakeStore) SaveCredentialPayload(_ context.Context, accountID, format string, payload []byte) error {
	if format != CredentialFormat {
		return fmt.Errorf("unexpected format %q", format)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	s.creds[accountID] = append([]byte(nil), payload...)
	s.saves++
	return nil
}

func (s *fakeStore) Observe(_ context.Context, id, remoteUID, status, lastError, lastKind string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observed = append(s.observed, strings.Join([]string{id, remoteUID, status, lastError, lastKind}, "|"))
	return nil
}

func (s *fakeStore) GetSecret(_ context.Context, key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.secrets[key]
	return value, ok, nil
}

func (s *fakeStore) GetProviderModelSetting(_ context.Context, provider, model string) (accounts.ProviderModelSetting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings[provider+"|"+model], nil
}

func (s *fakeStore) saveCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saves
}

func mustCredential(t *testing.T, accountID, token string) Credential {
	t.Helper()
	return Credential{
		Format:       CredentialFormat,
		AccessToken:  token,
		RefreshToken: "refresh-" + token,
		ExpiresAt:    time.Now().Add(2 * time.Hour).Unix(),
		DeviceID:     "device-" + accountID,
	}
}

func TestDecodeCredentialAcceptsKimiCodeCLIFile(t *testing.T) {
	payload := []byte(`{"access_token":"at","refresh_token":"rt","expires_at":1792818114,"scope":"openid","token_type":"Bearer","expires_in":3600}`)
	credential, err := DecodeCredential(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if credential.AccessToken != "at" || credential.RefreshToken != "rt" {
		t.Fatalf("tokens = %q/%q", credential.AccessToken, credential.RefreshToken)
	}
	if credential.ExpiresAt != 1792818114 {
		t.Fatalf("expires_at = %d", credential.ExpiresAt)
	}
	if credential.Format != CredentialFormat {
		t.Fatalf("format = %q", credential.Format)
	}
	if !credential.Ready() {
		t.Fatal("credential must be ready")
	}
}

func TestDecodeCredentialAliases(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    int64
	}{
		{"camel", `{"accessToken":"at","refreshToken":"rt","expiresAt":1700000000}`, 1700000000},
		{"rfc3339", `{"access_token":"at","expired":"2026-01-01T00:00:00Z"}`, 1767225600},
		{"string-seconds", `{"access_token":"at","expires_at":"1700000000"}`, 1700000000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			credential, err := DecodeCredential([]byte(tc.payload))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if credential.AccessToken != "at" {
				t.Fatalf("access token = %q", credential.AccessToken)
			}
			if credential.ExpiresAt != tc.want {
				t.Fatalf("expires_at = %d, want %d", credential.ExpiresAt, tc.want)
			}
		})
	}
}

func TestCredentialNeedsRefresh(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cases := []struct {
		name      string
		expiresAt int64
		want      bool
	}{
		{"unknown expiry never refreshes proactively", 0, false},
		{"far expiry", now.Add(2 * time.Hour).Unix(), false},
		{"inside refresh lead", now.Add(2 * time.Minute).Unix(), true},
		{"already expired", now.Add(-time.Minute).Unix(), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			credential := Credential{AccessToken: "at", ExpiresAt: tc.expiresAt}
			if got := credential.NeedsRefresh(now); got != tc.want {
				t.Fatalf("NeedsRefresh = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseCatalogJSONCurrentShape(t *testing.T) {
	raw := []byte(`{"object":"list","data":[
		{"id":"k3","display_name":"K3","limit":{"context":1048576},
		 "reasoning":{"type":"only","effort":{"valid":["low","high","max"],"default":"max"}},
		 "modalities":{"input":["text","image","video"]},
		 "supports_tool_use":true,"tools":{"dynamic_load":true}},
		{"id":"kimi-for-coding","limit":{"context":262144,"output":32000},
		 "reasoning":{"type":"both"},"modalities":{"input":["text"]}},
		{"id":"plain-model","limit":{"context":131072}},
		{"id":"no-tools","limit":{"context":131072},"supports_tool_use":false}
	]}`)
	models, err := ParseCatalogJSON(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(models) != 4 {
		t.Fatalf("models = %d", len(models))
	}
	k3 := models[0]
	if k3.NativeModel != "k3" || k3.DisplayName != "K3" {
		t.Fatalf("k3 = %+v", k3)
	}
	if k3.Capabilities.ContextWindow != 1048576 || k3.Capabilities.ContextWindowMax != 1048576 {
		t.Fatalf("k3 context = %d", k3.Capabilities.ContextWindow)
	}
	if !k3.Capabilities.Reasoning || k3.Capabilities.CanDisableThinking {
		t.Fatalf("k3 reasoning caps = %+v", k3.Capabilities)
	}
	if got := strings.Join(k3.Capabilities.ReasoningOptions, ","); got != "low,high,max" {
		t.Fatalf("k3 options = %q", got)
	}
	if k3.Capabilities.ReasoningDefault != "max" {
		t.Fatalf("k3 default = %q", k3.Capabilities.ReasoningDefault)
	}
	if !k3.Capabilities.Images || !k3.Capabilities.Tools {
		t.Fatalf("k3 image/tools = %+v", k3.Capabilities)
	}

	coding := models[1]
	if coding.Capabilities.MaxOutput != 32000 {
		t.Fatalf("coding max output = %d", coding.Capabilities.MaxOutput)
	}
	if !coding.Capabilities.Reasoning || !coding.Capabilities.CanDisableThinking {
		t.Fatalf("switchable thinking must be flagged: %+v", coding.Capabilities)
	}
	if coding.Capabilities.Images {
		t.Fatal("text-only model must not declare image input")
	}

	plain := models[2]
	if plain.Capabilities.Reasoning || plain.Capabilities.ReasoningOptions != nil {
		t.Fatalf("plain model reasoning = %+v", plain.Capabilities)
	}
	// The managed catalog omits supports_tool_use on every model it returns
	// today (a live CN account completed real tool calls on such a model), and
	// Kimi's own client treats the absent field as supported.
	if !plain.Capabilities.Tools {
		t.Fatal("supports_tool_use absent must default to true")
	}

	// An explicit declaration still wins in both directions.
	noTools := models[3]
	if noTools.Capabilities.Tools {
		t.Fatal("supports_tool_use:false must not be reported as tool-capable")
	}
}

func TestParseCatalogJSONLegacyShape(t *testing.T) {
	raw := []byte(`{"data":[{"id":"legacy","context_length":200000,
		"supports_reasoning":true,"supports_thinking_type":"only",
		"supports_image_in":true,
		"think_efforts":{"support":true,"valid_efforts":["low","high"],"default_effort":"high"}}]}`)
	models, err := ParseCatalogJSON(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	caps := models[0].Capabilities
	if caps.ContextWindow != 200000 {
		t.Fatalf("context = %d", caps.ContextWindow)
	}
	if !caps.Reasoning || caps.CanDisableThinking {
		t.Fatalf("reasoning = %+v", caps)
	}
	if !caps.Images {
		t.Fatal("legacy image flag must be honored")
	}
	if got := strings.Join(caps.ReasoningOptions, ","); got != "low,high" {
		t.Fatalf("options = %q", got)
	}
	if caps.ReasoningDefault != "high" {
		t.Fatalf("default = %q", caps.ReasoningDefault)
	}
}

func TestParseCatalogJSONRejectsEmpty(t *testing.T) {
	if _, err := ParseCatalogJSON([]byte(`{"data":[]}`)); err == nil {
		t.Fatal("empty catalog must fail")
	}
}

func TestParseQuotaJSON(t *testing.T) {
	raw := []byte(`{"goods_version":2,"usages":{
		"limit_5h":{"used_ratio":0.3,"reset_time":"2026-09-11T18:00:00Z"},
		"limit_7d":{"used_ratio":"0.2"},
		"limit_month_total":{"used_ratio":1.0},
		"limit_month_code":{"reset_time":"2026-10-01T00:00:00Z"}}}`)
	quota, err := ParseQuotaJSON(raw, "Vivace", time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if quota == nil {
		t.Fatal("quota must not be nil")
	}
	if len(quota.Windows) != 3 {
		t.Fatalf("windows = %d", len(quota.Windows))
	}
	if quota.Unit != QuotaUnit || quota.ProviderID != ProviderID {
		t.Fatalf("quota meta = %+v", quota)
	}
	if quota.Plan != "Vivace" {
		t.Fatalf("plan = %q", quota.Plan)
	}
	if quota.Percentage != 100 || !quota.Exceeded {
		t.Fatalf("tightest window = %+v", quota)
	}
	if quota.Windows[0].ResetAt != "2026-09-11T18:00:00Z" {
		t.Fatalf("reset at = %q", quota.Windows[0].ResetAt)
	}
	if quota.Windows[1].Used != 20 {
		t.Fatalf("string ratio window = %+v", quota.Windows[1])
	}
}

func TestParseQuotaJSONWithoutWindows(t *testing.T) {
	quota, err := ParseQuotaJSON([]byte(`{"usages":{}}`), "", time.Now())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if quota != nil {
		t.Fatalf("quota = %+v, want nil", quota)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"auth", 401, `{"error":{"message":"invalid token"}}`, accounts.KindAuth},
		{"forbidden", 403, `forbidden`, accounts.KindAuth},
		{"quota-429", 429, `{"error":{"type":"exceeded_current_quota_error"}}`, accounts.KindQuota},
		{"quota-balance", 429, `check your account balance`, accounts.KindQuota},
		{"rate-limit", 429, `{"error":{"message":"too many requests"}}`, accounts.KindRateLimit},
		{"model", 404, `{"error":{"message":"model not found"}}`, accounts.KindModelNotAvailable},
		{"invalid", 400, `{"error":{"message":"bad tool schema"}}`, accounts.KindInvalidRequest},
		{"server", 503, `upstream down`, accounts.KindUnavailable},
		{"canceled", 499, `canceled`, accounts.KindCanceled},
		{
			"plan entitlement",
			401,
			`{"error":{"message":"Your current subscription does not have access to k3. Upgrade to higher-tier Kimi Code plans.","type":"invalid_authentication_error"}}`,
			accounts.KindModelNotAvailable,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			classified := Classify(tc.status, tc.body)
			if classified.Kind != tc.want {
				t.Fatalf("kind = %q, want %q", classified.Kind, tc.want)
			}
		})
	}
}

func TestClassifyRedactsBearer(t *testing.T) {
	classified := Classify(401, `{"error":"bad auth header Bearer secret-token"}`)
	if strings.Contains(classified.Message, "secret-token") {
		t.Fatalf("message leaked the token: %q", classified.Message)
	}
}

func decodeChatBody(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var obj map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&obj); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return obj
}

func reasoningCaps(options ...string) providers.ModelCapabilities {
	return providers.ModelCapabilities{
		Reasoning:          true,
		ReasoningOptions:   options,
		ReasoningType:      "effort",
		CanDisableThinking: false,
	}
}

func TestBuildChatBodyThinkingUsesTopLevelObject(t *testing.T) {
	req := translate.ChatRequest{
		Model:           "k3",
		Messages:        []translate.ChatMessage{{Role: "user", Content: "hi"}},
		ReasoningEffort: json.RawMessage(`"high"`),
	}
	body, err := buildChatBody(req, "k3", reasoningCaps("low", "high", "max"), "high")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	obj := decodeChatBody(t, body)
	if _, ok := obj["reasoning_effort"]; ok {
		t.Fatal("reasoning_effort must not be sent")
	}
	thinking, ok := obj["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking = %#v", obj["thinking"])
	}
	if thinking["type"] != "enabled" || thinking["effort"] != "high" {
		t.Fatalf("thinking = %#v", thinking)
	}
	if obj["model"] != "k3" || obj["stream"] != true {
		t.Fatalf("model/stream = %#v/%#v", obj["model"], obj["stream"])
	}
	options, ok := obj["stream_options"].(map[string]any)
	if !ok || options["include_usage"] != true {
		t.Fatalf("stream_options = %#v", obj["stream_options"])
	}
}

func TestBuildChatBodyThinkingDisabledOnlyForSwitchableModels(t *testing.T) {
	req := translate.ChatRequest{Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}}
	switchable := providers.ModelCapabilities{Reasoning: true, ReasoningOptions: []string{"low", "high"}, CanDisableThinking: true}
	body, err := buildChatBody(req, "kimi-for-coding", switchable, "none")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	thinking, ok := decodeChatBody(t, body)["thinking"].(map[string]any)
	if !ok || thinking["type"] != "disabled" {
		t.Fatalf("thinking = %#v", decodeChatBody(t, body)["thinking"])
	}

	always := reasoningCaps("low", "high")
	body, err = buildChatBody(req, "k3", always, "none")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, ok := decodeChatBody(t, body)["thinking"]; ok {
		t.Fatal("always-thinking model must not receive a disable switch")
	}
}

func TestBuildChatBodyDropsThinkingForNonReasoningModel(t *testing.T) {
	req := translate.ChatRequest{
		Messages:        []translate.ChatMessage{{Role: "user", Content: "hi"}},
		ReasoningEffort: json.RawMessage(`"high"`),
	}
	body, err := buildChatBody(req, "plain", providers.ModelCapabilities{Tools: true}, "high")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	obj := decodeChatBody(t, body)
	if _, ok := obj["thinking"]; ok {
		t.Fatal("non-reasoning model must not receive a thinking object")
	}
	if _, ok := obj["reasoning_effort"]; ok {
		t.Fatal("reasoning_effort must be stripped")
	}
}

func TestBuildChatBodyTokenLimitAndRoles(t *testing.T) {
	req := translate.ChatRequest{
		Messages: []translate.ChatMessage{
			{Role: "developer", Content: "be terse"},
			{Role: "user", Content: "hi"},
		},
		MaxTokens:     json.RawMessage(`1024`),
		IsMaxMode:     boolPointer(true),
		ContextLength: json.RawMessage(`200000`),
	}
	body, err := buildChatBody(req, "k3", providers.ModelCapabilities{}, "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	obj := decodeChatBody(t, body)
	if _, ok := obj["max_tokens"]; ok {
		t.Fatal("max_tokens must not be sent")
	}
	if obj["max_completion_tokens"] != json.Number("1024") {
		t.Fatalf("max_completion_tokens = %#v", obj["max_completion_tokens"])
	}
	for _, key := range []string{"is_max_mode", "context_length"} {
		if _, ok := obj[key]; ok {
			t.Fatalf("%s must be stripped", key)
		}
	}
	messages := obj["messages"].([]any)
	first := messages[0].(map[string]any)
	if first["role"] != "system" {
		t.Fatalf("developer role = %#v", first["role"])
	}
}

func TestBuildChatBodyTemperatureRules(t *testing.T) {
	cases := []struct {
		name     string
		level    string
		caps     providers.ModelCapabilities
		temp     string
		wantKept bool
	}{
		{"thinking-on-1.0", "high", reasoningCaps("low", "high"), `1.0`, true},
		{"thinking-on-0.3", "high", reasoningCaps("low", "high"), `0.3`, false},
		{"thinking-off-0.6", "none", reasoningCaps("low", "high"), `0.6`, true},
		{"thinking-off-1.0", "none", reasoningCaps("low", "high"), `1.0`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := translate.ChatRequest{
				Messages:    []translate.ChatMessage{{Role: "user", Content: "hi"}},
				Temperature: json.RawMessage(tc.temp),
			}
			body, err := buildChatBody(req, "k3", tc.caps, tc.level)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			_, kept := decodeChatBody(t, body)["temperature"]
			if kept != tc.wantKept {
				t.Fatalf("temperature kept = %v, want %v", kept, tc.wantKept)
			}
		})
	}
}

func TestBuildChatBodyTruncatesToolCallIDs(t *testing.T) {
	long := strings.Repeat("a", 80)
	req := translate.ChatRequest{
		Messages: []translate.ChatMessage{
			{Role: "assistant", ToolCalls: json.RawMessage(`[{"id":"` + long + `","type":"function","function":{"name":"Read","arguments":"{}"}}]`)},
			{Role: "tool", ToolCallID: long, Content: "ok"},
		},
	}
	body, err := buildChatBody(req, "k3", providers.ModelCapabilities{}, "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	obj := decodeChatBody(t, body)
	messages := obj["messages"].([]any)
	call := messages[0].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	id, _ := call["id"].(string)
	if len(id) != 64 {
		t.Fatalf("tool call id length = %d", len(id))
	}
	if messages[1].(map[string]any)["tool_call_id"] != id {
		t.Fatalf("tool result id = %#v, want %q", messages[1].(map[string]any)["tool_call_id"], id)
	}
}

func TestAdapterWiring(t *testing.T) {
	client := NewClient(newFakeStore())
	adapter := client.Adapter()
	if adapter.ID != ProviderID {
		t.Fatalf("adapter id = %q", adapter.ID)
	}
	for _, capability := range []string{"credential", "login", "chat", "models", "classifier", "import_export", "prober"} {
		if !adapter.Supports(capability) {
			t.Fatalf("adapter missing capability %q", capability)
		}
	}
	if adapter.Supports("native_responses") {
		t.Fatal("kimi must not claim a native Responses adapter")
	}
}

func boolPointer(value bool) *bool { return &value }

func TestPrepareImportFromCLIFileJSON(t *testing.T) {
	codec := credentialCodec{}
	payload := []byte(`{"access_token":"at","refresh_token":"rt","expires_at":1792818114}`)
	prepared, err := codec.PrepareImport(payload)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !prepared.Ready {
		t.Fatal("import must be ready")
	}
	credential, err := DecodeCredential(prepared.Payload)
	if err != nil {
		t.Fatalf("decode prepared: %v", err)
	}
	if credential.AccessToken != "at" {
		t.Fatalf("token = %q", credential.AccessToken)
	}
	if credential.DeviceID == "" {
		t.Fatal("import must mint a device id")
	}
}

func TestPrepareImportRejectsMissingToken(t *testing.T) {
	codec := credentialCodec{}
	if _, err := codec.PrepareImport([]byte(`{"refresh_token":"rt"}`)); err == nil {
		t.Fatal("missing access token must fail")
	}
}

func TestPrepareImportFromLocalFile(t *testing.T) {
	home := t.TempDir()
	credentialDir := filepath.Join(home, "credentials")
	if err := os.MkdirAll(credentialDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := `{"access_token":"local-at","refresh_token":"local-rt","expires_at":1792818114}`
	if err := os.WriteFile(filepath.Join(credentialDir, "kimi-code.json"), []byte(file), 0o600); err != nil {
		t.Fatalf("write credential: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "device_id"), []byte("device-from-cli\n"), 0o600); err != nil {
		t.Fatalf("write device id: %v", err)
	}
	t.Setenv(kimiCodeHomeEnv, home)

	prepared, err := (credentialCodec{}).PrepareImport([]byte(`{"import_from":"local"}`))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	credential, err := DecodeCredential(prepared.Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if credential.AccessToken != "local-at" {
		t.Fatalf("token = %q", credential.AccessToken)
	}
	if credential.DeviceID != "device-from-cli" {
		t.Fatalf("device id = %q", credential.DeviceID)
	}
}

func TestLoadLocalCredentialMissing(t *testing.T) {
	t.Setenv(kimiCodeHomeEnv, t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if _, err := LoadLocalCredential(); err == nil {
		t.Fatal("missing local credential must fail")
	}
}
