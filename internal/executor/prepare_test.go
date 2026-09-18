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
		Request:       translate.ChatRequest{Model: "qoder/glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}},
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
