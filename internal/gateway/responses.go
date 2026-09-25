package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/executor"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func (h *Handler) HandleResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, translate.MaxNativeRequestBytes+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if len(body) > translate.MaxNativeRequestBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, "invalid_request", "request body too large")
		return
	}
	native, err := translate.ParseNativeResponses(body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	var translated translate.ResponsesTranslation
	compat, compatErr := native.Compat()
	if compatErr == nil {
		translated = compat
	}
	compatRequest := translate.ChatRequest{Model: native.Model(), Stream: native.Stream()}
	if compatErr == nil {
		compatRequest = translated.Chat
	}
	execution, err := h.PrepareNativeResponsesExecution(r, native, compatRequest)
	if err != nil {
		writeCompatibilityOpenAIError(w, err)
		return
	}
	execution.ResponseToolNames = translated.ToolNames
	if len(execution.ResponseToolNames) == 0 && execution.NativeRequest != nil {
		execution.ResponseToolNames = translate.ResponseToolNames(execution.NativeRequest.Body())
	}
	execution.UseNativeResponses = h.Executor.HasNativeResponsesRoute(execution.Context, execution.Request, execution.Prefer, execution.ProviderFilter)
	if !execution.UseNativeResponses && compatErr != nil {
		writeErr(w, http.StatusBadRequest, "unsupported_request", "this Responses request requires a native Responses adapter")
		return
	}
	w.Header().Set("X-Request-Id", execution.RequestID)
	if execution.Request.Stream {
		h.handleResponsesStream(w, r, execution)
		return
	}
	if execution.UseNativeResponses {
		nativeResult, err := h.Executor.NativeResponsesNonStream(execution.Context, execution.Request, execution.NativeRequest, execution.Prefer, execution.ProviderFilter)
		if err != nil || nativeResult.FinishReason == "error" {
			if err == nil {
				err = fmt.Errorf("upstream Responses request failed")
			}
			h.finishCompatibility(execution, nativeResult.AccountID, nativeResult.Provider, nativeResult.Routing, accounts.RequestStatusError, 0, nil, err, nativeResult.AttemptCount, nativeResult.ReasoningLevel)
			writeCompatibilityOpenAIError(w, err)
			return
		}
		h.finishCompatibility(execution, nativeResult.AccountID, nativeResult.Provider, nativeResult.Routing, responsesRequestStatus(nativeResult.FinishReason), 0, &StreamRelayStats{
			PromptTokens: nativeResult.PromptTokens, CompletionTokens: nativeResult.OutputTokens, CachedTokens: nativeResult.CachedTokens,
			FinishReason: nativeResult.FinishReason,
		}, nil, nativeResult.AttemptCount, nativeResult.ReasoningLevel)
		response := restoreNativeResponseJSON(nativeResult.Response, execution.ResponseToolNames)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(response)
		return
	}
	result, err := h.Executor.ChatNonStream(execution.Context, execution.Request, execution.Prefer, execution.ProviderFilter)
	if err != nil {
		h.finishCompatibility(execution, result.AccountID, result.Provider, result.Routing, accounts.RequestStatusError, 0, nil, err, result.AttemptCount, result.ReasoningLevel)
		writeCompatibilityOpenAIError(w, err)
		return
	}
	requestStatus := responsesRequestStatus(result.FinishReason)
	h.finishCompatibility(execution, result.AccountID, result.Provider, result.Routing, requestStatus, 0, &StreamRelayStats{
		PromptTokens: ptrInt(result.PromptTokens), CompletionTokens: ptrInt(result.CompletionTokens),
		CacheReadTokens: result.CacheReadTokens, CacheWriteTokens: result.CacheWriteTokens,
		CachedTokens: result.CachedTokens, UsageSource: result.UsageSource, Credits: result.Credits,
		ConsumedCredits: result.ConsumedCredits, Model: result.Model, FinishReason: result.FinishReason,
	}, nil, result.AttemptCount, result.ReasoningLevel)
	response := responsesResponse(
		execution.RequestID, firstNonEmpty(result.Model, execution.PublicModel), result.Content, result.Reasoning,
		decodeOpenAIToolCalls(result.ToolCalls), result.PromptTokens, result.CompletionTokens, result.FinishReason,
		result.CacheReadTokens, result.CachedTokens,
	)
	translate.RestoreResponseToolNames(response, execution.ResponseToolNames)
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleResponsesStream(w http.ResponseWriter, r *http.Request, execution Execution) {
	var upstream executor.StreamResult
	var err error
	if execution.UseNativeResponses {
		upstream, err = h.Executor.ChatStreamProxyNativeResponses(execution.Context, execution.Request, execution.NativeRequest, execution.Prefer, execution.ProviderFilter)
	} else {
		upstream, err = h.Executor.ChatStreamProxy(execution.Context, execution.Request, execution.Prefer, execution.ProviderFilter)
	}
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
	var stats StreamRelayStats
	var relayErr error
	if upstream.NativeResponses {
		// The upstream already speaks the Responses protocol: relay verbatim and
		// observe the terminal event for usage/finish accounting.
		stats, relayErr = RelayNativeResponsesStream(writer, upstream.Response.Body, execution.ResponseToolNames)
	} else {
		stats, relayErr = RelayResponsesStream(writer, upstream.Response.Body, execution.RequestID, firstNonEmpty(execution.PublicModel, execution.Request.Model), execution.ResponseToolNames)
	}
	status := streamRequestStatus(relayErr)
	if relayErr == nil {
		status = responsesRequestStatus(stats.FinishReason)
	}
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
	// A native failure event was already relayed as the stream's terminal
	// frame. Appending a second error event would contradict it.
	if !IsStreamClientDisconnect(relayErr) && !(upstream.NativeResponses && stats.FinishReason == "error") {
		_ = writeResponsesStreamError(writer, relayErr)
	}
	h.observeCompatibilityStreamFailure(r, execution, upstream, relayErr)
}

func restoreNativeResponseJSON(raw json.RawMessage, names map[string]translate.ResponseToolName) []byte {
	if len(names) == 0 {
		return raw
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return raw
	}
	translate.RestoreResponseToolNames(value, names)
	encoded, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return encoded
}

func writeCompatibilityOpenAIError(w http.ResponseWriter, err error) {
	var requestErr *chatHTTPError
	if errors.As(err, &requestErr) {
		writeErr(w, requestErr.Status, requestErr.Code, requestErr.Message)
		return
	}
	WriteClassifiedErr(w, err)
}

type responsesTerminal struct {
	status            string
	event             string
	incompleteDetails map[string]any
}

func responsesTerminalForFinishReason(finishReason string) responsesTerminal {
	if finishReason == "length" {
		return responsesTerminal{
			status: "incomplete",
			event:  "response.incomplete",
			incompleteDetails: map[string]any{
				"reason": "max_output_tokens",
			},
		}
	}
	return responsesTerminal{status: "completed", event: "response.completed"}
}

func responsesRequestStatus(finishReason string) string {
	if responsesTerminalForFinishReason(finishReason).status == "incomplete" {
		return accounts.RequestStatusIncomplete
	}
	return accounts.RequestStatusOK
}

func responsesResponse(
	requestID, model, content, reasoning string,
	toolCalls []proxyToolCall,
	promptTokens, completionTokens int,
	finishReason string,
	cacheReadTokens, cachedTokens *int,
) map[string]any {
	terminal := responsesTerminalForFinishReason(finishReason)
	response := map[string]any{
		"id": "resp_" + requestID, "object": "response", "created_at": time.Now().Unix(), "status": terminal.status, "model": model,
		"output": responsesOutputItems(requestID, content, reasoning, toolCalls),
		"usage":  responsesUsage(promptTokens, completionTokens, cacheReadTokens, cachedTokens),
	}
	if terminal.incompleteDetails != nil {
		response["incomplete_details"] = terminal.incompleteDetails
	}
	return response
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
