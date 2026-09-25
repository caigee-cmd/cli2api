package command

import (
	"context"
	"strings"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"upgrade_required code", 403, `{"error":{"type":"permission_error","code":"upgrade_required","message":"Your Go plan doesn't include API access."}}`, accounts.KindInvalidRequest},
		{"upgrade_required text", 403, `{"error":{"message":"Your Go plan doesn't include API access. Upgrade to Pro or higher"}}`, accounts.KindInvalidRequest},
		{"unauthorized", 401, `{"success":false,"error":{"code":"UNAUTHORIZED","status":401,"message":"Invalid 'Authorization' header or token."}}`, accounts.KindAuth},
		{"insufficient credits", 400, `{"type":"error","error":{"type":"invalid_request_error","message":"You have insufficient credits to make this request. Please purchase more credits."}}`, accounts.KindQuota},
		{"rate limit", 429, `{"error":{"code":"rate_limit_error","message":"slow down"}}`, accounts.KindRateLimit},
		{"validation", 400, `{"type":"error","error":{"message":"Validation error: expected string, received undefined at \"config.workingDir\""}}`, accounts.KindInvalidRequest},
		{"message schema", 400, `{"type":"error","error":{"message":"Invalid prompt: The messages do not match the ModelMessage[] schema."}}`, accounts.KindInvalidRequest},
		{"server error", 500, `oops`, accounts.KindUnavailable},
	}
	for _, tc := range cases {
		got := Classify(tc.status, tc.body)
		if got.Kind != tc.want {
			t.Errorf("%s: kind = %q, want %q", tc.name, got.Kind, tc.want)
		}
	}
}

func TestClassifyRedactsSecrets(t *testing.T) {
	got := Classify(401, `{"message":"token user_abcdefghijklmnop is invalid","Authorization":"Bearer user_abcdefghijklmnop"}`)
	if strings.Contains(got.Message, "user_abcdefghijklmnop") {
		t.Fatalf("secret leaked in classified message: %q", got.Message)
	}
	if !strings.Contains(got.Message, "user_[redacted]") {
		t.Errorf("expected redaction marker, got %q", got.Message)
	}
}

func TestAdapterCapabilities(t *testing.T) {
	store := &fakeStore{format: CredentialFormat, payload: []byte(`{"api_key":"user_abcdefghijklmnop"}`)}
	adapter := NewClient(store).Adapter()
	if adapter.ID != "command" {
		t.Errorf("id = %q", adapter.ID)
	}
	for _, cap := range []string{"credential", "chat", "models", "classifier", "prober", "import_export"} {
		if !adapter.Supports(cap) {
			t.Errorf("adapter must support %q", cap)
		}
	}
	if adapter.Supports("login") {
		t.Error("command has no browser-login capability")
	}
}

func TestMissingSecretReaderSkipsGlobalProxy(t *testing.T) {
	client := NewClient(&fakeStore{})
	got, err := client.globalProxy(context.Background())
	if err != nil {
		t.Fatalf("missing SecretReader must skip, not fail: %v", err)
	}
	if got != "" {
		t.Fatalf("missing SecretReader returned %q", got)
	}
}

var _ Store = (*fakeStore)(nil)
