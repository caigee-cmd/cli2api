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

type chatRequestBuild struct {
	httpReq         *http.Request
	originalByAlias map[string]string
	toolsDiag       string
}

func (c *Client) ChatNonStream(ctx context.Context, accountID string, req translate.ChatRequest) (providers.ChatOutcome, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	built, err := c.buildChatHTTPRequest(ctx, credential, req)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	client.Timeout = 0
	resp, err := client.Do(built.httpReq)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return providers.ChatOutcome{}, classifiedErrorWithToolsDiag(resp.StatusCode, string(body), built.toolsDiag)
	}
	aggregate, err := aggregateConnectStream(resp.Body, built.originalByAlias, built.toolsDiag)
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
	built, err := c.buildChatHTTPRequest(ctx, credential, req)
	if err != nil {
		return nil, err
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return nil, err
	}
	client.Timeout = 0
	resp, err := client.Do(built.httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		return nil, classifiedErrorWithToolsDiag(resp.StatusCode, string(body), built.toolsDiag)
	}
	return rewriteConnectStream(resp, firstNonEmpty(req.Model, "devin"), built.originalByAlias, built.toolsDiag)
}

func (c *Client) buildChatHTTPRequest(ctx context.Context, credential Credential, req translate.ChatRequest) (chatRequestBuild, error) {
	payload := BuildChatPayload(req, currentLevels())
	proto, err := BuildGetChatMessageRequest(
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
	if err != nil {
		return chatRequestBuild{}, fmt.Errorf("encode Devin chat request: %w", err)
	}
	body := WrapConnectEnvelope(proto)
	endpoint := strings.TrimRight(firstNonEmpty(credential.BaseURL, c.serverBase, ServerBase), "/") + PathGetChatMessage
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return chatRequestBuild{}, err
	}
	httpReq.Header.Set("Authorization", BasicAuthHeader(credential.SessionToken))
	httpReq.Header.Set("Content-Type", ContentTypeConnectProto)
	httpReq.Header.Set("Connect-Protocol-Version", ConnectProtocolVersion)
	httpReq.Header.Set("Accept", "*/*")
	httpReq.Header.Set("Sentry-Trace", GenerateSentryTrace())
	httpReq.Header["User-Agent"] = []string{""}
	return chatRequestBuild{
		httpReq:         httpReq,
		originalByAlias: payload.OriginalByAlias,
		toolsDiag:       payload.ToolsDiag,
	}, nil
}

type aggregateResult struct {
	Model            string
	Content          string
	Reasoning        string
	ToolCalls        []map[string]any
	FinishReason     string
	PromptTokens     int
	CompletionTokens int
	CacheReadTokens  *int
	CacheWriteTokens *int
}

type toolCallAccumulator struct{ calls []*ToolCallDelta }

func (a *toolCallAccumulator) add(delta ToolCallDelta) (int, *ToolCallDelta) {
	for index, call := range a.calls {
		if delta.ID != "" && call.ID == delta.ID {
			return index, mergeToolCallDelta(call, delta)
		}
	}
	if len(a.calls) > 0 {
		lastIndex := len(a.calls) - 1
		last := a.calls[lastIndex]
		if (delta.ID != "" && last.ID == "") || (delta.ID == "" && (delta.Name == "" || delta.Name == last.Name)) {
			return lastIndex, mergeToolCallDelta(last, delta)
		}
	}
	// LIMIT: Devin omits an index from ChatToolCall; interleaved anonymous tool
	// deltas cannot be disambiguated until the upstream protocol supplies one.
	a.calls = append(a.calls, &delta)
	return len(a.calls) - 1, &delta
}

func mergeToolCallDelta(target *ToolCallDelta, delta ToolCallDelta) *ToolCallDelta {
	if delta.ID != "" {
		target.ID = delta.ID
	}
	if delta.Name != "" {
		target.Name = delta.Name
	}
	target.Arguments += delta.Arguments
	return target
}

func aggregateConnectStream(r io.Reader, originalByAlias map[string]string, toolsDiag string) (aggregateResult, error) {
	var out aggregateResult
	out.FinishReason = "stop"
	var toolAcc toolCallAccumulator
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
				return out, classifiedErrorWithToolsDiag(status, trailerErr.Error(), toolsDiag)
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
			if delta.Name != "" {
				delta.Name = restoreToolName(delta.Name, originalByAlias)
			}
			toolAcc.add(delta)
		}
		if frame.Usage != nil {
			cacheRead, cacheWrite := int(frame.Usage.CachedTokens), int(frame.Usage.CacheWriteTokens)
			out.CacheReadTokens, out.CacheWriteTokens = &cacheRead, &cacheWrite
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
		return out, classifiedErrorWithToolsDiag(502, "devin stream truncated: missing EOS trailer", toolsDiag)
	}
	if len(toolAcc.calls) > 0 {
		out.FinishReason = "tool_calls"
		for idx, tc := range toolAcc.calls {
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
		CacheReadTokens:  aggregate.CacheReadTokens,
		CacheWriteTokens: aggregate.CacheWriteTokens,
		UsageSource:      "upstream",
	}
	if len(aggregate.ToolCalls) > 0 {
		raw, _ := json.Marshal(aggregate.ToolCalls)
		out.ToolCalls = raw
	}
	return out
}

func rewriteConnectStream(upstream *http.Response, model string, originalByAlias map[string]string, toolsDiag string) (*http.Response, error) {
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

		var toolAcc toolCallAccumulator
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
					_ = pw.CloseWithError(classifiedErrorWithToolsDiag(status, trailerErr.Error(), toolsDiag))
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
				delta.Name = name
				idx, _ := toolAcc.add(delta)
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
					"prompt_tokens":         frame.Usage.PromptTokens,
					"completion_tokens":     frame.Usage.CompletionTokens,
					"total_tokens":          frame.Usage.PromptTokens + frame.Usage.CompletionTokens,
					"cache_read_tokens":     frame.Usage.CachedTokens,
					"cache_write_tokens":    frame.Usage.CacheWriteTokens,
					"prompt_tokens_details": map[string]any{"cached_tokens": frame.Usage.CachedTokens},
				}
				if frame.Usage.ModelName != "" {
					model = frame.Usage.ModelName
				}
			}
		}
		if !sawEOS {
			_ = pw.CloseWithError(classifiedErrorWithToolsDiag(502, "devin stream truncated: missing EOS trailer", toolsDiag))
			return
		}
		finish := "stop"
		if len(toolAcc.calls) > 0 {
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
