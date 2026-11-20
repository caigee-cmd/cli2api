package minimaxcode

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

type openAIDelta struct {
	Content   string
	Reasoning string
	ToolCalls []toolCallDelta
	Finish    string // non-empty when a finish reason is known
}

type toolCallDelta struct {
	Index     int
	ID        string
	Name      string
	Arguments string
	HasID     bool
	HasName   bool
}

type anthropicDecoder struct {
	sentRole  bool
	toolIndex int
	finish    string
}

func newAnthropicDecoder() *anthropicDecoder { return &anthropicDecoder{toolIndex: -1} }

func stopReason(reason string) string {
	switch reason {
	case "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	case "refusal":
		return "content_filter"
	default:
		return "stop"
	}
}

func (d *anthropicDecoder) frame(eventType string, payload string) ([]openAIDelta, bool, error) {
	var event map[string]any
	if json.Unmarshal([]byte(payload), &event) != nil {
		return nil, false, nil
	}
	kind := first(jsonStr(event, "type"), eventType)
	switch kind {
	case "message_start", "message":
		if d.sentRole {
			return nil, false, nil
		}
		d.sentRole = true
		return []openAIDelta{{}}, false, nil // role delta handled by chat layer
	case "content_block_start":
		block, _ := event["content_block"].(map[string]any)
		if jsonStr(block, "type") == "tool_use" {
			d.toolIndex++
			call := toolCallDelta{Index: d.toolIndex, ID: jsonStr(block, "id"), Name: jsonStr(block, "name"), HasID: true, HasName: true}
			if call.ID == "" {
				call.ID = newToolID()
			}
			return []openAIDelta{{ToolCalls: []toolCallDelta{call}}}, false, nil
		}
	case "content_block_delta":
		delta, _ := event["delta"].(map[string]any)
		switch jsonStr(delta, "type") {
		case "text_delta":
			if text := jsonStr(delta, "text"); text != "" {
				return []openAIDelta{{Content: text}}, false, nil
			}
		case "input_json_delta":
			if partial := jsonStr(delta, "partial_json"); partial != "" {
				call := toolCallDelta{Index: d.toolIndex, Arguments: partial}
				return []openAIDelta{{ToolCalls: []toolCallDelta{call}}}, false, nil
			}
		}
	case "message_delta":
		delta, _ := event["delta"].(map[string]any)
		if reason := jsonStr(delta, "stop_reason"); reason != "" {
			d.finish = stopReason(reason)
		}
	case "message_stop":
		finish := d.finish
		if finish == "" {
			finish = "stop"
		}
		return nil, true, nil
	}
	return nil, false, nil
}

func readAnthropicSSE(src io.Reader, handle func(eventType, data string) (done bool, err error)) error {
	reader := bufio.NewReaderSize(src, 64<<10)
	var eventType string
	var dataLines []string
	dispatch := func() (bool, error) {
		if eventType == "" && len(dataLines) == 0 {
			return false, nil
		}
		payload := strings.Join(dataLines, "\n")
		event, data := eventType, payload
		eventType, dataLines = "", nil
		if strings.TrimSpace(payload) == "" {
			return false, nil
		}
		return handle(event, data)
	}
	for {
		line, err := reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			done, handleErr := dispatch()
			if handleErr != nil {
				return handleErr
			}
			if done {
				return nil
			}
		} else if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
		if err != nil {
			if err == io.EOF {
				_, handleErr := dispatch()
				return handleErr
			}
			return err
		}
	}
}
