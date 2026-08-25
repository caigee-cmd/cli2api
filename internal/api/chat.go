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
		if usage, ok := parseStreamUsageLine(line); ok {
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
// provider family; bare IDs stay exact. CROSS_PROVIDER_MODEL_POOL=1 decides
// whether a bare overlapping ID may route outside Qoder elsewhere in routing.
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
	return ""
}

func (s *Server) handleModelsAPI(w http.ResponseWriter, r *http.Request) {
	refresh := r.URL.Query().Get("refresh") == "1"
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   s.decorateModelsWithContext(r.Context(), s.fetchWorkerModelsFor(refresh, s.requestedAccount(r))),
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

	requestID := accounts.NewRequestID()
	started := time.Now().UTC()
	s.startRequestLog(accounts.RequestLog{
		ID: requestID, CreatedAt: started, Stream: req.Stream, Status: accounts.RequestStatusStarted,
		RequestedModel: firstNonEmpty(publicModel, req.Model),
	})
	ctx := executor.WithRequestID(r.Context(), requestID)
	w.Header().Set("X-Request-Id", requestID)

	if req.Stream {
		upstream, err := s.executor.ChatStreamProxy(ctx, req, prefer)
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
		if providerFilter != "" {
			w.Header().Set("X-CLI2API-Provider", providerFilter)
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
		s.finishRequestLog(requestID, started, req, publicModel, upstream.AccountID, status, upstream.TTFBMs, &stats, relayErr, upstream.AttemptCount)
		if relayErr != nil {
			panic(http.ErrAbortHandler)
		}
		return
	}

	res, err := s.executor.ChatNonStream(ctx, req, prefer)
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
	if providerFilter != "" {
		w.Header().Set("X-CLI2API-Provider", providerFilter)
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
