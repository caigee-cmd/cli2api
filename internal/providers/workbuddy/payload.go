package workbuddy

import (
	"encoding/json"
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
	out, err := json.Marshal(body)
	if err != nil {
		return src
	}
	return out
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
