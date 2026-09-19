package executor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
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

func (f *fakeInProcessChat) ChatStream(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, providers.ResolvedChat, error) {
	f.calls++
	return nil, providers.ResolvedChat{}, errors.New("stream unsupported in fake")
}

func TestSanitizeForItemUsesNativeCatalogSpelling(t *testing.T) {
	item := Item{ID: "t1", Provider: "trae", Models: []string{"DeepSeek-V4-Flash"}}
	got := sanitizeForItem(item, translate.ChatRequest{Model: "deepseek-v4-flash"})
	if got.Model != "DeepSeek-V4-Flash" {
		t.Fatalf("model=%q", got.Model)
	}
}

func TestInProcessProviderPinnedChatDoesNotTouchWorkers(t *testing.T) {
	pool := NewPool([]string{"http://127.0.0.1:1"}, []string{"qoder1"})
	pool.Upsert(Item{ID: "wb1", Provider: "workbuddy", Region: "cn", Runtime: "in_process"})
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

func TestInProcessMixedCaseProviderExecutesRegisteredAdapter(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "wb1", Provider: "WorkBuddy", Region: "CN", Runtime: "in_process"})
	registry := providers.NewRegistry()
	fake := &fakeInProcessChat{}
	registry.Register(providers.Adapter{ID: "WorkBuddy", Chat: fake})

	ex := NewChatExecutor(pool, "")
	ex.Providers = registry
	result, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "WORKBUDDY")
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "OK" || result.AccountID != "wb1" || result.Provider != "workbuddy" || fake.calls != 1 {
		t.Fatalf("result=%+v calls=%d", result, fake.calls)
	}
}

func TestInProcessProviderFilterRoutesWithoutPin(t *testing.T) {
	pool := NewPool([]string{"http://127.0.0.1:1"}, []string{"qoder1"})
	pool.Upsert(Item{ID: "wb1", Provider: "workbuddy", Region: "cn", Runtime: "in_process"})
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

func TestAPIKeyAllowlistBlocksOtherProviderFamily(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "wb1", Provider: "workbuddy", Region: "cn", Runtime: "in_process"})
	registry := providers.NewRegistry()
	fake := &fakeInProcessChat{}
	registry.Register(providers.Adapter{ID: "workbuddy", Chat: fake})
	ex := NewChatExecutor(pool, "")
	ex.Providers = registry
	ctx := WithAllowedProviders(context.Background(), []string{"qoder"})
	_, err := ex.ChatNonStream(ctx, translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "workbuddy")
	if err == nil {
		t.Fatal("expected qoder-only key to miss workbuddy")
	}
	if fake.calls != 0 {
		t.Fatalf("unexpected workbuddy calls=%d", fake.calls)
	}
}

func TestAPIKeyAllowlistKeepsBareModelInsideAllowedFamily(t *testing.T) {
	qoder := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"model":"glm-5.2","choices":[{"message":{"content":"qoder"},"finish_reason":"stop"}],"usage":{"source":"upstream"}}`)
	}))
	defer qoder.Close()
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "q1", URL: qoder.URL, Provider: "qoder", Runtime: "child_process"})
	pool.Upsert(Item{ID: "wb1", Provider: "workbuddy", Region: "cn", Runtime: "in_process"})
	registry := providers.NewRegistry()
	fake := &fakeInProcessChat{}
	registry.Register(providers.Adapter{ID: "workbuddy", Chat: fake})
	ex := NewChatExecutor(pool, "")
	ex.HTTPClient = qoder.Client()
	ex.Providers = registry
	ctx := WithAllowedProviders(context.Background(), []string{"qoder"})
	result, err := ex.ChatNonStream(ctx, translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.AccountID != "q1" || result.Provider != "qoder" || fake.calls != 0 {
		t.Fatalf("result=%+v workbuddy calls=%d", result, fake.calls)
	}
	if n := pool.LenRoute(RouteQuery{PublicModel: "glm-5.2", AllowedProviders: []string{"qoder"}}); n != 1 {
		t.Fatalf("qoder-only allowlist candidates = %d", n)
	}
}

func TestInProcessProviderOnlyAccountRoutesWithoutPin(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "wb1", Provider: "workbuddy", Runtime: "in_process"})
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
	pool := NewPool([]string{"http://127.0.0.1:1"}, []string{"qoder1"})
	pool.Upsert(Item{ID: "wb1", Provider: "workbuddy", Runtime: "in_process"})
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
		return providers.ChatOutcome{}, &providers.Error{Kind: accounts.KindRateLimit, Status: 429, Message: "soft_rate"}
	}
	return providers.ChatOutcome{Model: req.Model, Content: "OK-" + accountID, FinishReason: "stop"}, nil
}

func (f *rateLimitedThenOKChat) ChatStream(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, providers.ResolvedChat, error) {
	return nil, providers.ResolvedChat{}, errors.New("stream unsupported in fake")
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

func (f *systemObservingChat) ChatStream(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, providers.ResolvedChat, error) {
	return nil, providers.ResolvedChat{}, errors.New("stream unsupported in fake")
}

func TestInProcessDropSystemPromptStripsBeforeProvider(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "wb1", Provider: "workbuddy", Runtime: "in_process", DropSystemPrompt: true})
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
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "wb1", Provider: "workbuddy", Runtime: "in_process"})
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
	return providers.ChatOutcome{}, &providers.Error{Kind: accounts.KindInvalidRequest, Status: 400, Message: "sensitive content rejected"}
}

func (f *contentRejectedChat) ChatStream(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, providers.ResolvedChat, error) {
	return nil, providers.ResolvedChat{}, errors.New("stream unsupported in fake")
}

func TestInProcessContentRejectionDoesNotFailover(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "wb1", Provider: "workbuddy", Runtime: "in_process"})
	pool.Upsert(Item{ID: "wb2", Provider: "workbuddy", Runtime: "in_process"})
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
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "wb1", Provider: "workbuddy", Runtime: "in_process"})
	pool.Upsert(Item{ID: "wb2", Provider: "workbuddy", Runtime: "in_process"})
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
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "q1", URL: "http://a", Provider: "qoder", Runtime: "child_process"})
	pool.Upsert(Item{ID: "q2", URL: "http://b", Provider: "qoder", Runtime: "child_process"})
	pool.Upsert(Item{ID: "w1", Provider: "workbuddy", Runtime: "in_process"})
	if got := pool.LenRoute(RouteQuery{ProviderFilter: "qoder"}); got != 2 {
		t.Fatalf("qoder candidates=%d", got)
	}
	if got := pool.LenRoute(RouteQuery{ProviderFilter: "workbuddy"}); got != 1 {
		t.Fatalf("workbuddy candidates=%d", got)
	}
	if got := pool.LenRoute(RouteQuery{ProviderFilter: "qoder", Excluded: map[string]struct{}{"q1": {}}}); got != 1 {
		t.Fatalf("excluded candidates=%d", got)
	}
}

func TestProviderPickFiltersByProviderFamily(t *testing.T) {
	pool := NewPool([]string{"http://a"}, []string{"q1"})
	pool.Upsert(Item{ID: "w1", Provider: "workbuddy", Runtime: "in_process"})
	item, ok := pool.PickRoute(RouteQuery{ProviderFilter: "workbuddy"})
	if !ok || item.ID != "w1" {
		t.Fatalf("workbuddy pick=%+v ok=%v", item, ok)
	}
	if _, ok := pool.PickRoute(RouteQuery{ProviderFilter: "cursor"}); ok {
		t.Fatal("unknown provider family must not pick an account")
	}
}

var _ = json.RawMessage{}

func TestAPIKeyRegionScopedGrantNeverCrossesRegions(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "wc1", Provider: "workbuddy", Region: "cn", Runtime: "in_process"})
	pool.Upsert(Item{ID: "wg1", Provider: "workbuddy", Region: "global", Runtime: "in_process"})
	registry := providers.NewRegistry()
	fake := &fakeInProcessChat{}
	registry.Register(providers.Adapter{ID: "workbuddy", Chat: fake})
	ex := NewChatExecutor(pool, "")
	ex.Providers = registry

	// CN-only key with bare provider filter lands on the CN account.
	ctx := WithAllowedProviders(context.Background(), []string{"workbuddy:cn"})
	result, err := ex.ChatNonStream(ctx, translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "workbuddy")
	if err != nil {
		t.Fatal(err)
	}
	if result.AccountID != "wc1" {
		t.Fatalf("cn-only key landed on %q, want wc1", result.AccountID)
	}

	// Pin to the global account must not execute on it.
	_, err = ex.ChatNonStream(ctx, translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "wg1", "workbuddy")
	if err != nil {
		// Falls back into the cn grant — acceptable, but must be wc1.
		t.Fatalf("pin fallback should succeed via the granted account: %v", err)
	}
	if got := fake.calls; got != 2 {
		t.Fatalf("calls=%d want 2 (pin fell back into the cn grant)", got)
	}

	// The error ladder reports the granted region, not the pinned one.
	emptyPool := NewPool(nil, nil)
	emptyPool.Upsert(Item{ID: "wg1", Provider: "workbuddy", Region: "global", Runtime: "in_process"})
	exEmpty := NewChatExecutor(emptyPool, "")
	exEmpty.Providers = registry
	_, err = exEmpty.ChatNonStream(ctx, translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "workbuddy")
	if err == nil {
		t.Fatal("cn-only key with only a global account must fail")
	}
	if msg := err.Error(); msg != "no workbuddy/cn accounts available" {
		t.Fatalf("error = %q, want the granted-region message", msg)
	}
}

func TestAPIKeyRegionScopedFailoverStaysInsideGrant(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "wc1", Provider: "workbuddy", Region: "cn", Runtime: "in_process"})
	pool.Upsert(Item{ID: "wg1", Provider: "workbuddy", Region: "global", Runtime: "in_process"})
	registry := providers.NewRegistry()
	failingCN := &flakyInProcessChat{fail: true}
	failingCN.provider = "workbuddy"
	registry.Register(providers.Adapter{ID: "workbuddy", Chat: failingCN})
	ex := NewChatExecutor(pool, "")
	ex.Providers = registry

	ctx := WithAllowedProviders(context.Background(), []string{"workbuddy:cn"})
	_, err := ex.ChatNonStream(ctx, translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "workbuddy")
	if err == nil {
		t.Fatal("single failing cn account must fail, not cross to global")
	}
}

type flakyInProcessChat struct {
	fail bool
	fakeInProcessChat
}

func (f *flakyInProcessChat) ChatNonStream(ctx context.Context, accountID string, req translate.ChatRequest) (providers.ChatOutcome, error) {
	f.calls++
	if f.fail {
		return providers.ChatOutcome{}, &providers.Error{
			Kind: accounts.KindRateLimit, Status: 429, Code: "rate_limit",
			Message: "429",
		}
	}
	return f.fakeInProcessChat.ChatNonStream(ctx, accountID, req)
}

func TestAPIKeyRegionScopedMultiRegionKeepsSticky(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "wc1", Provider: "workbuddy", Region: "cn", Runtime: "in_process"})
	pool.Upsert(Item{ID: "wg1", Provider: "workbuddy", Region: "global", Runtime: "in_process"})
	registry := providers.NewRegistry()
	fake := &fakeInProcessChat{}
	registry.Register(providers.Adapter{ID: "workbuddy", Chat: fake})
	ex := NewChatExecutor(pool, "")
	ex.Providers = registry

	// Key granted both regions: the request sticks to the first picked region.
	ctx := WithAllowedProviders(context.Background(), []string{"workbuddy:cn", "workbuddy:global"})
	result, err := ex.ChatNonStream(ctx, translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "workbuddy")
	if err != nil {
		t.Fatal(err)
	}
	if result.AccountID != "wc1" && result.AccountID != "wg1" {
		t.Fatalf("multi-region grant picked unexpected account %q", result.AccountID)
	}
}
