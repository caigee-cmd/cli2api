package doubaowork

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// chatRun is one started upstream chat: an open event-stream body plus the
// lease and prepared request needed to translate and clean up.
type chatRun struct {
	sess     *session
	prepared preparedRequest
	selected doubaoModel
	lease    *lease
	body     io.ReadCloser
}

// startChat validates, selects a model, serializes setup, and opens the
// upstream event stream. The caller owns body and lease cleanup.
func (c *Client) startChat(ctx context.Context, accountID string, req translate.ChatRequest) (chatRun, error) {
	sess := c.sessionFor(accountID)
	if err := sess.ready(ctx); err != nil {
		return chatRun{}, err
	}
	if left := sess.challengeLeft(); left > 0 {
		minutes := int(left.Minutes()) + 1
		return chatRun{}, &providers.Error{
			Kind:    "invalid_request",
			Status:  403,
			Message: "豆包要求人工验证，已暂停请求（约 " + itoa(minutes) + " 分钟后自动恢复）。打开 https://www.doubao.com/chat/ 手动发一句话，过掉验证后立刻可用。",
		}
	}

	prepared, err := prepareChatRequest(req)
	if err != nil {
		return chatRun{}, err
	}
	if prepared.HasImages {
		return chatRun{}, &providers.Error{
			Kind:    "invalid_request",
			Status:  400,
			Message: "豆包工作这条路暂时发不了图片：上传入口需要前端生成的签名，纯 HTTP 侧造不出来。需要识图请换一个支持图片的供应商。",
		}
	}

	catalog, err := c.models(ctx, sess)
	if err != nil {
		return chatRun{}, err
	}
	selected, ok := selectModel(catalog, prepared.Model)
	if !ok {
		return chatRun{}, &providers.Error{Kind: "model_not_available", Status: 404, Message: "doubaowork has no model for " + prepared.Model}
	}

	localID := "local_" + randomHex()
	upstream := buildUpstreamBody(sess, prepared, selected, localID)

	if err := sess.acquire(ctx); err != nil {
		return chatRun{}, err
	}
	run := chatRun{sess: sess, prepared: prepared, selected: selected, lease: newLease(sess, localID)}
	if err := run.lease.begin(ctx); err != nil {
		sess.release()
		return chatRun{}, err
	}
	resp, err := sess.request(ctx, http.MethodPost, "/chat/completion", upstream, nil, map[string]string{"Accept": "text/event-stream"})
	sess.release() // release once headers are in hand; streaming does not hold the queue
	if err != nil {
		return chatRun{}, err
	}
	run.body = resp.Body
	return run, nil
}

// ChatStream returns a response whose body is OpenAI chat-completions deltas.
func (c *Client) ChatStream(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, providers.ResolvedChat, error) {
	run, err := c.startChat(ctx, accountID, req)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	pr, pw := io.Pipe()
	go func() {
		defer run.body.Close()
		var failure error
		defer func() {
			if finishErr := run.lease.finish(context.WithoutCancel(ctx), failure); finishErr != nil && failure == nil {
				// cleanup failures never downgrade a delivered stream
				_ = finishErr
			}
		}()
		failure = writeOpenAIStream(ctx, pw, run)
		if failure != nil {
			_ = pw.CloseWithError(failure)
			return
		}
		_ = pw.Close()
	}()
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"text/event-stream; charset=utf-8"}},
		Body:       pr,
	}, providers.ResolvedChat{ReasoningLevel: run.prepared.ReasoningEffort}, nil
}

// ChatNonStream collects the upstream stream and returns one ChatOutcome.
func (c *Client) ChatNonStream(ctx context.Context, accountID string, req translate.ChatRequest) (providers.ChatOutcome, error) {
	run, err := c.startChat(ctx, accountID, req)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	defer run.body.Close()
	content, reasoning, streamErr := collectStream(ctx, run)
	finishErr := run.lease.finish(ctx, streamErr)
	if streamErr != nil {
		return providers.ChatOutcome{}, streamErr
	}
	_ = finishErr

	messageContent, toolCalls, err := decodeReply(content, run.prepared)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	finish := "stop"
	if len(toolCalls) > 0 {
		finish = "tool_calls"
	}
	promptTokens := estimateTokens(run.prepared.Prompt)
	completionTokens := estimateTokens(content + reasoning)
	return providers.ChatOutcome{
		Model:            req.Model,
		Content:          messageContent,
		Reasoning:        reasoning,
		ToolCalls:        toolCalls,
		FinishReason:     finish,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		UsageSource:      "estimated",
		ReasoningLevel:   run.prepared.ReasoningEffort,
	}, nil
}

// collectStream consumes the translated deltas into full text/reasoning.
func collectStream(ctx context.Context, run chatRun) (string, string, error) {
	var content, reasoning strings.Builder
	err := readDoubaoTranslates(ctx, run.body, run.sess, run.lease, func(deltaContent, deltaReasoning string) error {
		content.WriteString(deltaContent)
		reasoning.WriteString(deltaReasoning)
		return nil
	})
	if err != nil {
		if isCaptchaError(err) {
			run.sess.noteChallenge()
		}
		return content.String(), reasoning.String(), err
	}
	return content.String(), reasoning.String(), nil
}

// writeOpenAIStream translates the upstream events into OpenAI SSE frames.
func writeOpenAIStream(ctx context.Context, w io.Writer, run chatRun) error {
	meta := streamMeta{
		ID:      "chatcmpl-" + randomHex(),
		Created: time.Now().Unix(),
		Model:   run.prepared.Model,
	}
	buffered := run.prepared.HasTools || run.prepared.Structured

	var contentBuf, reasoningBuf strings.Builder
	emit := func(deltaContent, deltaReasoning string) error {
		contentBuf.WriteString(deltaContent)
		reasoningBuf.WriteString(deltaReasoning)
		if buffered {
			return nil
		}
		delta := map[string]any{}
		if deltaReasoning != "" {
			delta["reasoning_content"] = deltaReasoning
		}
		if deltaContent != "" {
			delta["content"] = deltaContent
		}
		return writeSSEChunk(w, meta, delta, "")
	}

	err := readDoubaoTranslates(ctx, run.body, run.sess, run.lease, emit)
	if err != nil {
		if isCaptchaError(err) {
			run.sess.noteChallenge()
		}
		writeSSEError(w, err)
		return err
	}

	if buffered {
		messageContent, toolCalls, err := decodeReply(contentBuf.String(), run.prepared)
		if err != nil {
			writeSSEError(w, err)
			return err
		}
		if reasoningBuf.Len() > 0 {
			if err := writeSSEChunk(w, meta, map[string]any{"reasoning_content": reasoningBuf.String()}, ""); err != nil {
				return err
			}
		}
		delta := map[string]any{"role": "assistant"}
		if messageContent != "" {
			delta["content"] = messageContent
		}
		if len(toolCalls) > 0 {
			delta["tool_calls"] = indexedToolCalls(toolCalls)
		}
		finish := "stop"
		if len(toolCalls) > 0 {
			finish = "tool_calls"
		}
		usage := usageFor(run.prepared.Prompt, contentBuf.String()+reasoningBuf.String())
		if err := writeSSEChunkWithUsage(w, meta, delta, finish, usage); err != nil {
			return err
		}
		return writeDone(w)
	}

	usage := usageFor(run.prepared.Prompt, contentBuf.String()+reasoningBuf.String())
	if err := writeSSEChunkWithUsage(w, meta, map[string]any{"role": "assistant"}, "stop", usage); err != nil {
		return err
	}
	return writeDone(w)
}

type streamMeta struct {
	ID      string
	Created int64
	Model   string
}

func (m streamMeta) base() map[string]any {
	return map[string]any{
		"id":      m.ID,
		"object":  "chat.completion.chunk",
		"created": m.Created,
		"model":   m.Model,
	}
}

func writeSSEChunk(w io.Writer, meta streamMeta, delta map[string]any, finish string) error {
	return writeSSEChunkWithUsage(w, meta, delta, finish, nil)
}

func writeSSEChunkWithUsage(w io.Writer, meta streamMeta, delta map[string]any, finish string, usage map[string]any) error {
	chunk := meta.base()
	chunk["choices"] = []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finishOrNull(finish)}}
	if usage != nil {
		chunk["usage"] = usage
	}
	encoded, err := json.Marshal(chunk)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, "data: "+string(encoded)+"\n\n")
	return err
}

func writeSSEError(w io.Writer, err error) {
	payload := map[string]any{"error": map[string]any{"message": err.Error(), "type": "upstream_error", "code": "upstream_error"}}
	encoded, _ := json.Marshal(payload)
	_, _ = io.WriteString(w, "data: "+string(encoded)+"\n\n")
}

func writeDone(w io.Writer) error {
	_, err := io.WriteString(w, "data: [DONE]\n\n")
	return err
}

func finishOrNull(finish string) any {
	if finish == "" {
		return nil
	}
	return finish
}

func indexedToolCalls(raw json.RawMessage) []any {
	var calls []any
	if json.Unmarshal(raw, &calls) != nil {
		return nil
	}
	for index, call := range calls {
		if object, ok := call.(map[string]any); ok {
			object["index"] = index
		}
	}
	return calls
}

func usageFor(prompt, output string) map[string]any {
	promptTokens := estimateTokens(prompt)
	completionTokens := estimateTokens(output)
	return map[string]any{
		"prompt_tokens":     promptTokens,
		"completion_tokens": completionTokens,
		"total_tokens":      promptTokens + completionTokens,
	}
}

// isCaptchaError reports whether the error is a manual-verification block.
func isCaptchaError(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	return strings.Contains(text, "人工验证") || strings.Contains(text, "captcha") || strings.Contains(text, "710022004")
}

// estimateTokens approximates tokens: CJK runes ~1 token each, otherwise 4
// chars/token. Only for a plausible usage magnitude, never billing.
func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	cjk := 0
	total := 0
	for _, r := range text {
		total++
		if (r >= 0x3040 && r <= 0x30ff) || (r >= 0x3400 && r <= 0x9fff) ||
			(r >= 0xac00 && r <= 0xd7af) || (r >= 0xf900 && r <= 0xfaff) ||
			(r >= 0xff00 && r <= 0xffef) {
			cjk++
		}
	}
	nonCJK := total - cjk
	return cjk + (nonCJK+3)/4
}
