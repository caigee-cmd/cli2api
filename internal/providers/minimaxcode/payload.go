package minimaxcode

import (
	"encoding/json"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

// buildChatBody translates an OpenAI chat request into the Anthropic Messages
// body the MiniMax Code gateway expects.
func buildChatBody(req translate.ChatRequest, model string) (map[string]any, error) {
	systemParts := []string{}
	messages := make([]any, 0, len(req.Messages))
	for _, msg := range req.Messages {
		switch msg.Role {
		case "system", "developer":
			if text := messageText(msg.Content); strings.TrimSpace(text) != "" {
				systemParts = append(systemParts, text)
			}
			continue
		case "tool", "function":
			messages = append(messages, map[string]any{
				"role": "user",
				"content": []any{map[string]any{
					"type": "tool_result", "tool_use_id": first(msg.ToolCallID, msg.Name, "tool"),
					"content": messageText(msg.Content),
				}},
			})
			continue
		case "assistant":
			blocks := make([]any, 0)
			if text := messageText(msg.Content); strings.TrimSpace(text) != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": text})
			}
			if len(msg.ToolCalls) > 0 {
				var calls []map[string]any
				if json.Unmarshal(msg.ToolCalls, &calls) == nil {
					for _, call := range calls {
						id, _ := call["id"].(string)
						function, _ := call["function"].(map[string]any)
						name, _ := function["name"].(string)
						arguments, _ := function["arguments"].(string)
						var input any = map[string]any{}
						if arguments != "" {
							_ = json.Unmarshal([]byte(arguments), &input)
						}
						blocks = append(blocks, map[string]any{"type": "tool_use", "id": first(id, newToolID()), "name": first(name, "tool"), "input": input})
					}
				}
			}
			if len(blocks) > 0 {
				messages = append(messages, map[string]any{"role": "assistant", "content": blocks})
			}
			continue
		default:
			messages = append(messages, map[string]any{"role": "user", "content": contentBlocks(msg.Content)})
		}
	}
	body := map[string]any{
		"model":      model,
		"max_tokens": 8192,
		"messages":   messages,
	}
	if len(systemParts) > 0 {
		body["system"] = strings.Join(systemParts, "\n\n")
	}
	if req.Stream {
		body["stream"] = true
	}
	if len(req.Tools) > 0 {
		var tools []map[string]any
		if json.Unmarshal(req.Tools, &tools) == nil {
			anthropicTools := make([]any, 0, len(tools))
			for _, tool := range tools {
				typ, _ := tool["type"].(string)
				function, _ := tool["function"].(map[string]any)
				name, _ := function["name"].(string)
				if typ != "function" || name == "" {
					continue
				}
				description, _ := function["description"].(string)
				params := function["parameters"]
				if params == nil {
					params = map[string]any{"type": "object", "properties": map[string]any{}}
				}
				entry := map[string]any{"name": name, "input_schema": params}
				if description != "" {
					entry["description"] = description
				}
				anthropicTools = append(anthropicTools, entry)
			}
			if len(anthropicTools) > 0 {
				body["tools"] = anthropicTools
			}
		}
	}
	return body, nil
}

func messageText(content any) string {
	switch value := content.(type) {
	case string:
		return value
	case []any:
		parts := make([]string, 0, len(value))
		for _, raw := range value {
			part, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := part["text"].(string); ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func contentBlocks(content any) []any {
	switch value := content.(type) {
	case string:
		return []any{map[string]any{"type": "text", "text": value}}
	case []any:
		blocks := make([]any, 0, len(value))
		for _, raw := range value {
			part, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			typ, _ := part["type"].(string)
			if text, ok := part["text"].(string); ok && (typ == "text" || text != "") {
				blocks = append(blocks, map[string]any{"type": "text", "text": text})
			}
		}
		if len(blocks) > 0 {
			return blocks
		}
	}
	return []any{map[string]any{"type": "text", "text": ""}}
}

func newToolID() string {
	return "toolu_" + randomHex(8)
}
