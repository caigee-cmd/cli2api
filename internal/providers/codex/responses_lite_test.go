package codex

import (
	"encoding/json"
	"testing"
)

// Regression for #300: requests sent under the Responses-Lite header must
// carry reasoning.context=all_turns, or the backend rejects them with 400.
func TestNormalizeCodexUpstreamLiteForcesReasoningContext(t *testing.T) {
	for name, raw := range map[string]string{
		"missing":  "",
		"effort":   `{"effort":"high","summary":"auto"}`,
		"override": `{"context":"current_turn"}`,
	} {
		t.Run(name, func(t *testing.T) {
			fields := map[string]json.RawMessage{"input": json.RawMessage(`[]`)}
			if raw != "" {
				fields["reasoning"] = json.RawMessage(raw)
			}
			if err := normalizeCodexUpstream(fields, "gpt-5.6-sol", true, ""); err != nil {
				t.Fatal(err)
			}
			var reasoning map[string]string
			if err := json.Unmarshal(fields["reasoning"], &reasoning); err != nil {
				t.Fatalf("reasoning %s: %v", fields["reasoning"], err)
			}
			if reasoning["context"] != "all_turns" || string(fields["parallel_tool_calls"]) != "false" {
				t.Fatalf("reasoning=%s parallel_tool_calls=%s", fields["reasoning"], fields["parallel_tool_calls"])
			}
			if name == "effort" && (reasoning["effort"] != "high" || reasoning["summary"] != "auto") {
				t.Fatalf("existing members lost: %s", fields["reasoning"])
			}
		})
	}

	fields := map[string]json.RawMessage{"input": json.RawMessage(`[]`)}
	if err := normalizeCodexUpstream(fields, "gpt-5.5", false, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["reasoning"]; ok {
		t.Fatalf("non-lite request gained reasoning: %s", fields["reasoning"])
	}
}
