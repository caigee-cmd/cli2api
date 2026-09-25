package codex

import (
	"encoding/json"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// buildBody converts the provider-neutral ChatRequest into a codex Responses
// payload. Upstream is the ChatGPT backend Responses endpoint, so messages map
// to `input` items and most chat-completions fields carry over directly.
func buildBody(req translate.ChatRequest, caps providers.ModelCapabilities) ([]byte, providers.ResolvedChat, error) {
	obj := map[string]any{
		"model":  req.Model,
		"stream": true,
		// The codex backend requires instructions present; empty when the caller
		// did not provide a system turn (mirrors CLIProxyAPI normalizeCodexInstructions).
		"instructions": "",
	}

	input, err := buildInput(req)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	obj["input"] = input

	if len(req.Tools) > 0 {
		var tools []json.RawMessage
		if err := json.Unmarshal(req.Tools, &tools); err == nil && len(tools) > 0 {
			obj["tools"] = normalizeTools(tools)
			if req.ToolChoice != nil {
				obj["tool_choice"] = normalizeToolChoice(req.ToolChoice)
			}
			if req.ParallelToolCalls != nil {
				obj["parallel_tool_calls"] = *req.ParallelToolCalls
			}
		}
	}
	if len(req.ResponseFormat) > 0 && string(req.ResponseFormat) != "null" {
		obj["text"] = map[string]any{"format": req.ResponseFormat}
	}

	resolved := providers.ResolvedChat{}
	if level := resolveReasoning(req, caps); level != "" {
		obj["reasoning"] = map[string]any{"effort": level}
		resolved.ReasoningLevel = level
	}

	encoded, err := json.Marshal(obj)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	// Chat and Messages both arrive here already translated. The Codex backend
	// constraints are the same ones the native Responses path applies, so a
	// translated request is not a looser shape than a native one.
	if err := normalizeCodexUpstream(fields, req.Model, usesResponsesLite(req.Model), ""); err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	body, err := json.Marshal(fields)
	return body, resolved, err
}

// resolveReasoning maps the client value through the shared clamp: catalog
// options win, unknown/empty falls back to the model default.
func resolveReasoning(req translate.ChatRequest, caps providers.ModelCapabilities) string {
	requested := ""
	if len(req.ReasoningEffort) > 0 {
		var s string
		if json.Unmarshal(req.ReasoningEffort, &s) == nil {
			requested = s
		}
	}
	return providers.ResolveReasoningLevel(requested, caps)
}

// buildInput converts chat messages into Responses input items.
func buildInput(req translate.ChatRequest) ([]any, error) {
	input := make([]any, 0, len(req.Messages))
	for _, msg := range req.Messages {
		role := msg.Role
		switch role {
		case "system", "developer":
			role = "developer"
		}
		item := map[string]any{
			"type": "message",
			"role": role,
		}
		if msg.ToolCallID != "" {
			// Tool result → function_call_output item.
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": msg.ToolCallID,
				"output":  translate.ContentToString(msg.Content),
			})
			continue
		}
		content, err := responsesContent(msg)
		if err != nil {
			return nil, err
		}
		item["content"] = content
		input = append(input, item)
		if len(msg.ToolCalls) > 0 {
			calls, err := functionCallItems(msg.ToolCalls)
			if err != nil {
				return nil, err
			}
			input = append(input, calls...)
		}
	}
	return input, nil
}

// responsesContent maps a chat message's content to Responses content parts.
func responsesContent(msg translate.ChatMessage) (any, error) {
	switch content := msg.Content.(type) {
	case string:
		partType := "input_text"
		if msg.Role == "assistant" {
			partType = "output_text"
		}
		return []any{map[string]any{"type": partType, "text": content}}, nil
	case []any:
		parts := make([]any, 0, len(content))
		for _, raw := range content {
			part, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			typ, _ := part["type"].(string)
			switch typ {
			case "text":
				partType := "input_text"
				if msg.Role == "assistant" {
					partType = "output_text"
				}
				parts = append(parts, map[string]any{"type": partType, "text": part["text"]})
			case "image_url":
				parts = append(parts, map[string]any{"type": "input_image", "image_url": imageURLString(part["image_url"])})
			default:
				return nil, errUnsupportedContent(typ)
			}
		}
		return parts, nil
	case nil:
		return []any{}, nil
	default:
		return translate.ContentToString(content), nil
	}
}

func imageURLString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	if m, ok := value.(map[string]any); ok {
		if u, ok := m["url"].(string); ok {
			return u
		}
	}
	return ""
}

func errUnsupportedContent(typ string) error {
	return &providers.Error{Kind: "invalid_request", Status: 400, Message: "unsupported content type: " + typ}
}

// functionCallItems converts OpenAI tool_calls on an assistant message into
// Responses function_call items.
func functionCallItems(raw json.RawMessage) ([]any, error) {
	var calls []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &calls); err != nil {
		return nil, err
	}
	items := make([]any, 0, len(calls))
	for _, call := range calls {
		args := call.Function.Arguments
		if len(args) == 0 {
			args = json.RawMessage(`{}`)
		}
		// Responses expects arguments as a string.
		var argStr string
		if s, ok := rawJSONString(args); ok {
			argStr = s
		} else {
			argStr = string(args)
		}
		items = append(items, map[string]any{
			"type":      "function_call",
			"call_id":   call.ID,
			"name":      call.Function.Name,
			"arguments": argStr,
		})
	}
	return items, nil
}

// normalizeTools converts OpenAI function tools into Responses tool shape.
func normalizeTools(raw []json.RawMessage) []any {
	out := make([]any, 0, len(raw))
	for _, item := range raw {
		var tool map[string]json.RawMessage
		if err := json.Unmarshal(item, &tool); err != nil {
			continue
		}
		typ := rawMapString(tool, "type")
		switch typ {
		case "function":
			var fn map[string]json.RawMessage
			if err := json.Unmarshal(tool["function"], &fn); err != nil {
				continue
			}
			out = append(out, map[string]any{
				"type":        "function",
				"name":        rawMapString(fn, "name"),
				"description": rawMapString(fn, "description"),
				"parameters":  rawMapJSONValue(fn, "parameters"),
			})
		default:
			// Already Responses-shaped or a hosted tool — pass through.
			var passthrough map[string]any
			if json.Unmarshal(item, &passthrough) == nil {
				out = append(out, passthrough)
			}
		}
	}
	return out
}

// normalizeToolChoice converts an OpenAI tool_choice to the Responses form.
func normalizeToolChoice(raw json.RawMessage) any {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return nil
	}
	if rawMapString(obj, "type") == "function" {
		var fn map[string]json.RawMessage
		if json.Unmarshal(obj["function"], &fn) == nil {
			return map[string]any{"type": "function", "name": rawMapString(fn, "name")}
		}
	}
	return nil
}

func rawMapString(source map[string]json.RawMessage, key string) string {
	var s string
	if json.Unmarshal(source[key], &s) != nil {
		return ""
	}
	return s
}

func rawMapJSONValue(source map[string]json.RawMessage, key string) json.RawMessage {
	value := source[key]
	if len(value) == 0 || string(value) == "null" {
		return nil
	}
	return value
}

func rawJSONString(raw json.RawMessage) (string, bool) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}
