package command

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// rewriteStream converts the /alpha/generate NDJSON body into an
// OpenAI-compatible SSE stream. It returns a synthetic http.Response whose body
// is an io.Pipe, matching the other in-process adapters: the gateway relays the
// body verbatim.
func rewriteStream(upstream *http.Response, model string) *http.Response {
	pr, pw := io.Pipe()
	go func() {
		defer upstream.Body.Close()
		defer pw.Close()

		writer := &sseWriter{
			pw:         pw,
			id:         fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
			created:    time.Now().Unix(),
			model:      model,
			toolIndex:  map[string]int{},
			deltasSeen: map[string]bool{},
		}
		err := streamEvents(upstream.Body, writer.handle)
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		if err := writer.finish(); err != nil {
			_ = pw.CloseWithError(err)
		}
	}()

	header := make(http.Header)
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       pr,
	}
}

type sseWriter struct {
	pw      *io.PipeWriter
	id      string
	created int64
	model   string

	roleSent         bool
	sawTerminalEvent bool
	finishReason     string
	hasToolCalls     bool
	toolIndex        map[string]int
	deltasSeen       map[string]bool
	usage            map[string]any
}

func (w *sseWriter) handle(event generateEvent) error {
	switch event.Type {
	case "reasoning-delta":
		return w.writeDelta(map[string]any{"reasoning_content": event.Text})
	case "text-delta":
		return w.writeDelta(map[string]any{"content": event.Text})
	case "tool-input-start":
		w.hasToolCalls = true
		return w.writeToolStart(event)
	case "tool-input-delta":
		w.hasToolCalls = true
		return w.writeToolDelta(event)
	case "tool-call":
		w.hasToolCalls = true
		return w.writeToolCall(event)
	case "finish-step", "finish":
		w.sawTerminalEvent = true
		if event.FinishReason != "" {
			w.finishReason = event.FinishReason
		}
		// finish-step carries `usage`; finish carries `totalUsage`. They are
		// two terminal events in the same stream, so a nil from the second must
		// not clobber the first's numbers.
		if payload := usagePayload(event.usage()); payload != nil {
			w.usage = payload
		}
		return nil
	case "error":
		return errorFromEvent(event)
	default:
		return nil
	}
}

func (w *sseWriter) writeToolStart(event generateEvent) error {
	id := event.toolEventID()
	if id == "" {
		return nil
	}
	if _, ok := w.toolIndex[id]; !ok {
		w.toolIndex[id] = len(w.toolIndex)
	}
	delta := map[string]any{
		"tool_calls": []any{map[string]any{
			"index": w.toolIndex[id],
			"id":    id,
			"type":  "function",
			"function": map[string]any{
				"name":      event.toolEventName(),
				"arguments": "",
			},
		}},
	}
	return w.writeDelta(delta)
}

func (w *sseWriter) writeToolDelta(event generateEvent) error {
	id := event.toolEventID()
	if id == "" {
		return nil
	}
	index, ok := w.toolIndex[id]
	if !ok {
		index = len(w.toolIndex)
		w.toolIndex[id] = index
	}
	w.deltasSeen[id] = true
	delta := map[string]any{
		"tool_calls": []any{map[string]any{
			"index": index,
			"function": map[string]any{
				"arguments": event.Delta,
			},
		}},
	}
	return w.writeDelta(delta)
}

func (w *sseWriter) writeToolCall(event generateEvent) error {
	id := event.toolEventID()
	if id == "" {
		return nil
	}
	index, ok := w.toolIndex[id]
	if !ok {
		index = len(w.toolIndex)
		w.toolIndex[id] = index
	}
	// The final tool-call event is redundant with the preceding deltas: when
	// deltas already streamed the full arguments, re-emitting them would double
	// the payload. Only use the full input to backfill a tool call whose start
	// never arrived.
	if w.deltasSeen[id] {
		return nil
	}
	full := fullToolInput(event)
	delta := map[string]any{
		"tool_calls": []any{map[string]any{
			"index": index,
			"id":    id,
			"type":  "function",
			"function": map[string]any{
				"name":      event.toolEventName(),
				"arguments": firstNonEmpty(full, "{}"),
			},
		}},
	}
	return w.writeDelta(delta)
}

func (w *sseWriter) writeDelta(delta map[string]any) error {
	if !w.roleSent {
		w.roleSent = true
		if err := w.writeChunk(map[string]any{"role": "assistant"}, "", nil); err != nil {
			return err
		}
	}
	return w.writeChunk(delta, "", nil)
}

func (w *sseWriter) writeChunk(delta map[string]any, finish string, usage any) error {
	choice := map[string]any{"index": 0, "delta": delta}
	if finish != "" {
		choice["finish_reason"] = finish
	}
	chunk := map[string]any{
		"id":      w.id,
		"object":  "chat.completion.chunk",
		"created": w.created,
		"model":   w.model,
		"choices": []any{choice},
	}
	if usage != nil {
		chunk["usage"] = usage
	}
	encoded, err := json.Marshal(chunk)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w.pw, "data: %s\n\n", encoded)
	return err
}

func (w *sseWriter) finish() error {
	if !w.sawTerminalEvent {
		return newProviderError(502, "command code stream ended before a terminal event")
	}
	if !w.roleSent {
		w.roleSent = true
		if err := w.writeChunk(map[string]any{"role": "assistant"}, "", nil); err != nil {
			return err
		}
	}
	if err := w.writeChunk(map[string]any{}, normalizeFinishReason(w.finishReason, w.hasToolCalls), w.usage); err != nil {
		return err
	}
	_, err := io.WriteString(w.pw, "data: [DONE]\n\n")
	return err
}

func usagePayload(usage *eventUsage) map[string]any {
	if usage == nil {
		return nil
	}
	cacheRead := usage.cacheReadTokens()
	payload := map[string]any{
		"prompt_tokens":     usage.InputTokens,
		"completion_tokens": usage.OutputTokens,
		"total_tokens":      usage.InputTokens + usage.OutputTokens,
		"source":            "upstream",
	}
	if cacheRead > 0 {
		payload["cache_read_tokens"] = cacheRead
		payload["prompt_tokens_details"] = map[string]any{"cached_tokens": cacheRead}
	}
	return payload
}
