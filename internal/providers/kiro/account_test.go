package kiro

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

func TestKiroRejectsNonAnthropicProtocol(t *testing.T) {
	client := &Client{}
	for _, protocol := range []string{"", "openai_chat", "openai_responses"} {
		_, _, err := client.ChatStream(context.Background(), "", translate.ChatRequest{Protocol: protocol, Model: "claude-sonnet-4.5"})
		if err == nil || !strings.Contains(err.Error(), "only the Anthropic Messages API") {
			t.Fatalf("protocol %q err = %v", protocol, err)
		}
	}
}

func TestQuotaReadsCreditBreakdown(t *testing.T) {
	var gotProfile string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/getUsageLimits") {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer access-1" {
			t.Fatalf("authorization = %s", r.Header.Get("Authorization"))
		}
		gotProfile = r.URL.Query().Get("profileArn")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"nextDateReset":    1_800_000_000,
			"subscriptionInfo": map[string]any{"subscriptionTitle": "Kiro Pro"},
			"userInfo":         map[string]any{"email": "dev@example.com", "userId": "user-1"},
			"usageBreakdownList": []map[string]any{{
				"resourceType": "CREDIT", "currentUsageWithPrecision": 25.5, "usageLimitWithPrecision": 100,
				"freeTrialInfo": map[string]any{"currentUsage": 2, "usageLimit": 10, "freeTrialStatus": "ACTIVE"},
				"bonuses":       []map[string]any{{"displayName": "bonus", "currentUsage": 1, "usageLimit": 5, "status": "ACTIVE"}},
			}},
		})
	}))
	defer server.Close()
	client := &Client{store: &memStore{payload: mustCred(t, Credential{
		AccessToken: "access-1", AuthMethod: "builder_id", Region: "us-east-1",
	})}, http: server.Client()}
	defer setTestHosts(server.URL)()

	quota, err := client.Quota(context.Background(), "acct")
	if err != nil {
		t.Fatal(err)
	}
	if quota.Used != 25.5 || quota.Total != 100 || quota.Remaining != 74.5 || quota.Plan != "PRO" || quota.Unit != "credits" {
		t.Fatalf("quota = %+v", quota)
	}
	if len(quota.Windows) != 2 || quota.Windows[1].ID != "trial" || quota.Windows[1].Remaining != 8 {
		t.Fatalf("windows = %+v", quota.Windows)
	}
	if len(quota.Packages) != 1 || quota.Packages[0].Remain != 4 {
		t.Fatalf("packages = %+v", quota.Packages)
	}
	if quota.Exceeded {
		t.Fatal("plan credits remain, so the account is not exceeded")
	}
	if gotProfile != builderIDProfileArn {
		t.Fatalf("profile = %s", gotProfile)
	}
}

func TestListModelsFollowsEscapedNextToken(t *testing.T) {
	var pages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages = append(pages, r.URL.RawQuery)
		if r.URL.Query().Get("nextToken") == "" {
			if r.URL.Query().Get("maxResults") != "50" || r.URL.Query().Get("origin") != "AI_EDITOR" {
				t.Fatalf("query = %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models":    []map[string]any{{"modelId": "claude-sonnet-4.5"}},
				"nextToken": "page+2/more",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{{"modelId": "claude-opus-4.8"}, {"modelId": "auto"}},
		})
	}))
	defer server.Close()
	client := &Client{http: server.Client()}
	defer setTestHosts(server.URL)()
	models, err := client.listModels(context.Background(), server.Client(), Credential{AuthMethod: "social"})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 3 || models[2].NativeModel != "auto" {
		t.Fatalf("models = %+v", models)
	}
	if len(pages) != 2 || !strings.Contains(pages[1], "nextToken=page%2B2%2Fmore") {
		t.Fatalf("pages = %#v", pages)
	}
}

func TestListModelsCollectsMoreThanTwentyModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("nextToken") == "" {
			models := make([]map[string]any, 0, 20)
			for i := 0; i < 20; i++ {
				models = append(models, map[string]any{"modelId": fmt.Sprintf("model-%02d", i)})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"models": models, "nextToken": "page-2"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{{"modelId": "model-20"}, {"modelId": "model-21"}, {"modelId": "model-20"}},
		})
	}))
	defer server.Close()
	defer setTestHosts(server.URL)()
	client := &Client{http: server.Client()}
	models, err := client.listModels(context.Background(), server.Client(), Credential{AuthMethod: "social"})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 22 || models[21].NativeModel != "model-21" {
		t.Fatalf("models = %+v", models)
	}
}

func TestListModelsRejectsRepeatedNextToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models":    []map[string]any{{"modelId": "model"}},
			"nextToken": "same-token",
		})
	}))
	defer server.Close()
	defer setTestHosts(server.URL)()
	client := &Client{http: server.Client()}
	if _, err := client.listModels(context.Background(), server.Client(), Credential{AuthMethod: "social"}); err == nil || !strings.Contains(err.Error(), "repeated next token") {
		t.Fatalf("err = %v", err)
	}
}

func TestWithAdvertisedModelsAddsDocumentedKiroIDs(t *testing.T) {
	models := withAdvertisedModels([]providers.ModelInfo{{NativeModel: "auto", PublicModel: "auto"}})
	byID := make(map[string]providers.ModelInfo, len(models))
	for _, model := range models {
		byID[model.PublicModel] = model
	}
	for _, id := range []string{"claude-sonnet-4.6", "gpt-5.6-sol", "gpt-5.6-terra"} {
		model, ok := byID[id]
		if !ok {
			t.Fatalf("missing documented model %q in %+v", id, models)
		}
		if !model.Capabilities.Reasoning || len(model.Capabilities.ReasoningOptions) == 0 {
			t.Fatalf("capabilities for %s = %+v", id, model.Capabilities)
		}
	}
	if len(models) != 4 {
		t.Fatalf("models = %+v", models)
	}
}

func TestEnsureProfileArnDiscoversPagesAndPersistsProfile(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/ListAvailableProfiles" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var requestBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		if requestBody["maxResults"] != float64(10) {
			t.Fatalf("maxResults = %#v", requestBody["maxResults"])
		}
		calls++
		if calls == 1 {
			_, _ = io.WriteString(w, `{"profiles":[{"arn":"arn:aws:codewhisperer:us-east-1:123456789012:profile/one"}],"nextToken":"next"}`)
			return
		}
		_, _ = io.WriteString(w, `{"profiles":[{"arn":"arn:aws:codewhisperer:us-east-1:123456789012:profile/two"}]}`)
	}))
	defer server.Close()
	store := &memStore{payload: mustCred(t, Credential{AuthMethod: "social", AccessToken: "access", RefreshToken: "refresh"})}
	client := &Client{http: server.Client(), store: store}
	defer setTestHosts(server.URL)()
	cred, err := DecodeCredential(store.payload)
	if err != nil {
		t.Fatal(err)
	}
	cred, err = client.ensureProfileArn(context.Background(), "acct", server.Client(), cred)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if cred.ProfileArn != "arn:aws:codewhisperer:us-east-1:123456789012:profile/one" {
		t.Fatalf("profile = %q", cred.ProfileArn)
	}
	stored, err := DecodeCredential(store.payload)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ProfileArn != cred.ProfileArn {
		t.Fatalf("stored profile = %q", stored.ProfileArn)
	}
}

func TestExternalHeadersUseKiroGoTokenTypeCasing(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("tokentype", "stale")
	applyRuntimeHeaders(req, Credential{AuthMethod: "external_idp", AccessToken: "access"}, "example.com")
	if req.Header.Get("TokenType") != "EXTERNAL_IDP" {
		t.Fatalf("TokenType = %q headers = %#v", req.Header.Get("TokenType"), req.Header)
	}
	if len(req.Header.Values("TokenType")) != 1 {
		t.Fatalf("duplicate token type headers = %#v", req.Header)
	}
}

func TestListModelsReturnsOnlyUpstreamCatalogEntries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{{"modelId": "claude-sonnet-4.5", "modelName": "Sonnet 4.5"}},
		})
	}))
	defer server.Close()
	defer setTestHosts(server.URL)()
	client := &Client{http: server.Client()}
	models, err := client.listModels(context.Background(), server.Client(), Credential{AccessToken: "access", AuthMethod: "builder_id"})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].PublicModel != "claude-sonnet-4.5" {
		t.Fatalf("models = %+v", models)
	}
}

func TestQuotaOverageKeepsAccountSelectable(t *testing.T) {
	usage := usageLimits{
		OverageConfiguration: &struct {
			OverageStatus string `json:"overageStatus"`
		}{OverageStatus: "ENABLED"},
		UsageBreakdownList: []usageBreakdown{{
			ResourceType: "CREDIT", CurrentUsage: 100, UsageLimit: 100,
			OverageCap: 50, CurrentOverages: 12, Unit: "credits",
		}},
	}
	quota := quotaFromUsage(usage, time.Now().UTC())
	if quota.Exceeded || quota.Remaining != 38 || quota.Total != 50 {
		t.Fatalf("overage headroom lost: %+v", quota)
	}
	var overage *providers.QuotaWindow
	for i := range quota.Windows {
		if quota.Windows[i].ID == "overage" {
			overage = &quota.Windows[i]
		}
	}
	if overage == nil || overage.Used != 12 || overage.Total != 50 || overage.Exceeded {
		t.Fatalf("overage window = %+v", quota.Windows)
	}
}

func TestQuotaOverageExhaustedBlocksAccount(t *testing.T) {
	usage := usageLimits{
		OverageConfiguration: &struct {
			OverageStatus string `json:"overageStatus"`
		}{OverageStatus: "ENABLED"},
		UsageBreakdownList: []usageBreakdown{{
			ResourceType: "CREDIT", CurrentUsage: 100, UsageLimit: 100,
			OverageCap: 50, CurrentOverages: 50,
		}},
	}
	quota := quotaFromUsage(usage, time.Now().UTC())
	if !quota.Exceeded || quota.Remaining != 0 {
		t.Fatalf("exhausted overage still selectable: %+v", quota)
	}
}

func TestBuilderIDProfileIsRequestScoped(t *testing.T) {
	cred := Credential{AuthMethod: "builder_id", Region: "us-east-1", AccessToken: "access"}
	if cred.requestProfileArn() != builderIDProfileArn {
		t.Fatalf("profile = %s", cred.requestProfileArn())
	}
	encoded, err := cred.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "AAAACCCCXXXX") {
		t.Fatalf("fixed profile was stored: %s", encoded)
	}
}

func TestAPIKeyHeadersAndNoProfile(t *testing.T) {
	cred := Credential{AuthMethod: "api_key", AccessToken: "ksk_test", Region: "eu-central-1"}
	if cred.requestProfileArn() != "" || cred.dataPlaneRegion() != "eu-central-1" || cred.tokenType() != "API_KEY" {
		t.Fatalf("cred = %+v region %s", cred, cred.dataPlaneRegion())
	}
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	applyRuntimeHeaders(req, cred, "example.com")
	if req.Header.Get("tokentype") != "API_KEY" {
		t.Fatalf("tokentype = %s", req.Header.Get("tokentype"))
	}
}

func TestProfileArnRegionBeatsOIDCRegion(t *testing.T) {
	cred := Credential{
		AuthMethod: "idc", Region: "us-east-1",
		ProfileArn: "arn:aws:codewhisperer:eu-central-1:123456789012:profile/abc",
	}
	if cred.dataPlaneRegion() != "eu-central-1" {
		t.Fatalf("region = %s", cred.dataPlaneRegion())
	}
	got := regionalizeURL("https://codewhisperer.us-east-1.amazonaws.com/getUsageLimits", cred.dataPlaneRegion())
	if !strings.Contains(got, "q.eu-central-1.amazonaws.com") {
		t.Fatalf("url = %s", got)
	}
}

func TestImportedSocialCredentialRefreshesBeforeQuotaAndModels(t *testing.T) {
	const profile = "arn:aws:codewhisperer:us-east-1:123456789012:profile/example"
	store := &memStore{payload: []byte(`{"provider":"Github","accessToken":"old-access","refreshToken":"old-refresh"}`)}
	var refreshes, lookups int
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body string
		switch r.URL.String() {
		case socialRefreshURL:
			refreshes++
			var request struct {
				RefreshToken string `json:"refreshToken"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.RefreshToken != "old-refresh" {
				t.Fatal("refresh did not use the imported refresh token")
			}
			body = `{"accessToken":"new-access","refreshToken":"new-refresh","expiresIn":3600}`
		case kiroMetadataURL:
			body = fmt.Sprintf(`{"currentRelease":%q}`, IDEVersion)
		default:
			if r.Header.Get("Authorization") != "Bearer new-access" {
				t.Fatalf("%s did not use the refreshed access token", r.URL.Path)
			}
			if r.URL.Path != "/ListAvailableProfiles" && r.URL.Query().Get("profileArn") != profile {
				t.Fatalf("%s did not use the discovered profile", r.URL.Path)
			}
			switch r.URL.Path {
			case "/ListAvailableProfiles":
				lookups++
				body = fmt.Sprintf(`{"profiles":[{"arn":%q}]}`, profile)
			case "/getUsageLimits":
				body = `{"subscriptionInfo":{"subscriptionTitle":"KIRO PRO"}}`
			case "/ListAvailableModels":
				body = `{"models":[{"modelId":"auto"}]}`
			default:
				t.Fatalf("unexpected path %s", r.URL.Path)
			}
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	client := &Client{store: store, http: httpClient}
	ctx := context.Background()
	quota, err := client.Quota(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	if quota.Plan != "PRO" {
		t.Fatalf("plan = %q, want PRO", quota.Plan)
	}
	models, err := client.Models(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].NativeModel != "auto" {
		t.Fatalf("models = %+v", models)
	}
	if refreshes != 1 || lookups != 1 {
		t.Fatalf("refreshes = %d, profile lookups = %d, want one each", refreshes, lookups)
	}
	stored, err := DecodeCredential(store.payload)
	if err != nil {
		t.Fatal(err)
	}
	if stored.RefreshToken != "new-refresh" || stored.ExpiresAt <= time.Now().Unix() || stored.ProfileArn != profile {
		t.Fatal("refreshed credential, expiry, or profile was not saved")
	}
}

func TestRefreshDispatchesByAuthMethod(t *testing.T) {
	var got []string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		got = append(got, r.URL.String()+" "+string(raw))
		body := `{"accessToken":"new-access","refreshToken":"new-refresh","expiresIn":3600}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	social, err := refresh(context.Background(), client, Credential{AuthMethod: "social", RefreshToken: "social-refresh"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got[0], socialRefreshURL) || !strings.Contains(got[0], `"refreshToken":"social-refresh"`) {
		t.Fatalf("social request = %s", got[0])
	}
	if social.AccessToken != "new-access" || social.ExpiresAt == 0 {
		t.Fatalf("social = %+v", social)
	}

	if _, err := refresh(context.Background(), client, Credential{
		AuthMethod: "builder_id", Region: "us-east-1", RefreshToken: "oidc-refresh",
		ClientID: "client", ClientSecret: "secret",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got[1], "https://oidc.us-east-1.amazonaws.com/token") || !strings.Contains(got[1], `"grantType":"refresh_token"`) {
		t.Fatalf("oidc request = %s", got[1])
	}

	if _, err := refresh(context.Background(), client, Credential{AuthMethod: "builder_id", RefreshToken: "only-refresh"}); err == nil || !strings.Contains(err.Error(), "client id") {
		t.Fatalf("builder id without a client was accepted: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestExternalRefreshRejectsOtherHosts(t *testing.T) {
	cred := Credential{
		AuthMethod: "external_idp", ClientID: "11111111-1111-1111-1111-111111111111",
		RefreshToken: "refresh", Scopes: "offline_access api://11111111-1111-1111-1111-111111111111/codewhisperer:completions",
		TokenEndpoint: "https://evil.example.com/token",
	}
	if _, err := refreshExternal(context.Background(), http.DefaultClient, cred); err == nil {
		t.Fatal("a non-microsoft token endpoint was accepted")
	}
	cred.TokenEndpoint = "https://login.microsoftonline.com/22222222-2222-2222-2222-222222222222/oauth2/v2.0/token"
	cred.IssuerURL = "https://login.microsoftonline.com/33333333-3333-3333-3333-333333333333/v2.0"
	if _, err := refreshExternal(context.Background(), http.DefaultClient, cred); err == nil || !strings.Contains(err.Error(), "tenant") {
		t.Fatalf("mismatched tenant was accepted: %v", err)
	}
}

func TestConversationIDStaysStableAcrossTurns(t *testing.T) {
	first := translate.ChatRequest{Messages: []translate.ChatMessage{
		{Role: "system", Content: "be brief"},
		{Role: "user", Content: "read it"},
	}}
	second := translate.ChatRequest{Messages: []translate.ChatMessage{
		{Role: "system", Content: "be brief"},
		{Role: "user", Content: "read it"},
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "thanks"},
	}}
	a, err := buildConversation(first, Credential{}, "claude-sonnet-4.5")
	if err != nil {
		t.Fatal(err)
	}
	b, err := buildConversation(second, Credential{}, "claude-sonnet-4.5")
	if err != nil {
		t.Fatal(err)
	}
	if a.responsesBody(false).Metadata["kiro:conversation_id"] != b.responsesBody(false).Metadata["kiro:conversation_id"] {
		t.Fatal("conversation id changed between turns")
	}
	if a.responsesBody(false).Metadata["kiro:agent_continuation_id"] == "" {
		t.Fatal("continuation id missing")
	}
	if a.legacyBody(Origin, false).ConversationState.ConversationID != conversationID(a.Model, a.System, a.Anchor) {
		t.Fatal("legacy body carries a different conversation id")
	}
}

func TestToolChoiceAndImageRideTheUserTurn(t *testing.T) {
	req := translate.ChatRequest{
		Messages: []translate.ChatMessage{{Role: "user", Content: []any{
			map[string]any{"type": "text", "text": "look"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,AAAA"}},
		}}},
		Tools:      json.RawMessage(`[{"type":"function","function":{"name":"Read","parameters":{"type":"object"}}}]`),
		ToolChoice: json.RawMessage(`"required"`),
	}
	conv, err := buildConversation(req, Credential{AuthMethod: "social"}, "claude-sonnet-4.5")
	if err != nil {
		t.Fatal(err)
	}
	body := conv.responsesBody(true)
	user := body.Input[0]
	parts, _ := user["content"].([]map[string]any)
	if len(parts) != 2 || parts[1]["type"] != "input_image" {
		t.Fatalf("content = %#v", user["content"])
	}
	text := parts[0]["text"].(string)
	if !strings.Contains(text, "requires a tool call") {
		t.Fatalf("text = %s", text)
	}
	if body.ProfileArn != "" {
		t.Fatalf("social profile = %s", body.ProfileArn)
	}
}

func TestFeatureFlagSelectsCreateResponse(t *testing.T) {
	sum := sha256.Sum256([]byte(featureConfigSalt + "create_response_api"))
	key := hex.EncodeToString(sum[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/featureConfiguration" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"configuration": map[string]any{key: "create_response"}})
	}))
	defer server.Close()
	client := &Client{http: server.Client()}
	cred := Credential{AccessToken: "access", AuthMethod: "builder_id"}
	defer setTestHosts(server.URL)()
	if !client.useResponses(context.Background(), "acct", cred) {
		t.Fatal("flag did not select CreateResponse")
	}
	if client.useResponses(context.Background(), "acct", Credential{AccessToken: "ksk_x", AuthMethod: "api_key"}) {
		t.Fatal("api key must stay on the legacy runtime")
	}
}

func TestLegacyFallbackRetriesOnRateLimitFlag(t *testing.T) {
	var paths []string
	var flagReads int
	sum := sha256.Sum256([]byte(featureConfigSalt + "create_response_api"))
	flagKey := hex.EncodeToString(sum[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/featureConfiguration":
			flagReads++
			if flagReads == 1 {
				_, _ = w.Write([]byte(`{"configuration":{}}`))
				return
			}
			fmt.Fprintf(w, `{"configuration":{%q:%q}}`, flagKey, "create_response")
		case "/generateAssistantResponse":
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message":"USER_REQUEST_RATE_EXCEEDED"}`))
		case "/v1/responses":
			if !strings.Contains(string(body), `"model":"claude-sonnet-4.5"`) {
				t.Fatalf("body = %s", body)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n")
		default:
			t.Fatalf("unexpected %s", r.URL.Path)
		}
	}))
	defer server.Close()
	// Point every legacy host and the runtime host at the test server by
	// replacing the URL builders for this test.
	client := &Client{store: &memStore{payload: mustCred(t, Credential{AccessToken: "access", AuthMethod: "idc", ProfileArn: "arn:aws:codewhisperer:us-east-1:123456789012:profile/abc"})}, http: server.Client()}
	defer setTestHosts(server.URL)()
	featureFlags.Delete("acct")
	resp, _, err := client.ChatStream(context.Background(), "acct", translate.ChatRequest{Protocol: "anthropic_messages", Model: "claude-sonnet-4.5", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(raw), "ok") {
		t.Fatalf("stream = %s\npaths = %v", raw, paths)
	}
}

type memStore struct {
	payload []byte
	saved   []byte
}

func (m *memStore) Get(context.Context, string) (accounts.Account, error) {
	return accounts.Account{}, nil
}
func (m *memStore) LoadCredentialPayload(context.Context, string) (string, []byte, error) {
	return CredentialFormat, append([]byte(nil), m.payload...), nil
}
func (m *memStore) SaveCredentialPayload(_ context.Context, _, _ string, payload []byte) error {
	m.saved = append([]byte(nil), payload...)
	m.payload = m.saved
	return nil
}
func (m *memStore) Observe(context.Context, string, string, string, string, string) error {
	return nil
}
func (m *memStore) SetCredentialAuthType(context.Context, string, string) error { return nil }

func mustCred(t *testing.T, cred Credential) []byte {
	t.Helper()
	raw, err := cred.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
