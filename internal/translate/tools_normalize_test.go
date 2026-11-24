package translate

import (
	"encoding/json"
	"testing"
)

func toolsJSON(names ...string) json.RawMessage {
	items := make([]map[string]any, 0, len(names))
	for _, name := range names {
		items = append(items, map[string]any{
			"type":     "function",
			"function": map[string]any{"name": name, "description": name + " desc"},
		})
	}
	raw, _ := json.Marshal(items)
	return raw
}

func seedRequest(model string, tools json.RawMessage) ChatRequest {
	return ChatRequest{
		Model:    model,
		Tools:    tools,
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	}
}

func TestToolsOrderDoesNotChangeSessionSeed(t *testing.T) {
	ordered := toolsJSON("alpha", "beta", "gamma")
	shuffled := toolsJSON("gamma", "alpha", "beta")
	if ContentSessionSeed(seedRequest("glm-5.2", ordered)) != ContentSessionSeed(seedRequest("glm-5.2", shuffled)) {
		t.Fatal("tool order changed the session seed")
	}
	if PrefixSessionSeed(seedRequest("glm-5.2", ordered)) != PrefixSessionSeed(seedRequest("glm-5.2", shuffled)) {
		t.Fatal("tool order changed the shard prefix seed")
	}
}

func TestToolsOrderStillChangesWithDifferentDefinitions(t *testing.T) {
	if ContentSessionSeed(seedRequest("glm-5.2", toolsJSON("alpha", "beta"))) ==
		ContentSessionSeed(seedRequest("glm-5.2", toolsJSON("alpha", "delta"))) {
		t.Fatal("a different tool set must produce a different seed")
	}
}

func TestNormalizeToolsOrderSortsByName(t *testing.T) {
	raw := toolsJSON("beta", "alpha")
	var got []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(NormalizeToolsOrder(raw), &got); err != nil {
		t.Fatalf("decode normalized tools: %v", err)
	}
	if len(got) != 2 || got[0].Function.Name != "alpha" || got[1].Function.Name != "beta" {
		t.Fatalf("normalized order = %+v", got)
	}
}

func TestNormalizeToolsOrderKeepsInputWhenAlreadySorted(t *testing.T) {
	raw := toolsJSON("alpha", "beta")
	if string(NormalizeToolsOrder(raw)) != string(raw) {
		t.Fatal("an already-sorted array must be returned byte-identical")
	}
}

func TestNormalizeToolsOrderLeavesInvalidInputAlone(t *testing.T) {
	cases := []json.RawMessage{
		nil,
		json.RawMessage(`null`),
		json.RawMessage(`[]`),
		json.RawMessage(`"not-an-array"`),
		json.RawMessage(`[{"type":"function"}]`),
	}
	for _, raw := range cases {
		if string(NormalizeToolsOrder(raw)) != string(raw) {
			t.Fatalf("input %q was rewritten", string(raw))
		}
	}
}

func TestNormalizeToolsForPayloadObeysKillSwitch(t *testing.T) {
	raw := toolsJSON("beta", "alpha")

	t.Setenv("CLI2API_TOOLS_NORMALIZE", "")
	if string(NormalizeToolsForPayload(raw)) != string(raw) {
		t.Fatal("payload order must stay untouched by default")
	}

	t.Setenv("CLI2API_TOOLS_NORMALIZE", "1")
	if string(NormalizeToolsForPayload(raw)) == string(raw) {
		t.Fatal("payload order should be normalized when the switch is on")
	}
}

func TestSeedComponentsAttribution(t *testing.T) {
	base := ChatRequest{
		Model: "glm-5.2",
		Tools: toolsJSON("alpha", "beta"),
		Messages: []ChatMessage{
			{Role: "system", Content: "stable prefix"},
			{Role: "user", Content: "first question"},
		},
	}
	baseParts := SeedComponents(base)
	if baseParts.Model == "" || baseParts.Tools == "" || baseParts.Prefix == "" || baseParts.FirstUser == "" {
		t.Fatalf("components missing: %+v", baseParts)
	}

	// Reordering tools must not move any component hash.
	shuffled := base
	shuffled.Tools = toolsJSON("beta", "alpha")
	if got := SeedComponents(shuffled); got != baseParts {
		t.Fatalf("tool reorder changed components: %+v vs %+v", got, baseParts)
	}

	// A different model moves only the model component.
	otherModel := base
	otherModel.Model = "deepseek-v4.1-flash"
	got := SeedComponents(otherModel)
	if got.Model == baseParts.Model {
		t.Fatal("model change did not move the model component")
	}
	if got.Tools != baseParts.Tools || got.Prefix != baseParts.Prefix || got.FirstUser != baseParts.FirstUser {
		t.Fatalf("model change moved unrelated components: %+v", got)
	}

	// A volatile system prefix moves only the prefix component.
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
