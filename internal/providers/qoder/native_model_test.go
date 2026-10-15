package qoder

import (
	"testing"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// Qoder's catalog gives a friendly public ID and a distinct gateway key. The
// chat request must carry the gateway key, or the gateway waits without
// answering.
func TestNativeCatalogIDsFromInfosKeepsMappedKey(t *testing.T) {
	got := NativeCatalogIDsFromInfos([]providers.ModelInfo{
		{PublicModel: "qwen3.8-flash", NativeModel: "qfmodel", DisplayName: "Qwen3.8-Flash"},
		{PublicModel: "hy3", NativeModel: "hy3"},
		{PublicModel: "", NativeModel: "orphan"},
		{PublicModel: "no-native"},
	})
	if len(got) != 1 {
		t.Fatalf("mapping = %v", got)
	}
	if got["qwen3.8-flash"] != "qfmodel" {
		t.Fatalf("qwen3.8-flash -> %q", got["qwen3.8-flash"])
	}
	if _, ok := got["hy3"]; ok {
		t.Fatalf("self-mapping kept: %v", got)
	}
}

func TestNativeCatalogIDsFromInfosFoldsCanonicalForm(t *testing.T) {
	got := NativeCatalogIDsFromInfos([]providers.ModelInfo{
		{PublicModel: "Qwen3.8-Flash", NativeModel: "qfmodel"},
	})
	if got["qwen3.8-flash"] != "qfmodel" {
		t.Fatalf("mapping = %v", got)
	}
}
