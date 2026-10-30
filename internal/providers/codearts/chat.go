package codearts

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// ChatStream posts the mapped request and returns a normalized
// chat-completions SSE body. The upstream is OpenAI-shaped, but the adapter
// normalizes event names and appends a final usage chunk.
func (c *Client) ChatStream(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, providers.ResolvedChat, error) {
	upstream, level, err := c.doChat(ctx, accountID, req)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	reader, err := preflightWithTimeout(upstream)
	if err != nil {
		if providerErr, ok := err.(*providers.Error); ok && providerErr.Kind == accounts.KindModelNotAvailable {
			c.markModelUnusable(accountID, req.Model)
		}
		return nil, providers.ResolvedChat{}, err
	}
	pr, pw := io.Pipe()
	model := c.canonicalModel(accountID, req.Model)
	go func() {
		defer upstream.Body.Close()
		defer pw.Close()
		if err := streamCapture(pw, reader, model); err != nil {
			if providerErr, ok := err.(*providers.Error); ok && providerErr.Kind == accounts.KindModelNotAvailable {
				c.markModelUnusable(accountID, model)
			}
			_ = pw.CloseWithError(err)
		}
	}()
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "Cache-Control": []string{"no-cache"}},
		Body:       pr,
		Request:    upstream.Request,
	}, providers.ResolvedChat{ReasoningLevel: level}, nil
}

// ChatNonStream forces a stream and aggregates it, keeping usage, reasoning and
// tool-call assembly on the same parser as the streaming path.
func (c *Client) ChatNonStream(ctx context.Context, accountID string, req translate.ChatRequest) (providers.ChatOutcome, error) {
	upstream, level, err := c.doChat(ctx, accountID, req)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	defer upstream.Body.Close()
	completion, err := aggregateRaw(upstream.Body)
	if err != nil {
		if providerErr, ok := err.(*providers.Error); ok && providerErr.Kind == accounts.KindModelNotAvailable {
			c.markModelUnusable(accountID, req.Model)
		}
		return providers.ChatOutcome{}, err
	}
	outcome := providers.ChatOutcome{
		Model:          c.canonicalModel(accountID, req.Model),
		Content:        completion.Content,
		Reasoning:      completion.Reasoning,
		FinishReason:   completion.Finish,
		UsageSource:    "upstream",
		ReasoningLevel: level,
	}
	if len(completion.ToolCalls) > 0 {
		outcome.FinishReason = "tool_calls"
		raw, err := json.Marshal(completion.ToolCalls)
		if err != nil {
			return providers.ChatOutcome{}, err
		}
		outcome.ToolCalls = raw
	}
	if completion.Usage != nil {
		outcome.PromptTokens = int(numberFrom(completion.Usage, "prompt_tokens"))
		outcome.CompletionTokens = int(numberFrom(completion.Usage, "completion_tokens"))
		if details, ok := completion.Usage["prompt_tokens_details"].(map[string]any); ok {
			if cached := numberFrom(details, "cached_tokens"); cached > 0 {
				value := int(cached)
				outcome.CacheReadTokens = &value
			}
		}
	} else {
		outcome.UsageSource = ""
	}
	if outcome.FinishReason == "" {
		outcome.FinishReason = "stop"
	}
	return outcome, nil
}

func (c *Client) doChat(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, string, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return nil, "", err
	}
	if _, _, ok := c.cachedCatalog(accountID); !ok {
		// The benefit header and exact model spelling depend on the account
		// catalog. A failed discovery is not fatal: chat still carries the
		// client's model id and the upstream will answer with a clear error.
		_, _ = c.Models(ctx, accountID)
	}
	model := strings.TrimSpace(req.Model)
	model = strings.TrimPrefix(model, "codearts/")
	if model == "" || model == "auto" {
		return nil, "", &providers.Error{
			Kind:    accounts.KindInvalidRequest,
			Status:  http.StatusBadRequest,
			Message: "codearts model is required",
		}
	}
	model = c.canonicalModel(accountID, model)
	level, caps := c.resolveLevel(ctx, accountID, model, requestedLevel(req))
	body, err := buildChatBody(req, model, level, caps.Images, c.isBenefitModel(accountID, model))
	if err != nil {
		return nil, "", err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, "", err
	}
	resp, err := c.sendChat(ctx, accountID, credential, raw, model, c.isBenefitModel(accountID, model))
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if Classify(resp.StatusCode, string(payload)).Kind == accounts.KindModelNotAvailable {
			c.markModelUnusable(accountID, model)
			return nil, "", newProviderError(resp.StatusCode, string(payload))
		}
		refreshed, refreshErr := c.forceRefresh(ctx, accountID)
		if refreshErr != nil {
			return nil, "", refreshErr
		}
		resp, err = c.sendChat(ctx, accountID, refreshed, raw, model, c.isBenefitModel(accountID, model))
		if err != nil {
			return nil, "", err
		}
	}
	if resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if modelUnavailableBody(string(payload)) {
			c.markModelUnusable(accountID, model)
		}
		return nil, "", newProviderError(resp.StatusCode, string(payload))
	}
	c.markModelUsable(accountID, model)
	return resp, level, nil
}

// sendChat performs the signed request and enforces the TTFB deadline.
func (c *Client) sendChat(ctx context.Context, accountID string, credential Credential, body []byte, model string, benefit bool) (*http.Response, error) {
	base := c.apiBase(credential)
	requestCtx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, base+chatPath, bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("x-auth-token", credential.SecurityToken)
	req.Header.Set("x-snap-traceid", randomHex(16))
	req.Header.Set("X-Language", Language)
	req.Header.Set("app-id", AppID)
	req.Header.Set("is_confidential", "false")
	if benefit {
		req.Header.Set(HeaderMaasType, MaasBenefit)
	}
	signRequest(req, body, credential.Sign())
	if id, ok := payloadChatID(body); ok && id != "" {
		req.Header.Set("Chat-Id", id)
		req.Header.Set("Session-Id", id)
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		cancel()
		return nil, err
	}
	type result struct {
		resp *http.Response
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := client.Do(req)
		done <- result{resp: resp, err: err}
	}()
	select {
	case <-ctx.Done():
		cancel()
		return nil, ctx.Err()
	case <-time.After(firstByteTimeout):
		cancel()
		return nil, providers.ErrFirstByteTimeout
	case outcome := <-done:
		if outcome.err != nil {
			cancel()
			return nil, outcome.err
		}
		outcome.resp.Body = &cancelReadCloser{ReadCloser: outcome.resp.Body, cancel: cancel}
		return outcome.resp, nil
	}
}

type cancelReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelReadCloser) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

func payloadChatID(body []byte) (string, bool) {
	var probe struct {
		ChatID string `json:"chat_id"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return "", false
	}
	return probe.ChatID, probe.ChatID != ""
}

// resolveLevel applies the catalog clamp. CodeArts does not publish a per-model
// effort list, so a model with no options sends no reasoning_effort and relies
// on the upstream default.
func (c *Client) resolveLevel(ctx context.Context, accountID, model, requested string) (string, providers.ModelCapabilities) {
	caps := c.modelCapabilities(accountID, model)
	level := requested
	if level == "" {
		level = c.storedReasoning(ctx, model)
	}
	return providers.ResolveReasoningLevel(level, caps), caps
}

func (c *Client) modelCapabilities(accountID, model string) providers.ModelCapabilities {
	caps := providers.ModelCapabilities{}
	if models, _, ok := c.cachedCatalog(accountID); ok {
		for _, info := range models {
			if strings.EqualFold(info.NativeModel, model) || strings.EqualFold(info.PublicModel, model) {
				caps = info.Capabilities
				break
			}
		}
	}
	if strings.EqualFold(model, "deepseek-v4.1-flash-1017") {
		caps.ReasoningOptions = []string{"none", "low", "high", "max"}
		caps.ReasoningDefault = "high"
	}
	return caps
}

func requestedLevel(req translate.ChatRequest) string {
	if len(req.ReasoningEffort) == 0 {
		return ""
	}
	var value any
	if json.Unmarshal(req.ReasoningEffort, &value) != nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return providers.NormalizeReasoningLevel(typed)
	case map[string]any:
		for _, key := range []string{"effort", "level", "type"} {
			if text, ok := typed[key].(string); ok {
				return providers.NormalizeReasoningLevel(text)
			}
		}
	}
	return ""
}

// ModelSettingReader is optional. Missing it means the console reasoning
// default is skipped and the catalog default is used.
type ModelSettingReader interface {
	GetProviderModelSetting(context.Context, string, string) (accounts.ProviderModelSetting, error)
}

func (c *Client) storedReasoning(ctx context.Context, model string) string {
	reader, ok := c.store.(ModelSettingReader)
	if !ok {
		return ""
	}
	stored, err := reader.GetProviderModelSetting(ctx, ProviderID, accounts.CanonicalModelID(model))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(stored.ReasoningEffort)
}

func numberFrom(values map[string]any, key string) float64 {
	switch value := values[key].(type) {
	case float64:
		return value
	case json.Number:
		parsed, _ := value.Float64()
		return parsed
	case int:
		return float64(value)
	}
	return 0
}
