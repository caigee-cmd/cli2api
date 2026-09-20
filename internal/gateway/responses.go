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

func (h *Handler) HandleResponses(w http.ResponseWriter, r *http.Request) {
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
	execution, err := h.PrepareCompatibilityExecution(r, request)
	if err != nil {
		writeCompatibilityOpenAIError(w, err)
		return
	}
	w.Header().Set("X-Request-Id", execution.RequestID)
	if execution.Request.Stream {
		h.handleResponsesStream(w, r, execution)
		return
	}
	result, err := h.Executor.ChatNonStream(execution.Context, execution.Request, execution.Prefer, execution.ProviderFilter)
	if err != nil {
		h.finishCompatibility(execution, result.AccountID, result.Provider, result.Routing, accounts.RequestStatusError, 0, nil, err, result.AttemptCount, result.ReasoningLevel)
		writeCompatibilityOpenAIError(w, err)
		return
	}
	h.finishCompatibility(execution, result.AccountID, result.Provider, result.Routing, accounts.RequestStatusOK, 0, &StreamRelayStats{
		PromptTokens: ptrInt(result.PromptTokens), CompletionTokens: ptrInt(result.CompletionTokens),
		CacheReadTokens: result.CacheReadTokens, CacheWriteTokens: result.CacheWriteTokens,
		CachedTokens: result.CachedTokens, UsageSource: result.UsageSource, Credits: result.Credits,
		ConsumedCredits: result.ConsumedCredits, Model: result.Model,
	}, nil, result.AttemptCount, result.ReasoningLevel)
	response := responsesResponse(
		execution.RequestID, firstNonEmpty(result.Model, execution.PublicModel), result.Content, result.Reasoning,
		decodeOpenAIToolCalls(result.ToolCalls), result.PromptTokens, result.CompletionTokens,
		result.CacheReadTokens, result.CachedTokens,
	)
	translate.RestoreResponseToolNames(response, execution.Request.ResponseToolNames)
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleResponsesStream(w http.ResponseWriter, r *http.Request, execution Execution) {
	upstream, err := h.Executor.ChatStreamProxy(execution.Context, execution.Request, execution.Prefer, execution.ProviderFilter)
	if err != nil {
		h.finishCompatibility(execution, upstream.AccountID, upstream.Provider, upstream.Routing, accounts.RequestStatusError, upstream.TTFBMs, nil, err, upstream.AttemptCount, upstream.ReasoningLevel)
		writeCompatibilityOpenAIError(w, err)
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
	stats, relayErr := RelayResponsesStream(writer, upstream.Response.Body, execution.RequestID, firstNonEmpty(execution.PublicModel, execution.Request.Model), execution.Request.ResponseToolNames)
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
		_ = writeResponsesStreamError(writer, relayErr)
	}
	h.observeCompatibilityStreamFailure(r, execution, upstream, relayErr)
}

func writeCompatibilityOpenAIError(w http.ResponseWriter, err error) {
	var requestErr *chatHTTPError
	if errors.As(err, &requestErr) {
		writeErr(w, requestErr.Status, requestErr.Code, requestErr.Message)
		return
	}
	WriteClassifiedErr(w, err)
}

func responsesResponse(
	requestID, model, content, reasoning string,
	toolCalls []proxyToolCall,
	promptTokens, completionTokens int,
	cacheReadTokens, cachedTokens *int,
) map[string]any {
	return map[string]any{
		"id": "resp_" + requestID, "object": "response", "created_at": time.Now().Unix(), "status": "completed", "model": model,
		"output": responsesOutputItems(requestID, content, reasoning, toolCalls),
		"usage":  responsesUsage(promptTokens, completionTokens, cacheReadTokens, cachedTokens),
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
		items = append(items, proxyToolCallItem(requestID, callIndex, call))
	}
	return items
}

func responseFunctionCallItem(requestID string, callIndex int, call proxyToolCall) map[string]any {
	return map[string]any{
		"id": fmt.Sprintf("fc_%s_%d", requestID, callIndex), "type": "function_call", "status": "completed",
		"call_id": call.ID, "name": call.Name, "arguments": call.Arguments,
	}
}

func responsesUsage(promptTokens, completionTokens int, cacheReadTokens, cachedTokens *int) map[string]any {
	usage := map[string]any{"input_tokens": promptTokens, "output_tokens": completionTokens, "total_tokens": promptTokens + completionTokens}
	if cacheReadTokens == nil {
		cacheReadTokens = cachedTokens
	}
	if cacheReadTokens != nil {
		usage["input_tokens_details"] = map[string]any{"cached_tokens": *cacheReadTokens}
	}
	return usage
}
