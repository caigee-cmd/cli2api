package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/endpoint"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

type ChatExecutor struct {
	Endpoints  endpoint.Endpoints
	Pool       *accounts.Pool
	WorkerURL  string
	WorkerKey  string
	HTTPClient *http.Client
}

type ChatResult struct {
	Model            string
	Content          string
	Reasoning        string
	ToolCalls        json.RawMessage
	FinishReason     string
	PromptTokens     int
	CompletionTokens int
	UsageSource      string
	Credits          *float64
	AccountID        string
	RawNote          string
}

func NewChatExecutor(eps endpoint.Endpoints, pool *accounts.Pool) ChatExecutor {
	if pool == nil {
		pool = accounts.LoadFromEnv()
	}
	worker := ""
	if pool.Len() > 0 {
		if item, ok := pool.First(); ok {
			worker = item.URL
		}
	}
	if worker == "" {
		worker = strings.TrimRight(strings.TrimSpace(os.Getenv("QODER_WORKER_URL")), "/")
	}
	if worker == "" {
		worker = "http://127.0.0.1:3020"
	}
	return ChatExecutor{
		Endpoints: eps,
		Pool:      pool,
		WorkerURL: worker,
		WorkerKey: strings.TrimSpace(os.Getenv("QODER_WORKER_API_KEY")),
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
	if len(req.Tools) > 0 {
		payload["tools"] = json.RawMessage(req.Tools)
	}
	if len(req.ToolChoice) > 0 {
		payload["tool_choice"] = json.RawMessage(req.ToolChoice)
	}
	return payload
}

func (e ChatExecutor) pick(prefer string, excluded map[string]struct{}) (accounts.Item, error) {
	if e.Pool != nil {
		if item, ok := e.Pool.Pick(prefer, excluded); ok {
			return item, nil
		}
	}
	if e.WorkerURL != "" {
		return accounts.Item{ID: "default", URL: e.WorkerURL}, nil
	}
	return accounts.Item{}, fmt.Errorf("no worker accounts configured")
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

func (e ChatExecutor) newWorkerRequest(ctx context.Context, item accounts.Item, payload []byte, prefer string) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, item.URL+"/v1/chat/completions", bytes.NewReader(payload))
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

func (e ChatExecutor) ChatNonStream(ctx context.Context, req translate.ChatRequest, prefer string) (ChatResult, error) {
	payload, err := json.Marshal(buildWorkerPayload(req, false))
	if err != nil {
		return ChatResult{}, err
	}
	excluded := map[string]struct{}{}
	var lastErr error
	attempts := e.attempts()
	pinned := prefer
	for i := 0; i < attempts; i++ {
		item, err := e.pick(prefer, excluded)
		if err != nil {
			return ChatResult{}, err
		}
		headerAccount := item.ID
		if i == 0 && pinned != "" {
			headerAccount = pinned
		}
		prefer = ""
		httpReq, err := e.newWorkerRequest(ctx, item, payload, headerAccount)
		if err != nil {
			return ChatResult{}, err
		}
		resp, err := e.HTTPClient.Do(httpReq)
		if err != nil {
			classified := accounts.Classify(0, err.Error(), "", accounts.KindUnavailable, "")
			lastErr = fmt.Errorf("worker %s request failed: %w", item.ID, err)
			e.markClassified(item.ID, classified)
			excluded[item.ID] = struct{}{}
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if account := resp.Header.Get("X-Qoder-Account"); account != "" {
			item.ID = account
		}
		if resp.StatusCode >= 300 {
			msg := strings.TrimSpace(string(body))
			classified := classifyWorkerErr(resp, msg)
			lastErr = fmt.Errorf("worker %s status=%d: %s", item.ID, resp.StatusCode, msg)
			e.markClassified(item.ID, classified)
			if classified.Failover && i+1 < attempts {
				excluded[item.ID] = struct{}{}
				continue
			}
			return ChatResult{}, lastErr
		}
		result, err := decodeChatResult(req, body)
		if err != nil {
			return ChatResult{}, err
		}
		result.AccountID = item.ID
		e.markOK(item.ID)
		return result, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no worker accounts available")
	}
	return ChatResult{}, lastErr
}

func (e ChatExecutor) attempts() int {
	if e.Pool != nil && e.Pool.Len() > 0 {
		return e.Pool.Len()
	}
	return 1
}

func decodeChatResult(req translate.ChatRequest, body []byte) (ChatResult, error) {
	var parsed struct {
		Model string `json:"model"`
		Usage struct {
			PromptTokens     int      `json:"prompt_tokens"`
			CompletionTokens int      `json:"completion_tokens"`
			Source           string   `json:"source"`
			Credits          *float64 `json:"credits"`
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
		UsageSource:      source,
		Credits:          parsed.Usage.Credits,
	}, nil
}

func (e ChatExecutor) ChatStreamProxy(ctx context.Context, req translate.ChatRequest, prefer string) (*http.Response, string, error) {
	payload, err := json.Marshal(buildWorkerPayload(req, true))
	if err != nil {
		return nil, "", err
	}
	excluded := map[string]struct{}{}
	var lastErr error
	attempts := e.attempts()
	pinned := prefer
	for i := 0; i < attempts; i++ {
		item, err := e.pick(prefer, excluded)
		if err != nil {
			return nil, "", err
		}
		headerAccount := item.ID
		if i == 0 && pinned != "" {
			headerAccount = pinned
		}
		prefer = ""
		httpReq, err := e.newWorkerRequest(ctx, item, payload, headerAccount)
		if err != nil {
			return nil, "", err
		}
		resp, err := e.HTTPClient.Do(httpReq)
		if err != nil {
			classified := accounts.Classify(0, err.Error(), "", accounts.KindUnavailable, "")
			lastErr = fmt.Errorf("worker %s stream request failed: %w", item.ID, err)
			e.markClassified(item.ID, classified)
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
			if classified.Failover && i+1 < attempts {
				excluded[item.ID] = struct{}{}
				continue
			}
			return nil, "", lastErr
		}
		e.markOK(item.ID)
		return resp, item.ID, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no worker accounts available")
	}
	return nil, "", lastErr
}
