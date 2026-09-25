package providers

import "testing"

func TestListIncludesEveryRegisteredDescriptor(t *testing.T) {
	listed := List()
	seen := make(map[string]struct{}, len(listed))
	for _, descriptor := range listed {
		if _, duplicate := seen[descriptor.ID]; duplicate {
			t.Fatalf("duplicate provider %q", descriptor.ID)
		}
		seen[descriptor.ID] = struct{}{}
	}
	for id := range registry {
		if _, ok := seen[id]; !ok {
			t.Fatalf("provider %q is registered but missing from List", id)
		}
	}
	codex, ok := Get("codex")
	if !ok {
		t.Fatal("codex descriptor missing")
	}
	if codex.Runtime != RuntimeInProcess || codex.DefaultRegion != "global" || !codex.SupportsAuthType(AuthOAuth) {
		t.Fatalf("unexpected codex descriptor: %+v", codex)
	}
}
