package translate

import (
	"encoding/json"
	"fmt"
	"strings"
)

const defaultToolParameters = `{"type":"object","properties":{}}`

// NormalizeOpenAITools expands Codex/Desktop namespace wrappers into plain
// OpenAI function tools and drops hosted shells such as mcp / web_search.
// Nested tools may be Responses-flat or Chat Completions shaped. Short nested
// names are qualified as namespace__name; already-qualified mcp__* names stay
// unchanged. This does not apply Devin's mcp__ upstream aliasing.
func NormalizeOpenAITools(raw json.RawMessage) (json.RawMessage, error) {
	if emptyJSON(raw) {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("tools must be an array")
	}
	out := make([]map[string]any, 0, len(items))
	seen := map[string]struct{}{}
	appendTool := func(name, description string, parameters json.RawMessage) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		if len(parameters) == 0 || string(parameters) == "null" {
			parameters = json.RawMessage(defaultToolParameters)
		}
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        name,
				"description": description,
				"parameters":  json.RawMessage(parameters),
			},
		})
	}
	for _, item := range items {
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(item, &probe); err != nil {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(rawMapString(probe, "type")))
		switch typ {
		case "namespace":
			for _, nested := range expandNamespaceToolItems(item, strings.TrimSpace(rawMapString(probe, "name"))) {
				appendTool(nested.name, nested.description, nested.parameters)
			}
		case "mcp", "web_search", "web_search_preview":
			continue
		case "function", "":
			name, description, parameters := toolFields(probe)
			appendTool(name, description, parameters)
		default:
			continue
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return json.Marshal(out)
}

type normalizedTool struct {
	name        string
	description string
	parameters  json.RawMessage
}

func expandNamespaceToolItems(raw json.RawMessage, namespace string) []normalizedTool {
	var wrapper struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if json.Unmarshal(raw, &wrapper) != nil || len(wrapper.Tools) == 0 {
		return nil
	}
	out := make([]normalizedTool, 0, len(wrapper.Tools))
	for _, item := range wrapper.Tools {
		var probe map[string]json.RawMessage
		if json.Unmarshal(item, &probe) != nil {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(rawMapString(probe, "type")))
		if typ != "" && typ != "function" {
			continue
		}
		name, description, parameters := toolFields(probe)
		name = qualifyNamespaceToolName(namespace, name)
		if name == "" {
			continue
		}
		out = append(out, normalizedTool{name: name, description: description, parameters: parameters})
	}
	return out
}

func toolFields(source map[string]json.RawMessage) (name, description string, parameters json.RawMessage) {
	var function map[string]json.RawMessage
	if raw, ok := rawMapJSONValue(source, "function"); ok {
		_ = json.Unmarshal(raw, &function)
	}
	name = firstNonEmptyTrimmed(rawMapString(function, "name"), rawMapString(source, "name"))
	description = firstNonEmptyTrimmed(rawMapString(function, "description"), rawMapString(source, "description"))
	parameters = rawMapJSON(function, "parameters")
	if len(parameters) == 0 {
		parameters = rawMapJSON(source, "parameters")
	}
	return name, description, parameters
}

func qualifyNamespaceToolName(namespace, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.Contains(name, "__") || strings.HasPrefix(strings.ToLower(name), "mcp__") {
		return name
	}
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return name
	}
	return strings.TrimRight(namespace, "_") + "__" + strings.TrimLeft(name, "_")
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
