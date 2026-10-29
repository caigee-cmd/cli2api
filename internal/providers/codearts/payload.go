package codearts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// buildChatBody maps the internal request to the native CodeArts
// chat-completions shape. The upstream accepts the OpenAI wire format, with two
// deviations: tool_choice must be a string, and max_tokens is the only cap.
func buildChatBody(req translate.ChatRequest, model, level string, benefit bool) (map[string]any, error) {
	tools, choice, err := nativeTools(req.Tools, req.ToolChoice)
	if err != nil {
		return nil, err
	}
	messages := upstreamMessages(req.Messages)
	if len(messages) == 0 {
		return nil, fmt.Errorf("codearts request has no messages")
	}
	body := map[string]any{
		"model":            model,
		"stream":           true,
		"messages":         messages,
		"tool_stream":      true,
		"prompt_cache_key": chatIDFor(req.Messages),
	}
	if chatID := chatIDFor(req.Messages); chatID != "" {
		body["chat_id"] = chatID
	}
	if level != "" && level != "none" {
		body["reasoning_effort"] = level
	}
	if value := rawNumber(req.MaxTokens); value != nil {
		body["max_tokens"] = value
	}
	if req.MaxCompletionTokens != nil {
		if value := rawNumber(req.MaxCompletionTokens); value != nil {
			body["max_tokens"] = value
		}
	}
	if value := rawNumber(req.Temperature); value != nil {
		body["temperature"] = value
	}
	if value := rawNumber(req.TopP); value != nil {
		body["top_p"] = value
	}
	if len(tools) > 0 {
		body["tools"] = tools
		if choice != "" {
			body["tool_choice"] = choice
		}
	}
	_ = benefit // benefit routing is a header, not a body field
	return body, nil
}

// chatIDFor derives a stable upstream chat id from the first user message. The
// V2 endpoint does not keep semantic context (the full message list is replayed
// on every turn); the id only drives prompt-cache affinity.
func chatIDFor(messages []translate.ChatMessage) string {
	for _, message := range messages {
		if !strings.EqualFold(strings.TrimSpace(message.Role), "user") {
			continue
		}
		text := strings.TrimSpace(translate.ContentToString(message.Content))
		if text == "" {
			continue
		}
		sum := sha256.Sum256([]byte("codearts:" + text))
		return hex.EncodeToString(sum[:16])
	}
	return ""
}

// upstreamMessages maps roles and content, dropping incomplete tool pairs.
func upstreamMessages(messages []translate.ChatMessage) []map[string]any {
	keepCalls, keepResults := pairedToolMessages(messages)
	out := make([]map[string]any, 0, len(messages))
	for index, message := range messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role == "developer" {
			role = "system"
		}
		switch role {
		case "system", "user":
			content := translate.ContentToString(message.Content)
			if strings.TrimSpace(content) == "" {
				continue
			}
			out = append(out, map[string]any{"role": role, "content": content})
		case "assistant":
			wire := map[string]any{"role": "assistant", "content": translate.ContentToString(message.Content)}
			if strings.TrimSpace(message.ReasoningContent) != "" {
				wire["reasoning_content"] = message.ReasoningContent
			}
			if keepCalls[index] && len(message.ToolCalls) > 0 {
				if calls := decodeToolCalls(message.ToolCalls); len(calls) > 0 {
					wire["tool_calls"] = calls
					if strings.TrimSpace(translate.ContentToString(message.Content)) == "" {
						wire["content"] = nil
					}
				}
			}
			out = append(out, wire)
		case "tool":
			if !keepResults[index] {
				continue
			}
			content := translate.ContentToString(message.Content)
			if strings.TrimSpace(content) == "" {
				content = "(no output)"
			}
			wire := map[string]any{"role": "tool", "content": content, "tool_call_id": message.ToolCallID}
			if strings.TrimSpace(message.Name) != "" {
				wire["name"] = message.Name
			}
			out = append(out, wire)
		}
	}
	return out
}

// pairedToolMessages keeps an assistant tool_calls block only when every call
// has a matching tool result. CodeArts rejects orphaned tool history with a 400.
func pairedToolMessages(messages []translate.ChatMessage) (map[int]bool, map[int]bool) {
	resultIndex := map[string]int{}
	for index, message := range messages {
		if strings.EqualFold(strings.TrimSpace(message.Role), "tool") && message.ToolCallID != "" {
			resultIndex[message.ToolCallID] = index
		}
	}
	keepCalls := map[int]bool{}
	keepResults := map[int]bool{}
	for index, message := range messages {
		if !strings.EqualFold(strings.TrimSpace(message.Role), "assistant") || len(message.ToolCalls) == 0 {
			continue
		}
		calls := decodeToolCalls(message.ToolCalls)
		if len(calls) == 0 {
			continue
		}
		complete := true
		for _, call := range calls {
			id, _ := call["id"].(string)
			if id == "" {
				complete = false
				break
			}
			if _, ok := resultIndex[id]; !ok {
				complete = false
				break
			}
		}
		if !complete {
			continue
		}
		keepCalls[index] = true
		for _, call := range calls {
			id, _ := call["id"].(string)
			keepResults[resultIndex[id]] = true
		}
	}
	return keepCalls, keepResults
}

// decodeToolCalls normalizes arguments to the string form OpenAI clients send.
func decodeToolCalls(raw json.RawMessage) []map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var calls []map[string]any
	if err := json.Unmarshal(raw, &calls); err != nil {
		return nil
	}
	out := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		id, _ := call["id"].(string)
		callType, _ := call["type"].(string)
		if callType == "" {
			callType = "function"
		}
		fn, _ := call["function"].(map[string]any)
		name, _ := fn["name"].(string)
		if name == "" {
			continue
		}
		arguments := fn["arguments"]
		wire := map[string]any{
			"id":   id,
			"type": callType,
			"function": map[string]any{
				"name":      name,
				"arguments": rawArguments(arguments),
			},
		}
		out = append(out, wire)
	}
	return out
}

func rawArguments(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return "{}"
	default:
		if raw, err := json.Marshal(typed); err == nil {
			return string(raw)
		}
		return "{}"
	}
}

// nativeTools narrows a named function choice to that one tool and converts the
// choice to the string form CodeArts accepts.
func nativeTools(raw json.RawMessage, choiceRaw json.RawMessage) ([]map[string]any, string, error) {
	var tools []map[string]any
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &tools); err != nil {
			return nil, "", &providers.Error{Kind: accounts.KindInvalidRequest, Status: 400, Message: "codearts tools must be an array"}
		}
	}
	mode, function := parseToolChoice(choiceRaw)
	if mode == "" {
		mode = "auto"
	}
	if mode == "function" {
		var selected map[string]any
		for _, tool := range tools {
			fn, _ := tool["function"].(map[string]any)
			if name, _ := fn["name"].(string); name == function {
				if selected != nil {
					return nil, "", fmt.Errorf("tool_choice function %q is defined more than once", function)
				}
				selected = tool
			}
		}
		if selected == nil {
			return nil, "", fmt.Errorf("tool_choice function %q is not defined in tools", function)
		}
		tools = []map[string]any{selected}
		mode = "required"
	}
	if len(tools) == 0 {
		return nil, "", nil
	}
	switch mode {
	case "none", "required", "auto":
		return tools, mode, nil
	default:
		return tools, "auto", nil
	}
}

func parseToolChoice(raw json.RawMessage) (mode, function string) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		switch strings.ToLower(strings.TrimSpace(text)) {
		case "none", "required", "auto":
			return strings.ToLower(strings.TrimSpace(text)), ""
		}
		return "auto", ""
	}
	var object struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(raw, &object) == nil && object.Function.Name != "" {
		return "function", object.Function.Name
	}
	return "auto", ""
}

// rawNumber decodes an optional numeric JSON field without losing precision.
func rawNumber(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil
	}
	switch value.(type) {
	case json.Number:
		return value
	default:
		return nil
	}
}
