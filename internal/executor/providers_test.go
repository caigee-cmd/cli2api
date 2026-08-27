package executor

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/providers/workbuddy"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

type fakeInProcessChat struct {
	calls    int
	provider string
}

func (f *fakeInProcessChat) ChatNonStream(ctx context.Context, accountID string, req translate.ChatRequest) (providers.ChatOutcome, error) {
	f.calls++
	if req.Model != "glm-5.2" {
		return providers.ChatOutcome{}, errors.New("model unsupported")
	}
	return providers.ChatOutcome{Model: req.Model, Content: "OK", FinishReason: "stop"}, nil
}

func (f *fakeInProcessChat) ChatStream(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, error) {
	f.calls++
	return nil, errors.New("stream unsupported in fake")
}

func TestInProcessProviderPinnedChatDoesNotTouchWorkers(t *testing.T) {
	pool := accounts.NewPool([]string{"http://127.0.0.1:1"}, []string{"qoder1"})
	pool.Upsert(accounts.Item{ID: "wb1", Provider: "workbuddy", Region: "cn", Runtime: "in_process"})
	registry := providers.NewRegistry()
	fake := &fakeInProcessChat{}
	registry.Register(providers.Adapter{ID: "workbuddy", Chat: fake})

	ex := NewChatExecutor(pool, "")
	ex.Providers = registry
	result, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "wb1", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "OK" || result.AccountID != "wb1" || fake.calls != 1 {
		t.Fatalf("result=%+v calls=%d", result, fake.calls)
	}
}

func TestInProcessProviderFilterRoutesWithoutPin(t *testing.T) {
	pool := accounts.NewPool([]string{"http://127.0.0.1:1"}, []string{"qoder1"})
	pool.Upsert(accounts.Item{ID: "wb1", Provider: "workbuddy", Region: "cn", Runtime: "in_process"})
	registry := providers.NewRegistry()
	fake := &fakeInProcessChat{}
	registry.Register(providers.Adapter{ID: "workbuddy", Chat: fake})

	ex := NewChatExecutor(pool, "")
	ex.Providers = registry
	result, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "workbuddy")
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "OK" || result.AccountID != "wb1" || fake.calls != 1 {
		t.Fatalf("result=%+v calls=%d", result, fake.calls)
	}
}

func TestInProcessProviderOnlyAccountRoutesWithoutPin(t *testing.T) {
	pool := accounts.NewPool(nil, nil)
	pool.Upsert(accounts.Item{ID: "wb1", Provider: "workbuddy", Runtime: "in_process"})
	registry := providers.NewRegistry()
	fake := &fakeInProcessChat{}
	registry.Register(providers.Adapter{ID: "workbuddy", Chat: fake})

	ex := NewChatExecutor(pool, "")
	ex.Providers = registry
	result, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "qoder")
	if err == nil {
		t.Fatalf("expected no qoder account, got %+v", result)
	}

	result, err = ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.AccountID != "wb1" || fake.calls != 1 {
		t.Fatalf("result=%+v calls=%d", result, fake.calls)
	}
}

func TestInProcessProviderUnsupportedModelDoesNotFailoverToQoder(t *testing.T) {
	pool := accounts.NewPool([]string{"http://127.0.0.1:1"}, []string{"qoder1"})
	pool.Upsert(accounts.Item{ID: "wb1", Provider: "workbuddy", Runtime: "in_process"})
	registry := providers.NewRegistry()
	fake := &fakeInProcessChat{}
	registry.Register(providers.Adapter{ID: "workbuddy", Chat: fake})

	ex := NewChatExecutor(pool, "")
	ex.Providers = registry
	_, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model: "unknown-model", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "wb1", "")
	if err == nil {
		t.Fatal("expected unsupported model error")
	}
}

type rateLimitedThenOKChat struct {
	calls int
}

func (f *rateLimitedThenOKChat) ChatNonStream(ctx context.Context, accountID string, req translate.ChatRequest) (providers.ChatOutcome, error) {
	f.calls++
	if accountID == "wb1" {
		return providers.ChatOutcome{}, &workbuddy.ClassifiedError{Kind: accounts.KindRateLimit, Status: 429, Message: "soft_rate"}
	}
	return providers.ChatOutcome{Model: req.Model, Content: "OK-" + accountID, FinishReason: "stop"}, nil
}

func (f *rateLimitedThenOKChat) ChatStream(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, error) {
	return nil, errors.New("stream unsupported in fake")
}

type systemObservingChat struct {
	sawSystem []bool
}

func (f *systemObservingChat) ChatNonStream(ctx context.Context, accountID string, req translate.ChatRequest) (providers.ChatOutcome, error) {
	hasSystem := false
	for _, message := range req.Messages {
		if message.Role == "system" {
			hasSystem = true
		}
	}
	f.sawSystem = append(f.sawSystem, hasSystem)
	return providers.ChatOutcome{Model: req.Model, Content: "OK", FinishReason: "stop"}, nil
}

func (f *systemObservingChat) ChatStream(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, error) {
	return nil, errors.New("stream unsupported in fake")
}

func TestInProcessDropSystemPromptStripsBeforeProvider(t *testing.T) {
	pool := accounts.NewPool(nil, nil)
	pool.Upsert(accounts.Item{ID: "wb1", Provider: "workbuddy", Runtime: "in_process", DropSystemPrompt: true})
	registry := providers.NewRegistry()
	fake := &systemObservingChat{}
	registry.Register(providers.Adapter{ID: "workbuddy", Chat: fake})

	ex := NewChatExecutor(pool, "")
	ex.Providers = registry
	result, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{
			{Role: "system", Content: "third-party identity"},
			{Role: "user", Content: "hi"},
		},
	}, "", "workbuddy")
	if err != nil {
		t.Fatal(err)
	}
	if result.AccountID != "wb1" || len(fake.sawSystem) != 1 || fake.sawSystem[0] {
		t.Fatalf("result=%+v sawSystem=%v", result, fake.sawSystem)
	}
}

func TestInProcessKeepSystemPromptWhenFlagOff(t *testing.T) {
	pool := accounts.NewPool(nil, nil)
	pool.Upsert(accounts.Item{ID: "wb1", Provider: "workbuddy", Runtime: "in_process"})
	registry := providers.NewRegistry()
	fake := &systemObservingChat{}
	registry.Register(providers.Adapter{ID: "workbuddy", Chat: fake})

	ex := NewChatExecutor(pool, "")
	ex.Providers = registry
	_, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{
			{Role: "system", Content: "third-party identity"},
			{Role: "user", Content: "hi"},
		},
	}, "", "workbuddy")
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.sawSystem) != 1 || !fake.sawSystem[0] {
		t.Fatalf("system prompt must reach provider when the flag is off: %v", fake.sawSystem)
	}
}

type contentRejectedChat struct {
	calls int
}

func (f *contentRejectedChat) ChatNonStream(ctx context.Context, accountID string, req translate.ChatRequest) (providers.ChatOutcome, error) {
	f.calls++
	return providers.ChatOutcome{}, &workbuddy.ClassifiedError{Kind: accounts.KindInvalidRequest, Status: 400, Message: "sensitive content rejected"}
}

func (f *contentRejectedChat) ChatStream(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, error) {
	return nil, errors.New("stream unsupported in fake")
}

func TestInProcessContentRejectionDoesNotFailover(t *testing.T) {
	pool := accounts.NewPool(nil, nil)
	pool.Upsert(accounts.Item{ID: "wb1", Provider: "workbuddy", Runtime: "in_process"})
	pool.Upsert(accounts.Item{ID: "wb2", Provider: "workbuddy", Runtime: "in_process"})
	registry := providers.NewRegistry()
	fake := &contentRejectedChat{}
	registry.Register(providers.Adapter{ID: "workbuddy", Chat: fake})

	ex := NewChatExecutor(pool, "")
	ex.Providers = registry
	_, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "workbuddy")
	if err == nil {
		t.Fatal("expected the content rejection to surface to the caller")
	}
	if fake.calls != 1 {
		t.Fatalf("content rejection must not retry other accounts: calls=%d", fake.calls)
	}
	if item, ok := pool.ByID("wb1"); !ok || !item.DownUntil.IsZero() {
		t.Fatalf("rejected request must not put the account into cooldown: %+v", item)
	}
}

func TestInProcessFailoverRotatesAcrossWorkBuddyAccounts(t *testing.T) {
	pool := accounts.NewPool(nil, nil)
	pool.Upsert(accounts.Item{ID: "wb1", Provider: "workbuddy", Runtime: "in_process"})
	pool.Upsert(accounts.Item{ID: "wb2", Provider: "workbuddy", Runtime: "in_process"})
	registry := providers.NewRegistry()
	fake := &rateLimitedThenOKChat{}
	registry.Register(providers.Adapter{ID: "workbuddy", Chat: fake})

	ex := NewChatExecutor(pool, "")
	ex.Providers = registry
	result, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "workbuddy")
	if err != nil {
		t.Fatal(err)
	}
	if result.AccountID != "wb2" || result.Provider != "workbuddy" || result.Content != "OK-wb2" || fake.calls != 2 {
		t.Fatalf("result=%+v calls=%d", result, fake.calls)
	}
}

func TestAttemptsFollowProviderFilteredPool(t *testing.T) {
	pool := accounts.NewPool(nil, nil)
	pool.Upsert(accounts.Item{ID: "q1", URL: "http://a", Provider: "qoder", Runtime: "child_process"})
	pool.Upsert(accounts.Item{ID: "q2", URL: "http://b", Provider: "qoder", Runtime: "child_process"})
	pool.Upsert(accounts.Item{ID: "w1", Provider: "workbuddy", Runtime: "in_process"})
	if got := pool.LenRoute(accounts.RouteQuery{ProviderFilter: "qoder"}); got != 2 {
		t.Fatalf("qoder candidates=%d", got)
	}
	if got := pool.LenRoute(accounts.RouteQuery{ProviderFilter: "workbuddy"}); got != 1 {
		t.Fatalf("workbuddy candidates=%d", got)
	}
	if got := pool.LenRoute(accounts.RouteQuery{ProviderFilter: "qoder", Excluded: map[string]struct{}{"q1": {}}}); got != 1 {
		t.Fatalf("excluded candidates=%d", got)
	}
}

func TestProviderPickFiltersByProviderFamily(t *testing.T) {
	pool := accounts.NewPool([]string{"http://a"}, []string{"q1"})
	pool.Upsert(accounts.Item{ID: "w1", Provider: "workbuddy", Runtime: "in_process"})
	item, ok := pool.PickRoute(accounts.RouteQuery{ProviderFilter: "workbuddy"})
	if !ok || item.ID != "w1" {
		t.Fatalf("workbuddy pick=%+v ok=%v", item, ok)
	}
	if _, ok := pool.PickRoute(accounts.RouteQuery{ProviderFilter: "cursor"}); ok {
		t.Fatal("unknown provider family must not pick an account")
	}
}

var _ = json.RawMessage{}
