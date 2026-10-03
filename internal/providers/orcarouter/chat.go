package orcarouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

const maxChatResponseBytes = 64 << 20

// ChatNonStream relays one non-streaming chat completion and reads the OpenAI
// usage block back.
func (c *Client) ChatNonStream(ctx context.Context, accountID string, req translate.ChatRequest) (providers.ChatOutcome, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	payload, model, err := c.buildChatRequest(req, false)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	httpReq, err := c.newChatRequest(ctx, accountID, credential, payload)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return providers.ChatOutcome{}, newProviderError(0, err.Error())
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxChatResponseBytes))
	if err != nil {
		return providers.ChatOutcome{}, newProviderError(0, err.Error())
	}
	if resp.StatusCode >= 300 {
		c.noteAuthFailure(ctx, accountID, credential, resp.StatusCode, string(body))
		return providers.ChatOutcome{}, newProviderError(resp.StatusCode, string(body))
	}
	return parseChatResponse(body, model)
}

// ChatStream relays the upstream SSE body unchanged. The executor's gateway
// relays chat-completions deltas, so no rewriting is needed.
func (c *Client) ChatStream(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, providers.ResolvedChat, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	client.Timeout = 0
	payload, _, err := c.buildChatRequest(req, true)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	httpReq, err := c.newChatRequest(ctx, accountID, credential, payload)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, providers.ResolvedChat{}, newProviderError(0, err.Error())
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		c.noteAuthFailure(ctx, accountID, credential, resp.StatusCode, string(body))
		return nil, providers.ResolvedChat{}, newProviderError(resp.StatusCode, string(body))
	}
	return resp, providers.ResolvedChat{}, nil
}

// noteAuthFailure marks the exact credential generation that made a rejected
// request so a revoked key stops being retried, without touching a credential a
// newer login already installed.
func (c *Client) noteAuthFailure(ctx context.Context, accountID string, credential Credential, status int, body string) {
	if !classifyCredentialError(status, body) {
		return
	}
	c.markNeedsReauth(ctx, accountID, credential.Generation, status)
}

func (c *Client) newChatRequest(ctx context.Context, accountID string, credential Credential, payload []byte) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase(ctx, credential)+ChatPath, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+credential.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	for name, value := range credential.Headers {
		httpReq.Header.Set(name, value)
	}
	return httpReq, nil
}

// buildChatRequest rewrites the request into the OpenAI wire shape OrcaRouter
// speaks and returns the model id actually sent. The executor has already mapped
// a public id to this account's native spelling; a "<vendor>/<model>" id keeps
// its namespace because OrcaRouter addresses models that way.
func (c *Client) buildChatRequest(req translate.ChatRequest, stream bool) ([]byte, string, error) {
	model := strings.TrimSpace(req.Model)
	model = strings.TrimPrefix(model, ProviderID+"/")
	model = strings.TrimPrefix(model, OAuthProviderID+"/")
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, "", fmt.Errorf("orcarouter model required")
	}
	body := map[string]any{
		"model":    model,
		"messages": req.Messages,
		"stream":   stream,
	}
	copyRaw(body, "max_tokens", req.MaxTokens)
	copyRaw(body, "max_completion_tokens", req.MaxCompletionTokens)
	copyRaw(body, "temperature", req.Temperature)
	copyRaw(body, "top_p", req.TopP)
	copyRaw(body, "stop", req.Stop)
	copyRaw(body, "response_format", req.ResponseFormat)
	copyRaw(body, "tools", req.Tools)
	copyRaw(body, "tool_choice", req.ToolChoice)
	copyRaw(body, "reasoning_effort", req.ReasoningEffort)
	copyRaw(body, "thinking", req.Thinking)
	if req.EnableThinking != nil {
		body["enable_thinking"] = *req.EnableThinking
	}
	if req.ParallelToolCalls != nil {
		body["parallel_tool_calls"] = *req.ParallelToolCalls
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, "", err
	}
	return payload, model, nil
}

func copyRaw(body map[string]any, key string, raw json.RawMessage) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return
	}
	body[key] = json.RawMessage(trimmed)
}

type chatEnvelope struct {
	Model   string `json:"model"`
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content          string          `json:"content"`
			ReasoningContent string          `json:"reasoning_content"`
			Reasoning        string          `json:"reasoning"`
			ToolCalls        json.RawMessage `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		PromptDetails    struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
	Error json.RawMessage `json:"error"`
}

func parseChatResponse(body []byte, requested string) (providers.ChatOutcome, error) {
	var parsed chatEnvelope
	if err := json.Unmarshal(body, &parsed); err != nil {
		return providers.ChatOutcome{}, newProviderError(502, "orcarouter returned an unreadable response")
	}
	if len(parsed.Choices) == 0 {
		if len(parsed.Error) > 0 && string(parsed.Error) != "null" {
			return providers.ChatOutcome{}, newProviderError(502, string(parsed.Error))
		}
		return providers.ChatOutcome{}, newProviderError(502, "orcarouter returned no choices")
	}
	choice := parsed.Choices[0]
	out := providers.ChatOutcome{
		Model:            firstNonEmpty(parsed.Model, requested),
		Content:          choice.Message.Content,
		Reasoning:        firstNonEmpty(choice.Message.ReasoningContent, choice.Message.Reasoning),
		FinishReason:     normalizeFinishReason(choice.FinishReason, len(choice.Message.ToolCalls) > 0),
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
		UsageSource:      "upstream",
	}
	if len(choice.Message.ToolCalls) > 0 && string(choice.Message.ToolCalls) != "null" {
		out.ToolCalls = choice.Message.ToolCalls
	}
	if cached := parsed.Usage.PromptDetails.CachedTokens; cached > 0 {
		value := cached
		out.CacheReadTokens = &value
	}
	return out, nil
}

func normalizeFinishReason(reason string, hasToolCalls bool) string {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "length":
		return "length"
	case "tool-calls", "tool_calls", "tool_use", "tool-use":
		return "tool_calls"
	case "content_filter":
		return "content_filter"
	}
	if hasToolCalls {
		return "tool_calls"
	}
	return "stop"
}
