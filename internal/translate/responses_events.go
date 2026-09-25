package translate

import (
	"encoding/json"
	"strings"
)

// ResponsesEvent is the accounting view of one OpenAI Responses SSE event.
// The stream relay and the non-stream collector both read events through it,
// so "what counts as terminal" and "where usage lives" are decided once.
type ResponsesEvent struct {
	Type string
	// Terminal marks response.completed / response.incomplete /
	// response.failed and bare error events.
	Terminal bool
	// FinishReason is "stop", "length" (incomplete), or "error".
	FinishReason string
	// Response is the full response object carried by a terminal success
	// event, verbatim.
	Response json.RawMessage
	// Usage is present only when the terminal event reported it.
	InputTokens  *int
	OutputTokens *int
	CachedTokens *int
	// FirstToken marks an output delta (first-token timing).
	FirstToken bool
}

// ParseResponsesEvent decodes one SSE data payload. ok is false for payloads
// that are not a JSON object with a type (keep-alives, [DONE], junk); callers
// relay those untouched.
func isOutputTextDelta(eventType string) bool {
	return strings.HasSuffix(eventType, "output_text.delta")
}

func ParseResponsesEvent(data string) (ResponsesEvent, bool) {
	payload := strings.TrimSpace(data)
	if payload == "" || payload == "[DONE]" {
		return ResponsesEvent{}, false
	}
	var envelope struct {
		Type     string          `json:"type"`
		Response json.RawMessage `json:"response"`
	}
	if json.Unmarshal([]byte(payload), &envelope) != nil || envelope.Type == "" {
		return ResponsesEvent{}, false
	}
	event := ResponsesEvent{Type: envelope.Type, FirstToken: isOutputTextDelta(envelope.Type)}
	switch envelope.Type {
	case "response.completed":
		event.Terminal, event.FinishReason = true, "stop"
	case "response.incomplete":
		event.Terminal, event.FinishReason = true, "length"
	case "response.failed", "error":
		event.Terminal, event.FinishReason = true, "error"
		return event, true
	default:
		return event, true
	}
	event.Response = append(json.RawMessage(nil), envelope.Response...)
	var body struct {
		Usage *struct {
			InputTokens        *int `json:"input_tokens"`
			OutputTokens       *int `json:"output_tokens"`
			InputTokensDetails *struct {
				CachedTokens *int `json:"cached_tokens"`
			} `json:"input_tokens_details"`
		} `json:"usage"`
	}
	if json.Unmarshal(envelope.Response, &body) == nil && body.Usage != nil {
		event.InputTokens = body.Usage.InputTokens
		event.OutputTokens = body.Usage.OutputTokens
		if body.Usage.InputTokensDetails != nil {
			event.CachedTokens = body.Usage.InputTokensDetails.CachedTokens
		}
	}
	return event, true
}
