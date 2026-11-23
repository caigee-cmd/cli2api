package providers

import (
	"encoding/json"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

// DisabledThinkingType is the marker OpenAI-compatible clients use to ask for
// a response without a chain of thought (for example Qwen's
// `{"type":"disabled"}` and the value EverOS llm-router sends on extraction
// calls). Keep it as a separate channel from reasoning_effort levels:
// AGENTS.md says reasoning levels are catalog-driven and must be clamped to
// what a model advertises, so "none" cannot be forced through the level path.
const DisabledThinkingType = "disabled"

// RequestsDisabledThinking reports whether the caller explicitly asked to
// switch reasoning off with a nested thinking object. It deliberately does not
// treat reasoning_effort:"none" / enable_thinking:false as this signal: those
// are already handled by each adapter's level path and, for models that do not
// advertise "none", are clamped back to the catalog default.
func RequestsDisabledThinking(req translate.ChatRequest) bool {
	if len(req.Thinking) == 0 {
		return false
	}
	var raw any
	if json.Unmarshal(req.Thinking, &raw) != nil {
		return false
	}

	var typeOf func(v any) bool
	typeOf = func(v any) bool {
		switch typed := v.(type) {
		case string:
			// A bare string thinking value ("disabled" / "enabled") is used by
			// some clients. Only the disabled spelling is an off switch.
			return strings.EqualFold(strings.TrimSpace(typed), DisabledThinkingType)
		case map[string]any:
			t, _ := typed["type"].(string)
			return strings.EqualFold(strings.TrimSpace(t), DisabledThinkingType)
		case []any:
			for _, item := range typed {
				if typeOf(item) {
					return true
				}
			}
		}
		return false
	}
	return typeOf(raw)
}

// DisabledThinkingField returns the canonical body value for a disabled
// thinking request, ready to be set on an outbound JSON body.
func DisabledThinkingField() map[string]any {
	return map[string]any{"type": DisabledThinkingType}
}

// DisabledThinkingFieldJSON returns the canonical disabled-thinking marker as
// JSON for a provider-neutral request. It is used for diagnostic probes that
// must avoid reasoner token spend.
func DisabledThinkingFieldJSON() json.RawMessage {
	raw, _ := json.Marshal(DisabledThinkingField())
	return raw
}
