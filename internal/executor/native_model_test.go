package executor

import "testing"

// Qoder's catalog publishes a friendly public ID whose gateway key differs
// ("qwen3.8-flash" behind "qfmodel"). Sending the public ID leaves the gateway
// waiting without answering, so the request must carry the recorded key.
func TestNativeModelIDPrefersRecordedMapping(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "acc", Provider: "qoder", Region: "global"})
	pool.MergeModels("acc", []string{"qwen3.8-flash", "qfmodel"})
	pool.MergeNativeModels("acc", map[string]string{"qwen3.8-flash": "qfmodel"})

	item, ok := pool.ByID("acc")
	if !ok {
		t.Fatal("account missing from pool")
	}
	if got := NativeModelID(item, "qwen3.8-flash"); got != "qfmodel" {
		t.Fatalf("native = %q, want qfmodel", got)
	}
	// The gateway key itself has no mapping and keeps its own spelling.
	if got := NativeModelID(item, "qfmodel"); got != "qfmodel" {
		t.Fatalf("native for mapped key = %q", got)
	}
}

// A catalog that reports no distinct key must leave the public ID untouched:
// mapping is per-account and accounts can disagree about the same model.
func TestNativeModelIDWithoutMappingKeepsPublicID(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "acc", Provider: "qoder", Region: "global"})
	pool.MergeModels("acc", []string{"qwen3.8-flash"})

	item, _ := pool.ByID("acc")
	if got := NativeModelID(item, "qwen3.8-flash"); got != "qwen3.8-flash" {
		t.Fatalf("native = %q, want the public id", got)
	}
}

// Self-mappings and unknown models are dropped at merge time so the lookup
// path stays a plain map read.
func TestMergeNativeModelsSkipsSelfMappings(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "acc", Provider: "qoder", Region: "global"})
	pool.MergeNativeModels("acc", map[string]string{
		"hy3":  "hy3",
		"":     "orphan",
		"auto": "auto",
		"a":    "",
		"b":    "native-b",
	})
	item, _ := pool.ByID("acc")
	if len(item.NativeModels) != 1 || item.NativeModels["b"] != "native-b" {
		t.Fatalf("mapping = %v", item.NativeModels)
	}
}

// Dropping a model from the catalog must drop its mapping too, or a stale
// gateway key would keep routing to a model the account no longer serves.
func TestRemoveModelDropsNativeMapping(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "acc", Provider: "qoder", Region: "global"})
	pool.MergeModelsWithPricing("acc", []string{"qwen3.8-flash"}, []string{"qwen3.8-flash"})
	pool.MergeNativeModels("acc", map[string]string{"qwen3.8-flash": "qfmodel"})

	pool.RemoveModel("acc", "qwen3.8-flash")
	item, _ := pool.ByID("acc")
	if _, ok := item.NativeModels["qwen3.8-flash"]; ok {
		t.Fatalf("mapping survived removal: %v", item.NativeModels)
	}
	if len(item.FreeModels) != 0 {
		t.Fatalf("free list = %v", item.FreeModels)
	}
}

// The mapping is catalog data, so a runtime Upsert that carries no catalog must
// not drop it. Losing the mapping sends the public model ID upstream, which is
// exactly the failure the mapping exists to prevent.
func TestUpsertKeepsNativeModelsWhenCatalogUnsupplied(t *testing.T) {
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "acc", Provider: "qoder", Region: "global"})
	pool.MergeModels("acc", []string{"public-id", "gateway-key"})
	pool.MergeNativeModels("acc", map[string]string{"public-id": "gateway-key"})

	pool.Upsert(Item{ID: "acc", Provider: "qoder", Region: "global"})

	item, _ := pool.ByID("acc")
	if got := NativeModelID(item, "public-id"); got != "gateway-key" {
		t.Fatalf("native after Upsert = %q, want gateway-key", got)
	}
}
