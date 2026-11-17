package codex

import (
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func TestClassifyModelEntitlementIsModelNotAvailable(t *testing.T) {
	got := Classify(403, `{"error":{"code":"forbidden","message":"your plan does not have access to this model"}}`)
	if got.Kind != accounts.KindModelNotAvailable {
		t.Fatalf("got %+v", got)
	}
}
