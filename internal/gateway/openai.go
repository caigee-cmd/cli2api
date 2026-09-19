package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/executor"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func (h *Handler) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	var req translate.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if len(req.Messages) == 0 {
		writeErr(w, http.StatusBadRequest, "invalid_request", "messages required")
		return
	}
	if err := translate.ValidateChatRequest(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	execution, err := h.PrepareChatExecution(r, req)
	if err != nil {
		writeChatHTTPError(w, err)
		return
	}
	req = execution.Request
	publicModel := execution.PublicModel
	prefer := execution.Prefer
	providerFilter := execution.ProviderFilter
	requestID := execution.RequestID
	started := execution.Started
	ctx := execution.Context
	w.Header().Set("X-Request-Id", requestID)

	if req.Stream {
		upstream, err := h.Executor.ChatStreamProxy(ctx, req, prefer, providerFilter)
		if err != nil {
			h.finishRequestLog(requestID, started, req, publicModel, upstream.AccountID, firstNonEmpty(upstream.Provider, providerFilter), upstream.Routing, accounts.RequestStatusError, upstream.TTFBMs, nil, err, upstream.AttemptCount, upstream.ReasoningLevel)
			WriteClassifiedErr(w, err)
			return
		}
		defer upstream.Response.Body.Close()
		h.finishRequestLog(requestID, started, req, publicModel, upstream.AccountID, firstNonEmpty(upstream.Provider, providerFilter), upstream.Routing, accounts.RequestStatusStreaming, upstream.TTFBMs, nil, nil, upstream.AttemptCount, upstream.ReasoningLevel)
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		if upstream.AccountID != "" {
			w.Header().Set("X-Qoder-Account", upstream.AccountID)
			w.Header().Set("X-CLI2API-Account", upstream.AccountID)
		}
		if provider := firstNonEmpty(upstream.Provider, providerFilter); provider != "" {
			w.Header().Set("X-CLI2API-Provider", provider)
		}
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		stats, relayErr := RelayOpenAIStream(w, upstream.Response.Body)
		status := accounts.RequestStatusOK
		if relayErr != nil {
			if IsStreamClientDisconnect(relayErr) || r.Context().Err() != nil || errors.Is(relayErr, context.Canceled) || errors.Is(relayErr, context.DeadlineExceeded) {
				status = accounts.RequestStatusCanceled
			} else {
				status = accounts.RequestStatusError
			}
		}
		h.recordStreamDiagnostic(requestID, upstream.Response, started, stats, relayErr, r.Context().Err())
		ttfb := upstream.TTFBMs
		if stats.FirstTokenAt != nil {
			ttfb = int(stats.FirstTokenAt.Sub(started).Milliseconds())
			if ttfb < 1 {
				ttfb = 1
			}
		}
		logErr := relayErr
		if status == accounts.RequestStatusCanceled {
			logErr = context.Canceled
		}
		h.finishRequestLog(requestID, started, req, publicModel, upstream.AccountID, firstNonEmpty(upstream.Provider, providerFilter), upstream.Routing, status, ttfb, &stats, logErr, upstream.AttemptCount, upstream.ReasoningLevel)
		if relayErr == nil {
			h.Executor.CommitSession(ctx, req, upstream.Routing, upstream.AccountID)
		}
		if relayErr != nil {
			// The upstream answered 200 and failed inside the stream, so the
			// executor's attempt loop never saw it. Feed the classified state
			// back into the pool so the next request can route around a
			// quota-exhausted account. Use req.Model (prefix-stripped by
			// resolveProviderFilter) so the cooldown key matches the key
			// PickRoute uses; publicModel may still carry "qoder/" and would
			// write a cooldown that routing never hits.
			if r.Context().Err() == nil && !errors.Is(relayErr, context.Canceled) && !errors.Is(relayErr, context.DeadlineExceeded) && !IsStreamClientDisconnect(relayErr) {
				h.Executor.ObserveStreamFailure(upstream.AccountID, relayErr, req.Model)
			}
			panic(http.ErrAbortHandler)
		}
		return
	}

	res, err := h.Executor.ChatNonStream(ctx, req, prefer, providerFilter)
	if err != nil {
		h.finishRequestLog(requestID, started, req, publicModel, res.AccountID, firstNonEmpty(res.Provider, providerFilter), res.Routing, accounts.RequestStatusError, 0, nil, err, res.AttemptCount, res.ReasoningLevel)
		WriteClassifiedErr(w, err)
		return
	}
	if publicModel != "" {
		res.Model = publicModel
	}
	h.finishRequestLog(requestID, started, req, publicModel, res.AccountID, firstNonEmpty(res.Provider, providerFilter), res.Routing, accounts.RequestStatusOK, 0, &StreamRelayStats{
		PromptTokens: ptrInt(res.PromptTokens), CompletionTokens: ptrInt(res.CompletionTokens),
		CacheReadTokens: res.CacheReadTokens, CacheWriteTokens: res.CacheWriteTokens,
		CachedTokens: res.CachedTokens, UsageSource: res.UsageSource, Credits: res.Credits,
		ConsumedCredits: res.ConsumedCredits, Model: res.Model,
	}, nil, res.AttemptCount, res.ReasoningLevel)
	message := map[string]any{
		"role":    "assistant",
		"content": res.Content,
	}
	if res.Reasoning != "" {
		message["reasoning_content"] = res.Reasoning
	}
	if len(res.ToolCalls) > 0 && string(res.ToolCalls) != "null" {
		message["tool_calls"] = json.RawMessage(res.ToolCalls)
		if res.Content == "" {
			message["content"] = nil
		}
	}
	finishReason := res.FinishReason
	if finishReason == "" {
		if len(res.ToolCalls) > 0 && string(res.ToolCalls) != "null" {
			finishReason = "tool_calls"
		} else {
			finishReason = "stop"
		}
	}
	if res.AccountID != "" {
		w.Header().Set("X-Qoder-Account", res.AccountID)
		w.Header().Set("X-CLI2API-Account", res.AccountID)
	}
	if provider := firstNonEmpty(res.Provider, providerFilter); provider != "" {
		w.Header().Set("X-CLI2API-Provider", provider)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      "chatcmpl-" + requestID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   res.Model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       message,
			"finish_reason": finishReason,
		}},
		"usage": BuildChatUsage(res),
	})
}

func (h *Handler) finishRequestLog(requestID string, started time.Time, req translate.ChatRequest, publicModel, accountID, provider, routing, status string, ttfb int, stats *StreamRelayStats, err error, attemptCount int, resolvedReasoning string) {
	if h.Recorder == nil || requestID == "" {
		return
	}
	entry := accounts.RequestLog{
		ID:                 requestID,
		CreatedAt:          started,
		Stream:             req.Stream,
		Status:             status,
		RequestedModel:     firstNonEmpty(publicModel, req.Model),
		RequestedReasoning: executor.RequestedReasoningLevel(req),
		ResolvedReasoning:  resolvedReasoning,
		AccountID:          accountID,
		Provider:           provider,
		Routing:            routing,
		AttemptCount:       attemptCount,
	}
	if status != accounts.RequestStatusStarted && status != accounts.RequestStatusStreaming {
		finished := time.Now().UTC()
		latency := int(finished.Sub(started).Milliseconds())
		entry.FinishedAt = &finished
		entry.LatencyMs = &latency
	}
	if ttfb > 0 {
		entry.TTFBMs = &ttfb
	} else if !req.Stream && entry.LatencyMs != nil {
		entry.TTFBMs = entry.LatencyMs
	}
	if stats != nil {
		entry.PromptTokens = stats.PromptTokens
		entry.CompletionTokens = stats.CompletionTokens
		entry.CacheReadTokens = stats.CacheReadTokens
		entry.CacheWriteTokens = stats.CacheWriteTokens
		entry.UsageSource = stats.UsageSource
		consumed := stats.ConsumedCredits
		if consumed == nil {
			consumed = stats.Credits
		}
		entry.Credits = consumed
		if stats.Model != "" {
			entry.MappedModel = stats.Model
		}
	}
	if err != nil {
		classified := ClassifyAPIError(err)
		entry.ErrorKind = classified.Kind
		entry.ErrorCode = classified.Code
		entry.ErrorMessage = classified.Message
	}
	h.Recorder.Finish(entry)
	if stats != nil && entry.Credits != nil {
		h.Recorder.UsageDetail(accounts.RequestUsageDetail{
			RequestID: requestID,
			CreatedAt: started,
			Provider:  provider,
			Credit:    entry.Credits,
			Unit:      "credits",
		})
	}
}

func (h *Handler) recordStreamDiagnostic(requestID string, response *http.Response, started time.Time, stats StreamRelayStats, relayErr, contextErr error) {
	if h.Recorder == nil || requestID == "" {
		return
	}
	finished := time.Now().UTC()
	diagnostic := accounts.RequestStreamDiagnostic{
		RequestID:     requestID,
		CreatedAt:     started,
		FinishedAt:    &finished,
		SSEEventCount: stats.SSEEventCount,
		BytesRead:     stats.BytesRead,
		LastEvent:     stats.LastEvent,
		SawDone:       stats.SawDone,
	}
	if response != nil {
		status := response.StatusCode
		diagnostic.UpstreamStatus = &status
		if response.ContentLength >= 0 {
			contentLength := int(response.ContentLength)
			diagnostic.ContentLength = contentLength
		}
		diagnostic.UpstreamRequestID = firstNonEmpty(
			response.Header.Get("X-Request-ID"),
			response.Header.Get("X-Request-Id"),
			response.Header.Get("X-Upstream-Request-ID"),
		)
	}
	if contextErr != nil {
		diagnostic.ContextErr = contextErr.Error()
	}
	if relayErr != nil {
		diagnostic.RelayError = relayErr.Error()
	}
	switch {
	case IsStreamClientDisconnect(relayErr):
		diagnostic.CancellationSource = "client_disconnect"
	case contextErr != nil:
		diagnostic.CancellationSource = "request_context_canceled"
	case errors.Is(relayErr, context.DeadlineExceeded):
		diagnostic.CancellationSource = "request_timeout"
	case errors.Is(relayErr, context.Canceled):
		diagnostic.CancellationSource = "upstream_context_canceled"
	case relayErr != nil:
		diagnostic.CancellationSource = "upstream_stream_error"
	default:
		diagnostic.CancellationSource = "completed"
	}
	h.Recorder.StreamDiagnostic(diagnostic)
}

func ptrInt(value int) *int { return &value }
