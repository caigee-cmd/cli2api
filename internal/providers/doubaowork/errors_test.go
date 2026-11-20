package doubaowork

import (
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

func TestClassifyTable(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		kind   string
	}{
		{"login expired", 200, `{"code":710012001,"message":"login invalid"}`, accounts.KindAuth},
		{"user invalid", 200, `{"code":710012000,"message":"user invalid"}`, accounts.KindAuth},
		{"rate limited", 200, `{"code":710012004,"message":"slow down"}`, accounts.KindRateLimit},
		{"invalid param", 200, `{"code":710010202,"message":"bad param"}`, accounts.KindInvalidRequest},
		{"biz param", 200, `{"error_code":710020202,"message":"reject"}`, accounts.KindInvalidRequest},
		{"captcha", 200, `{"code":710022004,"message":"verify"}`, accounts.KindInvalidRequest},
		{"http 401", 401, `nope`, accounts.KindAuth},
		{"http 429", 429, `slow`, accounts.KindRateLimit},
		{"http 400", 400, `bad`, accounts.KindInvalidRequest},
		{"unknown 500", 500, `boom`, accounts.KindUnavailable},
	}
	for _, tc := range cases {
		got := Classify(tc.status, tc.body)
		if got.Kind != tc.kind {
			t.Fatalf("%s: kind = %q, want %q", tc.name, got.Kind, tc.kind)
		}
	}
}

func TestClassifyCaptchaSubtypeHint(t *testing.T) {
	device := Classify(403, `{"code":710022004,"extra":{"decision":"{\"subtype\":\"device_fingerprint\"}"}}`)
	if !containsAny(device.Message, "设备") {
		t.Fatalf("device hint missing: %q", device.Message)
	}
	rate := Classify(403, `{"code":710022004,"extra":{"decision":"{\"subtype\":\"rate_frequency\"}"}}`)
	if !containsAny(rate.Message, "限流", "停几分钟") {
		t.Fatalf("rate hint missing: %q", rate.Message)
	}
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if len(needle) > 0 && indexOf(text, needle) >= 0 {
			return true
		}
	}
	return false
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func TestAdapterWiresCapabilities(t *testing.T) {
	client := NewClient(&fakeStore{})
	adapter := client.Adapter()
	if adapter.ID != "doubaowork" {
		t.Fatalf("adapter id = %q", adapter.ID)
	}
	for _, capability := range []string{"credential", "chat", "models", "classifier", "import_export", "prober", "checkin"} {
		if !adapter.Supports(capability) {
			t.Fatalf("adapter missing capability %q", capability)
		}
	}
	if adapter.Login != nil {
		t.Fatal("doubaowork must not advertise browser login")
	}
	if adapter.Supports("login") {
		t.Fatal("login capability must be absent")
	}
}

var _ providers.Adapter
