package translate

import "testing"

// TestSeedComponentsExposePerComponentHash pins the diagnostic split used by
// the affinity debug log: each seed component must move independently, so an
// operator can tell a volatile system prefix from a changed model or a new
// first message. Tool reorder invariance is covered where the tools order is
// normalized, not here.
func TestSeedComponentsExposePerComponentHash(t *testing.T) {
	base := ChatRequest{
		Model: "glm-5.2",
		Messages: []ChatMessage{
			{Role: "system", Content: "stable prefix"},
			{Role: "user", Content: "first question"},
		},
	}
	baseParts := SeedComponents(base)
	if baseParts.Model == "" || baseParts.Prefix == "" || baseParts.FirstUser == "" {
		t.Fatalf("components missing: %+v", baseParts)
	}

	otherModel := base
	otherModel.Model = "deepseek-v4.1-flash"
	got := SeedComponents(otherModel)
	if got.Model == baseParts.Model {
		t.Fatal("model change did not move the model component")
	}
	if got.Prefix != baseParts.Prefix || got.FirstUser != baseParts.FirstUser {
		t.Fatalf("model change moved unrelated components: %+v", got)
	}

	volatilePrefix := base
	volatilePrefix.Messages = []ChatMessage{
		{Role: "system", Content: "today is 2026-11-24 17:45:03"},
		{Role: "user", Content: "first question"},
	}
	got = SeedComponents(volatilePrefix)
	if got.Prefix == baseParts.Prefix {
		t.Fatal("prefix change did not move the prefix component")
	}
	if got.FirstUser != baseParts.FirstUser {
		t.Fatalf("prefix change moved the first-user component: %+v", got)
	}
}

// TestSeedComponentsMissingWithoutUserAnchor keeps the "no user message means
// no session key" invariant visible: reporting all-zero components matches an
// empty ContentSessionSeed.
func TestSeedComponentsMissingWithoutUserAnchor(t *testing.T) {
	parts := SeedComponents(ChatRequest{
		Model:    "glm-5.2",
		Messages: []ChatMessage{{Role: "system", Content: "only a prefix"}},
	})
	if parts != (SessionSeedComponents{}) {
		t.Fatalf("components = %+v, want zero value", parts)
	}
}
