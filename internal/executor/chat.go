package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/endpoint"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/providers/workbuddy"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

type providerRegistry = providers.Registry

type AttemptHook func(accounts.RequestAttempt)

type ChatExecutor struct {
	Pool       *accounts.Pool
	WorkerKey  string
	HTTPClient *http.Client
	Providers  *providerRegistry
	OnAttempt  AttemptHook
}

type ChatResult struct {
	Model            string
	Content          string
	Reasoning        string
	ToolCalls        json.RawMessage
	FinishReason     string
	PromptTokens     int
	CompletionTokens int
	CacheReadTokens  *int
	CacheWriteTokens *int
	CachedTokens     *int
	UsageSource      string
	Credits          *float64
	AccountID        string
	Provider         string
	AttemptCount     int
	RawNote          string
}

type StreamResult struct {
	Response     *http.Response
	AccountID    string
	Provider     string
	AttemptCount int
	TTFBMs       int
}

type requestIDKey struct{}

func WithRequestID(ctx context.Context, id string) context.Context {
	if strings.TrimSpace(id) == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey{}, id)
}

func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func NewChatExecutor(pool *accounts.Pool, workerKey string) ChatExecutor {
	if pool == nil {
		pool = accounts.NewPool(nil, nil)
	}
	return ChatExecutor{
		Pool:      pool,
		WorkerKey: strings.TrimSpace(workerKey),
		HTTPClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func buildWorkerPayload(req translate.ChatRequest, stream bool) map[string]any {
	payload := map[string]any{
		"model":    req.Model,
		"messages": req.Messages,
		"stream":   stream,
	}
	if len(req.MaxTokens) > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if req.IsReasoning != nil {
		payload["is_reasoning"] = *req.IsReasoning
	}
	if req.EnableThinking != nil {
		payload["enable_thinking"] = *req.EnableThinking
	}
	if req.EnableReasoning != nil {
		payload["enable_reasoning"] = *req.EnableReasoning
	}
	if len(req.Thinking) > 0 {
		payload["thinking"] = json.RawMessage(req.Thinking)
	}
	if len(req.ReasoningEffort) > 0 {
		payload["reasoning_effort"] = json.RawMessage(req.ReasoningEffort)
	}
	if len(req.ReasoningBudgetTokens) > 0 {
		payload["reasoning_budget_tokens"] = json.RawMessage(req.ReasoningBudgetTokens)
	}
	if len(req.ContextLength) > 0 {
		payload["context_length"] = json.RawMessage(req.ContextLength)
	}
	if len(req.MaxInputTokens) > 0 {
		payload["max_input_tokens"] = json.RawMessage(req.MaxInputTokens)
	}
	if len(req.Tools) > 0 {
		payload["tools"] = json.RawMessage(req.Tools)
	}
	if len(req.ToolChoice) > 0 {
		payload["tool_choice"] = json.RawMessage(req.ToolChoice)
	}
	return payload
}

func (e ChatExecutor) pick(prefer, providerFilter, regionFilter string, excluded map[string]struct{}) (accounts.Item, error) {
	if e.Pool != nil {
		if item, ok := e.Pool.PickRoute(accounts.RouteQuery{
			PreferAccount:  prefer,
			ProviderFilter: providerFilter,
			RegionFilter:   regionFilter,
			Excluded:       excluded,
		}); ok {
			return item, nil
		}
	}
	if providerFilter != "" && regionFilter != "" {
		return accounts.Item{}, fmt.Errorf("no %s/%s accounts available", providerFilter, regionFilter)
	}
	if providerFilter != "" {
		return accounts.Item{}, fmt.Errorf("no %s accounts available", providerFilter)
	}
	return accounts.Item{}, fmt.Errorf("no worker accounts configured")
}

func (e ChatExecutor) attemptsFor(providerFilter, regionFilter string) int {
	if e.Pool != nil {
		if n := e.Pool.LenRoute(accounts.RouteQuery{ProviderFilter: providerFilter, RegionFilter: regionFilter}); n > 0 {
			return n
		}
	}
	return 1
}

func pinRegion(current, next string) string {
	if current != "" {
		return current
	}
	return strings.ToLower(strings.TrimSpace(next))
}

func isInProcessItem(item accounts.Item) bool {
	if item.Runtime == string(providers.RuntimeInProcess) {
		return true
	}
	if item.Provider != "" && item.Provider != "qoder" && item.URL == "" {
		return true
	}
	return false
}

func (e ChatExecutor) markClassified(id string, c accounts.Classified) {
	if e.Pool == nil || id == "" {
		return
	}
	e.Pool.MarkClassified(id, c)
}

func (e ChatExecutor) markOK(id string) {
	if e.Pool == nil {
		return
	}
	e.Pool.MarkOK(id)
}

func (e ChatExecutor) recordAttempt(ctx context.Context, attempt accounts.RequestAttempt) {
	if e.OnAttempt == nil {
		return
	}
	if attempt.ID == "" {
		attempt.ID = accounts.NewAttemptID()
	}
	if attempt.RequestID == "" {
		attempt.RequestID = RequestIDFromContext(ctx)
	}
	if attempt.RequestID == "" {
		return
	}
	e.OnAttempt(attempt)
}

func (e ChatExecutor) newWorkerRequest(ctx context.Context, item accounts.Item, payload []byte, prefer string) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, item.URL+endpoint.ChatCompletionsPath, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if e.WorkerKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+e.WorkerKey)
	}
	account := prefer
	if account == "" {
		account = item.ID
	}
	if account != "" {
		httpReq.Header.Set("X-Qoder-Account", account)
	}
	if requestID := RequestIDFromContext(ctx); requestID != "" {
		httpReq.Header.Set("X-Request-Id", requestID)
	}
	return httpReq, nil
}

func classifyWorkerErr(resp *http.Response, body string) accounts.Classified {
	status := 0
	retryAfter := ""
	kind := ""
	failover := ""
	if resp != nil {
		status = resp.StatusCode
		retryAfter = resp.Header.Get("Retry-After")
		kind = resp.Header.Get("X-Qoder-Error-Kind")
		failover = resp.Header.Get("X-Qoder-Failover")
	}
	return accounts.Classify(status, body, retryAfter, kind, failover)
}

func (e ChatExecutor) ChatNonStream(ctx context.Context, req translate.ChatRequest, prefer, providerFilter string) (ChatResult, error) {
	payload, err := json.Marshal(buildWorkerPayload(req, false))
	if err != nil {
		return ChatResult{}, err
	}
	excluded := map[string]struct{}{}
	var lastErr error
	regionFilter := ""
	if prefer != "" && e.Pool != nil {
		if pinnedItem, ok := e.Pool.ByID(prefer); ok {
			regionFilter = pinRegion("", pinnedItem.Region)
		}
	}
	attempts := e.attemptsFor(providerFilter, regionFilter)
	pinned := prefer
	for i := 0; i < attempts; i++ {
		item, err := e.pick(prefer, providerFilter, regionFilter, excluded)
		if err != nil {
			if lastErr != nil {
				return ChatResult{AttemptCount: i, AccountID: lastAccountID(excluded), Provider: providerFilter}, lastErr
			}
			return ChatResult{}, err
		}
		prefer = ""
		if regionFilter == "" {
			regionFilter = pinRegion(regionFilter, item.Region)
			attempts = e.attemptsFor(providerFilter, regionFilter)
		}
		if isInProcessItem(item) {
			result, classified, err := e.chatInProcessNonStreamAttempt(ctx, item, req, i)
			if err == nil {
				result.AttemptCount = i + 1
				return result, nil
			}
			lastErr = err
			if classified.Failover && i+1 < attempts {
				excluded[item.ID] = struct{}{}
				continue
			}
			return ChatResult{AttemptCount: i + 1, AccountID: item.ID, Provider: item.Provider}, err
		}
		headerAccount := item.ID
		if i == 0 && pinned != "" {
			headerAccount = pinned
		}
		httpReq, err := e.newWorkerRequest(ctx, item, payload, headerAccount)
		if err != nil {
			return ChatResult{}, err
		}
		started := time.Now()
		resp, err := e.HTTPClient.Do(httpReq)
		if err != nil {
			classified := accounts.Classify(0, err.Error(), "", accounts.KindUnavailable, "")
			lastErr = fmt.Errorf("worker %s request failed: %w", item.ID, err)
			e.markClassified(item.ID, classified)
			latency := int(time.Since(started).Milliseconds())
			e.recordAttempt(ctx, accounts.RequestAttempt{
				AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: ptrTime(time.Now().UTC()),
				Status: accounts.AttemptStatusFailover, ErrorKind: classified.Kind, ErrorMessage: lastErr.Error(), LatencyMs: &latency,
			})
			excluded[item.ID] = struct{}{}
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		finished := time.Now().UTC()
		latency := int(finished.Sub(started).Milliseconds())
		if account := resp.Header.Get("X-Qoder-Account"); account != "" {
			item.ID = account
		}
		if resp.StatusCode >= 300 {
			msg := strings.TrimSpace(string(body))
			classified := classifyWorkerErr(resp, msg)
			lastErr = fmt.Errorf("worker %s status=%d: %s", item.ID, resp.StatusCode, msg)
			e.markClassified(item.ID, classified)
			status := accounts.AttemptStatusError
			if classified.Failover && i+1 < attempts {
				status = accounts.AttemptStatusFailover
				excluded[item.ID] = struct{}{}
				e.recordAttempt(ctx, accounts.RequestAttempt{
					AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
					Status: status, HTTPStatus: ptrInt(resp.StatusCode), ErrorKind: classified.Kind,
					ErrorMessage: truncateErr(msg), LatencyMs: &latency,
				})
				continue
			}
			e.recordAttempt(ctx, accounts.RequestAttempt{
				AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
				Status: status, HTTPStatus: ptrInt(resp.StatusCode), ErrorKind: classified.Kind,
				ErrorMessage: truncateErr(msg), LatencyMs: &latency,
			})
			return ChatResult{AttemptCount: i + 1, AccountID: item.ID, Provider: firstNonEmpty(item.Provider, "qoder")}, lastErr
		}
		result, err := decodeChatResult(req, body)
		if err != nil {
			e.recordAttempt(ctx, accounts.RequestAttempt{
				AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
				Status: accounts.AttemptStatusError, HTTPStatus: ptrInt(resp.StatusCode),
				ErrorKind: accounts.KindUnavailable, ErrorMessage: truncateErr(err.Error()), LatencyMs: &latency,
			})
			return ChatResult{AttemptCount: i + 1, AccountID: item.ID, Provider: firstNonEmpty(item.Provider, "qoder")}, err
		}
		result.AccountID = item.ID
		result.Provider = firstNonEmpty(item.Provider, "qoder")
		result.AttemptCount = i + 1
		e.markOK(item.ID)
		e.recordAttempt(ctx, accounts.RequestAttempt{
			AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
			Status: accounts.AttemptStatusOK, HTTPStatus: ptrInt(resp.StatusCode), LatencyMs: &latency,
			PromptTokens: ptrInt(result.PromptTokens), CompletionTokens: ptrInt(result.CompletionTokens),
			UsageSource: result.UsageSource,
		})
		return result, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no worker accounts available")
	}
	return ChatResult{AttemptCount: attempts}, lastErr
}

func (e ChatExecutor) chatInProcessNonStreamAttempt(ctx context.Context, item accounts.Item, req translate.ChatRequest, attemptIndex int) (ChatResult, accounts.Classified, error) {
	adapter, _ := e.Providers.Get(item.Provider)
	if adapter.Chat == nil {
		return ChatResult{}, accounts.Classified{}, fmt.Errorf("provider %s does not implement chat", item.Provider)
	}
	started := time.Now()
	outcome, err := adapter.Chat.ChatNonStream(ctx, item.ID, req)
	finished := time.Now().UTC()
	latency := int(finished.Sub(started).Milliseconds())
	if err != nil {
		classified := e.classifyInProcessError(err)
		e.markClassified(item.ID, classified)
		status := accounts.AttemptStatusError
		if classified.Failover {
			status = accounts.AttemptStatusFailover
		}
		e.recordAttempt(ctx, accounts.RequestAttempt{
			AttemptIndex: attemptIndex, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
			Status: status, ErrorKind: classified.Kind, ErrorMessage: truncateErr(err.Error()), LatencyMs: &latency,
		})
		return ChatResult{AccountID: item.ID, Provider: item.Provider}, classified, err
	}
	e.markOK(item.ID)
	e.recordAttempt(ctx, accounts.RequestAttempt{
		AttemptIndex: attemptIndex, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
		Status: accounts.AttemptStatusOK, LatencyMs: &latency,
		PromptTokens: ptrInt(outcome.PromptTokens), CompletionTokens: ptrInt(outcome.CompletionTokens),
		UsageSource: outcome.UsageSource,
	})
	return ChatResult{
		Model:            outcome.Model,
		Content:          outcome.Content,
		Reasoning:        outcome.Reasoning,
		ToolCalls:        outcome.ToolCalls,
		FinishReason:     outcome.FinishReason,
		PromptTokens:     outcome.PromptTokens,
		CompletionTokens: outcome.CompletionTokens,
		UsageSource:      outcome.UsageSource,
		AccountID:        item.ID,
		Provider:         item.Provider,
	}, accounts.Classified{}, nil
}

func (e ChatExecutor) chatInProcessStreamAttempt(ctx context.Context, item accounts.Item, req translate.ChatRequest, attemptIndex int) (StreamResult, accounts.Classified, error) {
	adapter, _ := e.Providers.Get(item.Provider)
	if adapter.Chat == nil {
		return StreamResult{}, accounts.Classified{}, fmt.Errorf("provider %s does not implement chat", item.Provider)
	}
	started := time.Now()
	resp, err := adapter.Chat.ChatStream(ctx, item.ID, req)
	if err != nil {
		finished := time.Now().UTC()
		latency := int(finished.Sub(started).Milliseconds())
		classified := e.classifyInProcessError(err)
		e.markClassified(item.ID, classified)
		status := accounts.AttemptStatusError
		if classified.Failover {
			status = accounts.AttemptStatusFailover
		}
		e.recordAttempt(ctx, accounts.RequestAttempt{
			AttemptIndex: attemptIndex, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
			Status: status, ErrorKind: classified.Kind, ErrorMessage: truncateErr(err.Error()), LatencyMs: &latency,
		})
		return StreamResult{AccountID: item.ID, Provider: item.Provider}, classified, err
	}
	e.markOK(item.ID)
	ttfb := int(time.Since(started).Milliseconds())
	headerAt := time.Now().UTC()
	e.recordAttempt(ctx, accounts.RequestAttempt{
		AttemptIndex: attemptIndex, AccountID: item.ID, StartedAt: started, FinishedAt: &headerAt,
		Status: accounts.AttemptStatusOK, HTTPStatus: ptrInt(http.StatusOK), LatencyMs: &ttfb,
	})
	return StreamResult{Response: resp, AccountID: item.ID, Provider: item.Provider, TTFBMs: ttfb}, accounts.Classified{}, nil
}

// classifyInProcessError maps provider adapter errors into the shared cooldown
// taxonomy. Only provider-declared failures move the account; transport and
// decode errors stay classified as unavailable with a short cooldown.
func (e ChatExecutor) classifyInProcessError(err error) accounts.Classified {
	classified := accounts.Classify(0, err.Error(), "", accounts.KindUnavailable, "")
	var classifiedErr *workbuddy.ClassifiedError
	if errors.As(err, &classifiedErr) && classifiedErr.Kind != "" {
		cooldown := 60 * time.Second
		failover := true
		switch classifiedErr.Kind {
		case accounts.KindQuota:
			cooldown = time.Hour
			failover = false
		case accounts.KindAuth:
			cooldown = 30 * time.Minute
		case accounts.KindRateLimit:
			cooldown = time.Minute
		}
		classified = accounts.Classified{
			Kind: classifiedErr.Kind, Status: classifiedErr.Status, Failover: failover,
			Cooldown: cooldown, Code: classifiedErr.Kind, Message: classifiedErr.Message,
		}
	}
	return classified
}

func lastAccountID(excluded map[string]struct{}) string {
	for id := range excluded {
		return id
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func decodeChatResult(req translate.ChatRequest, body []byte) (ChatResult, error) {
	var parsed struct {
		Model string `json:"model"`
		Usage struct {
			PromptTokens     int      `json:"prompt_tokens"`
			CompletionTokens int      `json:"completion_tokens"`
			CacheReadTokens  *int     `json:"cache_read_tokens"`
			CacheWriteTokens *int     `json:"cache_write_tokens"`
			Source           string   `json:"source"`
			Credits          *float64 `json:"credits"`
			PromptDetails    struct {
				CachedTokens *int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content          string          `json:"content"`
				ReasoningContent string          `json:"reasoning_content"`
				ToolCalls        json.RawMessage `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ChatResult{}, fmt.Errorf("decode worker response: %w; body=%s", err, string(body))
	}
	content := ""
	reasoning := ""
	var toolCalls json.RawMessage
	finishReason := "stop"
	if len(parsed.Choices) > 0 {
		content = parsed.Choices[0].Message.Content
		reasoning = parsed.Choices[0].Message.ReasoningContent
		toolCalls = parsed.Choices[0].Message.ToolCalls
		if parsed.Choices[0].FinishReason != "" {
			finishReason = parsed.Choices[0].FinishReason
		} else if len(toolCalls) > 0 && string(toolCalls) != "null" {
			finishReason = "tool_calls"
		}
	}
	model := parsed.Model
	if model == "" {
		model = req.Model
	}
	source := parsed.Usage.Source
	if source == "" {
		source = "estimate"
	}
	return ChatResult{
		Model:            model,
		Content:          content,
		Reasoning:        reasoning,
		ToolCalls:        toolCalls,
		FinishReason:     finishReason,
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
		CacheReadTokens:  parsed.Usage.CacheReadTokens,
		CacheWriteTokens: parsed.Usage.CacheWriteTokens,
		CachedTokens:     parsed.Usage.PromptDetails.CachedTokens,
		UsageSource:      source,
		Credits:          parsed.Usage.Credits,
	}, nil
}

func (e ChatExecutor) ChatStreamProxy(ctx context.Context, req translate.ChatRequest, prefer, providerFilter string) (StreamResult, error) {
	payload, err := json.Marshal(buildWorkerPayload(req, true))
	if err != nil {
		return StreamResult{}, err
	}
	excluded := map[string]struct{}{}
	var lastErr error
	regionFilter := ""
	if prefer != "" && e.Pool != nil {
		if pinnedItem, ok := e.Pool.ByID(prefer); ok {
			regionFilter = pinRegion("", pinnedItem.Region)
		}
	}
	attempts := e.attemptsFor(providerFilter, regionFilter)
	pinned := prefer
	startedAll := time.Now()
	for i := 0; i < attempts; i++ {
		item, err := e.pick(prefer, providerFilter, regionFilter, excluded)
		if err != nil {
			if lastErr != nil {
				return StreamResult{AttemptCount: i, AccountID: lastAccountID(excluded), Provider: providerFilter}, lastErr
			}
			return StreamResult{}, err
		}
		prefer = ""
		if regionFilter == "" {
			regionFilter = pinRegion(regionFilter, item.Region)
			attempts = e.attemptsFor(providerFilter, regionFilter)
		}
		if isInProcessItem(item) {
			result, classified, err := e.chatInProcessStreamAttempt(ctx, item, req, i)
			if err == nil {
				result.AttemptCount = i + 1
				return result, nil
			}
			lastErr = err
			if classified.Failover && i+1 < attempts {
				excluded[item.ID] = struct{}{}
				continue
			}
			return StreamResult{AttemptCount: i + 1, AccountID: item.ID, Provider: item.Provider}, err
		}
		headerAccount := item.ID
		if i == 0 && pinned != "" {
			headerAccount = pinned
		}
		httpReq, err := e.newWorkerRequest(ctx, item, payload, headerAccount)
		if err != nil {
			return StreamResult{}, err
		}
		client := e.HTTPClient
		if client == nil {
			client = http.DefaultClient
		}
		if client.Timeout > 0 {
			streamClient := *client
			streamClient.Timeout = 0
			client = &streamClient
		}
		started := time.Now()
		resp, err := client.Do(httpReq)
		if err != nil {
			classified := accounts.Classify(0, err.Error(), "", accounts.KindUnavailable, "")
			lastErr = fmt.Errorf("worker %s stream request failed: %w", item.ID, err)
			e.markClassified(item.ID, classified)
			latency := int(time.Since(started).Milliseconds())
			e.recordAttempt(ctx, accounts.RequestAttempt{
				AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: ptrTime(time.Now().UTC()),
				Status: accounts.AttemptStatusFailover, ErrorKind: classified.Kind, ErrorMessage: lastErr.Error(), LatencyMs: &latency,
			})
			excluded[item.ID] = struct{}{}
			continue
		}
		if account := resp.Header.Get("X-Qoder-Account"); account != "" {
			item.ID = account
		}
		if resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			msg := strings.TrimSpace(string(body))
			classified := classifyWorkerErr(resp, msg)
			lastErr = fmt.Errorf("worker %s stream status=%d: %s", item.ID, resp.StatusCode, msg)
			e.markClassified(item.ID, classified)
			finished := time.Now().UTC()
			latency := int(finished.Sub(started).Milliseconds())
			status := accounts.AttemptStatusError
			if classified.Failover && i+1 < attempts {
				status = accounts.AttemptStatusFailover
				excluded[item.ID] = struct{}{}
				e.recordAttempt(ctx, accounts.RequestAttempt{
					AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
					Status: status, HTTPStatus: ptrInt(resp.StatusCode), ErrorKind: classified.Kind,
					ErrorMessage: truncateErr(msg), LatencyMs: &latency,
				})
				continue
			}
			e.recordAttempt(ctx, accounts.RequestAttempt{
				AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
				Status: status, HTTPStatus: ptrInt(resp.StatusCode), ErrorKind: classified.Kind,
				ErrorMessage: truncateErr(msg), LatencyMs: &latency,
			})
			return StreamResult{AttemptCount: i + 1, AccountID: item.ID, Provider: firstNonEmpty(item.Provider, "qoder")}, lastErr
		}
		e.markOK(item.ID)
		ttfb := int(time.Since(startedAll).Milliseconds())
		headerAt := time.Now().UTC()
		e.recordAttempt(ctx, accounts.RequestAttempt{
			AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: &headerAt,
			Status: accounts.AttemptStatusOK, HTTPStatus: ptrInt(resp.StatusCode), LatencyMs: &ttfb,
		})
		return StreamResult{
			Response:     resp,
			AccountID:    item.ID,
			Provider:     firstNonEmpty(item.Provider, "qoder"),
			AttemptCount: i + 1,
			TTFBMs:       ttfb,
		}, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no worker accounts available")
	}
	return StreamResult{AttemptCount: attempts}, lastErr
}

func ptrInt(value int) *int { return &value }

func ptrTime(value time.Time) *time.Time { return &value }

func truncateErr(msg string) string {
	msg = strings.TrimSpace(msg)
	if len(msg) <= 500 {
		return msg
	}
	return msg[:500]
}
