package gateway

import (
	"strings"
	"testing"
)

func feedChunks(t *testing.T, chunks ...string) *streamedChatOutput {
	t.Helper()
	output := &streamedChatOutput{}
	for _, chunk := range chunks {
		output.add([]byte(chunk))
	}
	return output
}

func TestStreamAccumulatesSplitArguments(t *testing.T) {
	output := feedChunks(t,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"read_file","arguments":""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a.go\"}"}}]},"finish_reason":"tool_calls"}]}`,
	)
	calls := output.calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1: %+v", len(calls), calls)
	}
	if calls[0].Arguments != `{"path":"a.go"}` {
		t.Fatalf("arguments = %q", calls[0].Arguments)
	}
}

// Some upstreams resend the whole argument buffer on every chunk. Concatenating
// those blindly yields `{"a":1}{"a":1}`, which clients reject as invalid JSON.
func TestStreamHandlesCumulativeArguments(t *testing.T) {
	output := feedChunks(t,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"write_file","arguments":"{\"path\":"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":\"a.go\""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":\"a.go\",\"body\":\"x\"}"}}]},"finish_reason":"tool_calls"}]}`,
	)
	calls := output.calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1: %+v", len(calls), calls)
	}
	if calls[0].Arguments != `{"path":"a.go","body":"x"}` {
		t.Fatalf("arguments = %q", calls[0].Arguments)
	}
	if err := validateProxyToolCallArguments(calls); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

// An upstream that omits `index` on continuation deltas must not lose the
// fragments into a phantom second call.
func TestStreamWithoutIndexKeepsOneCall(t *testing.T) {
	output := feedChunks(t,
		`{"choices":[{"delta":{"tool_calls":[{"id":"call_1","function":{"name":"grep","arguments":"{\"q\":"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"function":{"arguments":"\"x\"}"}}]}}]}`,
	)
	calls := output.calls()
	if len(calls) != 1 || calls[0].Arguments != `{"q":"x"}` {
		t.Fatalf("calls = %+v", calls)
	}
}

// Two calls that both report index 0 must stay separate calls.
func TestStreamZeroIndexPerCallStaysSeparate(t *testing.T) {
	output := feedChunks(t,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"a","arguments":"{\"x\":1}"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_2","function":{"name":"b","arguments":"{\"y\":2}"}}]},"finish_reason":"tool_calls"}]}`,
	)
	calls := output.calls()
	if len(calls) != 2 {
		t.Fatalf("calls = %+v", calls)
	}
	if calls[0].Name != "a" || calls[0].Arguments != `{"x":1}` {
		t.Fatalf("first call = %+v", calls[0])
	}
	if calls[1].Name != "b" || calls[1].Arguments != `{"y":2}` {
		t.Fatalf("second call = %+v", calls[1])
	}
}

// Some upstreams send a JSON object where the OpenAI schema expects a
// string. Decoding strictly as string dropped the whole chunk.
func TestStreamAcceptsObjectArguments(t *testing.T) {
	output := feedChunks(t,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"read_file","arguments":{"path":"a.go"}}}]}}]}`,
	)
	calls := output.calls()
	if len(calls) != 1 || calls[0].Arguments != `{"path":"a.go"}` {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestValidateRepairsFormattingDamage(t *testing.T) {
	calls := []proxyToolCall{{ID: "call_1", Name: "write_file", Arguments: `{"path":"a.go",`}}
	if err := validateProxyToolCallArguments(calls); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !strings.HasSuffix(calls[0].Arguments, "}") {
		t.Fatalf("arguments not repaired: %q", calls[0].Arguments)
	}
}

func TestValidateRejectsTruncatedArguments(t *testing.T) {
	calls := []proxyToolCall{{ID: "call_1", Name: "run_command", Arguments: `{"cmd":"rm -rf /tmp/fo`}}
	err := validateProxyToolCallArguments(calls)
	if err == nil {
		t.Fatal("expected truncated arguments to stay an error")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("unexpected error: %v", err)
	}
}
