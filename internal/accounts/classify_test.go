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

func TestClassifyModelNotAvailableDoesNotCooldown(t *testing.T) {
	got := Classify(400, `{"error":{"message":"model_not_available: hy3 is not available for this Qoder account","code":"model_not_available"}}`, "", "", "")
	if got.Kind != KindModelNotAvailable || !got.Failover || got.Cooldown != 0 || got.Status != 400 {
		t.Fatalf("got %+v", got)
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
