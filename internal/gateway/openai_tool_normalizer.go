package gateway

import (
	"encoding/json"
	"sort"
	"strconv"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

// openAIToolNormalizer keeps an OpenAI chat-completions stream faithful frame
// by frame except for tool-call arguments. Arguments are accumulated and
// flushed once per call, normalized to valid JSON, because a client that
// concatenates damaged upstream fragments aborts the round with "arguments are
// invalid JSON". Content, reasoning, usage and finish frames relay unchanged.
type openAIToolNormalizer struct {
	calls   map[int]*proxyToolCall
	emitted map[int]bool
	changed bool
	finish  string
	meta    map[string]any
}

func newOpenAIToolNormalizer() *openAIToolNormalizer {
	return &openAIToolNormalizer{
		calls:   map[int]*proxyToolCall{},
		emitted: map[int]bool{},
	}
}

// absorb consumes one chunk payload and returns the payload that should still
// be relayed, or nil when the chunk carried nothing but tool-call fragments.
func (n *openAIToolNormalizer) absorb(payload map[string]any) map[string]any {
	choices, ok := payload["choices"].([]any)
	if !ok || len(choices) == 0 {
		return payload
	}
	for _, key := range []string{"id", "object", "created", "model", "system_fingerprint"} {
		if value, ok := payload[key]; ok && value != nil {
			if n.meta == nil {
				n.meta = map[string]any{}
			}
			n.meta[key] = value
		}
	}
	dropped := false
	for _, rawChoice := range choices {
		choice, ok := rawChoice.(map[string]any)
		if !ok {
			continue
		}
		if finish, ok := choice["finish_reason"].(string); ok && finish != "" {
			n.finish = finish
		}
		delta, ok := choice["delta"].(map[string]any)
		if !ok {
			continue
		}
		rawCalls, hasCalls := delta["tool_calls"].([]any)
		if !hasCalls || len(rawCalls) == 0 {
			continue
		}
		n.absorbCalls(rawCalls)
		delete(delta, "tool_calls")
		if len(delta) == 0 && choice["finish_reason"] == nil {
			dropped = true
		}
	}
	if dropped {
		return nil
	}
	return payload
}

func (n *openAIToolNormalizer) absorbCalls(rawCalls []any) {
	for position, rawCall := range rawCalls {
		call, ok := rawCall.(map[string]any)
		if !ok {
			continue
		}
		id, _ := call["id"].(string)
		function, _ := call["function"].(map[string]any)
		name, _ := function["name"].(string)
		arguments := argumentFragment(rawArguments(function["arguments"]))
		index := n.resolveIndex(call, id, name, position)
		entry := n.calls[index]
		if entry == nil {
			entry = &proxyToolCall{}
			n.calls[index] = entry
		}
		if id != "" {
			entry.ID = id
		}
		if name != "" {
			entry.Name = name
		}
		merged := mergeToolArguments(entry.Arguments, arguments)
		if merged != entry.Arguments {
			entry.Arguments = merged
		}
	}
}

func (n *openAIToolNormalizer) resolveIndex(call map[string]any, id, name string, position int) int {
	declared := -1
	if value, ok := call["index"].(float64); ok {
		declared = int(value)
	}
	startsCall := id != "" || name != ""
	if declared >= 0 && !startsCall {
		return declared
	}
	if declared >= 0 {
		if n.callIsDifferent(declared, id, name) {
			return n.nextIndex()
		}
		return declared
	}
	if startsCall {
		if n.callIsDifferent(0, id, name) {
			return n.nextIndex()
		}
		return 0
	}
	return n.lastIndex(position)
}

func (n *openAIToolNormalizer) callIsDifferent(index int, id, name string) bool {
	existing := n.calls[index]
	if existing == nil || (existing.Name == "" && existing.ID == "") {
		return false
	}
	if id != "" && existing.ID != "" {
		return existing.ID != id
	}
	return name != "" && existing.Name != "" && existing.Name != name
}

func (n *openAIToolNormalizer) nextIndex() int {
	maximum := -1
	for index := range n.calls {
		if index > maximum {
			maximum = index
		}
	}
	return maximum + 1
}

func (n *openAIToolNormalizer) lastIndex(position int) int {
	maximum := -1
	for index, call := range n.calls {
		if call != nil && call.Name != "" && index > maximum {
			maximum = index
		}
	}
	if maximum >= 0 {
		return maximum
	}
	return position
}

// flushFrames returns one frame per call that has not been delivered yet, each
// carrying the normalized full argument string.
func (n *openAIToolNormalizer) flushFrames() []map[string]any {
	indexes := make([]int, 0, len(n.calls))
	for index, call := range n.calls {
		if call == nil || call.Name == "" || n.emitted[index] {
			continue
		}
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	frames := make([]map[string]any, 0, len(indexes))
	for _, index := range indexes {
		call := n.calls[index]
		arguments, changed := translate.NormalizeToolCallArguments(call.Arguments)
		if changed {
			n.changed = true
		}
		if call.ID == "" {
			call.ID = "call_" + call.Name + "_" + strconv.Itoa(index)
		}
		frames = append(frames, n.frame(map[string]any{
			"index": index,
			"id":    call.ID,
			"type":  "function",
			"function": map[string]any{
				"name":      call.Name,
				"arguments": arguments,
			},
		}))
		n.emitted[index] = true
	}
	return frames
}

func (n *openAIToolNormalizer) frame(call map[string]any) map[string]any {
	payload := map[string]any{
		"object": "chat.completion.chunk",
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         map[string]any{"tool_calls": []any{call}},
			"finish_reason": nil,
		}},
	}
	for key, value := range n.meta {
		payload[key] = value
	}
	return payload
}

func rawArguments(value any) json.RawMessage {
	if value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return encoded
}
