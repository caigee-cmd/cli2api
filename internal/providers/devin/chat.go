package devin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

type chatRequestBuild struct {
	httpReq         *http.Request
	payload         ChatPayload
	originalByAlias map[string]string
	toolsDiag       string
	// fallbackStage:
	// 0=initial
	// 1=strip MCP-looking tools
	// 2=keep only core local tools with minimal schemas (final)
	fallbackStage int
}

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

	built, err := c.buildChatHTTPRequest(ctx, credential, req)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	var lastErr error
	for {
		outcome, denial, err := c.chatNonStreamOnce(client, built, req.Model)
		if err == nil {
			return outcome, nil
		}
		lastErr = err
		if !denial {
			return providers.ChatOutcome{}, err
		}
		fallback, ok := c.buildNextMCPFallback(ctx, credential, built)
		if !ok {
			return providers.ChatOutcome{}, lastErr
		}
		built = fallback
	}
}

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

	built, err := c.buildChatHTTPRequest(ctx, credential, req)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	var lastErr error
	for {
		resp, denial, err := c.chatStreamOnce(client, built)
		if err == nil {
			streamResp, err := rewriteConnectStream(resp, firstNonEmpty(req.Model, "devin"), built.originalByAlias, built.toolsDiag)
			if err != nil {
				return nil, providers.ResolvedChat{}, err
			}
			// Devin does not accept a reasoning level; nothing to surface.
			return streamResp, providers.ResolvedChat{}, nil
		}
		lastErr = err
		if !denial {
			return nil, providers.ResolvedChat{}, err
		}
		fallback, ok := c.buildNextMCPFallback(ctx, credential, built)
		if !ok {
			return nil, providers.ResolvedChat{}, lastErr
		}
		built = fallback
	}
}

func (c *Client) chatNonStreamOnce(client *http.Client, built chatRequestBuild, model string) (providers.ChatOutcome, bool, error) {
	resp, err := client.Do(built.httpReq)
	if err != nil {
		return providers.ChatOutcome{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		err := classifiedErrorWithToolsDiag(resp.StatusCode, string(body), built.toolsDiag)
		return providers.ChatOutcome{}, isMCPConfigDenialError(err), err
	}
	aggregate, err := aggregateConnectStream(resp.Body, built.originalByAlias, built.toolsDiag)
	if err != nil {
		return providers.ChatOutcome{}, isMCPConfigDenialError(err), err
	}
	return outcomeFromAggregate(aggregate, firstNonEmpty(model, aggregate.Model)), false, nil
}

func (c *Client) chatStreamOnce(client *http.Client, built chatRequestBuild) (*http.Response, bool, error) {
	resp, err := client.Do(built.httpReq)
	if err != nil {
		return nil, false, err
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		err := classifiedErrorWithToolsDiag(resp.StatusCode, string(body), built.toolsDiag)
		return nil, isMCPConfigDenialError(err), err
	}
	// MCP configuration denials often arrive as an end-stream trailer on HTTP
	// 200 before any content. Peek while we still have a fallback left
	// (through core_tools). Do not drop all tools.
	if built.fallbackStage < 2 {
		peeked, denialErr, perr := peekConnectStreamMCPDenial(resp.Body, built.toolsDiag)
		if perr != nil {
			resp.Body.Close()
			return nil, false, perr
		}
		if denialErr != nil {
			resp.Body.Close()
			return nil, true, denialErr
		}
		resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(peeked), resp.Body))
	}
	return resp, false, nil
}

func (c *Client) buildChatHTTPRequest(ctx context.Context, credential Credential, req translate.ChatRequest) (chatRequestBuild, error) {
	payload := BuildChatPayload(req, currentLevels())
	return c.buildChatHTTPRequestFromPayload(ctx, credential, payload, 0)
}

func (c *Client) buildNextMCPFallback(ctx context.Context, credential Credential, built chatRequestBuild) (chatRequestBuild, bool) {
	switch built.fallbackStage {
	case 0:
		if len(built.payload.Tools) == 0 && !promptHasMCPSemantics(built.payload) {
			return chatRequestBuild{}, false
		}
		payload := built.payload
		before := len(payload.Tools)
		payload.Tools = stripMCPSemanticTools(payload.Tools, payload.OriginalByAlias)
		payload.Tools = scrubMCPTextFromTools(payload.Tools)
		payload.Prompts = scrubMCPToolCallsFromPrompts(payload.Prompts, payload.OriginalByAlias)
		payload.System = scrubMCPText(payload.System)
		payload.ToolsDiag = appendFallbackDiag(built.toolsDiag, "fallback=strip_mcp", payload.Tools)
		logMCPFallback("strip_mcp", before, payload.Tools, payload.ToolsDiag)
		next, err := c.buildChatHTTPRequestFromPayload(ctx, credential, payload, 1)
		if err != nil {
			return chatRequestBuild{}, false
		}
		return next, true
	case 1:
		before := len(built.payload.Tools)
		// Final fallback: always keep the core local tools with minimal
		// schemas. Never drop all tools — that leaves the model unable to read
		// or run commands.
		payload := built.payload
		payload.Tools = coreLocalTools()
		payload.Prompts = scrubMCPToolCallsFromPrompts(payload.Prompts, payload.OriginalByAlias)
		payload.System = scrubMCPText(payload.System)
		payload.ToolsDiag = appendFallbackDiag(built.toolsDiag, "fallback=core_tools", payload.Tools)
		logMCPFallback("core_tools", before, payload.Tools, payload.ToolsDiag)
		next, err := c.buildChatHTTPRequestFromPayload(ctx, credential, payload, 2)
		if err != nil {
			return chatRequestBuild{}, false
		}
		return next, true
	default:
		return chatRequestBuild{}, false
	}
}

func appendFallbackDiag(base, label string, tools []Tool) string {
	suffix := label + " out(" + fmt.Sprintf("%d", len(tools)) + ")"
	base = strings.TrimSpace(base)
	if base == "" {
		return suffix
	}
	return base + " | " + suffix
}

func logMCPFallback(stage string, before int, tools []Tool, diag string) {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		if name := strings.TrimSpace(tool.Name); name != "" {
			names = append(names, name)
		}
	}
	log.Printf("devin mcp fallback stage=%s before_tools=%d after_tools=%d names=%v diag=%s", stage, before, len(tools), names, diag)
}

func (c *Client) buildChatHTTPRequestFromPayload(ctx context.Context, credential Credential, payload ChatPayload, fallbackStage int) (chatRequestBuild, error) {
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
		payload:         payload,
		originalByAlias: payload.OriginalByAlias,
		toolsDiag:       payload.ToolsDiag,
		fallbackStage:   fallbackStage,
	}, nil
}

func isMCPConfigDenialError(err error) bool {
	if err == nil {
		return false
	}
	var providerErr *providers.Error
	if errors.As(err, &providerErr) {
		return isDevinMCPConfigDenial(strings.ToLower(providerErr.Message))
	}
	return isDevinMCPConfigDenial(strings.ToLower(err.Error()))
}

// peekConnectStreamMCPDenial reads Connect frames until the stream either
// produces visible output or ends. If it ends with an MCP configuration
// denial and no output, the denial error is returned so the caller can retry.
// Otherwise the exact bytes consumed are returned for splicing back into Body.
func peekConnectStreamMCPDenial(r io.Reader, toolsDiag string) (peeked []byte, denialErr error, err error) {
	var buf bytes.Buffer
	tee := io.TeeReader(r, &buf)
	sawOutput := false
	for {
		flag, payload, readErr := ReadConnectFrame(tee)
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return buf.Bytes(), nil, readErr
		}
		if flag&ConnectFlagEndStream != 0 {
			if status, trailerErr := ParseTrailerError(payload); trailerErr != nil {
				classified := classifiedErrorWithToolsDiag(status, trailerErr.Error(), toolsDiag)
				if !sawOutput && isMCPConfigDenialError(classified) {
					return buf.Bytes(), classified, nil
				}
				// Non-MCP trailer error (or denial after output): let the
				// normal rewrite path surface it from the spliced bytes.
				return buf.Bytes(), nil, nil
			}
			return buf.Bytes(), nil, nil
		}
		frame, parseErr := ParseFrame(payload)
		if parseErr != nil {
			return buf.Bytes(), nil, nil
		}
		if frame.ContentText != "" || frame.ThinkingText != "" || len(frame.ToolCallDeltas) > 0 {
			sawOutput = true
			return buf.Bytes(), nil, nil
		}
	}
	return buf.Bytes(), nil, nil
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
