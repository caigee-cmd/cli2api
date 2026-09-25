package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

const maxSSELineSize = 4 << 20

// rewriteStream wraps the upstream Responses SSE body and emits an OpenAI
// chat-completions delta stream (`choices[].delta`) that the gateway relay
// consumes. Terminal event is data: [DONE].
func rewriteStream(body io.Reader, model string) (*streamResult, error) {
	pr, pw := io.Pipe()
	state := &streamConverter{model: model, writer: pw}
	go state.run(body)
	return &streamResult{Reader: pr}, nil
}

type streamResult struct {
	Reader io.Reader
}

func (s *streamResult) Close() error {
	if closer, ok := s.Reader.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// streamConverter translates codex Responses SSE events into chat-completions
// chunks line by line.
type streamConverter struct {
	model  string
	writer *io.PipeWriter

	// per-turn state
	textStarted      bool
	reasoningText    strings.Builder
	reasoningDone    bool
	toolCalls        map[int]*pendingCall
	toolOrder        []int
	finishReason     string
	promptTokens     int
	completionTokens int
	sawTerminal      bool
}

type pendingCall struct {
	id        string
	name      string
	arguments strings.Builder
	announced bool
	index     int
}

func (c *streamConverter) run(body io.Reader) {
	defer c.writer.Close()
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineSize)
	var eventName, data strings.Builder
	flush := func() {
		event := strings.TrimSpace(eventName.String())
		payload := strings.TrimSpace(data.String())
		eventName.Reset()
		data.Reset()
		if payload == "" {
			return
		}
		c.handleEvent(event, payload)
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "event:") {
			if eventName.Len() > 0 {
				eventName.WriteString(" ")
			}
			eventName.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "event:")))
		} else if strings.HasPrefix(line, "data:") {
			if data.Len() > 0 {
				data.WriteString("\n")
			}
			data.WriteString(strings.TrimPrefix(line, "data:"))
		}
	}
	flush()
	if !c.sawTerminal {
		c.emitDone()
	}
}

func (c *streamConverter) handleEvent(event, payload string) {
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(payload), &obj) != nil {
		return
	}
	typ := event
	if t := rawMapString(obj, "type"); t != "" {
		typ = t
	}
	switch typ {
	case "response.output_text.delta":
		var delta struct {
			Delta string `json:"delta"`
		}
		if json.Unmarshal(obj["delta"], &delta.Delta) != nil {
			// delta is a plain string in the payload.
		}
		var d string
		if json.Unmarshal(obj["delta"], &d) == nil {
			c.emitContent(d)
		}
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		var d string
		if json.Unmarshal(obj["delta"], &d) == nil {
			c.emitReasoning(d)
		}
	case "response.output_item.added", "response.output_item.done":
		var item struct {
			Type   string `json:"type"`
			Status string `json:"status"`
		}
		if json.Unmarshal(obj["item"], &item) != nil {
			return
		}
	case "response.function_call_arguments.delta":
		var delta struct {
			Delta       string `json:"delta"`
			ItemID      string `json:"item_id"`
			OutputIndex int    `json:"output_index"`
		}
		if json.Unmarshal([]byte(payload), &delta) == nil {
			c.emitToolArgDelta(delta.OutputIndex, delta.ItemID, delta.Delta)
		}
	case "response.function_call_arguments.done", "response.custom_tool_call_input.done":
		var done struct {
			OutputIndex int    `json:"output_index"`
			ItemID      string `json:"item_id"`
			Name        string `json:"name"`
			CallID      string `json:"call_id"`
			Arguments   string `json:"arguments"`
			Input       string `json:"input"`
		}
		if json.Unmarshal([]byte(payload), &done) == nil {
			c.emitToolDone(done.OutputIndex, done.ItemID, done.CallID, done.Name, firstNonEmpty(done.Arguments, done.Input))
		}
	case "response.completed", "response.incomplete":
		var resp struct {
			Response struct {
				Status string `json:"status"`
				Usage  struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
				Output []json.RawMessage `json:"output"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(payload), &resp) == nil {
			c.promptTokens = resp.Response.Usage.InputTokens
			c.completionTokens = resp.Response.Usage.OutputTokens
			c.finishReason = "stop"
			if typ == "response.incomplete" {
				c.finishReason = "length"
			}
			c.sawTerminal = true
			c.emitUsage()
			c.emitFinish()
		}
	case "response.failed", "error":
		var errObj struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		}
		_ = json.Unmarshal([]byte(payload), &errObj)
		c.emitDone()
		c.sawTerminal = true
	case "response.created":
		// no-op; gateway synthesizes its own response.created
	}
}

func (c *streamConverter) emitContent(text string) {
	if text == "" {
		return
	}
	c.textStarted = true
	c.writeChunk(map[string]any{
		"choices": []any{map[string]any{
			"index": 0,
			"delta": map[string]any{"content": text},
		}},
	})
}

func (c *streamConverter) emitReasoning(text string) {
	if text == "" {
		return
	}
	c.reasoningText.WriteString(text)
	c.writeChunk(map[string]any{
		"choices": []any{map[string]any{
			"index": 0,
			"delta": map[string]any{"reasoning_content": text},
		}},
	})
}

func (c *streamConverter) toolCall(outputIndex int, itemID string) *pendingCall {
	if c.toolCalls == nil {
		c.toolCalls = map[int]*pendingCall{}
	}
	call, ok := c.toolCalls[outputIndex]
	if !ok {
		call = &pendingCall{index: len(c.toolOrder)}
		c.toolCalls[outputIndex] = call
		c.toolOrder = append(c.toolOrder, outputIndex)
	}
	if itemID != "" && call.id == "" {
		call.id = itemID
	}
	return call
}

func (c *streamConverter) emitToolArgDelta(outputIndex int, itemID, delta string) {
	call := c.toolCall(outputIndex, itemID)
	call.arguments.WriteString(delta)
	if !call.announced {
		return
	}
	c.writeChunk(map[string]any{
		"choices": []any{map[string]any{
			"index": 0,
			"delta": map[string]any{
				"tool_calls": []any{map[string]any{
					"index":    call.index,
					"function": map[string]any{"arguments": delta},
				}},
			},
		}},
	})
}

func (c *streamConverter) emitToolDone(outputIndex int, itemID, callID, name, arguments string) {
	call := c.toolCall(outputIndex, itemID)
	if callID != "" {
		call.id = callID
	}
	if name != "" {
		call.name = name
	}
	if arguments != "" && call.arguments.Len() == 0 {
		call.arguments.WriteString(arguments)
	}
	if call.announced {
		return
	}
	call.announced = true
	c.writeChunk(map[string]any{
		"choices": []any{map[string]any{
			"index": 0,
			"delta": map[string]any{
				"tool_calls": []any{map[string]any{
					"index": call.index,
					"id":    firstNonEmpty(call.id, fmt.Sprintf("call_%d", call.index)),
					"type":  "function",
					"function": map[string]any{
						"name":      call.name,
						"arguments": call.arguments.String(),
					},
				}},
			},
		}},
	})
}

func (c *streamConverter) emitUsage() {
	if c.promptTokens == 0 && c.completionTokens == 0 {
		return
	}
	c.writeChunk(map[string]any{
		"usage": map[string]any{
			"prompt_tokens":     c.promptTokens,
			"completion_tokens": c.completionTokens,
			"total_tokens":      c.promptTokens + c.completionTokens,
		},
	})
}

func (c *streamConverter) emitFinish() {
	c.writeChunk(map[string]any{
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         map[string]any{},
			"finish_reason": firstNonEmpty(c.finishReason, "stop"),
		}},
	})
	c.emitDone()
}

func (c *streamConverter) emitDone() {
	_, _ = c.writer.Write([]byte("data: [DONE]\n\n"))
}

func (c *streamConverter) writeChunk(obj map[string]any) {
	obj["object"] = "chat.completion.chunk"
	obj["model"] = c.model
	obj["created"] = time.Now().Unix()
	obj["id"] = "chatcmpl-codex"
	payload, err := json.Marshal(obj)
	if err != nil {
		return
	}
	_, _ = c.writer.Write([]byte("data: " + string(payload) + "\n\n"))
}

// aggregate reads a full upstream Responses SSE body and returns the final
// ChatOutcome, used for non-stream requests.
func aggregate(body io.Reader, model string) (providers.ChatOutcome, error) {
	outcome := providers.ChatOutcome{Model: model, FinishReason: "stop", UsageSource: "upstream"}
	var content strings.Builder
	var reasoning strings.Builder
	var toolCalls []json.RawMessage
	sawTerminal := false

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineSize)
	var eventData strings.Builder
	flush := func() {
		payload := strings.TrimSpace(eventData.String())
		eventData.Reset()
		if payload == "" {
			return
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal([]byte(payload), &obj) != nil {
			return
		}
		switch rawMapString(obj, "type") {
		case "response.output_text.delta":
			var d string
			if json.Unmarshal(obj["delta"], &d) == nil {
				content.WriteString(d)
			}
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			var d string
			if json.Unmarshal(obj["delta"], &d) == nil {
				reasoning.WriteString(d)
			}
		case "response.completed", "response.incomplete":
			var resp struct {
				Response struct {
					Status string `json:"status"`
					Usage  struct {
						InputTokens  int `json:"input_tokens"`
						OutputTokens int `json:"output_tokens"`
					} `json:"usage"`
					Output []json.RawMessage `json:"output"`
				} `json:"response"`
			}
			if json.Unmarshal([]byte(payload), &resp) == nil {
				outcome.PromptTokens = resp.Response.Usage.InputTokens
				outcome.CompletionTokens = resp.Response.Usage.OutputTokens
				if rawMapString(obj, "type") == "response.incomplete" {
					outcome.FinishReason = "length"
				}
				// Extract tool calls from completed output items.
				for _, item := range resp.Response.Output {
					var parsed struct {
						Type      string `json:"type"`
						CallID    string `json:"call_id"`
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}
					if json.Unmarshal(item, &parsed) != nil || parsed.Type != "function_call" {
						continue
					}
					encoded, _ := json.Marshal(map[string]any{
						"id":   parsed.CallID,
						"type": "function",
						"function": map[string]any{
							"name":      parsed.Name,
							"arguments": parsed.Arguments,
						},
					})
					toolCalls = append(toolCalls, encoded)
				}
				sawTerminal = true
			}
		}
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "data:") {
			if eventData.Len() > 0 {
				eventData.WriteString("\n")
			}
			eventData.WriteString(strings.TrimPrefix(line, "data:"))
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		return outcome, err
	}
	if !sawTerminal {
		return outcome, fmt.Errorf("codex stream ended without terminal event")
	}
	outcome.Content = content.String()
	outcome.Reasoning = reasoning.String()
	if len(toolCalls) > 0 {
		outcome.ToolCalls = json.RawMessage("[" + strings.Join(rawMessages(toolCalls), ",") + "]")
		outcome.FinishReason = "tool_calls"
	}
	return outcome, nil
}

func rawMessages(items []json.RawMessage) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, string(item))
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// responseBody returns a synthetic *http.Response wrapping the rewritten stream.
type responseBody struct {
	io.Reader
	closer io.Closer
}

func (r *responseBody) Close() error { return r.closer.Close() }
