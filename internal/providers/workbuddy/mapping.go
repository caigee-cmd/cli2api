package workbuddy

import (
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// officialTopLevelReasoningModels need the 2.132.0 CLI field names. The
// nested reasoning object that GLM / Hy4 still accept returns no thinking
// tokens for these ids. deep-model is the older native id some Global
// catalogs still expose for the same Deepseek Flash slot. deepseek-v4-pro
// (and the older deepseek-v4-flash spelling) hit the same nested-object
// silence on CN.
var officialTopLevelReasoningModels = map[string]struct{}{
	"deepseek-v4.1-flash": {},
	"deepseek-v4-flash":   {},
	"deepseek-v4-pro":     {},
	"deep-model":          {},
}

func requestedReasoningLevel(req translate.ChatRequest) string {
	return providers.ParseReasoningIntent(req).Level
}

func applyChatReasoning(obj map[string]any, req translate.ChatRequest, storedLevel string, caps providers.ModelCapabilities) string {
	if obj == nil {
		return ""
	}
	// An explicit thinking:{"type":"disabled"} is an off switch, not a
	// reasoning level. Reasoning levels stay catalog-driven and are clamped to
	// what the model advertises (AGENTS.md), so "none" cannot be forced via
	// that path. Clear every effort field and keep the canonical disabled
	// thinking marker: the WorkBuddy CLI channel (same copilot upstream as
	// workbuddy2api-panel) honors this shape to suppress reasoning tokens.
	// resolvedReasoningLevel is reported as "none" for the request log.
	if providers.RequestsDisabledThinking(req) {
		clearChatReasoning(obj)
		obj["thinking"] = providers.DisabledThinkingField()
		return "none"
	}
	level := requestedReasoningLevel(req)
	if level == "" {
		level = storedLevel
	}
	level = providers.ResolveReasoningLevel(level, caps)
	if level == "" {
		clearChatReasoning(obj)
		return ""
	}
	if level == "none" {
		if !caps.CanDisableThinking {
			// The model declares "none" but cannot actually turn thinking off.
			// Degrade down to the cheapest thinking tier; never fall back to the
			// model default, which could upgrade the level the client asked for.
			level = providers.LowestThinkingOption(caps)
			if level == "" {
				clearChatReasoning(obj)
				return ""
			}
		} else {
			clearChatReasoning(obj)
			return ""
		}
	}
	if workbuddyReasoningWire(req.Model) == providers.ReasoningWireFlatEffort || caps.ReasoningWire == providers.ReasoningWireFlatEffort {
		delete(obj, "reasoning")
		obj["reasoning_effort"] = level
		obj["reasoning_summary"] = "auto"
		obj["verbosity"] = "high"
		return level
	}
	delete(obj, "reasoning_effort")
	delete(obj, "reasoning_summary")
	delete(obj, "verbosity")
	obj["reasoning"] = map[string]any{"effort": level, "summary": "auto"}
	return level
}

func clearChatReasoning(obj map[string]any) {
	delete(obj, "reasoning")
	delete(obj, "reasoning_effort")
	delete(obj, "reasoning_summary")
	delete(obj, "verbosity")
}

func workbuddyReasoningWire(model string) providers.ReasoningWire {
	key := accounts.CanonicalModelID(model)
	if i := strings.LastIndex(key, "/"); i >= 0 {
		key = key[i+1:]
	}
	if _, ok := officialTopLevelReasoningModels[key]; ok {
		return providers.ReasoningWireFlatEffort
	}
	return providers.ReasoningWireNested
}
