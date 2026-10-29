package codearts

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// upstreamError is a business error embedded in an HTTP 200 SSE frame.
type upstreamError struct {
	Code string
	Msg  string
}

func (e *upstreamError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("codearts error code=%s msg=%s", e.Code, e.Msg)
	}
	return "codearts error: " + e.Msg
}

// rawCompletion is the aggregated non-stream result.
type rawCompletion struct {
	Content   string
	Reasoning string
	Finish    string
	ToolCalls []map[string]any
	Usage     map[string]any
}

// scanLine parses one SSE line. CodeArts usually sends one data: line per
// event without a blank separator; both that and the standard form are
// accepted.
func scanLine(line string, pendingEvent *string) (event, data string, ok bool) {
	line = strings.TrimRight(line, "\r")
	switch {
	case strings.HasPrefix(line, "event:"):
		*pendingEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		return "", "", false
	case strings.HasPrefix(line, "data:"):
		value := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if value == "" {
			return "", "", false
		}
		event = *pendingEvent
		*pendingEvent = ""
		return event, value, true
	case strings.HasPrefix(line, ":"), line == "":
		*pendingEvent = ""
		return "", "", false
	}
	return "", "", false
}

// deltaFromChunk extracts a standard OpenAI delta.
func deltaFromChunk(payload map[string]any) (delta map[string]any, finishReason string) {
	if direct, ok := payload["delta"].(map[string]any); ok {
		return direct, ""
	}
	choices, ok := payload["choices"].([]any)
	if !ok || len(choices) == 0 {
		return nil, ""
	}
	return choiceDelta(choices[0])
}

func choiceDelta(raw any) (map[string]any, string) {
	choice, ok := raw.(map[string]any)
	if !ok {
		return nil, ""
	}
	finish, _ := choice["finish_reason"].(string)
	delta, _ := choice["delta"].(map[string]any)
	return delta, finish
}

// applyToolCallDeltas merges streamed tool-call fragments by index.
func applyToolCallDeltas(delta map[string]any, calls map[int]map[string]any) {
	rawCalls, ok := delta["tool_calls"].([]any)
	if !ok {
		return
	}
	for position, raw := range rawCalls {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		index := position
		switch value := entry["index"].(type) {
		case float64:
			index = int(value)
		case json.Number:
			if parsed, err := value.Int64(); err == nil {
				index = int(parsed)
			}
		}
		call := calls[index]
		if call == nil {
			call = map[string]any{"index": index, "type": "function"}
			calls[index] = call
		}
		if id, ok := entry["id"].(string); ok && id != "" {
			call["id"] = id
		}
		if typ, ok := entry["type"].(string); ok && typ != "" {
			call["type"] = typ
		}
		if fn, ok := entry["function"].(map[string]any); ok {
			current, _ := call["function"].(map[string]any)
			if current == nil {
				current = map[string]any{"name": "", "arguments": ""}
				call["function"] = current
			}
			if name, ok := fn["name"].(string); ok {
				current["name"] = current["name"].(string) + name
			}
			if arguments, ok := fn["arguments"].(string); ok {
				current["arguments"] = current["arguments"].(string) + arguments
			}
		}
	}
}

func sortedToolCalls(calls map[int]map[string]any) []map[string]any {
	indexes := make([]int, 0, len(calls))
	for index := range calls {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	out := make([]map[string]any, 0, len(indexes))
	for _, index := range indexes {
		out = append(out, calls[index])
	}
	return out
}

// applyEvent updates the aggregate from one SSE frame.
func applyEvent(content, reasoning *strings.Builder, finish *string, upErr *error, event, data string) {
	event = strings.ToLower(strings.TrimSpace(event))
	var payload map[string]any
	if strings.TrimSpace(data) != "" {
		_ = json.Unmarshal([]byte(data), &payload)
	}
	text := func(keys ...string) string {
		for _, key := range keys {
			if value, ok := payload[key].(string); ok {
				return value
			}
		}
		return ""
	}
	code := text("error_code", "code")
	if code != "" && code != "0" && code != "0000" {
		*upErr = &upstreamError{Code: code, Msg: firstNonEmpty(text("error_msg", "message", "msg"), "request failed")}
		return
	}
	switch event {
	case "", "data", "onanswer", "answer", "delta", "message", "content":
		if strings.TrimSpace(data) == "[DONE]" {
			if *finish == "" {
				*finish = "stop"
			}
			return
		}
		delta, finishReason := deltaFromChunk(payload)
		if finishReason != "" {
			*finish = finishReason
		}
		if delta != nil {
			if value, ok := delta["content"].(string); ok && value != "" {
				content.WriteString(value)
			}
			if value, ok := delta["reasoning_content"].(string); ok && value != "" {
				reasoning.WriteString(value)
			}
			return
		}
		if t := text("text", "content", "delta"); t != "" {
			if strings.EqualFold(t, "done") || t == "[DONE]" {
				*finish = "stop"
				return
			}
			// CodeArts' text field is a full snapshot, not a delta.
			content.Reset()
			content.WriteString(t)
			return
		}
		if out, ok := payload["output"].([]any); ok && len(out) > 0 {
			var sb strings.Builder
			for _, item := range out {
				if object, ok := item.(map[string]any); ok && object["type"] == "output_text" {
					if value, ok := object["text"].(string); ok {
						sb.WriteString(value)
					}
				}
			}
			if sb.Len() > 0 {
				content.Reset()
				content.WriteString(sb.String())
			}
		}
	case "reasoning", "thinking", "onreasoning", "onthinking":
		if value := text("text", "reasoning", "content", "thinking"); value != "" {
			reasoning.WriteString(value)
		}
	case "done", "end", "finish":
		if value := text("finish_reason", "finish", "reason"); value != "" {
			*finish = value
		} else if *finish == "" {
			*finish = "stop"
		}
	}
}

// aggregateRaw reads a complete SSE stream and returns text, reasoning, tool
// calls and usage. It is the non-stream path.
func aggregateRaw(reader io.Reader) (*rawCompletion, error) {
	buffered := bufio.NewReaderSize(reader, 64*1024)
	content := &strings.Builder{}
	reasoning := &strings.Builder{}
	finish := "stop"
	calls := map[int]map[string]any{}
	var usage map[string]any
	var streamErr error
	var pendingEvent string
	for {
		line, err := buffered.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, err
		}
		if event, data, ok := scanLine(strings.TrimRight(line, "\r\n"), &pendingEvent); ok {
			var payload map[string]any
			if json.Unmarshal([]byte(data), &payload) == nil {
				if value, ok := payload["usage"].(map[string]any); ok {
					usage = value
				}
				delta, _ := deltaFromChunk(payload)
				applyToolCallDeltas(delta, calls)
			}
			applyEvent(content, reasoning, &finish, &streamErr, event, data)
		}
		if err == io.EOF {
			break
		}
	}
	if streamErr != nil {
		return nil, streamErr
	}
	return &rawCompletion{
		Content:   content.String(),
		Reasoning: reasoning.String(),
		Finish:    finish,
		ToolCalls: sortedToolCalls(calls),
		Usage:     usage,
	}, nil
}

// streamCapture rewrites upstream SSE to standard chat-completions chunks.
func streamCapture(writer io.Writer, reader io.Reader, model string) error {
	buffered := bufio.NewReaderSize(reader, 64*1024)
	id := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	content := &strings.Builder{}
	reasoning := &strings.Builder{}
	finish := "stop"
	calls := map[int]map[string]any{}
	var usage map[string]any
	var pendingEvent string
	sawDone := false
	var streamErr error

	write := func(payload map[string]any) error {
		raw, _ := json.Marshal(payload)
		if _, err := io.WriteString(writer, "data: "+string(raw)+"\n\n"); err != nil {
			return err
		}
		return nil
	}
	writeChunk := func(delta map[string]any, finishReason string) error {
		choice := map[string]any{"index": 0, "delta": delta}
		if finishReason != "" {
			choice["finish_reason"] = finishReason
		}
		return write(map[string]any{
			"id": id, "object": "chat.completion.chunk", "created": time.Now().Unix(),
			"model": model, "choices": []any{choice},
		})
	}
	writeDONE := func() error {
		if usage != nil {
			if err := write(map[string]any{
				"id": id, "object": "chat.completion.chunk", "created": time.Now().Unix(),
				"model": model, "choices": []any{}, "usage": usage,
			}); err != nil {
				return err
			}
		}
		_, err := io.WriteString(writer, "data: [DONE]\n\n")
		return err
	}
	writeError := func(message string) error {
		raw, _ := json.Marshal(map[string]any{"error": map[string]any{
			"message": message, "type": "upstream_error", "code": "CODEARTS_STREAM_ERROR",
		}})
		_, err := io.WriteString(writer, "event: error\ndata: "+string(raw)+"\n\n")
		return err
	}

	for {
		line, err := buffered.ReadString('\n')
		if err != nil && err != io.EOF {
			streamErr = err
			break
		}
		if event, data, ok := scanLine(strings.TrimRight(line, "\r\n"), &pendingEvent); ok {
			if strings.TrimSpace(data) == "[DONE]" {
				if !sawDone {
					if err := writeDONE(); err != nil {
						streamErr = err
						break
					}
					sawDone = true
				}
				break
			}
			var payload map[string]any
			_ = json.Unmarshal([]byte(data), &payload)
			if value, ok := payload["usage"].(map[string]any); ok {
				usage = value
			}
			nativeDelta, nativeFinish := deltaFromChunk(payload)
			applyToolCallDeltas(nativeDelta, calls)
			beforeContent, beforeReason := content.Len(), reasoning.Len()
			var upErr error
			applyEvent(content, reasoning, &finish, &upErr, event, data)
			if upErr != nil {
				if err := writeError(upErr.Error()); err != nil {
					streamErr = err
					break
				}
				_ = writeDONE()
				sawDone = true
				streamErr = upErr
				break
			}
			delta := map[string]any{}
			if role, ok := nativeDelta["role"].(string); ok && role != "" {
				delta["role"] = role
			}
			if content.Len() > beforeContent {
				delta["content"] = content.String()[beforeContent:]
			}
			if reasoning.Len() > beforeReason {
				delta["reasoning_content"] = reasoning.String()[beforeReason:]
			}
			if toolCalls, ok := nativeDelta["tool_calls"]; ok {
				delta["tool_calls"] = toolCalls
			}
			if len(delta) > 0 || nativeFinish != "" {
				if err := writeChunk(delta, nativeFinish); err != nil {
					streamErr = err
					break
				}
			}
			if strings.EqualFold(event, "done") || strings.EqualFold(event, "end") || strings.EqualFold(event, "finish") {
				if nativeFinish == "" {
					if err := writeChunk(map[string]any{}, finish); err != nil {
						streamErr = err
						break
					}
				}
				if err := writeDONE(); err != nil {
					streamErr = err
					break
				}
				sawDone = true
			}
		}
		if sawDone {
			break
		}
		if err == io.EOF {
			break
		}
	}
	if streamErr == nil && !sawDone {
		streamErr = writeDONE()
	}
	return streamErr
}

// preflightWithTimeout bounds the wait for the first SSE frame. A stuck
// upstream must surface as ErrFirstByteTimeout, not as a context deadline, so
// the executor fails over instead of treating it as a client cancellation.
func preflightWithTimeout(resp *http.Response) (io.ReadCloser, error) {
	type result struct {
		reader io.ReadCloser
		err    error
	}
	done := make(chan result, 1)
	go func() {
		reader, err := preflightSSE(resp.Body)
		done <- result{reader: reader, err: err}
	}()
	select {
	case outcome := <-done:
		return outcome.reader, outcome.err
	case <-time.After(firstByteTimeout):
		_ = resp.Body.Close()
		return nil, providers.ErrFirstByteTimeout
	}
}

// preflightSSE waits for the first data frame so a 200 with an embedded
// business error is returned as a provider error before the gateway commits a
// 200 response. It also gives ChatStream a place to enforce TTFB.
func preflightSSE(body io.ReadCloser) (io.ReadCloser, error) {
	buffered := bufio.NewReaderSize(body, 1<<20)
	var prefix bytes.Buffer
	for prefix.Len() < 2<<20 {
		line, err := buffered.ReadString('\n')
		prefix.WriteString(line)
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
			if data != "" && data != "[DONE]" {
				var payload map[string]any
				if json.Unmarshal([]byte(data), &payload) == nil {
					if code, _ := payload["error_code"].(string); code != "" && code != "0" && code != "0000" {
						_ = body.Close()
						return nil, newProviderError(http.StatusBadGateway, data)
					}
				}
			}
			return &replayReadCloser{Reader: io.MultiReader(bytes.NewReader(prefix.Bytes()), buffered), Closer: body}, nil
		}
		if err != nil {
			if err != io.EOF {
				_ = body.Close()
				return nil, err
			}
			return &replayReadCloser{Reader: bytes.NewReader(prefix.Bytes()), Closer: body}, nil
		}
	}
	return &replayReadCloser{Reader: io.MultiReader(bytes.NewReader(prefix.Bytes()), buffered), Closer: body}, nil
}

type replayReadCloser struct {
	io.Reader
	io.Closer
}
