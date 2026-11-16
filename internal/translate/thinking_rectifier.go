package translate

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RectifyResult mirrors cc-switch's thinking rectifier counters. The gateway
// only ever removes fields that OpenAI-compatible upstreams cannot validate;
// it never invents signatures or rewrites model output.
type RectifyResult struct {
	Applied                         bool `json:"applied"`
	RemovedThinkingBlocks           int  `json:"removed_thinking_blocks,omitempty"`
	RemovedRedactedThinkingBlocks   int  `json:"removed_redacted_thinking_blocks,omitempty"`
	RemovedSignatureFields          int  `json:"removed_signature_fields,omitempty"`
	RemovedTopLevelThinking         bool `json:"removed_top_level_thinking,omitempty"`
}

// ShouldRectifyThinking mirrors cc-switch's error-triggered detection. It is
// deliberately narrow: only upstream errors that name an invalid/missing
// thinking signature or a malformed thinking block sequence qualify.
func ShouldRectifyThinking(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}
	if strings.Contains(lower, "invalid") && strings.Contains(lower, "signature") && strings.Contains(lower, "thinking") {
		return true
	}
	if strings.Contains(lower, "thought signature") && (strings.Contains(lower, "not valid") || strings.Contains(lower, "invalid")) {
		return true
	}
	if strings.Contains(lower, "must start with a thinking block") {
		return true
	}
	if strings.Contains(lower, "expected") && (strings.Contains(lower, "thinking") || strings.Contains(lower, "redacted_thinking")) && strings.Contains(lower, "tool_use") {
		return true
	}
	if strings.Contains(lower, "signature") && (strings.Contains(lower, "field required") || strings.Contains(lower, "extra inputs are not permitted")) {
		return true
	}
	if (strings.Contains(lower, "thinking") || strings.Contains(lower, "redacted_thinking")) && strings.Contains(lower, "cannot be modified") {
		return true
	}
	if strings.Contains(lower, "budget_tokens") && strings.Contains(lower, "thinking") && (strings.Contains(lower, "1024") || strings.Contains(lower, "input should be")) {
		return true
	}
	return false
}

// RectifyAnthropicRequest applies the minimal cc-switch cleanup to a decoded
// Anthropic Messages body. Historical thinking blocks carry signatures that
// belong to a different provider; replaying them upstream causes 400 errors or
// visible <thinking> scaffolding in clients. Plain text, tool calls and tool
// results are preserved byte-for-byte where possible.
func RectifyAnthropicRequest(body []byte) ([]byte, RectifyResult, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, RectifyResult{}, err
	}
	result := RectifyResult{}
	messagesRaw := root["messages"]
	var messages []json.RawMessage
	if len(messagesRaw) > 0 && string(messagesRaw) != "null" {
		if err := json.Unmarshal(messagesRaw, &messages); err != nil {
			return nil, result, fmt.Errorf("messages: %w", err)
		}
	}
	changed := false
	for i, raw := range messages {
		var message map[string]json.RawMessage
		if err := json.Unmarshal(raw, &message); err != nil {
			continue
		}
		contentRaw := message["content"]
		var blocks []json.RawMessage
		if len(contentRaw) == 0 || json.Unmarshal(contentRaw, &blocks) != nil {
			continue
		}
		kept := make([]json.RawMessage, 0, len(blocks))
		messageChanged := false
		for _, blockRaw := range blocks {
			var block map[string]json.RawMessage
			if json.Unmarshal(blockRaw, &block) != nil {
				kept = append(kept, blockRaw)
				continue
			}
			kind := rawMapString(block, "type")
			if kind == "thinking" {
				result.RemovedThinkingBlocks++
				messageChanged = true
				continue
			}
			if kind == "redacted_thinking" {
				result.RemovedRedactedThinkingBlocks++
				messageChanged = true
				continue
			}
			if _, exists := block["signature"]; exists {
				delete(block, "signature")
				result.RemovedSignatureFields++
				messageChanged = true
				encoded, err := json.Marshal(block)
				if err != nil {
					return nil, result, err
				}
				kept = append(kept, encoded)
				continue
			}
			kept = append(kept, blockRaw)
		}
		if messageChanged {
			encoded, err := json.Marshal(kept)
			if err != nil {
				return nil, result, err
			}
			message["content"] = encoded
			encodedMessage, err := json.Marshal(message)
			if err != nil {
				return nil, result, err
			}
			messages[i] = encodedMessage
			changed = true
		}
	}
	// cc-switch fallback: when thinking is enabled and the last assistant turn
	// starts with a tool_use instead of a thinking block, drop the top-level
	// switch rather than replaying an invalid sequence.
	if shouldRemoveTopLevelThinking(root, messages) {
		delete(root, "thinking")
		result.RemovedTopLevelThinking = true
		changed = true
	}
	if changed {
		encodedMessages, err := json.Marshal(messages)
		if err != nil {
			return nil, result, err
		}
		root["messages"] = encodedMessages
		out, err := json.Marshal(root)
		if err != nil {
			return nil, result, err
		}
		result.Applied = true
		return out, result, nil
	}
	return body, result, nil
}

func shouldRemoveTopLevelThinking(root map[string]json.RawMessage, messages []json.RawMessage) bool {
	var thinking map[string]json.RawMessage
	if json.Unmarshal(root["thinking"], &thinking) != nil {
		return false
	}
	if rawMapString(thinking, "type") != "enabled" {
		return false
	}
	for i := len(messages) - 1; i >= 0; i-- {
		var message map[string]json.RawMessage
		if json.Unmarshal(messages[i], &message) != nil {
			continue
		}
		if rawMapString(message, "role") != "assistant" {
			continue
		}
		var blocks []json.RawMessage
		if json.Unmarshal(message["content"], &blocks) != nil || len(blocks) == 0 {
			return false
		}
		var first map[string]json.RawMessage
		if json.Unmarshal(blocks[0], &first) != nil {
			return false
		}
		kind := rawMapString(first, "type")
		if kind == "thinking" || kind == "redacted_thinking" {
			return false
		}
		for _, raw := range blocks {
			var block map[string]json.RawMessage
			if json.Unmarshal(raw, &block) == nil && rawMapString(block, "type") == "tool_use" {
				return true
			}
		}
		return false
	}
	return false
}
