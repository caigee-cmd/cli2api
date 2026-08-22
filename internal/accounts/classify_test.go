package accounts

import (
	"testing"
	"time"
)

func TestClassifyQuotaDoesNotFailover(t *testing.T) {
	got := Classify(500, `{"error":{"code":"insufficient_quota","message":"token-limit"}}`, "", "", "")
	if got.Kind != KindQuota || got.Failover || got.Status != 429 {
		t.Fatalf("got %+v", got)
	}
}

func TestClassifyRateLimitHonorsRetryAfter(t *testing.T) {
	got := Classify(429, "too many requests", "90", "", "")
	if got.Kind != KindRateLimit || !got.Failover || got.Cooldown != 90*time.Second {
		t.Fatalf("got %+v", got)
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
