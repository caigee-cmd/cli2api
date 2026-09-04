package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/executor"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

type compatibilityHTTPError struct {
	Status  int
	Code    string
	Message string
}

func (e *compatibilityHTTPError) Error() string { return e.Message }

type compatibilityExecution struct {
	ctx            context.Context
	requestID      string
	started        time.Time
	request        translate.ChatRequest
	publicModel    string
	providerFilter string
	prefer         string
}

func (s *Server) prepareCompatibilityExecution(r *http.Request, request translate.ChatRequest) (compatibilityExecution, error) {
	if len(request.Messages) == 0 {
		return compatibilityExecution{}, &compatibilityHTTPError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "input messages required"}
	}
	publicModel := request.Model
	if s.rejectsBareModel(publicModel) {
		return compatibilityExecution{}, &compatibilityHTTPError{Status: http.StatusBadRequest, Code: "provider_prefix_required", Message: "cross-provider model pool is disabled; use a provider-prefixed model ID such as qoder/glm-5.2"}
	}
	providerFilter := s.resolveProviderFilter(&request)
	prefer := s.requestedAccount(r)
	providerFilter = s.applyPinnedProviderFilter(providerFilter, publicModel, prefer)
	identity := s.requestIdentity(r)
	if providerFilter != "" && !identity.AllowsProvider(providerFilter) {
		return compatibilityExecution{}, &compatibilityHTTPError{Status: http.StatusForbidden, Code: "provider_not_allowed", Message: "This API key cannot use provider " + providerFilter}
	}
	if s.manager != nil {
		if err := s.applyModelContextDefaults(r.Context(), &request, providerFilter); err != nil {
			return compatibilityExecution{}, &compatibilityHTTPError{Status: http.StatusInternalServerError, Code: "model_setting_failed", Message: err.Error()}
		}
		s.manager.EnsureModelCatalogs(r.Context(), false)
	}
	requestID := accounts.NewRequestID()
	started := time.Now().UTC()
	s.startRequestLog(accounts.RequestLog{
		ID: requestID, CreatedAt: started, Stream: request.Stream, Status: accounts.RequestStatusStarted,
		RequestedModel: firstNonEmpty(publicModel, request.Model),
	})
	ctx := executor.WithAllowedProviders(executor.WithRequestID(r.Context(), requestID), identity.AllowedProviders)
	if sessionKey := requestSessionKey(r, identity); sessionKey != "" {
		ctx = executor.WithSessionKey(ctx, sessionKey)
	}
	return compatibilityExecution{
		ctx: ctx, requestID: requestID, started: started, request: request,
		publicModel: publicModel, providerFilter: providerFilter, prefer: prefer,
	}, nil
}

func (s *Server) finishCompatibility(execution compatibilityExecution, accountID, provider, routing, status string, ttfb int, stats *streamRelayStats, err error, attempts int) {
	s.finishRequestLog(execution.requestID, execution.started, execution.request, execution.publicModel, accountID, firstNonEmpty(provider, execution.providerFilter), routing, status, ttfb, stats, err, attempts)
}

func (s *Server) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
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
	execution, err := s.prepareCompatibilityExecution(r, request)
	if err != nil {
		writeAnthropicCompatibilityError(w, err)
		return
	}
	w.Header().Set("X-Request-Id", execution.requestID)
	if execution.request.Stream {
		s.handleAnthropicMessagesStream(w, r, execution)
		return
	}
	result, err := s.executor.ChatNonStream(execution.ctx, execution.request, execution.prefer, execution.providerFilter)
	if err != nil {
		s.finishCompatibility(execution, result.AccountID, result.Provider, result.Routing, accounts.RequestStatusError, 0, nil, err, result.AttemptCount)
		writeAnthropicCompatibilityError(w, err)
		return
	}
	s.finishCompatibility(execution, result.AccountID, result.Provider, result.Routing, accounts.RequestStatusOK, 0, &streamRelayStats{
		PromptTokens: ptrInt(result.PromptTokens), CompletionTokens: ptrInt(result.CompletionTokens),
		CacheReadTokens: result.CacheReadTokens, CacheWriteTokens: result.CacheWriteTokens,
		CachedTokens: result.CachedTokens, UsageSource: result.UsageSource, Credits: result.Credits, Model: result.Model,
	}, nil, result.AttemptCount)
	writeJSON(w, http.StatusOK, anthropicMessageResponse(execution.requestID, firstNonEmpty(result.Model, execution.publicModel), result.Content, result.Reasoning, decodeOpenAIToolCalls(result.ToolCalls), result.FinishReason, result.PromptTokens, result.CompletionTokens))
}

func (s *Server) handleAnthropicMessagesStream(w http.ResponseWriter, r *http.Request, execution compatibilityExecution) {
	upstream, err := s.executor.ChatStreamProxy(execution.ctx, execution.request, execution.prefer, execution.providerFilter)
	if err != nil {
		s.finishCompatibility(execution, upstream.AccountID, upstream.Provider, upstream.Routing, accounts.RequestStatusError, upstream.TTFBMs, nil, err, upstream.AttemptCount)
		writeAnthropicCompatibilityError(w, err)
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
	stats, relayErr := relayAnthropicStream(writer, upstream.Response.Body, execution.requestID, firstNonEmpty(execution.publicModel, execution.request.Model))
	status := streamRequestStatus(relayErr)
	ttfb := streamTTFB(execution.started, upstream.TTFBMs, stats)
	s.finishCompatibility(execution, upstream.AccountID, upstream.Provider, upstream.Routing, status, ttfb, &stats, relayErr, upstream.AttemptCount)
	if relayErr == nil {
		s.executor.CommitSession(execution.ctx, upstream.Routing, upstream.AccountID)
		return
	}
	if !isStreamClientDisconnect(relayErr) {
		_ = writeAnthropicStreamError(writer, relayErr)
	}
	s.observeCompatibilityStreamFailure(r, execution, upstream, relayErr)
}

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
		CachedTokens: result.CachedTokens, UsageSource: result.UsageSource, Credits: result.Credits, Model: result.Model,
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
	ttfb := streamTTFB(execution.started, upstream.TTFBMs, stats)
	s.finishCompatibility(execution, upstream.AccountID, upstream.Provider, upstream.Routing, status, ttfb, &stats, relayErr, upstream.AttemptCount)
	if relayErr == nil {
		s.executor.CommitSession(execution.ctx, upstream.Routing, upstream.AccountID)
		return
	}
	if !isStreamClientDisconnect(relayErr) {
		_ = writeResponsesStreamError(writer, relayErr)
	}
	s.observeCompatibilityStreamFailure(r, execution, upstream, relayErr)
}

func (s *Server) observeCompatibilityStreamFailure(r *http.Request, execution compatibilityExecution, upstream executor.StreamResult, relayErr error) {
	if r.Context().Err() == nil && !errors.Is(relayErr, context.Canceled) && !errors.Is(relayErr, context.DeadlineExceeded) && !isStreamClientDisconnect(relayErr) {
		s.executor.ObserveStreamFailure(upstream.AccountID, relayErr, execution.request.Model)
	}
}

func setCompatibilityStreamHeaders(w http.ResponseWriter, accountID, provider string) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	if accountID != "" {
		w.Header().Set("X-Qoder-Account", accountID)
		w.Header().Set("X-CLI2API-Account", accountID)
	}
	if provider != "" {
		w.Header().Set("X-CLI2API-Provider", provider)
	}
}

func compatibilityStreamWriter(w http.ResponseWriter) io.Writer {
	if flusher, ok := w.(http.Flusher); ok {
		return streamFlushWriter{w: w, f: flusher}
	}
	return w
}

func streamRequestStatus(err error) string {
	if err == nil {
		return accounts.RequestStatusOK
	}
	if isStreamClientDisconnect(err) {
		return accounts.RequestStatusCanceled
	}
	return accounts.RequestStatusError
}

func streamTTFB(started time.Time, fallback int, stats streamRelayStats) int {
	if stats.FirstTokenAt == nil {
		return fallback
	}
	ttfb := int(stats.FirstTokenAt.Sub(started).Milliseconds())
	if ttfb < 1 {
		return 1
	}
	return ttfb
}

func writeAnthropicCompatibilityError(w http.ResponseWriter, err error) {
	var requestErr *compatibilityHTTPError
	if errors.As(err, &requestErr) {
		writeAnthropicError(w, requestErr.Status, "invalid_request_error", requestErr.Message)
		return
	}
	classified := classifyAPIError(err)
	writeAnthropicError(w, classified.Status, anthropicErrorType(classified.Kind), classified.Message)
}

func writeCompatibilityOpenAIError(w http.ResponseWriter, err error) {
	var requestErr *compatibilityHTTPError
	if errors.As(err, &requestErr) {
		writeErr(w, requestErr.Status, requestErr.Code, requestErr.Message)
		return
	}
	writeClassifiedErr(w, err)
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

func writeSSEEvent(writer io.Writer, event string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if event != "" {
		if _, err := fmt.Fprintf(writer, "event: %s\n", event); err != nil {
			return &streamRelayWriteError{err: err}
		}
	}
	if _, err := fmt.Fprintf(writer, "data: %s\n\n", encoded); err != nil {
		return &streamRelayWriteError{err: err}
	}
	return nil
}

func writeAnthropicStreamError(writer io.Writer, err error) error {
	classified := classifyAPIError(err)
	return writeSSEEvent(writer, "error", map[string]any{"type": "error", "error": map[string]string{"type": anthropicErrorType(classified.Kind), "message": classified.Message}})
}

func writeResponsesStreamError(writer io.Writer, err error) error {
	classified := classifyAPIError(err)
	return writeSSEEvent(writer, "error", map[string]any{"type": "error", "error": map[string]any{"type": classified.Type, "code": classified.Code, "message": classified.Message}})
}

type streamedChatOutput struct {
	content      strings.Builder
	reasoning    strings.Builder
	toolCalls    map[int]*proxyToolCall
	finishReason string
}

func (o *streamedChatOutput) add(payload json.RawMessage) {
	var chunk struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Delta        struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				ToolCalls        []struct {
					Index    int    `json:"index"`
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if json.Unmarshal(payload, &chunk) != nil {
		return
	}
	for _, choice := range chunk.Choices {
		if choice.FinishReason != "" {
			o.finishReason = choice.FinishReason
		}
		if choice.Delta.Content != "" {
			o.content.WriteString(choice.Delta.Content)
		}
		if choice.Delta.ReasoningContent != "" {
			o.reasoning.WriteString(choice.Delta.ReasoningContent)
		}
		for callPosition, delta := range choice.Delta.ToolCalls {
			if o.toolCalls == nil {
				o.toolCalls = map[int]*proxyToolCall{}
			}
			index := delta.Index
			if index == 0 && callPosition > 0 {
				index = callPosition
			}
			call := o.toolCalls[index]
			if call == nil {
				call = &proxyToolCall{}
				o.toolCalls[index] = call
			}
			if delta.ID != "" {
				call.ID = delta.ID
			}
			if delta.Function.Name != "" {
				call.Name = delta.Function.Name
			}
			call.Arguments += delta.Function.Arguments
		}
	}
}

func (o *streamedChatOutput) calls() []proxyToolCall {
	indexes := make([]int, 0, len(o.toolCalls))
	for index := range o.toolCalls {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	calls := make([]proxyToolCall, 0, len(indexes))
	for _, index := range indexes {
		call := o.toolCalls[index]
		if call == nil || call.Name == "" {
			continue
		}
		calls = append(calls, *call)
	}
	return calls
}

func consumeOpenAIStream(body io.Reader, handle func(json.RawMessage, *streamedChatOutput) error) (streamRelayStats, streamedChatOutput, error) {
	var stats streamRelayStats
	var output streamedChatOutput
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineSize)
	var frame []string
	sawDone := false
	flush := func() error {
		if len(frame) == 0 {
			return nil
		}
		eventName, data := parseSSEFrame(frame)
		frame = nil
		if classified := classifyStreamSSEError(eventName, data); classified != nil {
			return classified
		}
		if strings.TrimSpace(data) == "[DONE]" {
			sawDone = true
			return nil
		}
		payload := json.RawMessage(strings.TrimSpace(data))
		if !json.Valid(payload) {
			return nil
		}
		if usage, ok := parseStreamUsageLine("data: " + string(payload)); ok {
			usage.FirstTokenAt = stats.FirstTokenAt
			stats = usage
		}
		beforeContent := output.content.Len()
		output.add(payload)
		if stats.FirstTokenAt == nil && output.content.Len() > beforeContent {
			now := time.Now()
			stats.FirstTokenAt = &now
		}
		return handle(payload, &output)
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := flush(); err != nil {
				return stats, output, err
			}
			continue
		}
		frame = append(frame, line)
	}
	if err := scanner.Err(); err != nil {
		return stats, output, newStreamProviderError("upstream_stream_interrupted", "stream read error: "+err.Error(), http.StatusBadGateway)
	}
	if err := flush(); err != nil {
		return stats, output, err
	}
	if !sawDone {
		return stats, output, newStreamProviderError("upstream_stream_incomplete", "stream ended before [DONE]", http.StatusBadGateway)
	}
	return stats, output, nil
}

func relayAnthropicStream(writer io.Writer, body io.Reader, requestID, model string) (streamRelayStats, error) {
	messageID := "msg_" + requestID
	if err := writeSSEEvent(writer, "message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": messageID, "type": "message", "role": "assistant", "model": model, "content": []any{}, "stop_reason": nil, "stop_sequence": nil,
		"usage": map[string]int{"input_tokens": 0, "output_tokens": 0},
	}}); err != nil {
		return streamRelayStats{}, err
	}
	textStarted := false
	textBlockIndex := -1
	thinkingStarted := false
	thinkingBlockIndex := -1
	toolBlocks := map[int]int{}
	nextBlock := 0
	closeThinking := func() error {
		if !thinkingStarted {
			return nil
		}
		if err := writeSSEEvent(writer, "content_block_stop", map[string]any{"type": "content_block_stop", "index": thinkingBlockIndex}); err != nil {
			return err
		}
		thinkingStarted = false
		return nil
	}
	closeText := func() error {
		if !textStarted {
			return nil
		}
		if err := writeSSEEvent(writer, "content_block_stop", map[string]any{"type": "content_block_stop", "index": textBlockIndex}); err != nil {
			return err
		}
		textStarted = false
		return nil
	}
	stats, output, err := consumeOpenAIStream(body, func(payload json.RawMessage, current *streamedChatOutput) error {
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					ToolCalls        []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal(payload, &chunk) != nil {
			return nil
		}
		for _, choice := range chunk.Choices {
			if reasoning := choice.Delta.ReasoningContent; reasoning != "" {
				if !thinkingStarted {
					thinkingBlockIndex = nextBlock
					nextBlock++
					if err := writeSSEEvent(writer, "content_block_start", map[string]any{"type": "content_block_start", "index": thinkingBlockIndex, "content_block": map[string]any{"type": "thinking", "thinking": ""}}); err != nil {
						return err
					}
					thinkingStarted = true
				}
				if err := writeSSEEvent(writer, "content_block_delta", map[string]any{"type": "content_block_delta", "index": thinkingBlockIndex, "delta": map[string]string{"type": "thinking_delta", "thinking": reasoning}}); err != nil {
					return err
				}
			}
			if content := choice.Delta.Content; content != "" {
				if err := closeThinking(); err != nil {
					return err
				}
				if !textStarted {
					textBlockIndex = nextBlock
					nextBlock++
					if err := writeSSEEvent(writer, "content_block_start", map[string]any{"type": "content_block_start", "index": textBlockIndex, "content_block": map[string]any{"type": "text", "text": ""}}); err != nil {
						return err
					}
					textStarted = true
				}
				if err := writeSSEEvent(writer, "content_block_delta", map[string]any{"type": "content_block_delta", "index": textBlockIndex, "delta": map[string]string{"type": "text_delta", "text": content}}); err != nil {
					return err
				}
			}
			for callPosition, delta := range choice.Delta.ToolCalls {
				if err := closeThinking(); err != nil {
					return err
				}
				index := delta.Index
				if index == 0 && callPosition > 0 {
					index = callPosition
				}
				blockIndex, exists := toolBlocks[index]
				if !exists && delta.Function.Name != "" {
					if textStarted {
						if err := closeText(); err != nil {
							return err
						}
					}
					blockIndex = nextBlock
					nextBlock++
					toolBlocks[index] = blockIndex
					callID := delta.ID
					if callID == "" {
						callID = fmt.Sprintf("toolu_%s_%d", requestID, index)
					}
					if err := writeSSEEvent(writer, "content_block_start", map[string]any{"type": "content_block_start", "index": blockIndex, "content_block": map[string]any{"type": "tool_use", "id": callID, "name": delta.Function.Name, "input": map[string]any{}}}); err != nil {
						return err
					}
					exists = true
				}
				if exists && delta.Function.Arguments != "" {
					if err := writeSSEEvent(writer, "content_block_delta", map[string]any{"type": "content_block_delta", "index": blockIndex, "delta": map[string]string{"type": "input_json_delta", "partial_json": delta.Function.Arguments}}); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return stats, err
	}
	if err := closeThinking(); err != nil {
		return stats, err
	}
	if err := closeText(); err != nil {
		return stats, err
	}
	for _, blockIndex := range sortedBlockIndexes(toolBlocks) {
		if err := writeSSEEvent(writer, "content_block_stop", map[string]any{"type": "content_block_stop", "index": blockIndex}); err != nil {
			return stats, err
		}
	}
	calls := output.calls()
	if err := writeSSEEvent(writer, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": anthropicStopReason(output.finishReason, calls), "stop_sequence": nil}, "usage": map[string]int{"output_tokens": derefInt(stats.CompletionTokens)}}); err != nil {
		return stats, err
	}
	if err := writeSSEEvent(writer, "message_stop", map[string]string{"type": "message_stop"}); err != nil {
		return stats, err
	}
	return stats, nil
}

type responsesEventWriter struct {
	writer         io.Writer
	sequenceNumber int
}

func (w *responsesEventWriter) write(event string, payload any) error {
	if object, ok := payload.(map[string]any); ok {
		object["sequence_number"] = w.sequenceNumber
		w.sequenceNumber++
	}
	return writeSSEEvent(w.writer, event, payload)
}

func relayResponsesStream(writer io.Writer, body io.Reader, requestID, model string) (streamRelayStats, error) {
	eventWriter := responsesEventWriter{writer: writer}
	responseID := "resp_" + requestID
	created := time.Now().Unix()
	inProgress := map[string]any{"id": responseID, "object": "response", "created_at": created, "status": "in_progress", "model": model, "output": []any{}}
	if err := eventWriter.write("response.created", map[string]any{"type": "response.created", "response": inProgress}); err != nil {
		return streamRelayStats{}, err
	}
	textStarted := false
	reasoningStarted := false
	reasoningID := "rs_" + requestID
	reasoningText := strings.Builder{}
	toolAnnounced := map[int]bool{}
	toolCallIDs := map[int]string{}
	toolCallNames := map[int]string{}
	toolArgumentLengths := map[int]int{}
	toolOutputIndexes := map[int]int{}
	nextOutputIndex := 0
	reasoningOutputIndex := -1
	textOutputIndex := -1
	closeReasoning := func() error {
		if !reasoningStarted {
			return nil
		}
		if err := eventWriter.write("response.reasoning_summary_text.done", map[string]any{"type": "response.reasoning_summary_text.done", "item_id": reasoningID, "output_index": reasoningOutputIndex, "summary_index": 0, "text": reasoningText.String()}); err != nil {
			return err
		}
		if err := eventWriter.write("response.reasoning_summary_part.done", map[string]any{"type": "response.reasoning_summary_part.done", "item_id": reasoningID, "output_index": reasoningOutputIndex, "summary_index": 0, "part": map[string]any{"type": "summary_text", "text": reasoningText.String()}}); err != nil {
			return err
		}
		if err := eventWriter.write("response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": reasoningOutputIndex, "item": map[string]any{"id": reasoningID, "type": "reasoning", "status": "completed", "summary": []any{map[string]any{"type": "summary_text", "text": reasoningText.String()}}}}); err != nil {
			return err
		}
		reasoningStarted = false
		return nil
	}
	stats, output, err := consumeOpenAIStream(body, func(payload json.RawMessage, current *streamedChatOutput) error {
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					ToolCalls        []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal(payload, &chunk) != nil {
			return nil
		}
		for _, choice := range chunk.Choices {
			if reasoning := choice.Delta.ReasoningContent; reasoning != "" {
				if !reasoningStarted {
					reasoningStarted = true
					reasoningOutputIndex = nextOutputIndex
					nextOutputIndex++
					if err := eventWriter.write("response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": reasoningOutputIndex, "item": map[string]any{"id": reasoningID, "type": "reasoning", "status": "in_progress", "summary": []any{}}}); err != nil {
						return err
					}
					if err := eventWriter.write("response.reasoning_summary_part.added", map[string]any{"type": "response.reasoning_summary_part.added", "item_id": reasoningID, "output_index": reasoningOutputIndex, "summary_index": 0, "part": map[string]any{"type": "summary_text", "text": ""}}); err != nil {
						return err
					}
				}
				reasoningText.WriteString(reasoning)
				if err := eventWriter.write("response.reasoning_summary_text.delta", map[string]any{"type": "response.reasoning_summary_text.delta", "item_id": reasoningID, "output_index": reasoningOutputIndex, "summary_index": 0, "delta": reasoning}); err != nil {
					return err
				}
			}
			if content := choice.Delta.Content; content != "" {
				if err := closeReasoning(); err != nil {
					return err
				}
				if !textStarted {
					textOutputIndex = nextOutputIndex
					nextOutputIndex++
					if err := eventWriter.write("response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": textOutputIndex, "item": map[string]any{"id": "msg_" + requestID, "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}}}); err != nil {
						return err
					}
					if err := eventWriter.write("response.content_part.added", map[string]any{"type": "response.content_part.added", "item_id": "msg_" + requestID, "output_index": textOutputIndex, "content_index": 0, "part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}}); err != nil {
						return err
					}
					textStarted = true
				}
				if err := eventWriter.write("response.output_text.delta", map[string]any{"type": "response.output_text.delta", "item_id": "msg_" + requestID, "output_index": textOutputIndex, "content_index": 0, "delta": content}); err != nil {
					return err
				}
			}
			for callPosition, delta := range choice.Delta.ToolCalls {
				index := delta.Index
				if index == 0 && callPosition > 0 {
					index = callPosition
				}
				if err := closeReasoning(); err != nil {
					return err
				}
				if delta.ID != "" {
					toolCallIDs[index] = delta.ID
				}
				if delta.Function.Name != "" {
					toolCallNames[index] = delta.Function.Name
				}
				if toolCallIDs[index] == "" {
					toolCallIDs[index] = fmt.Sprintf("call_%s_%d", requestID, index)
				}
				if delta.Function.Name != "" && !toolAnnounced[index] {
					toolOutputIndexes[index] = nextOutputIndex
					nextOutputIndex++
					item := map[string]any{"id": fmt.Sprintf("fc_%s_%d", requestID, index), "type": "function_call", "status": "in_progress", "call_id": toolCallIDs[index], "name": toolCallNames[index], "arguments": ""}
					if err := eventWriter.write("response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": toolOutputIndexes[index], "item": item}); err != nil {
						return err
					}
					toolAnnounced[index] = true
				}
				if toolAnnounced[index] {
					accumulated := ""
					if currentCall := current.toolCalls[index]; currentCall != nil {
						accumulated = currentCall.Arguments
					}
					emitted := toolArgumentLengths[index]
					if len(accumulated) > emitted {
						if err := eventWriter.write("response.function_call_arguments.delta", map[string]any{"type": "response.function_call_arguments.delta", "output_index": toolOutputIndexes[index], "item_id": fmt.Sprintf("fc_%s_%d", requestID, index), "call_id": toolCallIDs[index], "name": toolCallNames[index], "delta": accumulated[emitted:]}); err != nil {
							return err
						}
						toolArgumentLengths[index] = len(accumulated)
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return stats, err
	}
	if err := closeReasoning(); err != nil {
		return stats, err
	}
	content := output.content.String()
	calls := output.calls()
	if textStarted {
		if err := eventWriter.write("response.output_text.done", map[string]any{"type": "response.output_text.done", "item_id": "msg_" + requestID, "output_index": textOutputIndex, "content_index": 0, "text": content}); err != nil {
			return stats, err
		}
		if err := eventWriter.write("response.content_part.done", map[string]any{"type": "response.content_part.done", "item_id": "msg_" + requestID, "output_index": textOutputIndex, "content_index": 0, "part": map[string]any{"type": "output_text", "text": content, "annotations": []any{}}}); err != nil {
			return stats, err
		}
		if err := eventWriter.write("response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": textOutputIndex, "item": responsesOutputItems(requestID, content, "", nil)[0]}); err != nil {
			return stats, err
		}
	}
	for callIndex, call := range calls {
		if call.ID == "" {
			call.ID = toolCallIDs[callIndex]
			if call.ID == "" {
				call.ID = fmt.Sprintf("call_%s_%d", requestID, callIndex)
			}
		}
		item := responseFunctionCallItem(requestID, callIndex, call)
		outputIndex, ok := toolOutputIndexes[callIndex]
		if !ok {
			outputIndex = nextOutputIndex
			nextOutputIndex++
		}
		if !toolAnnounced[callIndex] {
			if err := eventWriter.write("response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": outputIndex, "item": map[string]any{"id": item["id"], "type": "function_call", "status": "in_progress", "call_id": call.ID, "name": call.Name, "arguments": ""}}); err != nil {
				return stats, err
			}
		}
		if err := eventWriter.write("response.function_call_arguments.done", map[string]any{"type": "response.function_call_arguments.done", "output_index": outputIndex, "item_id": item["id"], "call_id": call.ID, "name": call.Name, "arguments": call.Arguments}); err != nil {
			return stats, err
		}
		if err := eventWriter.write("response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": outputIndex, "item": item}); err != nil {
			return stats, err
		}
	}
	completed := responsesResponse(requestID, model, content, output.reasoning.String(), calls, derefInt(stats.PromptTokens), derefInt(stats.CompletionTokens))
	if err := eventWriter.write("response.completed", map[string]any{"type": "response.completed", "response": completed}); err != nil {
		return stats, err
	}
	return stats, nil
}

func sortedBlockIndexes(blocks map[int]int) []int {
	indexes := make([]int, 0, len(blocks))
	for _, blockIndex := range blocks {
		indexes = append(indexes, blockIndex)
	}
	sort.Ints(indexes)
	return indexes
}

func derefInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
