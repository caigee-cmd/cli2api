package kimi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// ChatStream posts the internal chat request to the managed Chat Completions
// endpoint and returns the upstream SSE body unchanged. The gateway already
// speaks chat-completions deltas, so no stream rewrite happens here.
func (c *Client) ChatStream(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, providers.ResolvedChat, error) {
	resp, resolved, err := c.doChat(ctx, accountID, req)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		return nil, providers.ResolvedChat{}, newProviderError(resp.StatusCode, string(body))
	}
	return resp, resolved, nil
}

// ChatNonStream forces a stream and collects it, so usage accounting and tool
// call assembly stay on one path with the streaming endpoint.
func (c *Client) ChatNonStream(ctx context.Context, accountID string, req translate.ChatRequest) (providers.ChatOutcome, error) {
	resp, resolved, err := c.doChat(ctx, accountID, req)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return providers.ChatOutcome{}, newProviderError(resp.StatusCode, string(body))
	}
	outcome, err := collectChatStream(resp.Body, req.Model)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	outcome.ReasoningLevel = resolved.ReasoningLevel
	return outcome, nil
}

func (c *Client) doChat(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, providers.ResolvedChat, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	model := resolveModel(req.Model)
	if model == "" {
		return nil, providers.ResolvedChat{}, &providers.Error{
			Kind:    accounts.KindInvalidRequest,
			Status:  http.StatusBadRequest,
			Message: "kimi model is required",
		}
	}
	level, caps := c.resolveLevel(ctx, accountID, model, requestedLevel(req))
	payload, err := buildChatBody(req, model, caps, level)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	resp, err := c.postChat(ctx, accountID, credential, payload)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		// Kimi reports a model the plan does not include with an
		// authentication-shaped error. Refreshing the token cannot fix that, so
		// classify before spending a refresh round.
		if Classify(resp.StatusCode, string(body)).Kind == accounts.KindModelNotAvailable {
			return nil, providers.ResolvedChat{}, newProviderError(resp.StatusCode, string(body))
		}
		// A token can be revoked between probes; refresh once before failing
		// the account so a rotated credential does not surface as an outage.
		refreshed, refreshErr := c.forceRefresh(ctx, accountID)
		if refreshErr != nil {
			return nil, providers.ResolvedChat{}, refreshErr
		}
		resp, err = c.postChat(ctx, accountID, refreshed, payload)
		if err != nil {
			return nil, providers.ResolvedChat{}, err
		}
	}
	return resp, providers.ResolvedChat{ReasoningLevel: level}, nil
}

// postChat sends one chat request with the given credential.
func (c *Client) postChat(ctx context.Context, accountID string, credential Credential, payload []byte) (*http.Response, error) {
	base := c.apiBase(credential)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+chatPath, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+credential.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	applyIdentityHeaders(req, credential.DeviceIDOr())
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

// forceRefresh rotates the access token now, ignoring the expiry schedule.
func (c *Client) forceRefresh(ctx context.Context, accountID string) (Credential, error) {
	lock := c.accountLock(accountID)
	lock.Lock()
	defer lock.Unlock()
	current, err := c.loadCredential(ctx, accountID)
	if err != nil {
		return Credential{}, err
	}
	refreshed, err := c.refreshCredential(ctx, current)
	if err != nil {
		return Credential{}, err
	}
	if err := c.persistRefreshed(ctx, accountID, refreshed); err != nil {
		return Credential{}, err
	}
	return refreshed, nil
}

// resolveLevel clamps the requested reasoning level against the declared model
// capabilities, falling back to the console default and then to the catalog
// default so the account's highest declared configuration is used.
func (c *Client) resolveLevel(ctx context.Context, accountID, reqModel, requested string) (string, providers.ModelCapabilities) {
	caps := providers.ModelCapabilities{}
	if info, ok := c.lookupModel(accountID, reqModel); ok {
		caps = info.Capabilities
	} else if models, err := c.Models(ctx, accountID); err == nil {
		for _, info := range models {
			if strings.EqualFold(info.NativeModel, reqModel) || strings.EqualFold(info.PublicModel, reqModel) {
				caps = info.Capabilities
				break
			}
		}
	}
	level := requested
	if level == "" {
		level = c.storedReasoning(ctx, reqModel)
	}
	// Priority is the client value, then the console default, then the catalog
	// default, and only then the clamp. ResolveReasoningLevel already fills the
	// catalog default when both inputs are empty, so the order has to be applied
	// before the clamp instead of after it.
	return providers.ResolveReasoningLevel(level, caps), caps
}

// resolveModel strips the provider routing prefix a client may send.
func resolveModel(model string) string {
	clean := strings.TrimSpace(model)
	clean = strings.TrimPrefix(clean, "kimi/")
	clean = strings.TrimPrefix(clean, "kimicode/")
	return strings.TrimSpace(clean)
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
				if level := providers.NormalizeReasoningLevel(text); level != "" {
					return level
				}
			}
		}
	}
	return ""
}

type sseAggregate struct {
	content      strings.Builder
	reasoning    strings.Builder
	tools        map[int]*toolCall
	order        []int
	usage        chatUsage
	cachedTokens *int
	finish       string
}

type toolCall struct {
	id   string
	name string
	args strings.Builder
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// collectChatStream folds an SSE chat-completions stream into one outcome.
func collectChatStream(body io.Reader, model string) (providers.ChatOutcome, error) {
	agg := &sseAggregate{tools: map[int]*toolCall{}, finish: "stop"}
	raw, err := io.ReadAll(io.LimitReader(body, 32<<20))
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					Reasoning        string `json:"reasoning"`
					ToolCalls        []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens        int `json:"prompt_tokens"`
				CompletionTokens    int `json:"completion_tokens"`
				PromptTokensDetails *struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}
		if chunk.Usage != nil {
			if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
				agg.usage = chatUsage{
					PromptTokens:     chunk.Usage.PromptTokens,
					CompletionTokens: chunk.Usage.CompletionTokens,
				}
			}
			if chunk.Usage.PromptTokensDetails != nil {
				cached := chunk.Usage.PromptTokensDetails.CachedTokens
				agg.cachedTokens = &cached
			}
		}
		for _, choice := range chunk.Choices {
			agg.content.WriteString(choice.Delta.Content)
			// Newer gateways renamed reasoning_content to reasoning; take
			// whichever dialect the stream speaks without trimming the token.
			if choice.Delta.ReasoningContent != "" {
				agg.reasoning.WriteString(choice.Delta.ReasoningContent)
			} else {
				agg.reasoning.WriteString(choice.Delta.Reasoning)
			}
			if choice.FinishReason != "" {
				agg.finish = choice.FinishReason
			}
			for _, call := range choice.Delta.ToolCalls {
				state := agg.tools[call.Index]
				if state == nil {
					state = &toolCall{}
					agg.tools[call.Index] = state
					agg.order = append(agg.order, call.Index)
				}
				if call.ID != "" {
					state.id = call.ID
				}
				if call.Function.Name != "" {
					state.name = call.Function.Name
				}
				state.args.WriteString(call.Function.Arguments)
			}
		}
	}
	outcome := providers.ChatOutcome{
		Model:            model,
		Content:          agg.content.String(),
		Reasoning:        agg.reasoning.String(),
		FinishReason:     agg.finish,
		PromptTokens:     agg.usage.PromptTokens,
		CompletionTokens: agg.usage.CompletionTokens,
		CacheReadTokens:  agg.cachedTokens,
	}
	if len(agg.order) > 0 {
		calls := make([]map[string]any, 0, len(agg.order))
		for _, index := range agg.order {
			state := agg.tools[index]
			if state.name == "" && state.args.Len() == 0 {
				continue
			}
			calls = append(calls, map[string]any{
				"id":   state.id,
				"type": "function",
				"function": map[string]any{
					"name":      state.name,
					"arguments": state.args.String(),
				},
			})
		}
		if len(calls) > 0 {
			encoded, err := json.Marshal(calls)
			if err != nil {
				return providers.ChatOutcome{}, err
			}
			outcome.ToolCalls = encoded
		}
	}
	return outcome, nil
}
