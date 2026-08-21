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

	"github.com/caigee-cmd/cli2api/internal/endpoint"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

type ChatExecutor struct {
	Endpoints  endpoint.Endpoints
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
	RawNote          string
}

func NewChatExecutor(eps endpoint.Endpoints) ChatExecutor {
	worker := strings.TrimRight(strings.TrimSpace(os.Getenv("QODER_WORKER_URL")), "/")
	if worker == "" {
		worker = "http://127.0.0.1:3020"
	}
	return ChatExecutor{
		Endpoints: eps,
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

func (e ChatExecutor) ChatNonStream(ctx context.Context, req translate.ChatRequest) (ChatResult, error) {
	payload, err := json.Marshal(buildWorkerPayload(req, false))
	if err != nil {
		return ChatResult{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.WorkerURL+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return ChatResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if e.WorkerKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+e.WorkerKey)
	}

	resp, err := e.HTTPClient.Do(httpReq)
	if err != nil {
		return ChatResult{}, fmt.Errorf("worker request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return ChatResult{}, fmt.Errorf("worker status=%d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed struct {
		Model string `json:"model"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
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
	return ChatResult{
		Model:            model,
		Content:          content,
		Reasoning:        reasoning,
		ToolCalls:        toolCalls,
		FinishReason:     finishReason,
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
	}, nil
}

func (e ChatExecutor) ChatStreamProxy(ctx context.Context, req translate.ChatRequest) (*http.Response, error) {
	payload, err := json.Marshal(buildWorkerPayload(req, true))
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.WorkerURL+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if e.WorkerKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+e.WorkerKey)
	}
	resp, err := e.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("worker stream request failed: %w", err)
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("worker stream status=%d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp, nil
}
