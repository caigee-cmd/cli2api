package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeOpenAIToolCallsDropsInvalidArguments(t *testing.T) {
	calls := decodeOpenAIToolCalls(json.RawMessage(`[
		{"id":"call_bad","function":{"name":"mcp__fastctx__read","arguments":"{\"path\":\"x\",\"error_retry:: 240}"}},
		{"id":"call_good","function":{"name":"mcp__fastctx__grep","arguments":"{\"pattern\":\"x\"}"}}
	]`))
	if len(calls) != 1 || calls[0].ID != "call_good" {
		t.Fatalf("calls=%#v", calls)
	}
}

func TestRelayResponsesStreamRejectsInvalidToolArgumentsBeforeFinalize(t *testing.T) {
	upstream := strings.NewReader(
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_bad","function":{"name":"mcp__fastctx__read","arguments":"{\"path\":\"x\",\"error_retry:: 240}"}}]}}]}` + "\n\n" +
			`data: {"choices":[{"finish_reason":"tool_calls"}]}` + "\n\n" +
			"data: [DONE]\n\n",
	)
	var output strings.Builder
	_, err := RelayResponsesStream(&output, upstream, "req", "model", nil)
	if err == nil || !strings.Contains(err.Error(), "arguments are invalid JSON") {
		t.Fatalf("error=%v", err)
	}
	body := output.String()
	for _, event := range []string{
		"event: response.function_call_arguments.done",
		"event: response.output_item.done",
		"event: response.completed",
	} {
		if strings.Contains(body, event) {
			t.Fatalf("invalid tool call was finalized with %q:\n%s", event, body)
		}
	}
}
