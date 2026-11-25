package gateway

import (
	"encoding/json"
	"net/http/httptest"
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
	if normalizeProxyToolCallArguments(calls) {
		t.Fatal("valid arguments should not be reported as changed")
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

// Two calls of the same tool, each announcing index 0 with its own call id,
// must not be merged into one argument buffer.
func TestStreamSameToolTwiceStaysSeparate(t *testing.T) {
	output := feedChunks(t,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"read_file","arguments":"{\"path\":\"a\"}"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_2","function":{"name":"read_file","arguments":"{\"path\":\"b\"}"}}]},"finish_reason":"tool_calls"}]}`,
	)
	calls := output.calls()
	if len(calls) != 2 {
		t.Fatalf("calls = %+v", calls)
	}
	if calls[0].Arguments != `{"path":"a"}` || calls[1].Arguments != `{"path":"b"}` {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestRelayOpenAIStreamNormalizesFragmentedToolArguments(t *testing.T) {
	body := strings.Join([]string{
		`data: {"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"write_file","arguments":"{\"path\":"}}]}}]}`,
		"",
		`data: {"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a.go\","}}]}}]}`,
		"",
		`data: {"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"body\":\"par"}}]}}]}`,
		"",
		`data: {"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	recorder := httptest.NewRecorder()
	stats, err := RelayOpenAIStream(recorder, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if !stats.SawDone {
		t.Fatalf("stats=%+v", stats)
	}
	var arguments []string
	for _, line := range strings.Split(recorder.Body.String(), "\n") {
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					ToolCalls []struct {
						Function struct {
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(payload), &chunk) != nil {
			continue
		}
		for _, choice := range chunk.Choices {
			for _, call := range choice.Delta.ToolCalls {
				arguments = append(arguments, call.Function.Arguments)
			}
		}
	}
	if len(arguments) != 1 {
		t.Fatalf("expected exactly one arguments frame, got %d: %#v", len(arguments), arguments)
	}
	// The body field was cut mid-string, so it is dropped rather than padded
	// into a plausible-looking value; everything the model finished survives.
	if arguments[0] != `{"path":"a.go"}` {
		t.Fatalf("arguments = %q", arguments[0])
	}
	if !json.Valid([]byte(arguments[0])) {
		t.Fatalf("arguments not valid JSON: %q", arguments[0])
	}
}

func TestNormalizeRepairsFormattingDamage(t *testing.T) {
	calls := []proxyToolCall{{ID: "call_1", Name: "write_file", Arguments: `{"path":"a.go",`}}
	if !normalizeProxyToolCallArguments(calls) {
		t.Fatal("expected repair to be reported")
	}
	if !json.Valid([]byte(calls[0].Arguments)) {
		t.Fatalf("arguments not valid after normalize: %q", calls[0].Arguments)
	}
}

func TestNormalizeTruncatedArgumentsNeverFails(t *testing.T) {
	calls := []proxyToolCall{{ID: "call_1", Name: "run_command", Arguments: `{"cmd":"rm -rf /tmp/fo`}}
	if !normalizeProxyToolCallArguments(calls) {
		t.Fatal("expected normalize to change truncated arguments")
	}
	if !json.Valid([]byte(calls[0].Arguments)) {
		t.Fatalf("arguments still invalid: %q", calls[0].Arguments)
	}
	// The unfinished command must not survive as a runnable string.
	if strings.Contains(calls[0].Arguments, "rm -rf") {
		t.Fatalf("truncated command leaked into arguments: %q", calls[0].Arguments)
	}
}
