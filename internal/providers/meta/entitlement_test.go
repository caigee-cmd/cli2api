package meta

import (
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func TestClassifyModelEntitlementIsModelNotAvailable(t *testing.T) {
	for _, body := range []string{
		`{"error":{"message":"you does not have access to this model"}}`,
		`{"error":{"code":"forbidden","message":"model not included in your plan"}}`,
	} {
		got := Classify(403, body)
		if got.Kind != accounts.KindModelNotAvailable {
			t.Fatalf("body=%s got %+v", body, got)
		}
	}
}
