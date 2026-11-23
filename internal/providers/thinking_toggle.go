package providers

import (
	"encoding/json"
	"strconv"
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

// ReasoningIntent is the provider-neutral interpretation of a client's
// reasoning request. Wire shapes stay in adapters; this struct only records
// what the caller asked for before catalog clamping.
type ReasoningIntent struct {
	Level    string // normalized level including "none", empty when unspecified
	Disabled bool   // explicit thinking:{type:"disabled"} off switch
	Budget   int    // Anthropic-style thinking.budget_tokens, 0 when absent
}

// ParseReasoningIntent centralizes the client-facing reasoning interpretation.
// Explicit disabled thinking is kept separate from the level path because a
// catalog may not advertise "none"; adapters decide how to emit the off switch.
func ParseReasoningIntent(req translate.ChatRequest) ReasoningIntent {
	intent := ReasoningIntent{}
	if level := normalizeReasoningEffortField(req.ReasoningEffort); level != "" {
		intent.Level = level
	}
	if len(req.Thinking) > 0 {
		var value map[string]any
		if json.Unmarshal(req.Thinking, &value) == nil {
			if raw := value["budget_tokens"]; raw != nil {
				if n, ok := reasoningBudget(raw); ok && n > 0 {
					intent.Budget = n
					if intent.Level == "" {
						intent.Level = ReasoningLevelFromThinkingBudget(n)
					}
				}
			}
			if intent.Level == "" {
				for _, key := range []string{"effort", "level", "type"} {
					if text, ok := value[key].(string); ok {
						if level := NormalizeReasoningLevel(text); level != "" && level != "none" {
							intent.Level = level
							break
						}
					}
				}
			}
		}
	}
	if RequestsDisabledThinking(req) {
		intent.Disabled = true
		intent.Level = "none"
		return intent
	}
	if intent.Level != "" {
		return intent
	}
	for _, enabled := range []*bool{req.EnableThinking, req.EnableReasoning, req.IsReasoning} {
		if enabled == nil {
			continue
		}
		if *enabled {
			intent.Level = "medium"
		} else {
			intent.Level = "none"
		}
		return intent
	}
	return intent
}

func normalizeReasoningEffortField(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return NormalizeReasoningLevel(typed)
	case map[string]any:
		for _, key := range []string{"effort", "level", "type"} {
			if text, ok := typed[key].(string); ok {
				if level := NormalizeReasoningLevel(text); level != "" {
					return level
				}
			}
		}
	}
	return ""
}

func reasoningBudget(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		return int(typed), true
	case json.Number:
		n, err := typed.Int64()
		return int(n), err == nil
	case int:
		return typed, true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(typed))
		return n, err == nil
	default:
		return 0, false
	}
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
