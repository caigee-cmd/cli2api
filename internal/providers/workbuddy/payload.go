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
	repairToolSequence(body)
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
		if messageContentEmpty(message) && !hasToolCalls(message) && !isToolResult(message) {
			continue
		}
		kept = append(kept, item)
	}
	body["messages"] = kept
}

func messageContentEmpty(message map[string]any) bool {
	content, exists := message["content"]
	if !exists || content == nil {
		return true
	}
	if value, ok := content.(string); ok {
		return strings.TrimSpace(value) == ""
	}
	if value, ok := content.([]any); ok {
		return len(value) == 0
	}
	return false
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

func isToolResult(message map[string]any) bool {
	return messageRole(message) == "tool" || strings.TrimSpace(stringField(message, "tool_call_id")) != ""
}

func messageRole(message map[string]any) string {
	role, _ := message["role"].(string)
	return strings.ToLower(strings.TrimSpace(role))
}

func stringField(message map[string]any, key string) string {
	value, _ := message[key].(string)
	return strings.TrimSpace(value)
}

// repairToolSequence preserves complete tool rounds byte-for-byte at the
// message level and drops only incomplete rounds. Clients that stop a stream
// mid-tool often resend an assistant tool_calls message without all results;
// WorkBuddy then rejects the next turn with code 11148.
func repairToolSequence(body map[string]any) {
	raw, ok := body["messages"]
	if !ok {
		return
	}
	list, ok := raw.([]any)
	if !ok {
		return
	}
	kept := make([]any, 0, len(list))
	for i := 0; i < len(list); {
		message, ok := list[i].(map[string]any)
		if !ok {
			kept = append(kept, list[i])
			i++
			continue
		}
		if messageRole(message) == "tool" {
			i++
			continue
		}
		if messageRole(message) != "assistant" || !hasToolCalls(message) {
			kept = append(kept, message)
			i++
			continue
		}
		calls, _ := message["tool_calls"].([]any)
		results := make([]map[string]any, 0)
		j := i + 1
		for j < len(list) {
			next, ok := list[j].(map[string]any)
			if !ok || messageRole(next) != "tool" {
				break
			}
			results = append(results, next)
			j++
		}
		if !hasCompleteToolRound(calls, results) {
			delete(message, "tool_calls")
			if !messageContentEmpty(message) {
				kept = append(kept, message)
			}
			i = j
			continue
		}
		kept = append(kept, message)
		for _, result := range results {
			kept = append(kept, result)
		}
		i = j
	}
	body["messages"] = kept
}

func hasCompleteToolRound(calls []any, results []map[string]any) bool {
	if len(calls) == 0 || len(calls) != len(results) {
		return false
	}
	callIDs := make(map[string]struct{}, len(calls))
	for _, raw := range calls {
		call, ok := raw.(map[string]any)
		if !ok {
			return false
		}
		id := toolCallID(call)
		if id == "" {
			return false
		}
		if _, exists := callIDs[id]; exists {
			return false
		}
		callIDs[id] = struct{}{}
	}
	resultIDs := make(map[string]struct{}, len(results))
	for _, result := range results {
		id := toolResultID(result)
		if id == "" {
			return false
		}
		if _, exists := callIDs[id]; !exists {
			return false
		}
		if _, exists := resultIDs[id]; exists {
			return false
		}
		resultIDs[id] = struct{}{}
	}
	return len(callIDs) == len(resultIDs)
}

func toolCallID(call map[string]any) string {
	if id := stringField(call, "id"); id != "" {
		return id
	}
	return stringField(call, "tool_call_id")
}

func toolResultID(result map[string]any) string {
	if id := stringField(result, "tool_call_id"); id != "" {
		return id
	}
	return stringField(result, "id")
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
