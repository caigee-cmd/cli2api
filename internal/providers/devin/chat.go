package devin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func (c *Client) ChatNonStream(ctx context.Context, accountID string, req translate.ChatRequest) (providers.ChatOutcome, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	httpReq, originalByAlias, err := c.buildChatHTTPRequest(ctx, credential, req)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	client.Timeout = 0
	resp, err := client.Do(httpReq)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return providers.ChatOutcome{}, classifiedError(resp.StatusCode, string(body))
	}
	aggregate, err := aggregateConnectStream(resp.Body, originalByAlias)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	return outcomeFromAggregate(aggregate, firstNonEmpty(req.Model, aggregate.Model)), nil
}

func (c *Client) ChatStream(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return nil, err
	}
	httpReq, originalByAlias, err := c.buildChatHTTPRequest(ctx, credential, req)
	if err != nil {
		return nil, err
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return nil, err
	}
	client.Timeout = 0
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		return nil, classifiedError(resp.StatusCode, string(body))
	}
	return rewriteConnectStream(resp, firstNonEmpty(req.Model, "devin"), originalByAlias)
}

func (c *Client) buildChatHTTPRequest(ctx context.Context, credential Credential, req translate.ChatRequest) (*http.Request, map[string]string, error) {
	payload := BuildChatPayload(req, currentLevels())
	proto := BuildGetChatMessageRequest(
		credential.SessionToken,
		credential.DeviceSeed,
		payload.ModelUID,
		payload.System,
		payload.Prompts,
		payload.Tools,
		payload.Temperature,
		payload.MaxTokens,
		"",
		"",
	)
	body := WrapConnectEnvelope(proto)
	endpoint := strings.TrimRight(firstNonEmpty(credential.BaseURL, c.serverBase, ServerBase), "/") + PathGetChatMessage
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	httpReq.Header.Set("Authorization", BasicAuthHeader(credential.SessionToken))
	httpReq.Header.Set("Content-Type", ContentTypeConnectProto)
	httpReq.Header.Set("Connect-Protocol-Version", ConnectProtocolVersion)
	httpReq.Header.Set("Accept", "*/*")
	httpReq.Header.Set("Sentry-Trace", GenerateSentryTrace())
	httpReq.Header["User-Agent"] = []string{""}
	return httpReq, payload.OriginalByAlias, nil
}

type aggregateResult struct {
	Model            string
	Content          string
	Reasoning        string
	ToolCalls        []map[string]any
	FinishReason     string
	PromptTokens     int
	CompletionTokens int
}

func aggregateConnectStream(r io.Reader, originalByAlias map[string]string) (aggregateResult, error) {
	var out aggregateResult
	out.FinishReason = "stop"
	toolAcc := map[int]*ToolCallDelta{}
	sawEOS := false
	for {
		flag, payload, err := ReadConnectFrame(r)
		if err == io.EOF {
			break
		}
		if err != nil {
			return out, err
		}
		if flag&ConnectFlagEndStream != 0 {
			sawEOS = true
			if status, trailerErr := ParseTrailerError(payload); trailerErr != nil {
				return out, classifiedError(status, trailerErr.Error())
			}
			break
		}
		frame, err := ParseFrame(payload)
		if err != nil {
			return out, err
		}
		if frame.ContentText != "" {
			out.Content += frame.ContentText
		}
		if frame.ThinkingText != "" {
			out.Reasoning += frame.ThinkingText
		}
		for _, delta := range frame.ToolCallDeltas {
			idx := delta.Index
			acc, ok := toolAcc[idx]
			if !ok {
				cp := delta
				if cp.Name != "" {
					cp.Name = restoreToolName(cp.Name, originalByAlias)
				}
				toolAcc[idx] = &cp
			} else {
				if delta.ID != "" {
					acc.ID = delta.ID
				}
				if delta.Name != "" {
					acc.Name = restoreToolName(delta.Name, originalByAlias)
				}
				acc.Arguments += delta.Arguments
			}
		}
		if frame.Usage != nil {
			if frame.Usage.PromptTokens > 0 {
				out.PromptTokens = int(frame.Usage.PromptTokens)
			}
			if frame.Usage.CompletionTokens > 0 {
				out.CompletionTokens = int(frame.Usage.CompletionTokens)
			}
			if frame.Usage.ModelName != "" {
				out.Model = frame.Usage.ModelName
			}
		}
		if frame.StopReason == 10 {
			out.FinishReason = "tool_calls"
		} else if frame.StopReason == 2 || frame.StopReason == 4 {
			out.FinishReason = "stop"
		}
	}
	if !sawEOS {
		return out, classifiedError(502, "devin stream truncated: missing EOS trailer")
	}
	if len(toolAcc) > 0 {
		out.FinishReason = "tool_calls"
		keys := make([]int, 0, len(toolAcc))
		for k := range toolAcc {
			keys = append(keys, k)
		}
		// stable-ish order by index
		for i := 0; i < len(keys); i++ {
			for j := i + 1; j < len(keys); j++ {
				if keys[j] < keys[i] {
					keys[i], keys[j] = keys[j], keys[i]
				}
			}
		}
		for _, idx := range keys {
			tc := toolAcc[idx]
			out.ToolCalls = append(out.ToolCalls, map[string]any{
				"id":   tc.ID,
				"type": "function",
				"function": map[string]any{
					"name":      tc.Name,
					"arguments": tc.Arguments,
				},
				"index": idx,
			})
		}
	}
	return out, nil
}

func outcomeFromAggregate(aggregate aggregateResult, fallbackModel string) providers.ChatOutcome {
	out := providers.ChatOutcome{
		Model:            firstNonEmpty(aggregate.Model, fallbackModel),
		Content:          aggregate.Content,
		Reasoning:        aggregate.Reasoning,
		FinishReason:     firstNonEmpty(aggregate.FinishReason, "stop"),
		PromptTokens:     aggregate.PromptTokens,
		CompletionTokens: aggregate.CompletionTokens,
		UsageSource:      "upstream",
	}
	if len(aggregate.ToolCalls) > 0 {
		raw, _ := json.Marshal(aggregate.ToolCalls)
		out.ToolCalls = raw
	}
	return out
}

func rewriteConnectStream(upstream *http.Response, model string, originalByAlias map[string]string) (*http.Response, error) {
	pr, pw := io.Pipe()
	go func() {
		defer upstream.Body.Close()
		defer pw.Close()
		id := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
		created := time.Now().Unix()
		writeChunk := func(delta map[string]any, finish string, usage any) error {
			chunk := map[string]any{
				"id":      id,
				"object":  "chat.completion.chunk",
				"created": created,
				"model":   model,
				"choices": []map[string]any{{
					"index": 0,
					"delta": delta,
				}},
			}
			if finish != "" {
				chunk["choices"].([]map[string]any)[0]["finish_reason"] = finish
			}
			if usage != nil {
				chunk["usage"] = usage
			}
			encoded, err := json.Marshal(chunk)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(pw, "data: %s\n\n", encoded)
			return err
		}

		toolAcc := map[int]*ToolCallDelta{}
		sawEOS := false
		var lastUsage any
		roleSent := false
		for {
			flag, payload, err := ReadConnectFrame(upstream.Body)
			if err == io.EOF {
				break
			}
			if err != nil {
				_ = pw.CloseWithError(err)
				return
			}
			if flag&ConnectFlagEndStream != 0 {
				sawEOS = true
				if status, trailerErr := ParseTrailerError(payload); trailerErr != nil {
					_ = pw.CloseWithError(classifiedError(status, trailerErr.Error()))
					return
				}
				break
			}
			frame, err := ParseFrame(payload)
			if err != nil {
				_ = pw.CloseWithError(err)
				return
			}
			if !roleSent {
				roleSent = true
				if err := writeChunk(map[string]any{"role": "assistant"}, "", nil); err != nil {
					_ = pw.CloseWithError(err)
					return
				}
			}
			if frame.ThinkingText != "" {
				if err := writeChunk(map[string]any{"reasoning_content": frame.ThinkingText}, "", nil); err != nil {
					_ = pw.CloseWithError(err)
					return
				}
			}
			if frame.ContentText != "" {
				if err := writeChunk(map[string]any{"content": frame.ContentText}, "", nil); err != nil {
					_ = pw.CloseWithError(err)
					return
				}
			}
			for _, delta := range frame.ToolCallDeltas {
				name := delta.Name
				if name != "" {
					name = restoreToolName(name, originalByAlias)
				}
				idx := delta.Index
				acc, ok := toolAcc[idx]
				if !ok {
					cp := delta
					cp.Name = name
					toolAcc[idx] = &cp
				} else {
					if delta.ID != "" {
						acc.ID = delta.ID
					}
					if name != "" {
						acc.Name = name
					}
					acc.Arguments += delta.Arguments
				}
				toolDelta := map[string]any{
					"index": idx,
					"id":    delta.ID,
					"type":  "function",
					"function": map[string]any{
						"name":      name,
						"arguments": delta.Arguments,
					},
				}
				if err := writeChunk(map[string]any{"tool_calls": []any{toolDelta}}, "", nil); err != nil {
					_ = pw.CloseWithError(err)
					return
				}
			}
			if frame.Usage != nil {
				lastUsage = map[string]any{
					"prompt_tokens":     frame.Usage.PromptTokens,
					"completion_tokens": frame.Usage.CompletionTokens,
					"total_tokens":      frame.Usage.PromptTokens + frame.Usage.CompletionTokens,
				}
				if frame.Usage.ModelName != "" {
					model = frame.Usage.ModelName
				}
			}
		}
		if !sawEOS {
			_ = pw.CloseWithError(classifiedError(502, "devin stream truncated: missing EOS trailer"))
			return
		}
		finish := "stop"
		if len(toolAcc) > 0 {
			finish = "tool_calls"
		}
		if err := writeChunk(map[string]any{}, finish, lastUsage); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_, _ = io.WriteString(pw, "data: [DONE]\n\n")
	}()

	header := make(http.Header)
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       pr,
	}, nil
}
