package gateway

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

func (h *Handler) HandleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAnthropicError(w, http.StatusMethodNotAllowed, "invalid_request_error", "POST only")
		return
	}
	var source translate.AnthropicMessagesRequest
	if err := json.NewDecoder(r.Body).Decode(&source); err != nil {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	request, err := translate.TranslateAnthropicMessages(source)
	if err != nil {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	execution, err := h.PrepareCompatibilityExecution(r, request)
	if err != nil {
		writeAnthropicCompatibilityError(w, err)
		return
	}
	w.Header().Set("X-Request-Id", execution.RequestID)
	if execution.Request.Stream {
		h.handleAnthropicMessagesStream(w, r, execution)
		return
	}
	result, err := h.Executor.ChatNonStream(execution.Context, execution.Request, execution.Prefer, execution.ProviderFilter)
	if err != nil {
		h.finishCompatibility(execution, result.AccountID, result.Provider, result.Routing, accounts.RequestStatusError, 0, nil, err, result.AttemptCount, result.ReasoningLevel)
		writeAnthropicCompatibilityError(w, err)
		return
	}
	h.finishCompatibility(execution, result.AccountID, result.Provider, result.Routing, accounts.RequestStatusOK, 0, &StreamRelayStats{
		PromptTokens: ptrInt(result.PromptTokens), CompletionTokens: ptrInt(result.CompletionTokens),
		CacheReadTokens: result.CacheReadTokens, CacheWriteTokens: result.CacheWriteTokens,
		CachedTokens: result.CachedTokens, UsageSource: result.UsageSource, Credits: result.Credits,
		ConsumedCredits: result.ConsumedCredits, Model: result.Model,
	}, nil, result.AttemptCount, result.ReasoningLevel)
	writeJSON(w, http.StatusOK, anthropicMessageResponse(execution.RequestID, firstNonEmpty(result.Model, execution.PublicModel), result.Content, result.Reasoning, decodeOpenAIToolCalls(result.ToolCalls), result.FinishReason, result.PromptTokens, result.CompletionTokens))
}

func (h *Handler) handleAnthropicMessagesStream(w http.ResponseWriter, r *http.Request, execution Execution) {
	upstream, err := h.Executor.ChatStreamProxy(execution.Context, execution.Request, execution.Prefer, execution.ProviderFilter)
	if err != nil {
		h.finishCompatibility(execution, upstream.AccountID, upstream.Provider, upstream.Routing, accounts.RequestStatusError, upstream.TTFBMs, nil, err, upstream.AttemptCount, upstream.ReasoningLevel)
		writeAnthropicCompatibilityError(w, err)
		return
	}
	defer upstream.Response.Body.Close()
	h.finishCompatibility(execution, upstream.AccountID, upstream.Provider, upstream.Routing, accounts.RequestStatusStreaming, upstream.TTFBMs, nil, nil, upstream.AttemptCount, upstream.ReasoningLevel)
	setCompatibilityStreamHeaders(w, upstream.AccountID, firstNonEmpty(upstream.Provider, execution.ProviderFilter))
	w.WriteHeader(http.StatusOK)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	writer := compatibilityStreamWriter(w)
	stats, relayErr := RelayAnthropicStream(writer, upstream.Response.Body, execution.RequestID, firstNonEmpty(execution.PublicModel, execution.Request.Model))
	status := streamRequestStatus(relayErr)
	if r.Context().Err() != nil || errors.Is(relayErr, context.Canceled) || errors.Is(relayErr, context.DeadlineExceeded) {
		status = accounts.RequestStatusCanceled
	}
	h.recordStreamDiagnostic(execution.RequestID, upstream.Response, execution.Started, stats, relayErr, r.Context().Err())
	ttfb := streamTTFB(execution.Started, upstream.TTFBMs, stats)
	logErr := relayErr
	if status == accounts.RequestStatusCanceled {
		logErr = context.Canceled
	}
	h.finishCompatibility(execution, upstream.AccountID, upstream.Provider, upstream.Routing, status, ttfb, &stats, logErr, upstream.AttemptCount, upstream.ReasoningLevel)
	if relayErr == nil {
		h.Executor.CommitSession(execution.Context, execution.Request, upstream.Routing, upstream.AccountID)
		return
	}
	if !IsStreamClientDisconnect(relayErr) {
		_ = writeAnthropicStreamError(writer, relayErr)
	}
	h.observeCompatibilityStreamFailure(r, execution, upstream, relayErr)
}

func writeAnthropicCompatibilityError(w http.ResponseWriter, err error) {
	var requestErr *chatHTTPError
	if errors.As(err, &requestErr) {
		writeAnthropicError(w, requestErr.Status, "invalid_request_error", requestErr.Message)
		return
	}
	classified := ClassifyAPIError(err)
	if classified.RetryAfter > 0 {
		seconds := int(classified.RetryAfter / time.Second)
		if classified.RetryAfter%time.Second != 0 {
			seconds++
		}
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", fmt.Sprintf("%d", seconds))
	}
	writeAnthropicError(w, classified.Status, anthropicErrorType(classified.Kind), classified.Message)
}

func writeAnthropicError(w http.ResponseWriter, status int, errorType, message string) {
	if status < 400 {
		status = http.StatusBadGateway
	}
	writeJSON(w, status, map[string]any{"type": "error", "error": map[string]string{"type": errorType, "message": message}})
}

func anthropicErrorType(kind string) string {
	switch kind {
	case accounts.KindInvalidRequest, accounts.KindModelNotAvailable:
		return "invalid_request_error"
	case accounts.KindAuth:
		return "authentication_error"
	case accounts.KindRateLimit, accounts.KindQuota:
		return "rate_limit_error"
	default:
		return "api_error"
	}
}

func anthropicMessageResponse(requestID, model, content, reasoning string, toolCalls []proxyToolCall, finishReason string, promptTokens, completionTokens int) map[string]any {
	blocks := make([]any, 0, 2+len(toolCalls))
	if reasoning != "" {
		blocks = append(blocks, map[string]any{"type": "thinking", "thinking": reasoning})
	}
	if content != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": content})
	}
	for _, call := range toolCalls {
		input := json.RawMessage(call.Arguments)
		if !json.Valid(input) {
			input = json.RawMessage(`{}`)
		}
		blocks = append(blocks, map[string]any{"type": "tool_use", "id": call.ID, "name": call.Name, "input": input})
	}
	if len(blocks) == 0 {
		blocks = append(blocks, map[string]any{"type": "text", "text": ""})
	}
	return map[string]any{
		"id": "msg_" + requestID, "type": "message", "role": "assistant", "model": model,
		"content": blocks, "stop_reason": anthropicStopReason(finishReason, toolCalls), "stop_sequence": nil,
		"usage": map[string]int{"input_tokens": promptTokens, "output_tokens": completionTokens},
	}
}

func anthropicStopReason(finishReason string, toolCalls []proxyToolCall) string {
	if len(toolCalls) > 0 || finishReason == "tool_calls" {
		return "tool_use"
	}
	if finishReason == "length" {
		return "max_tokens"
	}
	return "end_turn"
}
