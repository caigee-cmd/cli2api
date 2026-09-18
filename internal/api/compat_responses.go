package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	var source translate.ResponsesRequest
	if err := json.NewDecoder(r.Body).Decode(&source); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	request, err := translate.TranslateResponses(source)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	execution, err := s.prepareCompatibilityExecution(r, request)
	if err != nil {
		writeCompatibilityOpenAIError(w, err)
		return
	}
	w.Header().Set("X-Request-Id", execution.requestID)
	if execution.request.Stream {
		s.handleResponsesStream(w, r, execution)
		return
	}
	result, err := s.executor.ChatNonStream(execution.ctx, execution.request, execution.prefer, execution.providerFilter)
	if err != nil {
		s.finishCompatibility(execution, result.AccountID, result.Provider, result.Routing, accounts.RequestStatusError, 0, nil, err, result.AttemptCount)
		writeCompatibilityOpenAIError(w, err)
		return
	}
	s.finishCompatibility(execution, result.AccountID, result.Provider, result.Routing, accounts.RequestStatusOK, 0, &streamRelayStats{
		PromptTokens: ptrInt(result.PromptTokens), CompletionTokens: ptrInt(result.CompletionTokens),
		CacheReadTokens: result.CacheReadTokens, CacheWriteTokens: result.CacheWriteTokens,
		CachedTokens: result.CachedTokens, UsageSource: result.UsageSource, Credits: result.Credits,
		ConsumedCredits: result.ConsumedCredits, Model: result.Model,
	}, nil, result.AttemptCount)
	writeJSON(w, http.StatusOK, responsesResponse(execution.requestID, firstNonEmpty(result.Model, execution.publicModel), result.Content, result.Reasoning, decodeOpenAIToolCalls(result.ToolCalls), result.PromptTokens, result.CompletionTokens))
}

func (s *Server) handleResponsesStream(w http.ResponseWriter, r *http.Request, execution compatibilityExecution) {
	upstream, err := s.executor.ChatStreamProxy(execution.ctx, execution.request, execution.prefer, execution.providerFilter)
	if err != nil {
		s.finishCompatibility(execution, upstream.AccountID, upstream.Provider, upstream.Routing, accounts.RequestStatusError, upstream.TTFBMs, nil, err, upstream.AttemptCount)
		writeCompatibilityOpenAIError(w, err)
		return
	}
	defer upstream.Response.Body.Close()
	s.finishCompatibility(execution, upstream.AccountID, upstream.Provider, upstream.Routing, accounts.RequestStatusStreaming, upstream.TTFBMs, nil, nil, upstream.AttemptCount)
	setCompatibilityStreamHeaders(w, upstream.AccountID, firstNonEmpty(upstream.Provider, execution.providerFilter))
	w.WriteHeader(http.StatusOK)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	writer := compatibilityStreamWriter(w)
	stats, relayErr := relayResponsesStream(writer, upstream.Response.Body, execution.requestID, firstNonEmpty(execution.publicModel, execution.request.Model))
	status := streamRequestStatus(relayErr)
	if r.Context().Err() != nil || errors.Is(relayErr, context.Canceled) || errors.Is(relayErr, context.DeadlineExceeded) {
		status = accounts.RequestStatusCanceled
	}
	s.recordStreamDiagnostic(execution.requestID, upstream.Response, execution.started, stats, relayErr, r.Context().Err())
	ttfb := streamTTFB(execution.started, upstream.TTFBMs, stats)
	logErr := relayErr
	if status == accounts.RequestStatusCanceled {
		logErr = context.Canceled
	}
	s.finishCompatibility(execution, upstream.AccountID, upstream.Provider, upstream.Routing, status, ttfb, &stats, logErr, upstream.AttemptCount)
	if relayErr == nil {
		s.executor.CommitSession(execution.ctx, execution.request, upstream.Routing, upstream.AccountID)
		return
	}
	if !isStreamClientDisconnect(relayErr) {
		_ = writeResponsesStreamError(writer, relayErr)
	}
	s.observeCompatibilityStreamFailure(r, execution, upstream, relayErr)
}

func writeCompatibilityOpenAIError(w http.ResponseWriter, err error) {
	var requestErr *chatHTTPError
	if errors.As(err, &requestErr) {
		writeErr(w, requestErr.Status, requestErr.Code, requestErr.Message)
		return
	}
	writeClassifiedErr(w, err)
}

func responsesResponse(requestID, model, content, reasoning string, toolCalls []proxyToolCall, promptTokens, completionTokens int) map[string]any {
	return map[string]any{
		"id": "resp_" + requestID, "object": "response", "created_at": time.Now().Unix(), "status": "completed", "model": model,
		"output": responsesOutputItems(requestID, content, reasoning, toolCalls),
		"usage":  responsesUsage(promptTokens, completionTokens),
	}
}

func responsesOutputItems(requestID, content, reasoning string, toolCalls []proxyToolCall) []any {
	items := make([]any, 0, 2+len(toolCalls))
	if reasoning != "" {
		items = append(items, map[string]any{"id": "rs_" + requestID, "type": "reasoning", "status": "completed", "summary": []any{map[string]any{"type": "summary_text", "text": reasoning}}})
	}
	if content != "" || len(toolCalls) == 0 {
		items = append(items, map[string]any{
			"id": "msg_" + requestID, "type": "message", "status": "completed", "role": "assistant",
			"content": []any{map[string]any{"type": "output_text", "text": content, "annotations": []any{}}},
		})
	}
	for callIndex, call := range toolCalls {
		items = append(items, responseFunctionCallItem(requestID, callIndex, call))
	}
	return items
}

func responseFunctionCallItem(requestID string, callIndex int, call proxyToolCall) map[string]any {
	return map[string]any{
		"id": fmt.Sprintf("fc_%s_%d", requestID, callIndex), "type": "function_call", "status": "completed",
		"call_id": call.ID, "name": call.Name, "arguments": call.Arguments,
	}
}

func responsesUsage(promptTokens, completionTokens int) map[string]any {
	return map[string]any{"input_tokens": promptTokens, "output_tokens": completionTokens, "total_tokens": promptTokens + completionTokens}
}
