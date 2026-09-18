package api

import (
	"encoding/json"
	"net/http"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

type compatibilityExecution = chatExecution

type proxyToolCall struct {
	ID        string
	Name      string
	Arguments string
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

func (s *Server) prepareCompatibilityExecution(r *http.Request, request translate.ChatRequest) (compatibilityExecution, error) {
	if len(request.Messages) == 0 {
		return compatibilityExecution{}, &chatHTTPError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "input messages required"}
	}
	return s.prepareChatExecution(r, request)
}

func (s *Server) finishCompatibility(execution compatibilityExecution, accountID, provider, routing, status string, ttfb int, stats *streamRelayStats, err error, attempts int) {
	s.finishRequestLog(execution.requestID, execution.started, execution.request, execution.publicModel, accountID, firstNonEmpty(provider, execution.providerFilter), routing, status, ttfb, stats, err, attempts)
}
