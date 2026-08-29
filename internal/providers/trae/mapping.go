package trae

import (
	"encoding/json"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

var reasoningLevels = map[string]string{
	"none":       "none",
	"off":        "none",
	"disabled":   "none",
	"low":        "low",
	"light":      "low",
	"minimal":    "low",
	"medium":     "medium",
	"default":    "medium",
	"high":       "high",
	"xhigh":      "xhigh",
	"x-high":     "xhigh",
	"extra_high": "xhigh",
	"extra-high": "xhigh",
	"extrahigh":  "xhigh",
	"max":        "max",
}

func normalizeReasoningLevel(raw string) string {
	key := strings.ToLower(strings.TrimSpace(raw))
	key = strings.NewReplacer(" ", "", "_", "-").Replace(key)
	if mapped, ok := reasoningLevels[key]; ok {
		return mapped
	}
	key = strings.ReplaceAll(key, "-", "_")
	if mapped, ok := reasoningLevels[key]; ok {
		return mapped
	}
	return ""
}

func requestedReasoningLevel(req translate.ChatRequest) string {
	if len(req.ReasoningEffort) > 0 {
		var value any
		if json.Unmarshal(req.ReasoningEffort, &value) == nil {
			switch typed := value.(type) {
			case string:
				if level := normalizeReasoningLevel(typed); level != "" {
					return level
				}
			case map[string]any:
				for _, key := range []string{"effort", "level", "type"} {
					if text, ok := typed[key].(string); ok {
						if level := normalizeReasoningLevel(text); level != "" {
							return level
						}
					}
				}
			}
		}
	}
	if req.EnableThinking != nil {
		if *req.EnableThinking {
			return "medium"
		}
		return "none"
	}
	if req.EnableReasoning != nil {
		if *req.EnableReasoning {
			return "medium"
		}
		return "none"
	}
	if req.IsReasoning != nil {
		if *req.IsReasoning {
			return "medium"
		}
		return "none"
	}
	return ""
}

func clampReasoningLevel(level string, caps providers.ModelCapabilities) string {
	level = normalizeReasoningLevel(level)
	if level == "" {
		return ""
	}
	if len(caps.ReasoningOptions) == 0 {
		return ""
	}
	for _, option := range caps.ReasoningOptions {
		if option == level {
			return level
		}
	}
	if fallback := normalizeReasoningLevel(caps.ReasoningDefault); fallback != "" {
		return fallback
	}
	return caps.ReasoningOptions[0]
}

func applySoloChatFields(obj map[string]any, req translate.ChatRequest, maxMode bool, caps providers.ModelCapabilities) {
	if obj == nil {
		return
	}
	for _, key := range []string{
		"enable_thinking", "enable_reasoning", "is_reasoning", "reasoning_effort",
		"reasoning_budget_tokens", "thinking", "context_length", "max_input_tokens",
	} {
		delete(obj, key)
	}
	if maxMode && caps.MaxMode {
		obj["is_max_mode"] = 1
	} else {
		delete(obj, "is_max_mode")
	}
	level := clampReasoningLevel(requestedReasoningLevel(req), caps)
	if level == "" {
		delete(obj, "reasoning_effort_level")
		return
	}
	obj["reasoning_effort_level"] = level
}
