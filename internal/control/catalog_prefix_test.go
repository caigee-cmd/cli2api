package control

import "testing"

func TestPrefixModelEntriesAddsProviderPrefix(t *testing.T) {
	models := []map[string]any{
		{"id": "glm-5.2", "provider": "qoder", "owned_by": "qoder"},
		{"id": "glm-5.3-flash", "provider": "workbuddy", "regions": []string{"cn"}},
		{"id": "deepseek-v4.1-flash", "provider": "trae", "regions": []string{"cn", "global"}},
		// Already-prefixed and provider-less entries pass through untouched.
		{"id": "qoder/glm-5.2", "provider": "qoder"},
		{"id": "some-model", "provider": "", "owned_by": "qoder"},
	}
	got := PrefixModelEntries(models)
	if len(got) != len(models) {
		t.Fatalf("len=%d want %d", len(got), len(models))
	}
	cases := []struct {
		index int
		want  string
	}{
		{0, "qoder/glm-5.2"},
		{1, "workbuddy-cn/glm-5.3-flash"},
		{2, "trae/deepseek-v4.1-flash"},
		{3, "qoder/glm-5.2"},
		// Provider-less entries fall under the qoder view (owned_by=qoder); the
		// gateway's bare-model fallback routes them to qoder with the pool off,
		// so qoder/<id> is exactly the form it accepts.
		{4, "qoder/some-model"},
	}
	for _, c := range cases {
		id, _ := got[c.index]["id"].(string)
		if id != c.want {
			t.Fatalf("entry %d id=%q want %q", c.index, id, c.want)
		}
	}
	// The source entries must stay untouched: callers share the catalog cache.
	if models[0]["id"] != "glm-5.2" {
		t.Fatalf("source entry mutated: %v", models[0]["id"])
	}
}

func TestPrefixModelEntriesEmptyAndNil(t *testing.T) {
	if got := PrefixModelEntries(nil); got != nil {
		t.Fatalf("nil passthrough broken: %v", got)
	}
	if got := PrefixModelEntries([]map[string]any{}); len(got) != 0 {
		t.Fatalf("empty passthrough broken: %v", got)
	}
}
