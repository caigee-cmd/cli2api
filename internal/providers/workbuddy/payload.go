package workbuddy

import (
	"encoding/json"
	"strings"
)

// PrepareBody forces streaming and normalizes tool_choice to the string form
// the upstream API accepts. It only rewrites known keys; unknown fields stay.
func PrepareBody(src []byte) []byte {
	if len(src) == 0 {
		return src
	}
	var body map[string]any
	if err := json.Unmarshal(src, &body); err != nil {
		return src
	}
	body["stream"] = true
	normalizeToolChoice(body)
	dropEmptyTools(body)
	normalizeEmptyMessageContent(body)
	ensureLeadingSystem(body)
	out, err := json.Marshal(body)
	if err != nil {
		return src
	}
	return out
}

func normalizeEmptyMessageContent(body map[string]any) {
	raw, ok := body["messages"]
	if !ok {
		return
	}
	list, ok := raw.([]any)
	if !ok {
		return
	}
	kept := make([]any, 0, len(list))
	for _, item := range list {
		message, ok := item.(map[string]any)
		if !ok {
			kept = append(kept, item)
			continue
		}
		content, exists := message["content"]
		empty := !exists || content == nil
		if value, ok := content.(string); ok {
			empty = strings.TrimSpace(value) == ""
		}
		if value, ok := content.([]any); ok {
			empty = len(value) == 0
		}
		if empty && !hasToolCalls(message) {
			continue
		}
		kept = append(kept, item)
	}
	body["messages"] = kept
}

func hasToolCalls(message map[string]any) bool {
	raw, ok := message["tool_calls"]
	if !ok || raw == nil {
		return false
	}
	if calls, ok := raw.([]any); ok {
		return len(calls) > 0
	}
	return true
}

// ensureLeadingSystem satisfies WorkBuddy Global code 11128 ("first message
// is not system prompt"). Drop-system-prompt strips caller identity, which
// would otherwise leave a user message first. The placeholder must be non-empty
// because WorkBuddy rejects messages with empty content (code 11151).
func ensureLeadingSystem(body map[string]any) {
	raw, ok := body["messages"]
	if !ok {
		return
	}
	list, ok := raw.([]any)
	if !ok {
		return
	}
	placeholder := map[string]any{"role": "system", "content": "You are a helpful assistant."}
	if len(list) == 0 {
		body["messages"] = []any{placeholder}
		return
	}
	first, ok := list[0].(map[string]any)
	if ok {
		role, _ := first["role"].(string)
		if strings.EqualFold(strings.TrimSpace(role), "system") {
			return
		}
	}
	body["messages"] = append([]any{placeholder}, list...)
}

func normalizeToolChoice(body map[string]any) {
	raw, ok := body["tool_choice"]
	if !ok {
		return
	}
	switch value := raw.(type) {
	case string:
		if value == "none" {
			delete(body, "tool_choice")
			delete(body, "tools")
			delete(body, "functions")
			return
		}
		return
	case map[string]any:
		kind, _ := value["type"].(string)
		switch kind {
		case "none":
			delete(body, "tool_choice")
			delete(body, "tools")
			delete(body, "functions")
		case "auto", "required":
			body["tool_choice"] = kind
		case "function":
			name := functionName(value)
			if name == "" {
				name = "auto"
			}
			body["tool_choice"] = name
		default:
			delete(body, "tool_choice")
		}
	default:
		delete(body, "tool_choice")
	}
}

func functionName(value map[string]any) string {
	if fn, ok := value["function"].(map[string]any); ok {
		if name, _ := fn["name"].(string); name != "" {
			return name
		}
	}
	name, _ := value["name"].(string)
	return name
}

func dropEmptyTools(body map[string]any) {
	raw, ok := body["tools"]
	if !ok {
		return
	}
	if raw == nil {
		delete(body, "tools")
		return
	}
	list, ok := raw.([]any)
	if ok && len(list) == 0 {
		delete(body, "tools")
		delete(body, "tool_choice")
	}
}
