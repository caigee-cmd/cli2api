package workbuddy

import (
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func TestClassifyModel404IsModelNotAvailable(t *testing.T) {
	got := Classify(404, `{"message":"model not found"}`)
	if got.Kind != accounts.KindModelNotAvailable {
		t.Fatalf("404 must be model_not_available, got %+v", got)
	}
}

func TestClassifyRateLimitStaysRateLimit(t *testing.T) {
	// A 6004 usage-limit with an absolute reset is a time-window rate limit,
	// not a model entitlement problem: keep it rate_limit so the reset hint is
	// honored and only that model is cooled.
	got := Classify(429, `{"code":6004,"msg":"您的使用量已超出频率限制，将在 2026-09-01 13:56:47 UTC+8 重置"}`)
	if got.Kind != accounts.KindRateLimit {
		t.Fatalf("6004 must stay rate_limit, got %+v", got)
	}
}
