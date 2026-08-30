package workbuddy

import (
	"encoding/json"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func requestedReasoningLevel(req translate.ChatRequest) string {
	if len(req.ReasoningEffort) > 0 {
		var value any
		if json.Unmarshal(req.ReasoningEffort, &value) == nil {
			switch typed := value.(type) {
			case string:
				if level := providers.NormalizeReasoningLevel(typed); level != "" {
					return level
				}
			case map[string]any:
				for _, key := range []string{"effort", "level", "type"} {
					if text, ok := typed[key].(string); ok {
						if level := providers.NormalizeReasoningLevel(text); level != "" {
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

func applyChatReasoning(obj map[string]any, req translate.ChatRequest, storedLevel string, caps providers.ModelCapabilities) {
	if obj == nil {
		return
	}
	level := requestedReasoningLevel(req)
	if level == "" {
		level = storedLevel
	}
	level = providers.ResolveReasoningLevel(level, caps)
	if level == "" {
		delete(obj, "reasoning")
		return
	}
	if level == "none" {
		if !caps.CanDisableThinking {
			level = providers.ResolveReasoningLevel(caps.ReasoningDefault, caps)
			if level == "" || level == "none" {
				delete(obj, "reasoning")
				return
			}
		} else {
			delete(obj, "reasoning")
			return
		}
	}
	if level == "max" {
		level = "xhigh"
	}
	obj["reasoning"] = map[string]any{"effort": level, "summary": "auto"}
}
