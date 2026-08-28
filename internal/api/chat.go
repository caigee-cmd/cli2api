package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/executor"
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

func relayOpenAIStream(w http.ResponseWriter, body io.Reader) (streamRelayStats, error) {
	var writer io.Writer = w
	if flusher, ok := w.(http.Flusher); ok {
		writer = streamFlushWriter{w: w, f: flusher}
	}

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineSize)
	sawDone := false
	var stats streamRelayStats
	for scanner.Scan() {
		line := scanner.Text()
		if stats.FirstTokenAt == nil && sseDeltaHasToken(line) {
			now := time.Now()
			stats.FirstTokenAt = &now
		}
		if usage, ok := parseStreamUsageLine(line); ok {
			usage.FirstTokenAt = stats.FirstTokenAt
			stats = usage
		}
		if _, err := io.WriteString(writer, line+"\n"); err != nil {
			return stats, err
		}
		if strings.TrimSpace(strings.TrimPrefix(line, "data:")) == "[DONE]" {
			sawDone = true
		}
	}
	if err := scanner.Err(); err != nil {
		return stats, fmt.Errorf("stream read error: %w", err)
	}
	if !sawDone {
		return stats, fmt.Errorf("stream ended before [DONE]")
	}
	return stats, nil
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
		"data":   s.decorateModelsWithContext(r.Context(), s.fetchWorkerModels(false)),
	})
}

// resolveProviderFilter enforces public-model ID rules. Prefixed IDs pin one
// provider family. Bare IDs stay on Qoder unless CROSS_PROVIDER_MODEL_POOL=1,
// which leaves the filter empty so same-named models can share a route pool.
func (s *Server) resolveProviderFilter(req *translate.ChatRequest) string {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return ""
	}
	for _, prefix := range []string{"qoder/", "workbuddy/"} {
		if strings.HasPrefix(model, prefix) {
			req.Model = strings.TrimPrefix(model, prefix)
			return strings.TrimSuffix(prefix, "/")
		}
	}
	if s != nil && s.cfg.CrossProviderModelPool {
		return ""
	}
	return "qoder"
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
		"data":   s.decorateModelsWithContext(r.Context(), models),
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
	if err := s.applyModelContextDefaults(r.Context(), &req); err != nil {
		writeErr(w, http.StatusInternalServerError, "model_setting_failed", err.Error())
		return
	}

	publicModel := req.Model
	providerFilter := s.resolveProviderFilter(&req)
	prefer := s.requestedAccount(r)
	if s.manager != nil {
		s.manager.EnsureModelCatalogs(r.Context(), false)
	}

	requestID := accounts.NewRequestID()
	started := time.Now().UTC()
	s.startRequestLog(accounts.RequestLog{
		ID: requestID, CreatedAt: started, Stream: req.Stream, Status: accounts.RequestStatusStarted,
		RequestedModel: firstNonEmpty(publicModel, req.Model),
	})
	ctx := executor.WithRequestID(r.Context(), requestID)
	w.Header().Set("X-Request-Id", requestID)

	if req.Stream {
		upstream, err := s.executor.ChatStreamProxy(ctx, req, prefer, providerFilter)
		if err != nil {
			s.finishRequestLog(requestID, started, req, publicModel, upstream.AccountID, accounts.RequestStatusError, upstream.TTFBMs, nil, err, upstream.AttemptCount)
			writeClassifiedErr(w, err)
			return
		}
		defer upstream.Response.Body.Close()
		s.finishRequestLog(requestID, started, req, publicModel, upstream.AccountID, accounts.RequestStatusStreaming, upstream.TTFBMs, nil, nil, upstream.AttemptCount)
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
			if strings.Contains(relayErr.Error(), "before [DONE]") || strings.Contains(strings.ToLower(relayErr.Error()), "broken pipe") {
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
		s.finishRequestLog(requestID, started, req, publicModel, upstream.AccountID, status, ttfb, &stats, relayErr, upstream.AttemptCount)
		if relayErr != nil {
			panic(http.ErrAbortHandler)
		}
		return
	}

	res, err := s.executor.ChatNonStream(ctx, req, prefer, providerFilter)
	if err != nil {
		s.finishRequestLog(requestID, started, req, publicModel, res.AccountID, accounts.RequestStatusError, 0, nil, err, res.AttemptCount)
		writeClassifiedErr(w, err)
		return
	}
	if publicModel != "" {
		res.Model = publicModel
	}
	s.finishRequestLog(requestID, started, req, publicModel, res.AccountID, accounts.RequestStatusOK, 0, &streamRelayStats{
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

func writeClassifiedErr(w http.ResponseWriter, err error) {
	if err == nil {
		writeErr(w, http.StatusServiceUnavailable, "upstream_not_ready", "upstream not ready")
		return
	}
	classified := accounts.Classify(0, err.Error(), "", "", "")
	if classified.RetryAfter > 0 {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(classified.RetryAfter.Seconds())))
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

func (s *Server) finishRequestLog(requestID string, started time.Time, req translate.ChatRequest, publicModel, accountID, status string, ttfb int, stats *streamRelayStats, err error, attemptCount int) {
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
		classified := accounts.Classify(0, err.Error(), "", "", "")
		entry.ErrorKind = classified.Kind
		entry.ErrorCode = classified.Code
		entry.ErrorMessage = classified.Message
	}
	s.recorder.Finish(entry)
}

func ptrInt(value int) *int { return &value }
