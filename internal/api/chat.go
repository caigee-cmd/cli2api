package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/executor"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

const maxSSELineSize = 16 * 1024 * 1024

type streamFlushWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

func (w streamFlushWriter) Write(data []byte) (int, error) {
	n, err := w.w.Write(data)
	if n > 0 {
		w.f.Flush()
	}
	return n, err
}

type streamRelayStats struct {
	PromptTokens     *int
	CompletionTokens *int
	CacheReadTokens  *int
	CacheWriteTokens *int
	CachedTokens     *int
	UsageSource      string
	Credits          *float64
	Model            string
	FirstTokenAt     *time.Time
}

type streamRelayWriteError struct {
	err error
}

func (e *streamRelayWriteError) Error() string {
	if e == nil || e.err == nil {
		return "stream write error"
	}
	return "stream write error: " + e.err.Error()
}

func (e *streamRelayWriteError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func relayOpenAIStream(w http.ResponseWriter, body io.Reader) (streamRelayStats, error) {
	var writer io.Writer = w
	if flusher, ok := w.(http.Flusher); ok {
		writer = streamFlushWriter{w: w, f: flusher}
	}

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineSize)
	sawDone := false
	var stats streamRelayStats
	var frame []string
	var streamErr error

	flushFrame := func() error {
		if len(frame) == 0 {
			return nil
		}
		eventName, data := parseSSEFrame(frame)
		if classifiedErr := classifyStreamSSEError(eventName, data); classifiedErr != nil {
			if streamErr == nil {
				streamErr = classifiedErr
			}
			if err := writeStructuredStreamError(writer, classifiedErr); err != nil {
				return err
			}
			frame = nil
			return nil
		}
		for _, line := range frame {
			line = strings.TrimSuffix(line, "\r")
			if stats.FirstTokenAt == nil && sseDeltaHasToken(line) {
				now := time.Now()
				stats.FirstTokenAt = &now
			}
			if usage, ok := parseStreamUsageLine(line); ok {
				usage.FirstTokenAt = stats.FirstTokenAt
				stats = usage
			}
			if strings.TrimSpace(strings.TrimPrefix(line, "data:")) == "[DONE]" {
				sawDone = true
			}
		}
		output := strings.Join(frame, "\n") + "\n\n"
		if _, err := io.WriteString(writer, output); err != nil {
			return &streamRelayWriteError{err: err}
		}
		frame = nil
		return nil
	}

	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := flushFrame(); err != nil {
				return stats, err
			}
			continue
		}
		frame = append(frame, line)
	}
	if err := scanner.Err(); err != nil {
		if streamErr != nil {
			return stats, streamErr
		}
		streamErr := newStreamProviderError("upstream_stream_interrupted", "stream read error: "+err.Error(), http.StatusBadGateway)
		if writeErr := writeStructuredStreamError(writer, streamErr); writeErr != nil {
			return stats, writeErr
		}
		return stats, streamErr
	}
	if err := flushFrame(); err != nil {
		return stats, err
	}
	if streamErr != nil {
		return stats, streamErr
	}
	if !sawDone {
		streamErr := newStreamProviderError("upstream_stream_incomplete", "stream ended before [DONE]", http.StatusBadGateway)
		if writeErr := writeStructuredStreamError(writer, streamErr); writeErr != nil {
			return stats, writeErr
		}
		return stats, streamErr
	}
	return stats, nil
}

func writeStructuredStreamError(writer io.Writer, providerErr *providers.Error) error {
	if providerErr == nil {
		return nil
	}
	status := providerErr.Status
	if status == 0 {
		status = http.StatusBadGateway
	}
	code := firstNonEmpty(providerErr.Code, "upstream_error")
	typ := firstNonEmpty(providerErr.Type, "api_error")
	message := firstNonEmpty(providerErr.Message, code)
	failover := false
	if providerErr.Failover != nil {
		failover = *providerErr.Failover
	}
	retryAfter := providerErr.RetryAfter
	if retryAfter <= 0 {
		retryAfter = providerErr.Cooldown
	}
	errorPayload := map[string]any{
		"message":  message,
		"type":     typ,
		"code":     code,
		"kind":     firstNonEmpty(providerErr.Kind, accounts.KindUnavailable),
		"status":   status,
		"failover": failover,
	}
	if retryAfter > 0 {
		seconds := int(retryAfter / time.Second)
		if retryAfter%time.Second != 0 {
			seconds++
		}
		if seconds < 1 {
			seconds = 1
		}
		errorPayload["retry_after"] = seconds
	}
	payload, err := json.Marshal(map[string]any{"error": errorPayload})
	if err != nil {
		return err
	}
	if _, err := io.WriteString(writer, "data: "+string(payload)+"\n\n"); err != nil {
		return &streamRelayWriteError{err: err}
	}
	return nil
}

func parseSSEFrame(lines []string) (eventName, data string) {
	eventName = "message"
	dataLines := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			if eventName == "" {
				eventName = "message"
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			value := strings.TrimPrefix(line, "data:")
			if strings.HasPrefix(value, " ") {
				value = value[1:]
			}
			dataLines = append(dataLines, value)
		}
	}
	return eventName, strings.Join(dataLines, "\n")
}

func classifyStreamSSEError(eventName, data string) *providers.Error {
	force := strings.EqualFold(strings.TrimSpace(eventName), "error")
	if !force && !streamJSONLooksLikeError(data) {
		return nil
	}
	status := streamErrorStatus(data)
	body := strings.TrimSpace(data)
	if inner := streamErrorBody(data); inner != "" {
		body = inner
	}
	classified := accounts.Classify(status, body, "", "", "")
	return providerErrorFromClassified(classified)
}

func streamJSONLooksLikeError(raw string) bool {
	var value any
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &value) != nil {
		return false
	}
	return streamValueLooksLikeError(value)
}

func streamValueLooksLikeError(value any) bool {
	object, ok := value.(map[string]any)
	if !ok {
		if nested, ok := value.(string); ok {
			var parsed any
			return json.Unmarshal([]byte(strings.TrimSpace(nested)), &parsed) == nil && streamValueLooksLikeError(parsed)
		}
		return false
	}
	if _, ok := object["choices"]; ok {
		return false
	}
	if _, ok := object["error"]; ok {
		return true
	}
	if nested, ok := object["body"]; ok {
		if streamValueLooksLikeError(nested) {
			return true
		}
	}
	if status, ok := object["status"].(float64); ok && status >= 400 {
		return true
	}
	for _, key := range []string{"code", "msgCode", "kind"} {
		if _, ok := object[key]; ok {
			return true
		}
	}
	if message, ok := object["message"].(string); ok && strings.TrimSpace(message) != "" {
		return true
	}
	return false
}

func streamErrorBody(raw string) string {
	var value any
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &value) != nil {
		return ""
	}
	for {
		switch current := value.(type) {
		case string:
			var nested any
			if json.Unmarshal([]byte(strings.TrimSpace(current)), &nested) != nil {
				return current
			}
			value = nested
		case map[string]any:
			nested, ok := current["body"]
			if !ok {
				encoded, err := json.Marshal(current)
				if err != nil {
					return raw
				}
				return string(encoded)
			}
			value = nested
		default:
			return raw
		}
	}
}

func streamErrorStatus(raw string) int {
	var value any
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &value) != nil {
		return 0
	}
	for {
		switch current := value.(type) {
		case map[string]any:
			for _, key := range []string{"status", "statusCode", "statusCodeValue"} {
				if status, ok := current[key].(float64); ok && int(status) >= 400 {
					return int(status)
				}
			}
			if nested, ok := current["body"]; ok {
				value = nested
				continue
			}
			if nested, ok := current["error"]; ok {
				value = nested
				continue
			}
			return 0
		case string:
			var nested any
			if json.Unmarshal([]byte(strings.TrimSpace(current)), &nested) != nil {
				return 0
			}
			value = nested
		default:
			return 0
		}
	}
}

func newStreamProviderError(code, message string, status int) *providers.Error {
	body, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
			"kind":    accounts.KindUnavailable,
		},
	})
	classified := accounts.Classify(status, string(body), "", accounts.KindUnavailable, "1")
	return providerErrorFromClassified(classified)
}

func providerErrorFromClassified(classified accounts.Classified) *providers.Error {
	failover := classified.Failover
	retryAfter := classified.RetryAfter
	if retryAfter <= 0 {
		retryAfter = classified.Cooldown
	}
	return &providers.Error{
		Kind:       classified.Kind,
		Status:     classified.Status,
		Message:    classified.Message,
		Code:       classified.Code,
		Type:       classified.Type,
		Cooldown:   classified.Cooldown,
		RetryAfter: retryAfter,
		Failover:   &failover,
	}
}

func isStreamClientDisconnect(err error) bool {
	var writeErr *streamRelayWriteError
	return errors.As(err, &writeErr)
}

func sseDeltaHasToken(line string) bool {
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "" || payload == "[DONE]" {
		return false
	}
	var parsed struct {
		Choices []struct {
			Delta struct {
				Content          json.RawMessage `json:"content"`
				ReasoningContent json.RawMessage `json:"reasoning_content"`
				ToolCalls        json.RawMessage `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if json.Unmarshal([]byte(payload), &parsed) != nil || len(parsed.Choices) == 0 {
		return false
	}
	delta := parsed.Choices[0].Delta
	return jsonHasText(delta.Content) || jsonHasText(delta.ReasoningContent) || jsonHasArray(delta.ToolCalls)
}

func jsonHasText(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text) != ""
	}
	var parts []any
	if json.Unmarshal(raw, &parts) == nil {
		return len(parts) > 0
	}
	return true
}

func jsonHasArray(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var parts []any
	if json.Unmarshal(raw, &parts) != nil {
		return false
	}
	return len(parts) > 0
}

func parseStreamUsageLine(line string) (streamRelayStats, bool) {
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "" || payload == "[DONE]" {
		return streamRelayStats{}, false
	}
	var parsed struct {
		Model string `json:"model"`
		Usage *struct {
			PromptTokens     *int     `json:"prompt_tokens"`
			CompletionTokens *int     `json:"completion_tokens"`
			CacheReadTokens  *int     `json:"cache_read_tokens"`
			CacheWriteTokens *int     `json:"cache_write_tokens"`
			Source           string   `json:"source"`
			Credits          *float64 `json:"credits"`
			PromptDetails    struct {
				CachedTokens *int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil || parsed.Usage == nil {
		return streamRelayStats{}, false
	}
	return streamRelayStats{
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
		CacheReadTokens:  parsed.Usage.CacheReadTokens,
		CacheWriteTokens: parsed.Usage.CacheWriteTokens,
		CachedTokens:     parsed.Usage.PromptDetails.CachedTokens,
		UsageSource:      firstNonEmpty(parsed.Usage.Source, "estimate"),
		Credits:          parsed.Usage.Credits,
		Model:            parsed.Model,
	}, true
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   s.decorateModelsWithContext(r.Context(), s.filterModelsForIdentity(r, s.fetchWorkerModels(false))),
	})
}

func providerPrefix(model string) string {
	model = strings.TrimSpace(model)
	for _, prefix := range []string{"qoder/", "workbuddy/", "trae/"} {
		if strings.HasPrefix(model, prefix) {
			return strings.TrimSuffix(prefix, "/")
		}
	}
	return ""
}

func normalizeProviderFamily(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return "qoder"
	}
	return provider
}

// resolveProviderFilter enforces public-model ID rules. Prefixed IDs pin one
// provider family. Bare IDs are rejected when the cross-provider model pool
// setting is disabled; when enabled, the filter is empty for a shared route
// pool.
func (s *Server) rejectsBareModel(model string) bool {
	return strings.TrimSpace(model) != "" && !s.crossProviderModelPool.Load() && providerPrefix(model) == ""
}

func (s *Server) resolveProviderFilter(req *translate.ChatRequest) string {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return ""
	}
	if prefix := providerPrefix(model); prefix != "" {
		req.Model = strings.TrimPrefix(model, prefix+"/")
		return prefix
	}
	if s != nil && s.crossProviderModelPool.Load() {
		return ""
	}
	return "qoder"
}

// applyPinnedProviderFilter lets an explicit account pin select its provider
// family for bare model IDs. Prefixed model IDs keep their forced family so a
// mismatched pin still fails closed inside PickRoute.
func (s *Server) applyPinnedProviderFilter(providerFilter, publicModel, prefer string) string {
	if prefer == "" || s == nil || s.pool == nil {
		return providerFilter
	}
	if providerPrefix(publicModel) != "" {
		return providerFilter
	}
	item, ok := s.pool.ByID(prefer)
	if !ok {
		return providerFilter
	}
	pinned := normalizeProviderFamily(item.Provider)
	if providerFilter == "" || providerFilter == pinned {
		return providerFilter
	}
	return pinned
}

func (s *Server) filterModelsForIdentity(r *http.Request, models []map[string]any) []map[string]any {
	identity := s.requestIdentity(r)
	if len(identity.AllowedProviders) == 0 {
		return models
	}
	filtered := make([]map[string]any, 0, len(models))
	for _, model := range models {
		provider, _ := model["provider"].(string)
		if provider == "" {
			provider, _ = model["owned_by"].(string)
		}
		if identity.AllowsProvider(provider) {
			filtered = append(filtered, model)
		}
	}
	return filtered
}

func (s *Server) handleModelsAPI(w http.ResponseWriter, r *http.Request) {
	refresh := r.URL.Query().Get("refresh") == "1"
	models, err := s.fetchWorkerModelsFor(refresh, s.requestedAccount(r))
	if err != nil {
		// 503, not 502: some reverse proxies replace origin 502 JSON with
		// their own HTML error page, which the console then renders as the
		// catalog failure message.
		writeErr(w, http.StatusServiceUnavailable, "catalog_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   s.decorateModelsWithContext(r.Context(), s.filterModelsForIdentity(r, models)),
	})
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
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
	publicModel := req.Model
	if s.rejectsBareModel(publicModel) {
		writeErr(w, http.StatusBadRequest, "provider_prefix_required", "cross-provider model pool is disabled; use a provider-prefixed model ID such as qoder/glm-5.2")
		return
	}
	providerFilter := s.resolveProviderFilter(&req)
	prefer := s.requestedAccount(r)
	providerFilter = s.applyPinnedProviderFilter(providerFilter, publicModel, prefer)
	identity := s.requestIdentity(r)
	if providerFilter != "" && !identity.AllowsProvider(providerFilter) {
		writeErr(w, http.StatusForbidden, "provider_not_allowed", "This API key cannot use provider "+providerFilter)
		return
	}
	if err := s.applyModelContextDefaults(r.Context(), &req, providerFilter); err != nil {
		writeErr(w, http.StatusInternalServerError, "model_setting_failed", err.Error())
		return
	}
	if s.manager != nil {
		s.manager.EnsureModelCatalogs(r.Context(), false)
	}

	requestID := accounts.NewRequestID()
	started := time.Now().UTC()
	s.startRequestLog(accounts.RequestLog{
		ID: requestID, CreatedAt: started, Stream: req.Stream, Status: accounts.RequestStatusStarted,
		RequestedModel: firstNonEmpty(publicModel, req.Model),
	})
	ctx := executor.WithAllowedProviders(executor.WithRequestID(r.Context(), requestID), identity.AllowedProviders)
	w.Header().Set("X-Request-Id", requestID)

	if req.Stream {
		upstream, err := s.executor.ChatStreamProxy(ctx, req, prefer, providerFilter)
		if err != nil {
			s.finishRequestLog(requestID, started, req, publicModel, upstream.AccountID, firstNonEmpty(upstream.Provider, providerFilter), accounts.RequestStatusError, upstream.TTFBMs, nil, err, upstream.AttemptCount)
			writeClassifiedErr(w, err)
			return
		}
		defer upstream.Response.Body.Close()
		s.finishRequestLog(requestID, started, req, publicModel, upstream.AccountID, firstNonEmpty(upstream.Provider, providerFilter), accounts.RequestStatusStreaming, upstream.TTFBMs, nil, nil, upstream.AttemptCount)
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
		stats, relayErr := relayOpenAIStream(w, upstream.Response.Body)
		status := accounts.RequestStatusOK
		if relayErr != nil {
			if isStreamClientDisconnect(relayErr) {
				status = accounts.RequestStatusCanceled
			} else {
				status = accounts.RequestStatusError
			}
		}
		ttfb := upstream.TTFBMs
		if stats.FirstTokenAt != nil {
			ttfb = int(stats.FirstTokenAt.Sub(started).Milliseconds())
			if ttfb < 1 {
				ttfb = 1
			}
		}
		s.finishRequestLog(requestID, started, req, publicModel, upstream.AccountID, firstNonEmpty(upstream.Provider, providerFilter), status, ttfb, &stats, relayErr, upstream.AttemptCount)
		if relayErr != nil {
			// The upstream answered 200 and failed inside the stream, so the
			// executor's attempt loop never saw it. Feed the classified state
			// back into the pool so the next request can route around a
			// quota-exhausted account. Use req.Model (prefix-stripped by
			// resolveProviderFilter) so the cooldown key matches the key
			// PickRoute uses; publicModel may still carry "qoder/" and would
			// write a cooldown that routing never hits.
			if r.Context().Err() == nil && !errors.Is(relayErr, context.Canceled) && !errors.Is(relayErr, context.DeadlineExceeded) && !isStreamClientDisconnect(relayErr) {
				s.executor.ObserveStreamFailure(upstream.AccountID, relayErr, req.Model)
			}
			panic(http.ErrAbortHandler)
		}
		return
	}

	res, err := s.executor.ChatNonStream(ctx, req, prefer, providerFilter)
	if err != nil {
		s.finishRequestLog(requestID, started, req, publicModel, res.AccountID, firstNonEmpty(res.Provider, providerFilter), accounts.RequestStatusError, 0, nil, err, res.AttemptCount)
		writeClassifiedErr(w, err)
		return
	}
	if publicModel != "" {
		res.Model = publicModel
	}
	s.finishRequestLog(requestID, started, req, publicModel, res.AccountID, firstNonEmpty(res.Provider, providerFilter), accounts.RequestStatusOK, 0, &streamRelayStats{
		PromptTokens: ptrInt(res.PromptTokens), CompletionTokens: ptrInt(res.CompletionTokens),
		CacheReadTokens: res.CacheReadTokens, CacheWriteTokens: res.CacheWriteTokens,
		CachedTokens: res.CachedTokens, UsageSource: res.UsageSource, Credits: res.Credits, Model: res.Model,
	}, nil, res.AttemptCount)
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
		"usage": buildChatUsage(res),
	})
}

func buildChatUsage(res executor.ChatResult) map[string]any {
	out := map[string]any{
		"prompt_tokens":     res.PromptTokens,
		"completion_tokens": res.CompletionTokens,
		"total_tokens":      res.PromptTokens + res.CompletionTokens,
		"source":            firstNonEmpty(res.UsageSource, "estimate"),
	}
	if res.CacheReadTokens != nil {
		out["cache_read_tokens"] = *res.CacheReadTokens
	}
	if res.CacheWriteTokens != nil {
		out["cache_write_tokens"] = *res.CacheWriteTokens
	}
	cachedTokens := res.CachedTokens
	if cachedTokens == nil {
		cachedTokens = res.CacheReadTokens
	}
	if cachedTokens != nil {
		out["prompt_tokens_details"] = map[string]any{"cached_tokens": *cachedTokens}
	}
	if res.Credits != nil {
		out["credits"] = *res.Credits
	}
	return out
}

func classifyAPIError(err error) accounts.Classified {
	if err == nil {
		return accounts.Classify(0, "", "", accounts.KindUnavailable, "")
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return accounts.Classified{
			Kind: accounts.KindUnavailable, Status: 499, Failover: false,
			Code: "request_canceled", Message: err.Error(),
		}
	}
	var classifiedErr *providers.Error
	if errors.As(err, &classifiedErr) && classifiedErr != nil {
		message := strings.TrimSpace(classifiedErr.Message)
		if message == "" {
			message = classifiedErr.Error()
		}
		raw := strings.TrimSpace(strings.Join([]string{message, classifiedErr.Code, classifiedErr.Type}, " "))
		failoverHint := ""
		if classifiedErr.Failover != nil {
			if *classifiedErr.Failover {
				failoverHint = "1"
			} else {
				failoverHint = "0"
			}
		}
		classified := accounts.Classify(classifiedErr.Status, raw, "", classifiedErr.Kind, failoverHint)
		if classifiedErr.Code != "" {
			classified.Code = classifiedErr.Code
		}
		if classifiedErr.Type != "" {
			classified.Type = classifiedErr.Type
		}
		if classifiedErr.Message != "" {
			classified.Message = classifiedErr.Message
		}
		providerRetryAfter := classifiedErr.RetryAfter
		if providerRetryAfter <= 0 {
			providerRetryAfter = classifiedErr.Cooldown
		}
		if providerRetryAfter > 0 {
			classified.Cooldown = providerRetryAfter
			if classified.Kind == accounts.KindRateLimit && classified.Cooldown < 30*time.Second {
				classified.Cooldown = 30 * time.Second
			}
		}
		classified.RetryAfter = classified.Cooldown
		return classified
	}
	return accounts.Classify(0, err.Error(), "", "", "")
}

func writeClassifiedErr(w http.ResponseWriter, err error) {
	if err == nil {
		writeErr(w, http.StatusServiceUnavailable, "upstream_not_ready", "upstream not ready")
		return
	}
	classified := classifyAPIError(err)
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
	w.Header().Set("X-Qoder-Error-Kind", classified.Kind)
	if classified.Failover {
		w.Header().Set("X-Qoder-Failover", "1")
	} else {
		w.Header().Set("X-Qoder-Failover", "0")
	}
	writeErr(w, classified.Status, classified.Code, classified.Message)
}

func (s *Server) startRequestLog(entry accounts.RequestLog) {
	if s.recorder == nil {
		return
	}
	s.recorder.Start(entry)
}

func (s *Server) finishRequestLog(requestID string, started time.Time, req translate.ChatRequest, publicModel, accountID, provider, status string, ttfb int, stats *streamRelayStats, err error, attemptCount int) {
	if s.recorder == nil || requestID == "" {
		return
	}
	entry := accounts.RequestLog{
		ID:             requestID,
		CreatedAt:      started,
		Stream:         req.Stream,
		Status:         status,
		RequestedModel: firstNonEmpty(publicModel, req.Model),
		AccountID:      accountID,
		Provider:       provider,
		AttemptCount:   attemptCount,
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
		entry.Credits = stats.Credits
		if stats.Model != "" {
			entry.MappedModel = stats.Model
		}
	}
	if err != nil {
		classified := classifyAPIError(err)
		entry.ErrorKind = classified.Kind
		entry.ErrorCode = classified.Code
		entry.ErrorMessage = classified.Message
	}
	s.recorder.Finish(entry)
}

func ptrInt(value int) *int { return &value }
