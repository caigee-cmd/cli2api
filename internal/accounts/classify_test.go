package accounts

import (
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestClassifyQuotaDoesNotFailover(t *testing.T) {
	got := Classify(500, `{"error":{"code":"insufficient_quota","message":"token-limit"}}`, "", "", "")
	if got.Kind != KindQuota || got.Failover || got.Status != 429 {
		t.Fatalf("got %#v", got)
	}
}

func TestClassifyCodeBuddyQuotaExhausted(t *testing.T) {
	got := Classify(400, `{"error":{"data":{"code":14018,"msg":"额度已用尽，请购买加量包"}}}`, "", "", "")
	if got.Kind != KindQuota || got.Failover || got.Status != 429 || got.Cooldown != time.Hour {
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
