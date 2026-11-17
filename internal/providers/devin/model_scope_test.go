package devin

import (
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func TestClassifyResourceExhaustedStaysModelRateLimit(t *testing.T) {
	// resource_exhausted is a model-level resource-pool signal. Even though the
	// text mentions credits, it must not be promoted to account-level quota.
	got := Classify(429, `{"message":"RESOURCE_EXHAUSTED: model credit pool exhausted for gpt-x"}`)
	if got.Kind != accounts.KindRateLimit {
		t.Fatalf("resource_exhausted must stay rate_limit, got %+v", got)
	}
}

func TestClassifyAccountUsageQuotaIsQuota(t *testing.T) {
	got := Classify(429, `{"message":"your daily usage quota is exhausted"}`)
	if got.Kind != accounts.KindQuota {
		t.Fatalf("account usage quota must be quota, got %+v", got)
	}
}

func TestClassifyModel404IsModelNotAvailable(t *testing.T) {
	got := Classify(404, `{"message":"model not found"}`)
	if got.Kind != accounts.KindModelNotAvailable {
		t.Fatalf("404 must be model_not_available, got %+v", got)
	}
}
