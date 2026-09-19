package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

type proxyToolCall struct {
	ID        string
	Name      string
	Arguments string
}

func proxyToolCallItem(requestID string, callIndex int, call proxyToolCall) map[string]any {
	namespace, name, custom := translate.DecodeCustomToolName(call.Name)
	if !custom {
		return responseFunctionCallItem(requestID, callIndex, call)
	}
	input := ""
	if raw := strings.TrimSpace(call.Arguments); raw != "" {
		var payload struct {
			Input string `json:"input"`
		}
		if json.Unmarshal([]byte(raw), &payload) == nil {
			input = payload.Input
		} else {
			input = call.Arguments
		}
	}
	item := map[string]any{
		"id": fmt.Sprintf("ctc_%s_%d", requestID, callIndex), "type": "custom_tool_call", "status": "completed",
		"call_id": call.ID, "name": name, "input": input,
	}
	if namespace != "" {
		item["namespace"] = namespace
	}
	return item
}

func decodeOpenAIToolCalls(raw json.RawMessage) []proxyToolCall {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var source []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	if json.Unmarshal(raw, &source) != nil {
		return nil
	}
	calls := make([]proxyToolCall, 0, len(source))
	for _, call := range source {
		if call.Function.Name == "" {
			continue
		}
		calls = append(calls, proxyToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
	}
	return calls
}

func (h *Handler) PrepareCompatibilityExecution(r *http.Request, request translate.ChatRequest) (Execution, error) {
	if len(request.Messages) == 0 {
		return Execution{}, &chatHTTPError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "input messages required"}
	}
	return h.PrepareChatExecution(r, request)
}

func (h *Handler) finishCompatibility(execution Execution, accountID, provider, routing, status string, ttfb int, stats *StreamRelayStats, err error, attempts int, resolvedReasoning string) {
	h.finishRequestLog(execution.RequestID, execution.Started, execution.Request, execution.PublicModel, accountID, firstNonEmpty(provider, execution.ProviderFilter), routing, status, ttfb, stats, err, attempts, resolvedReasoning)
}
