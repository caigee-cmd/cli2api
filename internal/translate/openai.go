package translate

import "encoding/json"

type ChatRequest struct {
	Model                 string          `json:"model"`
	Messages              []ChatMessage   `json:"messages"`
	Stream                bool            `json:"stream"`
	MaxTokens             json.RawMessage `json:"max_tokens"`
	Temperature           json.RawMessage `json:"temperature"`
	Tools                 json.RawMessage `json:"tools,omitempty"`
	ToolChoice            json.RawMessage `json:"tool_choice,omitempty"`
	IsReasoning           *bool           `json:"is_reasoning,omitempty"`
	EnableThinking        *bool           `json:"enable_thinking,omitempty"`
	EnableReasoning       *bool           `json:"enable_reasoning,omitempty"`
	Thinking              json.RawMessage `json:"thinking,omitempty"`
	ReasoningEffort       json.RawMessage `json:"reasoning_effort,omitempty"`
	ReasoningBudgetTokens json.RawMessage `json:"reasoning_budget_tokens,omitempty"`
	ContextLength         json.RawMessage `json:"context_length,omitempty"`
	MaxInputTokens        json.RawMessage `json:"max_input_tokens,omitempty"`
}

type ChatMessage struct {
	Role       string          `json:"role"`
	Content    any             `json:"content"`
	Name       string          `json:"name,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`
}

// DropSystemMessages removes caller system/developer messages from a chat
// request. Provider-native upstreams with content screening reject many
// third-party system prompts, so accounts can opt to strip them before send.
func DropSystemMessages(req ChatRequest) ChatRequest {
	kept := make([]ChatMessage, 0, len(req.Messages))
	for _, message := range req.Messages {
		if message.Role == "system" || message.Role == "developer" {
			continue
		}
		kept = append(kept, message)
	}
	req.Messages = kept
	return req
}

func ContentToString(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		parts := make([]byte, 0)
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if t, ok := m["text"].(string); ok {
				if len(parts) > 0 {
					parts = append(parts, '\n')
				}
				parts = append(parts, t...)
			}
		}
		return string(parts)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}
