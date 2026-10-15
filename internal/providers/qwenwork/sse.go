package qwenwork

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

// The QwenWork gateway wraps every OpenAI chunk in an envelope frame:
//
//	data:{"headers":{},"body":"{\"choices\":…}","statusCodeValue":200}
//
// Errors are signaled by statusCodeValue >= 400 or by a body-level 4xx code
// (the desktop convention, e.g. {"code":"401","message":"…"}). The wrapper is
// stripped here; the gateway relays plain OpenAI chat-completion chunks.

// upstreamFrame is one unwrapped upstream event.
type upstreamFrame struct {
	role      string
	content   string
	reasoning string
	tools     json.RawMessage
	finish    string
	usage     json.RawMessage
	errText   string
	errStatus int
	errKind   string
	errCode   string
}

// errorOnly reports whether the frame is an error with no payload alongside.
func (f upstreamFrame) errorOnly() bool {
	return f.errText != "" && f.content == "" && f.reasoning == "" && len(f.tools) == 0
}

// providerError renders the frame's error for the executor.
func (f upstreamFrame) providerError() *providers.Error {
	kind := f.errKind
	if kind == "" {
		kind = accounts.KindUnavailable
	}
	status := f.errStatus
	if status == 0 {
		status = http.StatusBadGateway
	}
	return &providers.Error{Kind: kind, Status: status, Code: f.errCode, Message: f.errText}
}

// parseUpstreamLine decodes one SSE data payload into the inner OpenAI chunk
// it wraps. ok is false for blanks, [DONE], heartbeat frames, and empty
// bodies. A frame status of 400+ survives the unwrap so the walker can
// classify the frame as an error.
func parseUpstreamLine(data string) (body map[string]any, frameStatus int, ok bool) {
	var outer map[string]any
	if json.Unmarshal([]byte(data), &outer) != nil {
		return nil, 0, false
	}
	if status, isNumber := numberField(outer["statusCodeValue"]); isNumber {
		frameStatus = status
	}
	body = outer
	if nested, present := outer["body"]; present {
		switch typed := nested.(type) {
		case string:
			trimmed := strings.TrimSpace(typed)
			if trimmed == "" || trimmed == "[DONE]" || trimmed == "null" || trimmed == "{}" {
				return nil, 0, false
			}
			var inner map[string]any
			if json.Unmarshal([]byte(trimmed), &inner) == nil {
				if len(inner) == 0 {
					return nil, 0, false
				}
				body = inner
			} else if frameStatus >= 400 || errorMarkerText(trimmed) {
				// Bare text body carries meaning only as an error message.
				body = map[string]any{"message": trimmed}
			} else {
				return nil, 0, false
			}
		case map[string]any:
			if len(typed) == 0 {
				return nil, 0, false
			}
			body = typed
		}
	}
	return body, frameStatus, true
}

func numberField(value any) (int, bool) {
	number, ok := value.(float64)
	if !ok {
		return 0, false
	}
	return int(number), true
}

// walkUpstream walks the wrapped SSE body and calls fn with each unwrapped
// chunk plus the frame status it arrived with.
func walkUpstream(r io.Reader, fn func(body map[string]any, frameStatus int) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		body, frameStatus, ok := parseUpstreamLine(data)
		if !ok {
			continue
		}
		if err := fn(body, frameStatus); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// upstreamError extracts the error one frame carries: an OpenAI-style error
// object, a wrapper statusCodeValue >= 400, or a body-level 4xx code with a
// message. Quota code 14018 and "Model is not available for this user" are
// classified through the shared taxonomy.
func upstreamError(body map[string]any, frameStatus int) (text string, status int, kind, code string) {
	if len(body) == 0 {
		return "", 0, "", ""
	}
	if raw, ok := body["error"]; ok && raw != nil {
		encoded, _ := json.Marshal(raw)
		var parsed struct {
			Message string `json:"message"`
			Code    any    `json:"code"`
		}
		_ = json.Unmarshal(encoded, &parsed)
		text = strings.TrimSpace(parsed.Message)
		if text == "" {
			text = strings.TrimSpace(string(encoded))
		}
		status = firstStatus(frameStatus, http.StatusBadGateway)
		code = scalarCode(parsed.Code)
		return text, status, classifiedKind(status, text, code), code
	}
	code = scalarCode(body["code"])
	message, _ := body["message"].(string)
	message = strings.TrimSpace(message)
	if frameStatus < 400 && !errorCodeString(code) && code != quotaCode && !errorMarkerText(message) {
		return "", 0, "", ""
	}
	status = frameStatus
	if status < 400 {
		if code == quotaCode {
			status = http.StatusPaymentRequired
		} else if n, err := strconv.Atoi(code); err == nil && n >= 400 && n < 600 {
			status = n
		} else {
			status = http.StatusBadGateway
		}
	}
	if message == "" {
		message = "qwenwork upstream error"
		if code != "" {
			message += " code=" + code
		}
	}
	return message, status, classifiedKind(status, message, code), code
}

// errorCodeString reports whether a body-level code marks the frame as an
// error regardless of the wrapper status.
func errorCodeString(code string) bool {
	switch code {
	case "400", "401", "402", "403", "429":
		return true
	}
	return false
}

// errorMarkerText reports whether a bare frame message is one of the known
// error texts that can ride inside a 200-class wrapper.
func errorMarkerText(message string) bool {
	if message == "" {
		return false
	}
	lower := strings.ToLower(message)
	return strings.Contains(lower, "model is not available") || quotaLike(lower)
}

// classifiedKind maps an extracted frame error onto the taxonomy; an empty
// kind means "no classification", which callers treat as unavailable.
func classifiedKind(status int, message, code string) string {
	kind := classifyKind(status, message, code)
	if kind == "" {
		return accounts.KindUnavailable
	}
	return kind
}

func frameFromBody(body map[string]any, frameStatus int) upstreamFrame {
	var frame upstreamFrame
	if errText, status, kind, code := upstreamError(body, frameStatus); errText != "" {
		frame.errText = errText
		frame.errStatus = status
		frame.errKind = kind
		frame.errCode = code
	}
	if usage, ok := body["usage"]; ok && usage != nil {
		raw, _ := json.Marshal(usage)
		frame.usage = raw
	}
	choices, _ := body["choices"].([]any)
	if len(choices) == 0 {
		return frame
	}
	choice, _ := choices[0].(map[string]any)
	if choice == nil {
		return frame
	}
	if finish, _ := choice["finish_reason"].(string); finish != "" {
		frame.finish = finish
	}
	delta, _ := choice["delta"].(map[string]any)
	message, _ := choice["message"].(map[string]any)
	part := delta
	if part == nil {
		part = message
	}
	if part == nil {
		return frame
	}
	if role, _ := part["role"].(string); role != "" {
		frame.role = role
	}
	if text, _ := part["content"].(string); text != "" {
		frame.content = text
	}
	if text, _ := part["reasoning_content"].(string); text != "" {
		frame.reasoning = text
	}
	if tools := part["tool_calls"]; tools != nil {
		raw, _ := json.Marshal(tools)
		frame.tools = raw
	}
	return frame
}

func deltaOf(frame upstreamFrame) map[string]any {
	delta := map[string]any{}
	if frame.role != "" {
		delta["role"] = frame.role
	}
	if frame.content != "" {
		delta["content"] = frame.content
	}
	if frame.reasoning != "" {
		delta["reasoning_content"] = frame.reasoning
	}
	if len(frame.tools) > 0 {
		var tools any
		if json.Unmarshal(frame.tools, &tools) == nil {
			delta["tool_calls"] = tools
		}
	}
	return delta
}

func emptyOrNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// collectUpstream folds a full wrapped SSE body into one frame for the
// non-stream path. Tool-call deltas are merged by index because the upstream
// fragments one call across many chunks.
func collectUpstream(r io.Reader) (upstreamFrame, error) {
	var frame upstreamFrame
	var content, reasoning strings.Builder
	toolCalls := map[int]*mergedToolCall{}
	var toolOrder []int
	err := walkUpstream(r, func(body map[string]any, frameStatus int) error {
		part := frameFromBody(body, frameStatus)
		if part.errText != "" {
			frame.errText, frame.errStatus, frame.errKind, frame.errCode = part.errText, part.errStatus, part.errKind, part.errCode
		}
		if part.usage != nil {
			frame.usage = part.usage
		}
		if part.finish != "" {
			frame.finish = part.finish
		}
		content.WriteString(part.content)
		reasoning.WriteString(part.reasoning)
		if len(part.tools) > 0 {
			toolOrder = mergeToolCallDeltas(part.tools, toolCalls, toolOrder)
		}
		return nil
	})
	frame.content = content.String()
	frame.reasoning = reasoning.String()
	frame.tools = renderToolCalls(toolOrder, toolCalls)
	return frame, err
}

// mergedToolCall accumulates one fragmented tool-call delta.
type mergedToolCall struct {
	index int
	id    string
	typ   string
	name  strings.Builder
	args  strings.Builder
}

// mergeToolCallDeltas folds one tool_calls delta array into the merge map and
// returns the (possibly extended) index order.
func mergeToolCallDeltas(raw json.RawMessage, merged map[int]*mergedToolCall, order []int) []int {
	var calls []struct {
		Index    *int   `json:"index"`
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	if json.Unmarshal(raw, &calls) != nil {
		return order
	}
	for _, call := range calls {
		index := 0
		if call.Index != nil {
			index = *call.Index
		}
		entry, ok := merged[index]
		if !ok {
			entry = &mergedToolCall{index: index}
			merged[index] = entry
			order = append(order, index)
		}
		if entry.id == "" {
			entry.id = call.ID
		}
		if entry.typ == "" {
			entry.typ = call.Type
		}
		entry.name.WriteString(call.Function.Name)
		entry.args.WriteString(call.Function.Arguments)
	}
	return order
}

// renderToolCalls rebuilds the merged calls in index order. A nil result means
// the stream carried no tool calls.
func renderToolCalls(order []int, merged map[int]*mergedToolCall) json.RawMessage {
	if len(order) == 0 {
		return nil
	}
	sorted := append([]int(nil), order...)
	sort.Ints(sorted)
	out := make([]map[string]any, 0, len(sorted))
	for _, index := range sorted {
		entry := merged[index]
		call := map[string]any{"index": index}
		if entry.id != "" {
			call["id"] = entry.id
		}
		if entry.typ != "" {
			call["type"] = entry.typ
		}
		function := map[string]any{}
		if entry.name.Len() > 0 {
			function["name"] = entry.name.String()
		}
		if entry.args.Len() > 0 {
			function["arguments"] = entry.args.String()
		}
		call["function"] = function
		out = append(out, call)
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return encoded
}

// outcomeFromUpstreamSSE aggregates a wrapped upstream SSE body into the
// provider-neutral outcome. An error frame anywhere in the stream is the
// upstream's verdict, so it wins over partial content — matching the JS
// reference's collectSync. A body without content, reasoning, tools, a
// terminal finish_reason, or an error is a silent upstream reject.
func outcomeFromUpstreamSSE(model string, raw []byte) (providers.ChatOutcome, error) {
	frame, err := collectUpstream(bytes.NewReader(raw))
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	if frame.errText != "" {
		return providers.ChatOutcome{}, frame.providerError()
	}
	if frame.finish == "" && frame.content == "" && frame.reasoning == "" && len(frame.tools) == 0 {
		return providers.ChatOutcome{}, emptyUpstreamError()
	}
	out := providers.ChatOutcome{
		Model:        model,
		Content:      frame.content,
		Reasoning:    frame.reasoning,
		ToolCalls:    frame.tools,
		FinishReason: frame.finish,
		UsageSource:  "upstream",
	}
	if out.FinishReason == "" {
		if len(out.ToolCalls) > 0 {
			out.FinishReason = "tool_calls"
		} else {
			out.FinishReason = "stop"
		}
	}
	applyUsage(&out, frame.usage)
	return out, nil
}

func applyUsage(out *providers.ChatOutcome, raw json.RawMessage) {
	if len(raw) == 0 {
		out.UsageSource = "estimate"
		return
	}
	var usage struct {
		PromptTokens     int      `json:"prompt_tokens"`
		CompletionTokens int      `json:"completion_tokens"`
		CacheReadTokens  *int     `json:"cache_read_tokens"`
		CacheWriteTokens *int     `json:"cache_write_tokens"`
		Credits          *float64 `json:"credits"`
	}
	if json.Unmarshal(raw, &usage) != nil {
		out.UsageSource = "estimate"
		return
	}
	out.PromptTokens = usage.PromptTokens
	out.CompletionTokens = usage.CompletionTokens
	out.CacheReadTokens = usage.CacheReadTokens
	out.CacheWriteTokens = usage.CacheWriteTokens
	out.Credits = usage.Credits
}

// rewriteUpstreamSSE pipes the wrapped upstream body through an OpenAI
// chat-completion chunk rewriter.
func rewriteUpstreamSSE(resp *http.Response, model string) *http.Response {
	pr, pw := io.Pipe()
	go func() {
		defer resp.Body.Close()
		err := writeOpenAIChunks(pw, resp.Body, model)
		_ = pw.CloseWithError(err)
	}()
	headers := resp.Header.Clone()
	headers.Set("Content-Type", "text/event-stream")
	headers.Del("Content-Length")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     headers,
		Body:       io.NopCloser(pr),
		Request:    resp.Request,
	}
}

func writeOpenAIChunks(w io.Writer, src io.Reader, model string) error {
	id := "chatcmpl-" + strings.ReplaceAll(newID(), "-", "")
	created := time.Now().Unix()
	writeChunk := func(delta map[string]any, finish any, usage json.RawMessage) error {
		chunk := map[string]any{
			"id": id, "object": "chat.completion.chunk", "created": created, "model": model,
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
		}
		if len(usage) > 0 {
			var decoded any
			if json.Unmarshal(usage, &decoded) == nil {
				chunk["usage"] = decoded
			}
		}
		payload, err := json.Marshal(chunk)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "data: %s\n\n", payload)
		return err
	}
	var sawFinish, sawTools, sawPayload, sawError bool
	err := walkUpstream(src, func(body map[string]any, frameStatus int) error {
		frame := frameFromBody(body, frameStatus)
		if frame.finish != "" {
			sawFinish = true
		}
		if frame.content != "" || frame.reasoning != "" || len(frame.tools) > 0 || frame.usage != nil {
			sawPayload = true
		}
		if len(frame.tools) > 0 {
			sawTools = true
		}
		if !frame.errorOnly() {
			if err := writeChunk(deltaOf(frame), emptyOrNil(frame.finish), frame.usage); err != nil {
				return err
			}
		}
		if frame.errText != "" {
			// The error object follows any payload the same frame carried, so
			// partial content is never dropped and the error is never hidden.
			sawError = true
			return writeErrorChunk(w, frame.providerError())
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !sawFinish {
		if sawPayload && !sawError {
			// Upstream streamed real content but was cut before the terminal
			// finish_reason. The bytes are already on the wire, so salvage the
			// completion with a synthesized terminal chunk instead of leaving
			// strict OpenAI clients with "Stream ended without finish_reason".
			// After an error chunk the error object is the terminal event, so
			// no synthetic finish is appended.
			finish := "stop"
			if sawTools {
				finish = "tool_calls"
			}
			if err := writeChunk(map[string]any{}, finish, nil); err != nil {
				return err
			}
		} else if !sawError {
			// Empty upstream stream that slipped past the pre-relay guard:
			// surface it as an error frame so the gateway classifies and logs
			// the failure instead of reporting a successful empty answer.
			if err := writeErrorChunk(w, emptyUpstreamError()); err != nil {
				return err
			}
		}
	}
	_, err = io.WriteString(w, "data: [DONE]\n\n")
	return err
}

// writeErrorChunk relays a classified error as an OpenAI error object. code,
// kind, and status ride along so the gateway's stream classifier keeps the
// provider verdict (quota 14018, auth, model-level) instead of re-guessing
// from the message text.
func writeErrorChunk(w io.Writer, providerErr *providers.Error) error {
	code := providerErr.Code
	if code == "" {
		code = providerErr.Kind
	}
	payload, err := json.Marshal(map[string]any{"error": map[string]any{
		"message": providerErr.Message,
		"type":    "api_error",
		"code":    code,
		"kind":    providerErr.Kind,
		"status":  providerErr.Status,
	}})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", payload)
	return err
}

// emptyUpstreamError mirrors the worker path's upstream_empty classification.
// The executor cools the account down and fails over instead of relaying a
// spec-invalid completion (no content, no finish_reason) that strict OpenAI
// clients reject with "Stream ended without finish_reason".
func emptyUpstreamError() *providers.Error {
	return &providers.Error{
		Kind:    accounts.KindUnavailable,
		Status:  http.StatusBadGateway,
		Code:    "upstream_empty",
		Message: "upstream returned empty content (possible context overflow / silent reject)",
	}
}

// ensureMeaningfulUpstream peeks the upstream SSE body before anything is
// relayed to the client, so an empty or error-only upstream can still fail
// over to another account. On success the buffered bytes are replayed ahead
// of the remaining body, preserving real-time streaming.
func ensureMeaningfulUpstream(resp *http.Response) (*http.Response, error) {
	br := bufio.NewReader(resp.Body)
	var pending bytes.Buffer
	for {
		line, readErr := br.ReadString('\n')
		if line != "" {
			pending.WriteString(line)
			if data, ok := strings.CutPrefix(strings.TrimSpace(line), "data:"); ok {
				if frame, meaningful := meaningfulDataLine(strings.TrimSpace(data)); meaningful {
					if frame.errorOnly() {
						// Nothing has reached the client yet: return the
						// classified error so the executor cools this account
						// (quota, auth, model) and fails over.
						resp.Body.Close()
						return nil, frame.providerError()
					}
					resp.Body = peekedBody{Reader: io.MultiReader(&pending, br), Closer: resp.Body}
					return resp, nil
				}
			}
		}
		if readErr != nil {
			resp.Body.Close()
			if errors.Is(readErr, io.EOF) {
				return nil, emptyUpstreamError()
			}
			return nil, &providers.Error{
				Kind:    accounts.KindUnavailable,
				Status:  http.StatusBadGateway,
				Code:    "upstream_stream_interrupted",
				Message: readErr.Error(),
			}
		}
	}
}

// peekedBody keeps the original body's Close wired to the upstream response
// while prepending the peeked bytes.
type peekedBody struct {
	io.Reader
	io.Closer
}

// meaningfulDataLine reports whether a single upstream SSE data line carries
// any payload a relayed completion needs: content, reasoning, tool calls, a
// terminal finish_reason, usage, or an error. Heartbeat and empty-delta
// frames are not meaningful.
func meaningfulDataLine(payload string) (upstreamFrame, bool) {
	if payload == "" || payload == "[DONE]" {
		return upstreamFrame{}, false
	}
	body, frameStatus, ok := parseUpstreamLine(payload)
	if !ok {
		return upstreamFrame{}, false
	}
	frame := frameFromBody(body, frameStatus)
	return frame, frame.content != "" || frame.reasoning != "" || len(frame.tools) > 0 || frame.finish != "" || frame.usage != nil || frame.errText != ""
}
