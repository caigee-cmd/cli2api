package executor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

func TestClassifyPromptLimitDoesNotCoolAccount(t *testing.T) {
	got := Classify(500, `{"error":{"code":"insufficient_quota","message":"token-limit"}}`, "", "", "")
	if got.Kind != KindInvalidRequest || got.Failover || got.Status != 400 || got.Cooldown != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestNextLocalMidnightCooldown(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2026, time.September, 4, 23, 45, 0, 0, loc)
	if got := NextLocalMidnightCooldownAt(now); got != 15*time.Minute {
		t.Fatalf("cooldown=%s", got)
	}

	utc := time.Date(2026, time.September, 4, 23, 45, 0, 0, time.UTC)
	if got := NextLocalMidnightCooldownAt(utc); got != 15*time.Minute {
		t.Fatalf("utc cooldown=%s", got)
	}
}

func TestClassifyHardQuotaDoesNotFailover(t *testing.T) {
	got := Classify(429, `{"error":{"code":"insufficient_quota","message":"account quota exhausted"}}`, "", "", "")
	if got.Kind != KindQuota || got.Failover || got.Status != 429 || got.Cooldown <= 0 || got.Cooldown > 24*time.Hour {
		t.Fatalf("got %#v", got)
	}
}

func TestClassifyCodeBuddyQuotaExhausted(t *testing.T) {
	got := Classify(400, `{"error":{"data":{"code":14018,"msg":"额度已用尽，请购买加量包"}}}`, "", "", "")
	if got.Kind != KindQuota || got.Failover || got.Status != 429 || got.Cooldown <= 0 || got.Cooldown > 24*time.Hour {
		t.Fatalf("got %+v", got)
	}
}

func TestClassifyRateLimitHonorsRetryAfter(t *testing.T) {
	got := Classify(429, "too many requests", "90", "", "")
	if got.Kind != KindRateLimit || !got.Failover || got.Cooldown != 90*time.Second {
		t.Fatalf("got %+v", got)
	}
}

func TestClassifyErrorClampsProviderRateLimitCooldown(t *testing.T) {
	got := ClassifyError(&providers.Error{
		Kind: KindRateLimit, Status: 429, Message: "slow down", RetryAfter: 5 * time.Second,
	})
	if got.Kind != KindRateLimit || got.Cooldown != 30*time.Second || got.RetryAfter != 30*time.Second {
		t.Fatalf("got %+v", got)
	}
}

func TestClassifyErrorUsesTraeHardRateCode(t *testing.T) {
	got := ClassifyError(&providers.Error{
		Kind: KindRateLimit, Status: 429, Message: "hard rate limit", Code: "4011",
	})
	if got.Kind != KindRateLimit || !got.Failover || got.Cooldown != 5*time.Minute {
		t.Fatalf("4011 cooldown must be executor-owned, got %+v", got)
	}
}

func TestClassifyErrorKeepsJSONRetryAfterWhenCodeIsSet(t *testing.T) {
	got := ClassifyError(&providers.Error{
		Kind: KindRateLimit, Status: 429, Code: "429",
		Message: `{"code":429,"message":"slow down","retry_after":120}`,
	})
	if got.Kind != KindRateLimit || got.Cooldown != 120*time.Second || got.RetryAfter != 120*time.Second {
		t.Fatalf("retry_after JSON must survive a separate code field, got %+v", got)
	}
}

func TestClassifyErrorIgnoresProviderFailoverAndAuthCooldown(t *testing.T) {
	got := ClassifyError(&providers.Error{
		Kind: KindQuota, Status: 429, Message: "plan exhausted", RetryAfter: time.Minute,
	})
	if got.Kind != KindQuota || got.Failover || got.Cooldown <= 0 || got.Cooldown > 24*time.Hour {
		t.Fatalf("quota must not fail over and must use local midnight, got %+v", got)
	}

	got = ClassifyError(&providers.Error{
		Kind: KindAuth, Status: 401, Message: "session dead", RetryAfter: 30 * time.Minute,
	})
	if got.Kind != KindAuth || !got.Failover || got.Cooldown != 30*time.Second {
		t.Fatalf("auth cooldown/failover must be executor-owned, got %+v", got)
	}
}

func TestClassifyAuth(t *testing.T) {
	got := Classify(403, "unauthorized credential", "", "", "")
	if got.Kind != KindAuth || !got.Failover || got.Status != 403 {
		t.Fatalf("got %+v", got)
	}
}

func TestClassifyFailoverHintOverrides(t *testing.T) {
	got := Classify(429, "too many requests", "10", KindRateLimit, "0")
	if got.Failover {
		t.Fatalf("hint 0 should disable failover: %+v", got)
	}
}

func TestClassifyModelNotAvailableDoesNotCooldown(t *testing.T) {
	got := Classify(400, `{"error":{"message":"model_not_available: hy3 is not available for this Qoder account","code":"model_not_available"}}`, "", "", "")
	if got.Kind != KindModelNotAvailable || !got.Failover || got.Cooldown != 0 || got.Status != 400 {
		t.Fatalf("got %+v", got)
	}
}

func TestClassifyModelCatalogUnavailablePreservesDistinctCode(t *testing.T) {
	got := Classify(400, `{"error":{"message":"model_catalog_unavailable: dynamic model catalog is unavailable","code":"model_not_available"}}`, "", "", "")
	if got.Kind != KindModelNotAvailable || !got.Failover || got.Cooldown != 0 {
		t.Fatalf("got %#v", got)
	}
	if got.Status != 503 || got.Code != "model_catalog_unavailable" || got.Type != "api_error" {
		t.Fatalf("catalog outage must remain distinguishable, got %#v", got)
	}
}

func TestClassifyTraePlanLimitIsQuota(t *testing.T) {
	got := Classify(0, `{"code":1005,"message":""}`, "", "", "")
	if got.Kind != KindQuota || got.Failover || got.Status != 429 {
		t.Fatalf("got %+v", got)
	}
}

func TestClassifyContentScreeningStaysRequestLevel(t *testing.T) {
	for _, body := range []string{"sensitive content rejected", "内容包含敏感信息"} {
		got := Classify(400, body, "", "", "")
		if got.Kind != KindInvalidRequest || got.Failover || got.Cooldown != 0 || got.Status != 400 {
			t.Fatalf("body=%q got %+v", body, got)
		}
	}
}

func TestParseRetryAfterSupportsDurationAndDateFormats(t *testing.T) {
	got := ParseRetryAfter("708.717057ms", time.Minute)
	if got < 708*time.Millisecond || got > 709*time.Millisecond {
		t.Fatalf("duration=%v", got)
	}

	future := time.Now().Add(45 * time.Second).UTC()
	for _, raw := range []string{future.Format(time.RFC3339), future.Format(http.TimeFormat)} {
		got = ParseRetryAfter(raw, 0)
		if got < 40*time.Second || got > 46*time.Second {
			t.Fatalf("raw=%q date duration=%v", raw, got)
		}
	}
}

func TestParseRetryAfterSupportsUnixSecondsAndMilliseconds(t *testing.T) {
	future := time.Now().Add(45 * time.Second)
	for _, raw := range []string{
		strconv.FormatInt(future.Unix(), 10),
		strconv.FormatInt(future.UnixMilli(), 10),
	} {
		got := ParseRetryAfter(raw, 0)
		if got < 40*time.Second || got > 46*time.Second {
			t.Fatalf("raw=%q duration=%v", raw, got)
		}
	}
}

func TestClassifyRateLimitUsesBodyHintAndMinimumCooldown(t *testing.T) {
	got := Classify(400, `{"error":{"code":"RESOURCE_EXHAUSTED","message":"busy","quotaResetDelay":"708.717057ms"}}`, "", "", "")
	if got.Kind != KindRateLimit || !got.Failover || got.Status != 429 || got.Cooldown != 30*time.Second {
		t.Fatalf("got %+v", got)
	}
}

func TestClassifyErrorKeepsExecutionErrorClassification(t *testing.T) {
	want := Classified{
		Kind: KindRateLimit, Status: 429, Code: "rate_limit", Type: "api_error",
		Message: "all accounts at capacity", Failover: true,
		Cooldown: 5 * time.Second, RetryAfter: 5 * time.Second,
	}
	got := ClassifyError(NewExecutionError(want, errors.New("wrapped")))
	if got != want {
		t.Fatalf("execution error was reclassified: %+v", got)
	}
}

func TestStreamReadErrorClassifiesProviderOnce(test *testing.T) {
	original := &providers.Error{Kind: KindQuota, Status: 429, Code: "quota_exhausted", Message: "quota exhausted"}
	wrapped := fmt.Errorf("Connect trailer: %w", original)
	got := StreamReadError(wrapped)
	var executionErr *ExecutionError
	if !errors.As(got, &executionErr) {
		test.Fatalf("stream error was not classified: %T %v", got, got)
	}
	var providerErr *providers.Error
	if !errors.As(got, &providerErr) || providerErr != original || !errors.Is(got, wrapped) {
		test.Fatalf("original error chain was lost: %v", got)
	}
	want := executionErr.Classified
	if want.Kind != KindQuota || want.Status != 429 || want.Failover || want.Cooldown <= 0 {
		test.Fatalf("classification=%+v", want)
	}
	original.Kind = KindAuth
	original.Status = 401
	if classified := ClassifyError(got); classified != want {
		test.Fatalf("classification changed with upstream error: %+v want %+v", classified, want)
	}
}

func TestStreamReadErrorPreservesExecutionErrorWrapper(test *testing.T) {
	original := errors.New("read failure")
	want := Classified{Kind: KindRateLimit, Status: 429, RetryAfter: 5 * time.Second, Model: "swe-2"}
	wrapped := fmt.Errorf("stream wrapper: %w", NewExecutionError(want, original))
	got := StreamReadError(wrapped)
	if got != wrapped || !errors.Is(got, original) || ClassifyError(got) != want {
		test.Fatalf("execution error wrapper changed: %v", got)
	}
}

func TestStreamReadErrorPreservesCancellation(test *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, scenario := range []struct {
			name string
			err  error
		}{
			{"direct", cause},
			{"wrapped", fmt.Errorf("stream read: %w", cause)},
			{"joined", errors.Join(&providers.Error{Kind: KindUnavailable, Status: 502, Message: "stream closed"}, cause)},
		} {
			test.Run(cause.Error()+"/"+scenario.name, func(test *testing.T) {
				got := StreamReadError(scenario.err)
				if !errors.Is(got, cause) || !errors.Is(got, scenario.err) {
					test.Fatalf("cancellation cause was lost: %v", got)
				}
				var executionErr *ExecutionError
				if !errors.As(got, &executionErr) {
					test.Fatalf("cancellation was not classified: %T", got)
				}
				classified := ClassifyError(got)
				if classified.Kind != KindCanceled || classified.Status != 499 || classified.Failover || classified.Cooldown != 0 || classified.RetryAfter != 0 {
					test.Fatalf("cancellation classification=%+v", classified)
				}
			})
		}
	}
}

func TestObserveStreamFailureIgnoresCancellation(test *testing.T) {
	for _, failure := range []error{
		context.Canceled,
		fmt.Errorf("stream read: %w", context.DeadlineExceeded),
		&providers.Error{Kind: KindCanceled, Status: 499, Message: "canceled"},
		NewExecutionError(Classified{Kind: KindCanceled, Status: 499}, nil),
	} {
		test.Run(failure.Error(), func(test *testing.T) {
			pool := NewPool(nil, nil)
			pool.Upsert(Item{ID: "healthy"})
			before, _ := pool.ByID("healthy")
			NewChatExecutor(pool, "").ObserveStreamFailure("healthy", failure, "swe-2")
			after, _ := pool.ByID("healthy")
			if after.LastKind != "" || !after.DownUntil.IsZero() || len(after.ModelDownUntil) != 0 || after.StateVersion != before.StateVersion {
				test.Fatalf("cancellation mutated pool state: %+v", after)
			}
		})
	}
}
