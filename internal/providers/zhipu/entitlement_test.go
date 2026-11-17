package zhipu

import (
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func TestClassifyModelEntitlementIsModelNotAvailable(t *testing.T) {
	for _, body := range []string{
		`{"message":"您无权限使用该模型"}`,
		`{"error":{"message":"model is not available for your plan"}}`,
	} {
		got := Classify(403, body)
		if got.Kind != accounts.KindModelNotAvailable {
			t.Fatalf("body=%s got %+v", body, got)
		}
	}
}

func TestClassifyPlainQuotaWordIsNotAccountQuota(t *testing.T) {
	// A generic "quota" word without an explicit exhaustion phrase must not
	// cool the whole account.
	got := Classify(400, `{"message":"please check the model quota setting"}`)
	if got.Kind == accounts.KindQuota {
		t.Fatalf("generic quota word must not be account quota, got %+v", got)
	}
}
