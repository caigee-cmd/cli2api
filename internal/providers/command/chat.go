package command

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// ChatNonStream sends one non-streaming /alpha/generate call and aggregates the
// NDJSON response. The upstream endpoint always streams, so the adapter forces
// stream:true and collects the full result server-side.
func (c *Client) ChatNonStream(ctx context.Context, accountID string, req translate.ChatRequest) (providers.ChatOutcome, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	client.Timeout = 0
	model, err := resolveModel(req.Model)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	resp, err := c.doGenerate(ctx, client, credential, req, model)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return providers.ChatOutcome{}, newProviderError(resp.StatusCode, string(body))
	}
	return aggregateResponse(resp.Body, model)
}

// ChatStream sends one /alpha/generate call and rewrites the NDJSON response
// into an OpenAI-compatible SSE stream (io.Pipe + a synthetic http.Response),
// mirroring the pattern used by the other in-process adapters.
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
	model, err := resolveModel(req.Model)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	resp, err := c.doGenerate(ctx, client, credential, req, model)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		return nil, providers.ResolvedChat{}, newProviderError(resp.StatusCode, string(body))
	}
	streamResp := rewriteStream(resp, model)
	return streamResp, providers.ResolvedChat{}, nil
}

func (c *Client) doGenerate(ctx context.Context, client *http.Client, credential Credential, req translate.ChatRequest, model string) (*http.Response, error) {
	payload, err := buildGenerateRequest(req, model)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpointBase(credential)+PathGenerate, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+credential.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/x-ndjson")
	httpReq.Header.Set("x-cli-environment", CLIEnvironment)
	httpReq.Header.Set("x-command-code-version", c.cliVersion())
	httpReq.Header.Set("x-session-id", newSessionID())
	if strings.TrimSpace(os.Getenv("CMD_ZDR")) != "" {
		httpReq.Header.Set("x-cmd-zdr", "1")
	}
	return client.Do(httpReq)
}

// resolveModel normalizes the requested model to a catalog id. The executor has
// already mapped a public id to its native id when the account advertises it.
func resolveModel(model string) (string, error) {
	clean := strings.TrimSpace(model)
	clean = strings.TrimPrefix(clean, "command/")
	clean = strings.TrimPrefix(clean, "command-code/")
	clean = strings.TrimSpace(clean)
	if clean == "" {
		return "", fmt.Errorf("command model required")
	}
	return clean, nil
}

// toolCallState accumulates one streamed tool call keyed by its gateway id.
type toolCallState struct {
	index   int
	id      string
	name    string
	args    strings.Builder
	started bool
}

// aggregate collects a full NDJSON response into a non-stream outcome.
type aggregate struct {
	content          strings.Builder
	reasoning        strings.Builder
	toolOrder        []string
	tools            map[string]*toolCallState
	inputTokens      int
	outputTokens     int
	cacheReadTokens  int
	finishReason     string
	sawTerminalEvent bool
	sawError         error
}

func newAggregate() *aggregate {
	return &aggregate{tools: map[string]*toolCallState{}, finishReason: "stop"}
}

func (a *aggregate) apply(event generateEvent) {
	switch event.Type {
	case "reasoning-delta":
		a.reasoning.WriteString(event.Text)
	case "text-delta":
		a.content.WriteString(event.Text)
	case "tool-input-start", "tool-input-delta", "tool-input-end", "tool-call":
		a.toolEvent(event)
	case "finish-step", "finish":
		a.terminal(event)
	case "error":
		a.sawError = errorFromEvent(event)
	}
}

func (a *aggregate) toolEvent(event generateEvent) {
	id := event.toolEventID()
	if id == "" {
		return
	}
	state := a.tools[id]
	if state == nil {
		state = &toolCallState{index: len(a.toolOrder), id: id}
		a.tools[id] = state
		a.toolOrder = append(a.toolOrder, id)
	}
	if name := event.toolEventName(); name != "" {
		state.name = name
	}
	if event.Type == "tool-input-delta" {
		state.args.WriteString(event.Delta)
	}
	if event.Type == "tool-call" {
		if full := fullToolInput(event); full != "" {
			state.args.Reset()
			state.args.WriteString(full)
		}
	}
}

func (a *aggregate) terminal(event generateEvent) {
	a.sawTerminalEvent = true
	if usage := event.usage(); usage != nil {
		a.inputTokens = usage.InputTokens
		a.outputTokens = usage.OutputTokens
		a.cacheReadTokens = usage.cacheReadTokens()
	}
	if event.FinishReason != "" {
		a.finishReason = event.FinishReason
	}
}

func (a *aggregate) toolCalls() []map[string]any {
	if len(a.toolOrder) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(a.toolOrder))
	for _, id := range a.toolOrder {
		state := a.tools[id]
		if state.name == "" {
			continue
		}
		args := strings.TrimSpace(state.args.String())
		if args == "" {
			args = "{}"
		}
		out = append(out, map[string]any{
			"id":   state.id,
			"type": "function",
			"function": map[string]any{
				"name":      state.name,
				"arguments": args,
			},
			"index": state.index,
		})
	}
	return out
}

func aggregateResponse(body io.Reader, model string) (providers.ChatOutcome, error) {
	agg := newAggregate()
	if err := streamEvents(body, func(event generateEvent) error {
		agg.apply(event)
		return nil
	}); err != nil {
		return providers.ChatOutcome{}, err
	}
	if agg.sawError != nil {
		return providers.ChatOutcome{}, agg.sawError
	}
	if !agg.sawTerminalEvent {
		return providers.ChatOutcome{}, newProviderError(502, "command code stream ended before a terminal event")
	}
	return outcomeFromAggregate(agg, model), nil
}

func outcomeFromAggregate(agg *aggregate, model string) providers.ChatOutcome {
	out := providers.ChatOutcome{
		Model:            model,
		Content:          agg.content.String(),
		Reasoning:        agg.reasoning.String(),
		FinishReason:     normalizeFinishReason(agg.finishReason, len(agg.toolOrder) > 0),
		PromptTokens:     agg.inputTokens,
		CompletionTokens: agg.outputTokens,
		UsageSource:      "upstream",
	}
	if agg.cacheReadTokens > 0 {
		cacheRead := agg.cacheReadTokens
		out.CacheReadTokens = &cacheRead
	}
	if calls := agg.toolCalls(); len(calls) > 0 {
		raw, _ := json.Marshal(calls)
		out.ToolCalls = raw
	}
	return out
}

// normalizeFinishReason maps the gateway's finishReason onto OpenAI's. Some OSS
// models report "stop" even when they emitted tool calls, so tool calls win.
func normalizeFinishReason(reason string, hasToolCalls bool) string {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "length":
		return "length"
	case "tool-calls", "tool_calls", "tool_use", "tool-use":
		return "tool_calls"
	}
	if hasToolCalls {
		return "tool_calls"
	}
	return "stop"
}

func fullToolInput(event generateEvent) string {
	for _, raw := range []json.RawMessage{event.Input, event.Args} {
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		if json.Valid(raw) {
			return string(raw)
		}
	}
	return ""
}

func newSessionID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("session-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw[:])
}
