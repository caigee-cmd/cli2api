package kiro

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// writeOpenAIStream reads a CreateResponse SSE body once and writes OpenAI
// chat-completion chunks. A tool call is announced only after the upstream
// emits a complete call (name plus valid argument JSON), because the Anthropic
// relay opens a tool_use block on the first chunk that carries a name and then
// treats every later arguments fragment as input_json_delta. Sending the name
// before the JSON is finished would leave the client with a partial object.
func writeOpenAIStream(body io.ReadCloser, model string, names map[string]string, filter *markupFilter, cache *cacheTurn) *http.Response {
	pr, pw := io.Pipe()
	go func() {
		defer body.Close()
		defer pw.Close()
		writeConsumed(pw, model, responsesSSESource(body), names, filter, cache)
	}()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(pr),
	}
}

// writeConsumed turns one event source into OpenAI chat chunks. Tool calls are
// announced only after the upstream emits a complete call. names maps a Kiro
// tool name back to the name the client declared. cache, when set, adds the
// local prompt-cache estimate and is stored only after a clean stream.
func writeConsumed(w io.Writer, model string, source eventSource, names map[string]string, filter *markupFilter, cache *cacheTurn) {
	index := 0
	var inputTokens, outputTokens int
	var credits, contextPct float64
	sawCredits := false
	var produced tokenCounter
	finish := "stop"
	err := consumeEvents(source, &streamCallbacks{
		OnText: func(text string, thinking bool) {
			produced.add(text)
			field := "content"
			if thinking {
				field = "reasoning_content"
			}
			if !thinking {
				text = filter.push(text)
			}
			if text == "" {
				return
			}
			writeChunk(w, model, map[string]any{field: text}, nil, "")
		},
		OnToolUse: func(tool toolUse) {
			finish = "tool_calls"
			filter.seen(tool)
			if client := names[tool.Name]; client != "" {
				tool.Name = client
			}
			arguments, _ := json.Marshal(tool.Input)
			produced.add(tool.Name)
			produced.add(string(arguments))
			writeChunk(w, model, map[string]any{}, []map[string]any{toolChunk(index, tool.ID, tool.Name, string(arguments))}, "")
			index++
		},
		OnComplete: func(input, output int) { inputTokens, outputTokens = input, output },
		OnCredits: func(value float64) {
			credits, sawCredits = value, true
		},
		OnContextUsage: func(pct float64) { contextPct = pct },
	})
	if tail := filter.flush(); tail != "" && err == nil {
		writeChunk(w, model, map[string]any{"content": tail}, nil, "")
	}
	for _, tool := range filter.rescued(err == nil) {
		finish = "tool_calls"
		if client := names[tool.Name]; client != "" {
			tool.Name = client
		}
		if tool.ID == "" {
			tool.ID = newToolID()
		}
		arguments, _ := json.Marshal(tool.Input)
		writeChunk(w, model, map[string]any{}, []map[string]any{toolChunk(index, tool.ID, tool.Name, string(arguments))}, "")
		index++
	}
	if err != nil {
		payload, _ := json.Marshal(map[string]any{"error": map[string]any{"message": err.Error(), "type": "upstream_error"}})
		fmt.Fprintf(w, "data: %s\n\n", payload)
	}
	usage := streamUsage(model, inputTokens, outputTokens, contextPct, produced.total(), credits, sawCredits, cache.estimate())
	if err == nil {
		cache.commit()
	}
	done := map[string]any{
		"id": "chatcmpl-kiro", "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": finish}},
	}
	if len(usage) > 0 {
		done["usage"] = usage
	}
	encoded, _ := json.Marshal(done)
	fmt.Fprintf(w, "data: %s\n\n", encoded)
	io.WriteString(w, "data: [DONE]\n\n")
}

func writeChunk(w io.Writer, model string, delta map[string]any, tools []map[string]any, finish string) {
	if len(tools) > 0 {
		delta["tool_calls"] = tools
	}
	payload := map[string]any{
		"id": "chatcmpl-kiro", "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model,
		"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": nil}},
	}
	if finish != "" {
		payload["choices"] = []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", encoded)
}

func toolChunk(index int, id, name, arguments string) map[string]any {
	return map[string]any{
		"index": index,
		"id":    id,
		"type":  "function",
		"function": map[string]any{
			"name":      name,
			"arguments": arguments,
		},
	}
}
