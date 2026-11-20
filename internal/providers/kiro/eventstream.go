package kiro

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
)

// Event-stream framing and toolUseEvent assembly follow kiro-reverse-api
// (MIT, see NOTICE). A tool call is many fragments that share a toolUseId;
// arguments concatenate until a stop frame, and only then is the call emitted.
// Incomplete JSON is an error: a tool must not run with an empty input.

var errInvalidEventStream = errors.New("invalid kiro event stream")

type streamEvent struct {
	Type  string
	Event map[string]any
}

type eventSource func() (streamEvent, error)

type toolUse struct {
	ID    string
	Name  string
	Input map[string]any
}

type streamCallbacks struct {
	OnText     func(text string, thinking bool)
	OnToolUse  func(tool toolUse)
	OnComplete func(inputTokens, outputTokens int)
	// OnCredits receives the running credit total. CreateResponse states it
	// once; generateAssistantResponse sends one meteringEvent per charge.
	OnCredits func(credits float64)
	// OnContextUsage receives the share of the model's input window the
	// request filled, in percent. Kiro sends it instead of input tokens.
	OnContextUsage func(percentage float64)
	OnError        func(err error)
}

func eventStreamSource(body io.Reader) eventSource {
	return func() (streamEvent, error) {
		for {
			prelude := make([]byte, 12)
			if _, err := io.ReadFull(body, prelude); err != nil {
				if err == io.EOF {
					return streamEvent{}, io.EOF
				}
				if errors.Is(err, io.ErrUnexpectedEOF) {
					return streamEvent{}, fmt.Errorf("%w: truncated frame", errInvalidEventStream)
				}
				return streamEvent{}, err
			}
			total := int(eventUint32(prelude[0:4]))
			headersLen := int(eventUint32(prelude[4:8]))
			if total < 16 || total > maxEventStreamMessage || headersLen > total-16 {
				return streamEvent{}, fmt.Errorf("%w: invalid frame length %d", errInvalidEventStream, total)
			}
			if eventUint32(prelude[8:12]) != crc32.ChecksumIEEE(prelude[:8]) {
				return streamEvent{}, fmt.Errorf("%w: prelude crc mismatch", errInvalidEventStream)
			}
			message := make([]byte, total-len(prelude))
			if _, err := io.ReadFull(body, message); err != nil {
				return streamEvent{}, fmt.Errorf("%w: truncated frame", errInvalidEventStream)
			}
			if eventUint32(message[len(message)-4:]) != eventChecksum(prelude, message[:len(message)-4]) {
				return streamEvent{}, fmt.Errorf("%w: message crc mismatch", errInvalidEventStream)
			}
			headers, err := parseEventHeaders(message[:headersLen])
			if err != nil {
				return streamEvent{}, fmt.Errorf("%w: %v", errInvalidEventStream, err)
			}
			eventType := headers[":event-type"]
			payload := message[headersLen : len(message)-4]
			event := map[string]any{}
			if len(payload) > 0 {
				if err := json.Unmarshal(payload, &event); err != nil {
					if isOutputEvent(eventType) {
						return streamEvent{}, fmt.Errorf("%w: decode %s: %v", errInvalidEventStream, eventType, err)
					}
					continue
				}
			}
			switch headers[":message-type"] {
			case "error", "exception":
				return streamEvent{}, eventStreamError(headers, event)
			}
			return streamEvent{Type: eventType, Event: event}, nil
		}
	}
}

func isOutputEvent(eventType string) bool {
	switch eventType {
	case "assistantResponseEvent", "reasoningContentEvent", "toolUseEvent", "metadataEvent":
		return true
	}
	return false
}

func eventStreamError(headers map[string]string, event map[string]any) error {
	detail := firstString(event, "message", "Message")
	kind := headers[":exception-type"]
	if kind == "" {
		kind = headers[":error-code"]
	}
	if kind != "" && detail != "" {
		detail = kind + ": " + detail
	} else if detail == "" {
		detail = kind
	}
	if detail == "" {
		detail = "kiro upstream stream error"
	}
	return errors.New(detail)
}

type toolUseState struct {
	id        string
	name      string
	input     []byte
	generated bool
}

type pendingTools struct {
	byID   map[string]*toolUseState
	order  []string
	lastID string
}

func (p *pendingTools) get(id string) *toolUseState {
	if p.byID == nil {
		return nil
	}
	return p.byID[id]
}

func (p *pendingTools) add(state *toolUseState) {
	if p.byID == nil {
		p.byID = map[string]*toolUseState{}
	}
	p.byID[state.id] = state
	p.order = append(p.order, state.id)
	p.lastID = state.id
}

func (p *pendingTools) rekey(state *toolUseState, id string) {
	old := state.id
	delete(p.byID, old)
	for i, existing := range p.order {
		if existing == old {
			p.order[i] = id
			break
		}
	}
	state.id = id
	state.generated = false
	p.byID[id] = state
	if p.lastID == old {
		p.lastID = id
	}
}

func (p *pendingTools) remove(id string) {
	delete(p.byID, id)
	for i, existing := range p.order {
		if existing == id {
			p.order = append(p.order[:i], p.order[i+1:]...)
			break
		}
	}
	if p.lastID == id {
		p.lastID = ""
	}
}

func (p *pendingTools) flush(cb *streamCallbacks) error {
	order, byID := p.order, p.byID
	p.byID, p.order, p.lastID = nil, nil, ""
	for _, id := range order {
		if err := finishTool(byID[id], cb); err != nil {
			return err
		}
	}
	return nil
}

// handleToolUseEvent accumulates one fragment. A fragment without an id
// continues the tool in flight; a new name without an id closes that tool
// first so two calls never share an argument buffer.
func handleToolUseEvent(event map[string]any, pending *pendingTools, cb *streamCallbacks) error {
	id := firstString(event, "toolUseId", "toolUseID", "tool_use_id", "id")
	name := firstString(event, "name", "toolName", "tool_name")
	stop := firstBool(event, "stop", "isStop", "done")

	var state *toolUseState
	switch {
	case id != "":
		state = pending.get(id)
		if state == nil && pending.lastID != "" {
			if prev := pending.get(pending.lastID); prev != nil && prev.generated && (name == "" || prev.name == name) {
				pending.rekey(prev, id)
				state = prev
			}
		}
		if state == nil {
			if name == "" {
				return nil
			}
			state = &toolUseState{id: id, name: name}
			pending.add(state)
		} else {
			if name != "" && state.name == "" {
				state.name = name
			}
			pending.lastID = state.id
		}
	case pending.lastID != "" && pending.get(pending.lastID) != nil:
		state = pending.get(pending.lastID)
		if name != "" && state.name != name {
			if err := finishTool(state, cb); err != nil {
				return err
			}
			pending.remove(state.id)
			state = &toolUseState{id: newToolID(), name: name, generated: true}
			pending.add(state)
		}
	case name != "":
		state = &toolUseState{id: newToolID(), name: name, generated: true}
		pending.add(state)
	default:
		return nil
	}

	switch input := event["input"].(type) {
	case string:
		state.input = append(state.input, input...)
	case map[string]any:
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		state.input = encoded
	}

	if stop {
		if err := finishTool(state, cb); err != nil {
			return err
		}
		pending.remove(state.id)
	}
	return nil
}

func finishTool(state *toolUseState, cb *streamCallbacks) error {
	if state == nil || state.name == "" {
		return nil
	}
	if state.id == "" {
		state.id = newToolID()
	}
	input := map[string]any{}
	if raw := trimSpace(state.input); len(raw) > 0 {
		if err := json.Unmarshal(raw, &input); err != nil {
			return fmt.Errorf("incomplete tool input for %s: %w", state.name, err)
		}
	}
	if cb != nil && cb.OnToolUse != nil {
		cb.OnToolUse(toolUse{ID: state.id, Name: state.name, Input: input})
	}
	return nil
}

// consumeEvents reads one upstream event source to completion. Text and
// thinking are forwarded as they arrive. Tool fragments are held until stop
// (or the end of the stream) and then emitted as one complete call.
func consumeEvents(source eventSource, cb *streamCallbacks) error {
	if cb == nil {
		cb = &streamCallbacks{}
	}
	pending := &pendingTools{}
	var inputTokens, outputTokens int
	var metered float64
	stated := false
	for {
		next, err := source()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		event := next.Event
		if event == nil {
			event = map[string]any{}
		}
		inputTokens, outputTokens = updateTokens(event, inputTokens, outputTokens)
		if credits, ok := numberAny(event["credits"]); ok {
			stated = true
			if cb.OnCredits != nil {
				cb.OnCredits(credits)
			}
		}
		if pct, ok := numberAny(event["contextUsagePercentage"]); ok && cb.OnContextUsage != nil {
			cb.OnContextUsage(pct)
		}
		switch next.Type {
		case "meteringEvent":
			// usage is the credits this charge cost. A stated total wins so
			// the two sources are never added together.
			if used, ok := numberAny(event["usage"]); ok && !stated {
				metered += used
				if cb.OnCredits != nil {
					cb.OnCredits(metered)
				}
			}
		case "assistantResponseEvent":
			if text := firstString(event, "content"); text != "" && cb.OnText != nil {
				cb.OnText(text, false)
			}
		case "reasoningContentEvent":
			if text := firstString(event, "text"); text != "" && cb.OnText != nil {
				cb.OnText(text, true)
			}
		case "toolUseEvent":
			if err := handleToolUseEvent(event, pending, cb); err != nil {
				return err
			}
		case "kiro:toolUseDrop":
			if id := firstString(event, "toolUseId"); id != "" {
				pending.remove(id)
			}
		}
	}
	if err := pending.flush(cb); err != nil {
		return err
	}
	if cb.OnComplete != nil {
		cb.OnComplete(inputTokens, outputTokens)
	}
	return nil
}

func updateTokens(event map[string]any, input, output int) (int, int) {
	if usage, ok := event["usage"].(map[string]any); ok {
		if n, ok := numberField(usage, "inputTokens", "input_tokens"); ok {
			input = n
		}
		if n, ok := numberField(usage, "outputTokens", "output_tokens"); ok {
			output = n
		}
	}
	return input, output
}

func numberField(m map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		switch value := m[key].(type) {
		case float64:
			return int(value), true
		case json.Number:
			n, err := value.Int64()
			if err == nil {
				return int(n), true
			}
		}
	}
	return 0, false
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func firstBool(m map[string]any, keys ...string) bool {
	for _, key := range keys {
		if value, ok := m[key].(bool); ok && value {
			return true
		}
	}
	return false
}

func eventUint32(data []byte) uint32 {
	return uint32(data[0])<<24 | uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3])
}

func eventChecksum(prelude, message []byte) uint32 {
	sum := crc32.Update(0, crc32.IEEETable, prelude)
	return crc32.Update(sum, crc32.IEEETable, message)
}

func parseEventHeaders(data []byte) (map[string]string, error) {
	headers := map[string]string{}
	for offset := 0; offset < len(data); {
		nameLen := int(data[offset])
		offset++
		if nameLen == 0 || offset+nameLen >= len(data) {
			return nil, errors.New("malformed event header name")
		}
		name := string(data[offset : offset+nameLen])
		offset += nameLen
		valueType := data[offset]
		offset++
		valueLen := 0
		switch valueType {
		case 0, 1:
			continue
		case 2:
			valueLen = 1
		case 3:
			valueLen = 2
		case 4:
			valueLen = 4
		case 5, 8:
			valueLen = 8
		case 9:
			valueLen = 16
		case 6, 7:
			if offset+2 > len(data) {
				return nil, errors.New("malformed variable event header")
			}
			valueLen = int(data[offset])<<8 | int(data[offset+1])
			offset += 2
		default:
			return nil, fmt.Errorf("unsupported event header type %d", valueType)
		}
		if offset+valueLen > len(data) {
			return nil, errors.New("truncated event header value")
		}
		if valueType == 7 {
			headers[name] = string(data[offset : offset+valueLen])
		}
		offset += valueLen
	}
	return headers, nil
}

func trimSpace(raw []byte) []byte {
	start, end := 0, len(raw)
	for start < end && (raw[start] == ' ' || raw[start] == '\n' || raw[start] == '\t' || raw[start] == '\r') {
		start++
	}
	for end > start && (raw[end-1] == ' ' || raw[end-1] == '\n' || raw[end-1] == '\t' || raw[end-1] == '\r') {
		end--
	}
	return raw[start:end]
}
