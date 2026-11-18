package kiro

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// responsesSSESource turns a CreateResponse server-sent event stream into the
// same events consumeEvents reads for generateAssistantResponse. The mapping
// follows kiro-reverse-api (MIT, see NOTICE): a function_call opens a
// toolUseEvent, argument deltas append to that call's input, and the done
// item closes it with stop=true. An incomplete call is dropped rather than
// emitted with partial arguments.

type responsesAdapter struct {
	scanner *bufio.Scanner
	queue   []streamEvent

	callIDs   map[string]string
	lastCall  string
	open      map[string]bool
	argued    map[string]bool
	textSent  map[string]bool
	reasoning bool
	refusal   bool
}

func responsesSSESource(body io.Reader) eventSource {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64<<10), maxResponsesSSELine)
	adapter := &responsesAdapter{
		scanner:  scanner,
		callIDs:  map[string]string{},
		open:     map[string]bool{},
		argued:   map[string]bool{},
		textSent: map[string]bool{},
	}
	return adapter.next
}

func (a *responsesAdapter) next() (streamEvent, error) {
	for len(a.queue) == 0 {
		if !a.scanner.Scan() {
			if err := a.scanner.Err(); err != nil {
				if err == bufio.ErrTooLong {
					return streamEvent{}, fmt.Errorf("%w: sse line over %d bytes", errInvalidEventStream, maxResponsesSSELine)
				}
				return streamEvent{}, err
			}
			return streamEvent{}, io.EOF
		}
		if err := a.line(a.scanner.Text()); err != nil {
			return streamEvent{}, err
		}
	}
	event := a.queue[0]
	a.queue = a.queue[1:]
	return event, nil
}

func (a *responsesAdapter) emit(eventType string, event map[string]any) {
	a.queue = append(a.queue, streamEvent{Type: eventType, Event: event})
}

func (a *responsesAdapter) line(raw string) error {
	data, ok := strings.CutPrefix(strings.TrimRight(raw, "\r"), "data:")
	if !ok {
		return nil
	}
	data = strings.TrimSpace(data)
	if data == "" || data == "[DONE]" || data[0] != '{' {
		return nil
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return fmt.Errorf("%w: decode sse: %v", errInvalidEventStream, err)
	}
	return a.event(event)
}

func (a *responsesAdapter) event(event map[string]any) error {
	switch firstString(event, "type") {
	case "response.output_text.delta":
		if delta := firstString(event, "delta"); delta != "" {
			a.textSent[partKey(event)] = true
			a.emit("assistantResponseEvent", map[string]any{"content": delta})
		}
	case "response.output_text.done":
		if text := firstString(event, "text"); text != "" && !a.textSent[partKey(event)] {
			a.emit("assistantResponseEvent", map[string]any{"content": text})
		}
	case "response.refusal.delta":
		if delta := firstString(event, "delta"); delta != "" {
			a.refusal = true
			a.emit("assistantResponseEvent", map[string]any{"content": delta})
		}
	case "response.refusal.done":
		if text := firstString(event, "refusal"); text != "" && !a.refusal {
			a.emit("assistantResponseEvent", map[string]any{"content": text})
		}
	case "response.reasoning_text.delta", "response.reasoning.delta":
		if delta := firstString(event, "delta"); delta != "" {
			a.reasoning = true
			a.emit("reasoningContentEvent", map[string]any{"text": delta})
		}
	case "response.output_item.added":
		item, _ := event["item"].(map[string]any)
		switch firstString(item, "type") {
		case "reasoning":
			a.reasoning = false
		case "function_call":
			a.startCall(item)
		}
	case "response.function_call_arguments.delta":
		if delta := firstString(event, "delta"); delta != "" {
			if id := a.callFor(event); id != "" {
				a.argued[id] = true
				a.emit("toolUseEvent", map[string]any{"toolUseId": id, "input": delta})
			}
		}
	case "response.function_call_arguments.done":
		if id := a.callFor(event); id != "" && !a.argued[id] {
			if arguments := firstString(event, "arguments"); arguments != "" {
				a.argued[id] = true
				a.emit("toolUseEvent", map[string]any{"toolUseId": id, "input": arguments})
			}
		}
	case "response.output_item.done":
		item, _ := event["item"].(map[string]any)
		switch firstString(item, "type") {
		case "reasoning":
			a.reasoningDone(item)
		case "function_call":
			a.finishCall(item)
		}
	case "response.completed", "response.incomplete":
		a.completed(event)
	case "response.failed", "error":
		return responsesFailure(event)
	}
	return nil
}

func partKey(event map[string]any) string {
	return firstString(event, "item_id") + "\x00" + fmt.Sprint(event["content_index"])
}

func (a *responsesAdapter) startCall(item map[string]any) {
	id := firstString(item, "call_id", "id")
	if id == "" {
		return
	}
	if itemID := firstString(item, "id"); itemID != "" {
		a.callIDs[itemID] = id
	}
	a.lastCall = id
	a.open[id] = true
	event := map[string]any{"toolUseId": id, "name": firstString(item, "name")}
	if arguments := firstString(item, "arguments"); arguments != "" {
		a.argued[id] = true
		event["input"] = arguments
	}
	a.emit("toolUseEvent", event)
}

func (a *responsesAdapter) callFor(event map[string]any) string {
	if itemID := firstString(event, "item_id"); itemID != "" {
		if id := a.callIDs[itemID]; id != "" {
			return id
		}
	}
	if id := firstString(event, "call_id"); id != "" && a.open[id] {
		return id
	}
	return a.lastCall
}

// reasoningDone reads a finished reasoning item. The text already streamed as
// deltas is not repeated; the signature is kept so a later turn can replay it.
func (a *responsesAdapter) reasoningDone(item map[string]any) {
	raw := firstString(item, "encrypted_content")
	var envelope struct {
		ReasoningText *struct {
			Text      string `json:"text"`
			Signature string `json:"signature"`
		} `json:"reasoningText"`
		RedactedContent string `json:"redactedContent"`
	}
	if raw == "" || json.Unmarshal([]byte(raw), &envelope) != nil {
		return
	}
	if block := envelope.ReasoningText; block != nil {
		if block.Text != "" && !a.reasoning {
			a.emit("reasoningContentEvent", map[string]any{"text": block.Text})
		}
		if block.Signature != "" {
			a.emit("reasoningContentEvent", map[string]any{"signature": block.Signature})
		}
	}
	if envelope.RedactedContent != "" {
		a.emit("reasoningContentEvent", map[string]any{"redactedContent": envelope.RedactedContent})
	}
	a.reasoning = false
}

func (a *responsesAdapter) finishCall(item map[string]any) {
	id := firstString(item, "call_id", "id")
	if itemID := firstString(item, "id"); itemID != "" {
		if mapped := a.callIDs[itemID]; mapped != "" {
			id = mapped
		}
	}
	if id == "" {
		return
	}
	if !a.open[id] {
		if firstString(item, "name") == "" || a.argued[id] {
			return
		}
		a.startCall(item)
	}
	delete(a.open, id)
	if firstString(item, "status") == "incomplete" {
		a.emit("kiro:toolUseDrop", map[string]any{"toolUseId": id})
		return
	}
	if arguments := firstString(item, "arguments"); arguments != "" && !a.argued[id] {
		a.argued[id] = true
		a.emit("toolUseEvent", map[string]any{"toolUseId": id, "input": arguments})
	}
	a.emit("toolUseEvent", map[string]any{"toolUseId": id, "stop": true})
}

func (a *responsesAdapter) completed(event map[string]any) {
	response, _ := event["response"].(map[string]any)
	usage, _ := response["usage"].(map[string]any)
	if usage == nil {
		return
	}
	// CreateResponse reports Kiro credits and context occupancy here, not only
	// token counts. Both ride along so the OpenAI usage chunk can carry them.
	metadata := map[string]any{"usage": usage}
	if credits, ok := usage["kiro:credits"].(map[string]any); ok {
		if value, ok := numberAny(credits["value"]); ok {
			metadata["credits"] = value
		}
	}
	if pct, ok := numberAny(usage["kiro:context_usage_percentage"]); ok {
		metadata["contextUsagePercentage"] = pct
	}
	a.emit("metadataEvent", metadata)
}

func numberAny(value any) (float64, bool) {
	switch n := value.(type) {
	case float64:
		return n, true
	case json.Number:
		parsed, err := n.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func responsesFailure(event map[string]any) error {
	if response, ok := event["response"].(map[string]any); ok {
		if failure, ok := response["error"].(map[string]any); ok {
			if message := firstString(failure, "message"); message != "" {
				return errorsNew(message)
			}
		}
	}
	if message := firstString(event, "message"); message != "" {
		return errorsNew(message)
	}
	encoded, _ := json.Marshal(event)
	return errorsNew(string(encoded))
}

func errorsNew(message string) error { return &streamFailure{message: message} }

type streamFailure struct{ message string }

func (e *streamFailure) Error() string { return e.message }
