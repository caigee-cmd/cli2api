package executor

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

type stubContexts struct {
	value int
	ok    bool
	err   error
	got   string
}

func (s *stubContexts) GetModelContext(_ context.Context, modelID string) (int, bool, error) {
	s.got = modelID
	return s.value, s.ok, s.err
}

type stubCatalogs struct {
	calls int
}

func (s *stubCatalogs) EnsureModelCatalogs(context.Context, bool) {
	s.calls++
}

type stubLogs struct {
	entries []accounts.RequestLog
}

func (s *stubLogs) Start(entry accounts.RequestLog) {
	s.entries = append(s.entries, entry)
}

func TestPrepareRejectsBareModelWhenPoolDisabled(t *testing.T) {
	ex := NewChatExecutor(NewPool(nil, nil), "")
	_, err := ex.Prepare(PrepareInput{
		Request: translate.ChatRequest{Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}},
	})
	var prepared *PrepareError
	if err == nil || !asPrepareError(err, &prepared) || prepared.Code != "provider_prefix_required" || prepared.Status != 400 {
		t.Fatalf("err=%v", err)
	}
}

func TestPrepareStripsPrefixAndChecksGrants(t *testing.T) {
	ex := NewChatExecutor(NewPool(nil, nil), "")
	got, err := ex.Prepare(PrepareInput{
		Request: translate.ChatRequest{Model: "workbuddy/glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}},
		Identity: auth.KeyIdentity(accounts.APIKey{
			ID: "key_1", Name: "ci", Providers: []string{"qoder", "workbuddy"}, Enabled: true,
		}),
		PreferAccount: "account-a",
		NewRequestID:  func() string { return "req_fixed" },
		Now:           time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ProviderFilter != "workbuddy" || got.Prefer != "account-a" || got.Request.Model != "glm-5.2" || got.PublicModel != "workbuddy/glm-5.2" {
		t.Fatalf("%+v", got)
	}
	if got.RequestID != "req_fixed" {
		t.Fatalf("request id=%s", got.RequestID)
	}
}

func TestPrepareDeniesUngrantedProvider(t *testing.T) {
	ex := NewChatExecutor(NewPool(nil, nil), "")
	_, err := ex.Prepare(PrepareInput{
		Request: translate.ChatRequest{Model: "workbuddy/glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}},
		Identity: auth.KeyIdentity(accounts.APIKey{
			ID: "key_1", Name: "ci", Providers: []string{"qoder"}, Enabled: true,
		}),
	})
	var prepared *PrepareError
	if err == nil || !asPrepareError(err, &prepared) || prepared.Code != "provider_not_allowed" || prepared.Status != 403 {
		t.Fatalf("err=%v", err)
	}
}

func TestPrepareAppliesQoderContextAndStartsLog(t *testing.T) {
	ex := NewChatExecutor(NewPool(nil, nil), "")
	contexts := &stubContexts{value: 500000, ok: true}
	catalogs := &stubCatalogs{}
	logs := &stubLogs{}
	got, err := ex.Prepare(PrepareInput{
		Request: translate.ChatRequest{
			Model: "qoder/glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
			ReasoningEffort: json.RawMessage(`"high"`),
		},
		ModelContexts: contexts,
		Catalogs:      catalogs,
		Logs:          logs,
		NewRequestID:  func() string { return "req_log" },
		Now:           time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if contexts.got != "glm-5.2" {
		t.Fatalf("context key=%s", contexts.got)
	}
	if string(got.Request.ContextLength) != "500000" || string(got.Request.MaxInputTokens) != "500000" {
		t.Fatalf("defaults=%s %s", got.Request.ContextLength, got.Request.MaxInputTokens)
	}
	if catalogs.calls != 1 {
		t.Fatalf("catalog calls=%d", catalogs.calls)
	}
	if len(logs.entries) != 1 || logs.entries[0].ID != "req_log" || logs.entries[0].Status != accounts.RequestStatusStarted {
		t.Fatalf("logs=%+v", logs.entries)
	}
	if logs.entries[0].RequestedReasoning != "high" {
		t.Fatalf("requested reasoning=%q", logs.entries[0].RequestedReasoning)
	}
}

func TestPrepareNativeResponsesKeepsCompatSeedAndReasoning(t *testing.T) {
	ex := NewChatExecutor(NewPool(nil, nil), "")
	native, err := translate.ParseNativeResponses([]byte(`{"model":"codex/gpt-5.5","input":[{"role":"user","content":"hi"}],"reasoning":{"effort":"high"},"instructions":"rules"}`))
	if err != nil {
		t.Fatal(err)
	}
	compat, err := native.Compat()
	if err != nil {
		t.Fatal(err)
	}
	logs := &stubLogs{}
	got, err := ex.Prepare(PrepareInput{
		Request:       compat.Chat,
		NativeRequest: native,
		Logs:          logs,
		Identity:      auth.ConsoleIdentity(),
	})
	if err != nil {
		t.Fatal(err)
	}
	chatOnly, err := ex.Prepare(PrepareInput{Request: compat.Chat, Identity: auth.ConsoleIdentity()})
	if err != nil {
		t.Fatal(err)
	}
	if sessionKeyFromContext(got.Context) == "" || sessionKeyFromContext(got.Context) != sessionKeyFromContext(chatOnly.Context) {
		t.Fatal("native request replaced the compatibility session seed")
	}
	if len(logs.entries) != 1 || logs.entries[0].RequestedReasoning != "high" {
		t.Fatalf("native reasoning log=%+v", logs.entries)
	}
}

func TestPrepareSkipsNonQoderContextDefaults(t *testing.T) {
	ex := NewChatExecutor(NewPool(nil, nil), "")
	contexts := &stubContexts{value: 500000, ok: true}
	got, err := ex.Prepare(PrepareInput{
		Request:       translate.ChatRequest{Model: "trae/glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}},
		ModelContexts: contexts,
	})
	if err != nil {
		t.Fatal(err)
	}
	if contexts.got != "" {
		t.Fatalf("trae must not read qoder context, got %q", contexts.got)
	}
	if len(got.Request.ContextLength) != 0 {
		t.Fatalf("context=%s", got.Request.ContextLength)
	}
}

func TestPrepareBareModelEmptyFilterWhenPoolEnabled(t *testing.T) {
	ex := NewChatExecutor(NewPool(nil, nil), "")
	got, err := ex.Prepare(PrepareInput{
		Request:           translate.ChatRequest{Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}},
		CrossProviderPool: true,
		Identity: auth.KeyIdentity(accounts.APIKey{
			ID: "key_1", Name: "ci", Providers: []string{"qoder"}, Enabled: true,
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ProviderFilter != "" || got.Request.Model != "glm-5.2" {
		t.Fatalf("%+v", got)
	}
}

func TestPrepareSessionKeyIsolatesByIdentity(t *testing.T) {
	ex := NewChatExecutor(NewPool(nil, nil), "")
	req := translate.ChatRequest{Model: "qoder/glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}}
	console, err := ex.Prepare(PrepareInput{Request: req, Identity: auth.ConsoleIdentity()})
	if err != nil {
		t.Fatal(err)
	}
	keyed, err := ex.Prepare(PrepareInput{
		Request: req,
		Identity: auth.KeyIdentity(accounts.APIKey{
			ID: "key_1", Name: "ci", Providers: []string{"qoder"}, Enabled: true,
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if sessionKeyFromContext(console.Context) == "" || sessionKeyFromContext(console.Context) == sessionKeyFromContext(keyed.Context) {
		t.Fatalf("session keys must differ by identity")
	}
	header, err := ex.Prepare(PrepareInput{
		Request:       req,
		Identity:      auth.ConsoleIdentity(),
		SessionHeader: "explicit",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sessionKeyFromContext(header.Context) == sessionKeyFromContext(console.Context) {
		t.Fatal("header session must not reuse content seed")
	}
}

func TestPreparePreservesExplicitContext(t *testing.T) {
	ex := NewChatExecutor(NewPool(nil, nil), "")
	contexts := &stubContexts{value: 111, ok: true}
	got, err := ex.Prepare(PrepareInput{
		Request: translate.ChatRequest{
			Model:          "qoder/glm-5.2",
			Messages:       []translate.ChatMessage{{Role: "user", Content: "hi"}},
			ContextLength:  json.RawMessage("222"),
			MaxInputTokens: json.RawMessage("333"),
		},
		ModelContexts: contexts,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Request.ContextLength) != "222" || string(got.Request.MaxInputTokens) != "333" {
		t.Fatalf("explicit values overwritten: %s %s", got.Request.ContextLength, got.Request.MaxInputTokens)
	}
}

func asPrepareError(err error, out **PrepareError) bool {
	if err == nil {
		return false
	}
	got, ok := err.(*PrepareError)
	if !ok {
		return false
	}
	*out = got
	return true
}

func TestProviderPrefixRecognizesCommand(t *testing.T) {
	cases := map[string]string{
		"qoder/glm-5.2":                    "qoder",
		"workbuddy/deepseek-v4-pro":        "workbuddy",
		"trae/kimi-k2.6":                   "trae",
		"devin/swe-2":                      "devin",
		"codex/gpt-5.5":                    "codex",
		"zhipu/glm-5.2":                    "zhipu",
		"command/deepseek/deepseek-v4-pro": "command",
		"command/moonshotai/Kimi-K3":       "command",
		"codearts/GLM-5.2":                 "codearts",
		// Region-qualified dash form.
		"workbuddy-cn/glm-5.3-flash":   "workbuddy",
		"workbuddy-gl/glm-5.3-flash":   "workbuddy",
		"workbuddy-auto/glm-5.3-flash": "workbuddy",
		"qoder-auto/glm-5.2":           "qoder",
		// A bare org-namespaced Command Code id must NOT be read as a provider.
		"deepseek/deepseek-v4-pro": "",
		"moonshotai/Kimi-K3":       "",
	}
	for model, want := range cases {
		if got := ProviderPrefix(model); got != want {
			t.Errorf("ProviderPrefix(%q) = %q, want %q", model, got, want)
		}
	}
}

func TestResolveProviderFilterRegionPrefix(t *testing.T) {
	cases := []struct {
		model        string
		wantProvider string
		wantRegion   string
		wantStripped string
	}{
		{"workbuddy-cn/glm-5.3-flash", "workbuddy", "cn", "glm-5.3-flash"},
		{"workbuddy-gl/glm-5.3-flash", "workbuddy", "global", "glm-5.3-flash"},
		{"workbuddy/glm-5.3-flash", "workbuddy", "", "glm-5.3-flash"},
		{"qoder-cn/glm-5.2", "qoder", "cn", "glm-5.2"},
		{"qoder-auto/glm-5.2", "qoder", "", "glm-5.2"},
		{"workbuddy-auto/glm-5.3-flash", "workbuddy", "", "glm-5.3-flash"},
		{"command/deepseek/deepseek-v4-pro", "command", "", "deepseek/deepseek-v4-pro"},
		{"codearts/GLM-5.2", "codearts", "", "GLM-5.2"},
	}
	for _, tc := range cases {
		req := translate.ChatRequest{Model: tc.model}
		provider, region := ResolveProviderFilter(&req, false)
		if provider != tc.wantProvider || region != tc.wantRegion || req.Model != tc.wantStripped {
			t.Errorf("ResolveProviderFilter(%q) = (%q, %q, %q), want (%q, %q, %q)",
				tc.model, provider, region, req.Model,
				tc.wantProvider, tc.wantRegion, tc.wantStripped)
		}
	}
}

func TestPrepareStripsAutoPrefix(t *testing.T) {
	ex := NewChatExecutor(NewPool(nil, nil), "")
	got, err := ex.Prepare(PrepareInput{
		Request:      translate.ChatRequest{Model: "qoder-auto/qwen3.8-max", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}},
		Identity:     auth.KeyIdentity(accounts.APIKey{ID: "k", Name: "ci", Providers: []string{"qoder"}, Enabled: true}),
		NewRequestID: func() string { return "req_a" },
	})
	if err != nil {
		t.Fatalf("Prepare error: %v", err)
	}
	if got.ProviderFilter != "qoder" {
		t.Errorf("ProviderFilter = %q, want qoder", got.ProviderFilter)
	}
	if got.Request.Model != "qwen3.8-max" {
		t.Errorf("Request.Model = %q, want qwen3.8-max (prefix stripped)", got.Request.Model)
	}
	if r := regionFilterFromContext(got.Context); r != "" {
		t.Errorf("region = %q, want empty (auto selects region in pool)", r)
	}
}

func TestPrepareStripsRegionPrefixIntoContext(t *testing.T) {
	ex := NewChatExecutor(NewPool(nil, nil), "")
	got, err := ex.Prepare(PrepareInput{
		Request: translate.ChatRequest{Model: "workbuddy-cn/glm-5.3-flash", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}},
		Identity: auth.KeyIdentity(accounts.APIKey{
			ID: "key_1", Name: "ci", Providers: []string{"qoder", "workbuddy"}, Enabled: true,
		}),
		NewRequestID: func() string { return "req_r" },
		Now:          time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ProviderFilter != "workbuddy" || got.Request.Model != "glm-5.3-flash" || got.PublicModel != "workbuddy-cn/glm-5.3-flash" {
		t.Fatalf("provider=%q model=%q publicModel=%q", got.ProviderFilter, got.Request.Model, got.PublicModel)
	}
	if r := regionFilterFromContext(got.Context); r != "cn" {
		t.Fatalf("regionFilterFromContext = %q, want cn", r)
	}
}

func TestPrepareBarePrefixSetsNoRegionFilter(t *testing.T) {
	// A bare "qoder/model" must not default to cn: no region filter reaches the
	// route loop, so international-only pools still route.
	ex := NewChatExecutor(NewPool(nil, nil), "")
	got, err := ex.Prepare(PrepareInput{
		Request:      translate.ChatRequest{Model: "qoder/qwen3.8-max", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}},
		Identity:     auth.KeyIdentity(accounts.APIKey{ID: "k", Name: "ci", Providers: []string{"qoder"}, Enabled: true}),
		NewRequestID: func() string { return "req_b" },
	})
	if err != nil {
		t.Fatalf("Prepare error: %v", err)
	}
	if got.ProviderFilter != "qoder" || got.Request.Model != "qwen3.8-max" {
		t.Errorf("provider=%q model=%q, want qoder / qwen3.8-max", got.ProviderFilter, got.Request.Model)
	}
	if r := regionFilterFromContext(got.Context); r != "" {
		t.Errorf("region = %q, want empty (bare prefix must not pin cn)", r)
	}
}

func TestPrepareStripsCommandPrefix(t *testing.T) {
	ex := NewChatExecutor(NewPool(nil, nil), "")
	got, err := ex.Prepare(PrepareInput{
		Request:  translate.ChatRequest{Model: "command/deepseek/deepseek-v4-pro", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}},
		Identity: auth.KeyIdentity(accounts.APIKey{ID: "k", Name: "ci", Providers: []string{"command"}, Enabled: true}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ProviderFilter != "command" || got.Request.Model != "deepseek/deepseek-v4-pro" {
		t.Fatalf("filter=%q model=%q, want command / deepseek/deepseek-v4-pro", got.ProviderFilter, got.Request.Model)
	}
}
