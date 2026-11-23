package executor

import (
	"context"
	"net/http"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

type probeCaptureChat struct {
	req      translate.ChatRequest
	reqs     []translate.ChatRequest
	accounts []string
	err      error
}

func (f *probeCaptureChat) ChatNonStream(_ context.Context, accountID string, req translate.ChatRequest) (providers.ChatOutcome, error) {
	f.req = req
	f.reqs = append(f.reqs, req)
	f.accounts = append(f.accounts, accountID)
	if f.err != nil {
		return providers.ChatOutcome{}, f.err
	}
	return providers.ChatOutcome{Model: req.Model, Content: "OK", FinishReason: "stop"}, nil
}

func (f *probeCaptureChat) ChatStream(_ context.Context, _ string, _ translate.ChatRequest) (*http.Response, providers.ResolvedChat, error) {
	return nil, providers.ResolvedChat{}, nil
}

func TestProbeModelUsesMinimalRequestAndDoesNotCoolAccount(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "wb1", Provider: "workbuddy", Runtime: string(providers.RuntimeInProcess), Models: []string{"glm-5.2"}})
	chat := &probeCaptureChat{}
	registry := providers.NewRegistry()
	registry.Register(providers.Adapter{ID: "workbuddy", Chat: chat})
	e := ChatExecutor{Pool: pool, Providers: registry}

	res := e.ProbeModel(context.Background(), "glm-5.2")
	if !res.OK {
		t.Fatalf("probe failed: %+v", res)
	}
	if got := string(chat.req.MaxTokens); got != "1" {
		t.Fatalf("max_tokens=%q want 1", got)
	}
	if got := string(chat.req.MaxCompletionTokens); got != "1" {
		t.Fatalf("max_completion_tokens=%q want 1", got)
	}
	if !providers.RequestsDisabledThinking(chat.req) {
		t.Fatal("probe must disable thinking to avoid reasoning token spend")
	}
	if chat.req.Stream {
		t.Fatal("probe must be non-streaming")
	}
	if len(chat.req.Messages) != 1 || chat.req.Messages[0].Role != "user" || chat.req.Messages[0].Content != "hi" {
		t.Fatalf("unexpected probe messages: %+v", chat.req.Messages)
	}
	if len(res.Accounts) != 1 {
		t.Fatalf("account results=%d want 1: %+v", len(res.Accounts), res.Accounts)
	}
	item, ok := pool.ByID("wb1")
	if !ok {
		t.Fatal("probe account missing")
	}
	if !item.DownUntil.IsZero() || len(item.ModelDownUntil) != 0 {
		t.Fatalf("probe must not cool the account: down=%v model=%v", item.DownUntil, item.ModelDownUntil)
	}
	if item.InFlight != 0 {
		t.Fatalf("probe leaked in-flight slot: %d", item.InFlight)
	}
}

func TestProbeModelDoesNotMutateRoutingCursor(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "wb1", Provider: "workbuddy", Runtime: string(providers.RuntimeInProcess), Models: []string{"glm-5.2"}})
	pool.Upsert(Item{ID: "wb2", Provider: "workbuddy", Runtime: string(providers.RuntimeInProcess), Models: []string{"glm-5.2"}})
	chat := &probeCaptureChat{}
	registry := providers.NewRegistry()
	registry.Register(providers.Adapter{ID: "workbuddy", Chat: chat})
	e := ChatExecutor{Pool: pool, Providers: registry}

	routeKey := rotationKey(RouteQuery{PublicModel: "glm-5.2"})
	pool.mu.Lock()
	pool.ensureRotationKey(routeKey)
	pool.lastPicked[routeKey] = "wb1"
	before := pool.lastPicked[routeKey]
	pool.mu.Unlock()

	_ = e.ProbeModel(context.Background(), "glm-5.2")

	pool.mu.Lock()
	after := pool.lastPicked[routeKey]
	pool.mu.Unlock()
	if after != before {
		t.Fatalf("probe mutated routing cursor: before=%q after=%q", before, after)
	}
}

func TestProbeModelChecksEveryAccountEvenAfterSuccess(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "wb1", Provider: "workbuddy", Runtime: string(providers.RuntimeInProcess), Models: []string{"glm-5.2"}})
	pool.Upsert(Item{ID: "wb2", Provider: "workbuddy", Runtime: string(providers.RuntimeInProcess), Models: []string{"glm-5.2"}})
	chat := &probeCaptureChat{}
	registry := providers.NewRegistry()
	registry.Register(providers.Adapter{ID: "workbuddy", Chat: chat})
	e := ChatExecutor{Pool: pool, Providers: registry}

	res := e.ProbeModel(context.Background(), "glm-5.2")
	if !res.OK {
		t.Fatalf("probe failed: %+v", res)
	}
	if len(chat.accounts) != 2 {
		t.Fatalf("probed accounts=%v want both wb1 and wb2", chat.accounts)
	}
	if len(res.Accounts) != 2 {
		t.Fatalf("account results=%d want 2: %+v", len(res.Accounts), res.Accounts)
	}
	seen := map[string]bool{}
	for _, account := range res.Accounts {
		if !account.OK {
			t.Fatalf("account result not OK: %+v", account)
		}
		seen[account.AccountID] = true
	}
	if !seen["wb1"] || !seen["wb2"] {
		t.Fatalf("account results missing an account: %+v", res.Accounts)
	}
}

func TestProbeModelFailureDoesNotCoolAccount(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "wb1", Provider: "workbuddy", Runtime: string(providers.RuntimeInProcess), Models: []string{"glm-5.2"}})
	chat := &probeCaptureChat{err: &providers.Error{
		Kind:    accounts.KindRateLimit,
		Status:  http.StatusTooManyRequests,
		Code:    "rate_limit",
		Message: "busy",
	}}
	registry := providers.NewRegistry()
	registry.Register(providers.Adapter{ID: "workbuddy", Chat: chat})
	e := ChatExecutor{Pool: pool, Providers: registry}

	res := e.ProbeModel(context.Background(), "glm-5.2")
	if res.OK {
		t.Fatal("probe should report failure")
	}
	if res.Kind != accounts.KindRateLimit {
		t.Fatalf("kind=%q want %q", res.Kind, accounts.KindRateLimit)
	}
	if len(res.Accounts) != 1 || res.Accounts[0].Kind != accounts.KindRateLimit {
		t.Fatalf("account result missing failure kind: %+v", res.Accounts)
	}
	item, _ := pool.ByID("wb1")
	if !item.DownUntil.IsZero() || len(item.ModelDownUntil) != 0 {
		t.Fatalf("probe failure must not cool the account: down=%v model=%v", item.DownUntil, item.ModelDownUntil)
	}
	if item.InFlight != 0 {
		t.Fatalf("probe leaked in-flight slot: %d", item.InFlight)
	}
}
