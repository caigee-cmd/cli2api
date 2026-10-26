package kimi

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// buildChatBody serializes the internal chat request for the managed
// Chat Completions endpoint. The gateway request is already an OpenAI body, so
// the adapter only applies Kimi's deviations from plain OpenAI:
//
//   - `thinking` is a top-level object, not `reasoning_effort`;
//   - `max_tokens` is not accepted, only `max_completion_tokens`;
//   - `temperature` only accepts 0.6 (thinking off) or 1.0 (thinking on);
//   - the `developer` role is not accepted;
//   - `stream_options.include_usage` is required or usage never arrives;
//   - tool-call ids are capped at 64 characters.
func buildChatBody(req translate.ChatRequest, model string, caps providers.ModelCapabilities, level string) ([]byte, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&obj); err != nil {
		return nil, err
	}

	obj["model"] = model
	obj["stream"] = true
	obj["stream_options"] = map[string]any{"include_usage": true}
	normalizeTokenLimit(obj)
	rewriteDeveloperRoles(obj)
	applyThinking(obj, caps, level)
	normalizeTemperature(obj, caps, level)
	sanitizeToolCallIDs(obj)
	dropGatewayFields(obj)
	return json.Marshal(obj)
}

// normalizeTokenLimit keeps the single cap Kimi accepts. `max_completion_tokens`
// wins when both are present, matching the CLI.
func normalizeTokenLimit(obj map[string]any) {
	if _, ok := obj["max_completion_tokens"]; !ok {
		if legacy, ok := obj["max_tokens"]; ok && legacy != nil {
			obj["max_completion_tokens"] = legacy
		}
	}
	delete(obj, "max_tokens")
}

func rewriteDeveloperRoles(obj map[string]any) {
	messages, ok := obj["messages"].([]any)
	if !ok {
		return
	}
	for _, item := range messages {
		message, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role, _ := message["role"].(string)
		if strings.EqualFold(strings.TrimSpace(role), "developer") {
			message["role"] = "system"
		}
	}
}

// applyThinking writes Kimi's `thinking` object. `none` is only sent as
// `disabled` for models that declare a switch; always-thinking models must
// never receive a disable request. Models without declared reasoning send no
// thinking object at all.
func applyThinking(obj map[string]any, caps providers.ModelCapabilities, level string) {
	delete(obj, "reasoning_effort")
	if len(caps.ReasoningOptions) == 0 {
		delete(obj, "thinking")
		return
	}
	if level == "none" {
		if caps.CanDisableThinking {
			obj["thinking"] = map[string]any{"type": "disabled"}
		} else {
			delete(obj, "thinking")
		}
		return
	}
	thinking := map[string]any{"type": "enabled"}
	if effort := providers.NormalizeReasoningLevel(level); effort != "" && effort != "none" {
		thinking["effort"] = effort
	}
	obj["thinking"] = thinking
}

// normalizeTemperature drops a temperature Kimi would reject. The upstream only
// accepts 1.0 while thinking is on and 0.6 while it is off; any other value must
// be omitted rather than clamped, so the caller's intent is never silently
// rewritten into a different sampling regime.
func normalizeTemperature(obj map[string]any, caps providers.ModelCapabilities, level string) {
	raw, ok := obj["temperature"]
	if !ok || raw == nil {
		return
	}
	text := strings.TrimSpace(rawText(raw))
	if text == "" {
		delete(obj, "temperature")
		return
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		delete(obj, "temperature")
		return
	}
	allowed := 0.6
	if thinkingEnabled(caps, level) {
		allowed = 1.0
	}
	if value != allowed {
		delete(obj, "temperature")
	}
}

func thinkingEnabled(caps providers.ModelCapabilities, level string) bool {
	if len(caps.ReasoningOptions) == 0 {
		return false
	}
	if level == "none" {
		return false
	}
	return true
}

// sanitizeToolCallIDs caps tool-call ids at 64 characters, the limit the Kimi
// endpoint enforces, on both the calls and the tool results that reference them.
// The mapping is applied consistently so a tool result keeps pointing at the
// call it answers.
func sanitizeToolCallIDs(obj map[string]any) {
	messages, ok := obj["messages"].([]any)
	if !ok {
		return
	}
	remap := map[string]string{}
	for _, item := range messages {
		message, ok := item.(map[string]any)
		if !ok {
			continue
		}
		calls, ok := message["tool_calls"].([]any)
		if !ok {
			continue
		}
		for _, entry := range calls {
			call, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			id, _ := call["id"].(string)
			if id == "" {
				continue
			}
			safe := toolCallID(id)
			if safe != id {
				remap[id] = safe
				call["id"] = safe
			}
		}
	}
	if len(remap) == 0 {
		return
	}
	for _, item := range messages {
		message, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := message["tool_call_id"].(string)
		if safe, ok := remap[id]; ok {
			message["tool_call_id"] = safe
		}
	}
}

func toolCallID(id string) string {
	if len(id) <= 64 {
		return id
	}
	return id[:64]
}

// dropGatewayFields removes router-only members that are part of the internal
// request model but not of the OpenAI wire contract.
func dropGatewayFields(obj map[string]any) {
	for _, key := range []string{
		"is_reasoning",
		"enable_thinking",
		"enable_reasoning",
		"reasoning_budget_tokens",
		"context_length",
		"max_input_tokens",
		"is_max_mode",
	} {
		delete(obj, key)
	}
}

func rawText(value any) string {
	switch typed := value.(type) {
	case json.Number:
		return typed.String()
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}
