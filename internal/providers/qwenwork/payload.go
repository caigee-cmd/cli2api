package qwenwork

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

const (
	defaultMaxTokens      = 32000
	defaultMaxInputTokens = 180000
)

// buildChatBody is the plaintext agent_chat_generation document the QwenWork
// desktop sends. System and developer turns are lifted into "system"; the
// remaining turns go upstream unchanged, so multi-turn history is carried by
// the caller on every request. maxOutput is the catalog output limit (0 when
// unknown); max_tokens never exceeds it.
func buildChatBody(req translate.ChatRequest, modelKey string, maxOutput int) map[string]any {
	var systemParts []string
	messages := make([]map[string]any, 0, len(req.Messages))
	lastUser := ""
	for _, message := range req.Messages {
		if message.Role == "system" || message.Role == "developer" {
			if text := strings.TrimSpace(translate.ContentToString(message.Content)); text != "" {
				systemParts = append(systemParts, text)
			}
			continue
		}
		item := map[string]any{"role": message.Role, "content": normalizeMessageContent(message)}
		if message.Name != "" {
			item["name"] = message.Name
		}
		if message.ToolCallID != "" {
			item["tool_call_id"] = message.ToolCallID
		}
		if len(message.ToolCalls) > 0 {
			item["tool_calls"] = json.RawMessage(message.ToolCalls)
		}
		if message.Role == "user" {
			lastUser = translate.ContentToString(message.Content)
		}
		messages = append(messages, item)
	}
	if len(messages) == 0 {
		messages = []map[string]any{{"role": "user", "content": "ping"}}
	}
	if strings.TrimSpace(lastUser) == "" {
		lastUser = "ping"
	}
	systemText := strings.Join(systemParts, "\n\n")

	maxTokens := defaultMaxTokens
	if n, ok := rawNumber(req.MaxCompletionTokens); ok && n > 0 {
		maxTokens = n
	} else if n, ok := rawNumber(req.MaxTokens); ok && n > 0 {
		maxTokens = n
	}
	if maxOutput > 0 && maxTokens > maxOutput {
		maxTokens = maxOutput
	}
	parameters := map[string]any{"max_tokens": maxTokens}
	copyRaw(parameters, "temperature", req.Temperature)
	copyRaw(parameters, "top_p", req.TopP)
	copyRaw(parameters, "stop", req.Stop)

	tools := rawOrEmpty(req.Tools)
	if strings.Trim(string(bytes.TrimSpace(req.ToolChoice)), `"`) == "none" {
		tools = []any{}
	}

	requestID := newID()
	return map[string]any{
		"request_id":     requestID,
		"request_set_id": requestID,
		"chat_record_id": requestID,
		"session_id":     newID(),
		"stream":         true,
		"chat_task":      "FREE_INPUT",
		"chat_context": map[string]any{
			"text":     lastUser,
			"features": []any{},
			"extra": map[string]any{
				"context":         []any{},
				"modelConfig":     map[string]any{"key": modelKey, "is_reasoning": false},
				"originalContent": lastUser,
			},
			"chatPrompt": "",
			"imageUrls":  nil,
		},
		"is_reply":         true,
		"is_retry":         false,
		"source":           1,
		"version":          "3",
		"agent_id":         "agent_common",
		"task_id":          "common",
		"session_type":     businessProduct,
		"aliyun_user_type": "",
		"model_config": map[string]any{
			"key": modelKey, "display_name": modelKey, "model": "", "format": "openai",
			"is_vl": true, "is_reasoning": false, "api_key": "", "url": "",
			"source": "system", "max_input_tokens": defaultMaxInputTokens,
		},
		"system":     systemText,
		"messages":   messages,
		"tools":      tools,
		"parameters": parameters,
		"business": map[string]any{
			"product": businessProduct, "type": businessType, "version": "1",
			"feature_switches": map[string]any{},
		},
	}
}

// normalizeMessageContent sends "" instead of null for an assistant turn that
// is pure tool calls; the gateway only pairs the following role:"tool"
// message when content is a string.
func normalizeMessageContent(message translate.ChatMessage) any {
	if len(message.ToolCalls) > 0 && message.Content == nil {
		return ""
	}
	return message.Content
}

func rawNumber(raw json.RawMessage) (int, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var n int
	if json.Unmarshal(raw, &n) != nil {
		return 0, false
	}
	return n, true
}

func copyRaw(dst map[string]any, key string, raw json.RawMessage) {
	if len(bytes.TrimSpace(raw)) == 0 || string(raw) == "null" {
		return
	}
	dst[key] = json.RawMessage(raw)
}

func rawOrEmpty(raw json.RawMessage) any {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return []any{}
	}
	return json.RawMessage(raw)
}
