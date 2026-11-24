package executor

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// scriptedChat fails or succeeds per call index so a test can express
// "first attempt hiccups, second succeeds" without a real upstream.
type scriptedChat struct {
	mu    sync.Mutex
	calls int
	fn    func(call int) (providers.ChatOutcome, error)
}

func (s *scriptedChat) ChatNonStream(_ context.Context, _ string, req translate.ChatRequest) (providers.ChatOutcome, error) {
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.mu.Unlock()
	return s.fn(call)
}

func (s *scriptedChat) ChatStream(context.Context, string, translate.ChatRequest) (*http.Response, providers.ResolvedChat, error) {
	return nil, providers.ResolvedChat{}, errors.New("stream not scripted")
}

func (s *scriptedChat) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func inProcessExecutor(t *testing.T, chat providers.ProviderChat, items ...Item) ChatExecutor {
	t.Helper()
	pool := NewPool(nil, nil)
	for _, item := range items {
		if item.Provider == "" {
			item.Provider = "workbuddy"
		}
		if item.Region == "" {
			item.Region = "cn"
		}
		if item.Runtime == "" {
			item.Runtime = string(providers.RuntimeInProcess)
		}
		pool.Upsert(item)
	}
	registry := providers.NewRegistry()
	registry.Register(providers.Adapter{ID: "workbuddy", Chat: chat})
	ex := NewChatExecutor(pool, "")
	ex.Providers = registry
	return ex
}

func okOutcome(req translate.ChatRequest) (providers.ChatOutcome, error) {
	return providers.ChatOutcome{Model: req.Model, Content: "ok", FinishReason: "stop"}, nil
}

func emptyStreamErr() error {
	return &providers.Error{
		Kind: accounts.KindUnavailable, Status: 200,
		Message: "upstream answered 200 but the stream was empty", Code: "upstream_empty",
	}
}

func TestTransientFailureRetriesSameAccountBeforeFailover(t *testing.T) {
	chat := &scriptedChat{fn: func(call int) (providers.ChatOutcome, error) {
		if call == 1 {
			return providers.ChatOutcome{}, emptyStreamErr()
		}
		return providers.ChatOutcome{Model: "glm-5.2", Content: "ok", FinishReason: "stop"}, nil
	}}
	ex := inProcessExecutor(t, chat, Item{ID: "wb1"})

	ctx := WithSessionKey(context.Background(), "retry-session")
	result, err := ex.ChatNonStream(ctx, translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if result.AccountID != "wb1" {
		t.Fatalf("account = %q, want wb1", result.AccountID)
	}
	// The retry must not consume the failover budget: this request had exactly
	// one candidate account.
	if result.AttemptCount != 1 {
		t.Fatalf("attempt count = %d, want 1", result.AttemptCount)
	}
	if chat.Calls() != 2 {
		t.Fatalf("adapter calls = %d, want 2", chat.Calls())
	}
	if bound, ok := ex.SessionAffinity.Get("retry-session"); !ok || bound != "wb1" {
		t.Fatalf("binding = %q ok=%v, want wb1", bound, ok)
	}
}

func TestRateLimitIsNotRetriedOnSameAccount(t *testing.T) {
	chat := &scriptedChat{fn: func(int) (providers.ChatOutcome, error) {
		return providers.ChatOutcome{}, &providers.Error{
			Kind: accounts.KindRateLimit, Status: 429,
			Message: "too many requests", Code: "rate_limit", RetryAfter: time.Minute,
		}
	}}
	ex := inProcessExecutor(t, chat, Item{ID: "wb1"})

	_, err := ex.ChatNonStream(context.Background(), translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	if chat.Calls() != 1 {
		t.Fatalf("adapter calls = %d, want 1 (no same-account retry for 429)", chat.Calls())
	}
}

func TestTransientFailureDoesNotEscalateBackoff(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "a"})
	ex := NewChatExecutor(pool, "")

	for i := 0; i < 4; i++ {
		ex.markClassified("a", Classified{
			Kind: accounts.KindUnavailable, Cooldown: 10 * time.Second, Transient: true,
		}, "")
	}
	item, ok := pool.ByID("a")
	if !ok {
		t.Fatal("account missing")
	}
	if item.BackoffLevel != 0 {
		t.Fatalf("backoff level = %d, want 0 for transient failures", item.BackoffLevel)
	}
	if remaining := time.Until(item.DownUntil); remaining > 15*time.Second {
		t.Fatalf("cooldown = %s, want the classifier window, not an escalated one", remaining)
	}
}

func TestExactResetIsNotMultiplied(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "a"})
	ex := NewChatExecutor(pool, "")

	for i := 0; i < 4; i++ {
		ex.markClassified("a", Classified{
			Kind: accounts.KindQuota, Cooldown: 15 * time.Minute, ExactReset: true,
		}, "")
	}
	item, ok := pool.ByID("a")
	if !ok {
		t.Fatal("account missing")
	}
	if item.BackoffLevel != 0 {
		t.Fatalf("backoff level = %d, want 0 for an authoritative reset", item.BackoffLevel)
	}
	if remaining := time.Until(item.DownUntil); remaining > 16*time.Minute {
		t.Fatalf("cooldown = %s, want the upstream reset window", remaining)
	}
}

func TestDurableAuthFailureStillEscalates(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "a"})
	ex := NewChatExecutor(pool, "")

	for i := 0; i < 4; i++ {
		ex.markClassified("a", Classified{Kind: accounts.KindAuth, Cooldown: 30 * time.Second}, "")
	}
	item, ok := pool.ByID("a")
	if !ok {
		t.Fatal("account missing")
	}
	if remaining := time.Until(item.DownUntil); remaining <= 30*time.Second {
		t.Fatalf("cooldown = %s, want escalation above the base window", remaining)
	}
}

func TestShortCooldownKeepsAnchorAndReturnsToOriginal(t *testing.T) {
	chat := &scriptedChat{fn: func(int) (providers.ChatOutcome, error) {
		return providers.ChatOutcome{Model: "glm-5.2", Content: "ok", FinishReason: "stop"}, nil
	}}
	ex := inProcessExecutor(t, chat, Item{ID: "a"}, Item{ID: "b"})
	ex.SessionAffinity.Bind("cache-session", "a")
	// A short cooling window keeps a's prompt cache plausibly warm.
	ex.Pool.MarkClassified("a", Classified{Kind: accounts.KindRateLimit, Cooldown: 30 * time.Second, Model: "glm-5.2"})

	ctx := WithSessionKey(context.Background(), "cache-session")
	first, err := ex.ChatNonStream(ctx, translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if first.AccountID != "b" {
		t.Fatalf("served by %q, want the healthy sibling b", first.AccountID)
	}
	if bound, ok := ex.SessionAffinity.Get("cache-session"); !ok || bound != "a" {
		t.Fatalf("binding = %q ok=%v, want the original anchor a", bound, ok)
	}

	// Once the cooldown lifts, the chat returns to its original account.
	ex.Pool.MarkOK("a", "glm-5.2")
	second, err := ex.ChatNonStream(ctx, translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if second.AccountID != "a" || second.Routing != routingSticky {
		t.Fatalf("second = %+v, want sticky back on a", second)
	}
}

func TestLongCooldownReanchorsToServingAccount(t *testing.T) {
	chat := &scriptedChat{fn: func(int) (providers.ChatOutcome, error) {
		return providers.ChatOutcome{Model: "glm-5.2", Content: "ok", FinishReason: "stop"}, nil
	}}
	ex := inProcessExecutor(t, chat, Item{ID: "a"}, Item{ID: "b"})
	ex.SessionAffinity.Bind("long-session", "a")
	ex.Pool.MarkClassified("a", Classified{Kind: accounts.KindAuth, Cooldown: time.Hour})

	_, err := ex.ChatNonStream(WithSessionKey(context.Background(), "long-session"), translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// A one-hour wait outlives the cached prefix, so the chat follows the
	// account that actually served it.
	if bound, ok := ex.SessionAffinity.Get("long-session"); !ok || bound != "b" {
		t.Fatalf("binding = %q ok=%v, want b", bound, ok)
	}
}

func TestZeroCooldownFailoverStillForgets(t *testing.T) {
	chat := &scriptedChat{fn: func(call int) (providers.ChatOutcome, error) {
		if call <= 2 {
			return providers.ChatOutcome{}, emptyStreamErr()
		}
		return providers.ChatOutcome{Model: "glm-5.2", Content: "ok", FinishReason: "stop"}, nil
	}}
	// Both accounts share one adapter, so account a fails on its retry and
	// then the failover to b succeeds.
	ex := inProcessExecutor(t, chat, Item{ID: "a"}, Item{ID: "b"})
	ex.SessionAffinity.Bind("cold-stream", "a")

	result, err := ex.ChatNonStream(WithSessionKey(context.Background(), "cold-stream"), translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if result.AccountID != "b" {
		t.Fatalf("account = %q, want b", result.AccountID)
	}
	if bound, ok := ex.SessionAffinity.Get("cold-stream"); !ok || bound != "b" {
		t.Fatalf("binding = %q ok=%v, want b after a zero-cooldown failure", bound, ok)
	}
}

func TestQuotaResetHintIsNotClampedToRateLimitWindow(t *testing.T) {
	body := `{"error":{"message":"insufficient_quota","code":"insufficient_quota"},"resets_in_seconds":7200}`
	classified := Classify(429, body, "", "", "")
	if classified.Kind != accounts.KindQuota {
		t.Fatalf("kind = %q, want quota", classified.Kind)
	}
	if classified.Cooldown < 90*time.Minute {
		t.Fatalf("cooldown = %s, want the full upstream reset window", classified.Cooldown)
	}
	if !classified.ExactReset {
		t.Fatal("quota reset from upstream must be marked ExactReset")
	}
}

func TestQuotaWithoutHintFallsBackToLocalMidnight(t *testing.T) {
	classified := Classify(429, `{"error":{"message":"insufficient_quota","code":"insufficient_quota"}}`, "", "", "")
	if classified.Kind != accounts.KindQuota {
		t.Fatalf("kind = %q, want quota", classified.Kind)
	}
	if classified.Cooldown <= 0 {
		t.Fatalf("cooldown = %s, want the local-midnight fallback", classified.Cooldown)
	}
	if classified.ExactReset {
		t.Fatal("a guessed midnight fallback must not claim an exact reset")
	}
}

func TestSessionAffinityReportsReasonDistributions(t *testing.T) {
	affinity := NewSessionAffinity(time.Minute, 32)
	affinity.Bind("one", "a")
	if _, ok := affinity.Get("one"); !ok {
		t.Fatal("expected a hit")
	}
	affinity.Get("missing")
	// Shrink the TTL before binding so the next binding is already expiring.
	affinity.ttl = time.Millisecond
	affinity.Bind("short", "a")
	time.Sleep(5 * time.Millisecond)
	affinity.Get("short")
	affinity.RecordEscape("provider_cooldown")
	affinity.RecordEscape("provider_cooldown")
	affinity.RecordEscape("concurrency_saturated")

	stats := affinity.Stats()
	if stats.MissReasons["not_found"] != 1 || stats.MissReasons["expired"] != 1 {
		t.Fatalf("miss reasons = %v", stats.MissReasons)
	}
	if stats.EscapeReasons["provider_cooldown"] != 2 || stats.EscapeReasons["concurrency_saturated"] != 1 {
		t.Fatalf("escape reasons = %v", stats.EscapeReasons)
	}
	if stats.Hits != 1 || stats.Misses != 2 || stats.Escapes != 3 {
		t.Fatalf("counters = hits %d misses %d escapes %d", stats.Hits, stats.Misses, stats.Escapes)
	}
}

func TestPeekDoesNotCountAsHit(t *testing.T) {
	affinity := NewSessionAffinity(time.Minute, 8)
	affinity.Bind("s", "a")
	if anchor, ok := affinity.Peek("s"); !ok || anchor != "a" {
		t.Fatalf("peek = %q ok=%v", anchor, ok)
	}
	if stats := affinity.Stats(); stats.Hits != 0 && stats.Misses != 0 {
		t.Fatalf("peek changed routing counters: %+v", stats)
	}
}

func TestResolveSessionAffinityConfigDefaultsAndOverrides(t *testing.T) {
	t.Setenv("CLI2API_AFFINITY_TTL", "")
	t.Setenv("CLI2API_AFFINITY_CAPACITY", "")
	ttl, capacity := ResolveSessionAffinityConfig()
	if ttl != 4*time.Hour || capacity != 10_000 {
		t.Fatalf("defaults = %s/%d", ttl, capacity)
	}

	t.Setenv("CLI2API_AFFINITY_TTL", "90m")
	t.Setenv("CLI2API_AFFINITY_CAPACITY", "2048")
	ttl, capacity = ResolveSessionAffinityConfig()
	if ttl != 90*time.Minute || capacity != 2048 {
		t.Fatalf("override = %s/%d", ttl, capacity)
	}

	// Bare seconds stay supported, invalid values fall back, and the ceiling
	// keeps a typo from making bindings effectively permanent.
	t.Setenv("CLI2API_AFFINITY_TTL", "7200")
	if ttl, _ = ResolveSessionAffinityConfig(); ttl != 2*time.Hour {
		t.Fatalf("seconds override = %s", ttl)
	}
	t.Setenv("CLI2API_AFFINITY_TTL", "not-a-duration")
	if ttl, _ = ResolveSessionAffinityConfig(); ttl != 4*time.Hour {
		t.Fatalf("invalid override = %s", ttl)
	}
	t.Setenv("CLI2API_AFFINITY_TTL", "720h")
	if ttl, _ = ResolveSessionAffinityConfig(); ttl != maxSessionAffinityTTL {
		t.Fatalf("ceiling = %s", ttl)
	}
}

func TestStickyCoolingEscapesOnlyWhenWaitIsLongAndFamilyDiffers(t *testing.T) {
	chat := &scriptedChat{fn: func(int) (providers.ChatOutcome, error) {
		return providers.ChatOutcome{Model: "shared", Content: "ok", FinishReason: "stop"}, nil
	}}
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "q1", Provider: "qoder", Region: "global", Runtime: string(providers.RuntimeInProcess), Models: []string{"shared"}})
	pool.Upsert(Item{ID: "w1", Provider: "workbuddy", Region: "cn", Runtime: string(providers.RuntimeInProcess), Models: []string{"shared"}})
	registry := providers.NewRegistry()
	registry.Register(providers.Adapter{ID: "qoder", Chat: chat})
	registry.Register(providers.Adapter{ID: "workbuddy", Chat: chat})
	ex := NewChatExecutor(pool, "")
	ex.Providers = registry
	ex.SessionAffinity.Bind("cooling-session", "q1")

	// A short rate-limit window is better served by Retry-After: a cross-provider
	// hop would lose the prompt cache for a wait the client can absorb.
	pool.MarkClassified("q1", Classified{Kind: accounts.KindRateLimit, Cooldown: 20 * time.Second, Model: "shared"})
	_, err := ex.ChatNonStream(WithSessionKey(context.Background(), "cooling-session"), translate.ChatRequest{
		Model: "shared", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	var execution *ExecutionError
	if !errors.As(err, &execution) || execution.Classified.Status != 429 {
		t.Fatalf("short cooldown err = %v, want a 429 retry-after", err)
	}

	// A quota wait (typically until local midnight) would leave the chat dead
	// while another provider is idle, so escaping is correct there.
	pool.MarkClassified("q1", Classified{Kind: accounts.KindQuota, Cooldown: 30 * time.Minute})
	result, err := ex.ChatNonStream(WithSessionKey(context.Background(), "cooling-session"), translate.ChatRequest{
		Model: "shared", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}, "", "")
	if err != nil {
		t.Fatalf("long cooldown err = %v", err)
	}
	if result.AccountID != "w1" || result.Routing != routingStickyEscape {
		t.Fatalf("result = %+v, want escaped to w1", result)
	}
}

func TestKeyedIdentityStillIsolatesBindings(t *testing.T) {
	if got := SessionKeyFor("h", auth.ConsoleIdentity(), translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}); got == "" {
		t.Fatal("empty session key")
	}
}
