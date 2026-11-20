package minimaxcode

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func (c *Client) ChatStream(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, providers.ResolvedChat, error) {
	upstream, err := c.doChat(ctx, accountID, req, true)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	pr, pw := io.Pipe()
	go func() {
		defer upstream.Body.Close()
		defer pw.Close()
		if err := relayStream(pw, upstream.Body, req.Model); err != nil {
			_ = pw.CloseWithError(err)
		}
	}()
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "Cache-Control": []string{"no-cache"}},
		Body:       pr,
		Request:    upstream.Request,
	}, providers.ResolvedChat{}, nil
}

func (c *Client) ChatNonStream(ctx context.Context, accountID string, req translate.ChatRequest) (providers.ChatOutcome, error) {
	upstream, err := c.doChat(ctx, accountID, req, false)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	defer upstream.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(upstream.Body, 8<<20))
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	outcome, err := parseNonStream(raw, req.Model)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	outcome.UsageSource = "upstream"
	return outcome, nil
}

func (c *Client) doChat(ctx context.Context, accountID string, req translate.ChatRequest, stream bool) (*http.Response, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return nil, err
	}
	models, err := c.Models(ctx, accountID)
	if err != nil {
		return nil, err
	}
	model, found := normalizedModelKey(models, req.Model)
	if !found {
		return nil, &providers.Error{
			Kind:    accounts.KindModelNotAvailable,
			Status:  http.StatusNotFound,
			Message: fmt.Sprintf("minimaxcode model %q is not available", req.Model),
		}
	}
	copyReq := req
	copyReq.Stream = stream
	body, err := buildChatBody(copyReq, model)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(body)
	endpoint := credential.agentBase() + messagesPath
	makeRequest := func(token string) (*http.Request, error) {
		reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		reqHTTP.Header.Set("Content-Type", "application/json")
		reqHTTP.Header.Set("Accept", "application/json, text/event-stream")
		reqHTTP.Header.Set("Authorization", "Bearer "+token)
		reqHTTP.Header.Set("X-Api-Key", "sk-xxx")
		reqHTTP.Header.Set("Anthropic-Version", AnthropicVersion)
		reqHTTP.Header.Set("User-Agent", UserAgent)
		reqHTTP.Header.Set("X-Mavis-Agent-Id", AgentID)
		_, zoneOffset := time.Now().Zone()
		reqHTTP.Header.Set("X-Mavis-Timezone-Offset", fmt.Sprintf("%d", -zoneOffset))
		reqHTTP.Header.Set("X-Mavis-Session-Id", randomUUID())
		return reqHTTP, nil
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return nil, err
	}
	httpReq, err := makeRequest(credential.AccessToken)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, &providers.Error{Kind: accounts.KindUnavailable, Status: http.StatusBadGateway, Message: err.Error()}
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		refreshed, refreshErr := c.forceRefresh(ctx, accountID)
		if refreshErr != nil {
			return nil, refreshErr
		}
		httpReq, err = makeRequest(refreshed.AccessToken)
		if err != nil {
			return nil, err
		}
		resp, err = client.Do(httpReq)
		if err != nil {
			return nil, &providers.Error{Kind: accounts.KindUnavailable, Status: http.StatusBadGateway, Message: err.Error()}
		}
		_ = payload
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		return nil, newProviderError(resp.StatusCode, string(payload))
	}
	return resp, nil
}

func relayStream(dst io.Writer, src io.Reader, model string) error {
	decoder := newAnthropicDecoder()
	id := "chatcmpl-" + randomHex(12)
	created := time.Now().Unix()
	emit := func(delta map[string]any, finish any) error {
		choice := map[string]any{"index": 0, "delta": delta}
		if finish != nil && finish != "" {
			choice["finish_reason"] = finish
		}
		chunk := map[string]any{"id": id, "object": "chat.completion.chunk", "created": created, "model": model, "choices": []any{choice}}
		raw, _ := json.Marshal(chunk)
		_, err := fmt.Fprintf(dst, "data: %s\n\n", raw)
		return err
	}
	finish := ""
	err := readAnthropicSSE(src, func(eventType, data string) (bool, error) {
		deltas, done, err := decoder.frame(eventType, data)
		if err != nil {
			return false, err
		}
		for _, delta := range deltas {
			out := map[string]any{}
			if delta.Content != "" {
				out["content"] = delta.Content
			} else if len(delta.ToolCalls) == 0 {
				out["role"] = "assistant"
			}
			if len(delta.ToolCalls) > 0 {
				calls := make([]any, 0, len(delta.ToolCalls))
				for _, call := range delta.ToolCalls {
					function := map[string]any{}
					if call.HasName {
						function["name"] = call.Name
					}
					if call.Arguments != "" {
						function["arguments"] = call.Arguments
					}
					entry := map[string]any{"index": call.Index, "type": "function", "function": function}
					if call.HasID {
						entry["id"] = call.ID
					}
					calls = append(calls, entry)
				}
				out["tool_calls"] = calls
			}
			if len(out) > 0 {
				if err := emit(out, nil); err != nil {
					return false, err
				}
			}
		}
		if done {
			finish = decoder.finish
			if finish == "" {
				finish = "stop"
			}
			return true, nil
		}
		return false, nil
	})
	if err != nil && err != io.EOF {
		return err
	}
	if finish == "" {
		finish = "stop"
	}
	if err := emit(map[string]any{}, finish); err != nil {
		return err
	}
	_, err = io.WriteString(dst, "data: [DONE]\n\n")
	return err
}

func parseNonStream(raw []byte, model string) (providers.ChatOutcome, error) {
	var payload struct {
		ID         string `json:"id"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return providers.ChatOutcome{}, err
	}
	outcome := providers.ChatOutcome{Model: first(model, payload.Model), FinishReason: stopReason(payload.StopReason)}
	var toolCalls []map[string]any
	for _, block := range payload.Content {
		switch block.Type {
		case "text":
			outcome.Content += block.Text
		case "tool_use":
			arguments := string(block.Input)
			if arguments == "" {
				arguments = "{}"
			}
			toolCalls = append(toolCalls, map[string]any{
				"id": block.ID, "type": "function",
				"function": map[string]any{"name": block.Name, "arguments": arguments},
			})
		}
	}
	if len(toolCalls) > 0 {
		encoded, _ := json.Marshal(toolCalls)
		outcome.ToolCalls = encoded
		outcome.FinishReason = "tool_calls"
	}
	outcome.PromptTokens = payload.Usage.InputTokens
	outcome.CompletionTokens = payload.Usage.OutputTokens
	return outcome, nil
}

func randomUUID() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[:4], buf[4:6], buf[6:8], buf[8:10], buf[10:])
}

func randomHex(size int) string {
	buf := make([]byte, size)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

var _ = strings.TrimSpace
