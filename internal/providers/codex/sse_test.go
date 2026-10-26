package codex

import (
	"io"
	"strings"
	"testing"
)

// A Codex stream announces the tool name on response.output_item.added and then
// sends the arguments in separate events that carry only the item id. The relay
// must announce the tool call with that name: a client cannot dispatch a call
// whose name is empty, and replaying the empty name in history makes the
// upstream reject the request with "Invalid 'input[n].name': empty string".
func TestRewriteStreamKeepsToolNameFromOutputItem(t *testing.T) {
	sse := `event: response.output_item.added
data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather","arguments":""}}

event: response.function_call_arguments.delta
data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"{\"city\":\"Tokyo\"}"}

event: response.function_call_arguments.done
data: {"type":"response.function_call_arguments.done","output_index":0,"item_id":"fc_1","arguments":"{\"city\":\"Tokyo\"}"}

event: response.completed
data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":10,"output_tokens":5}}}

`

	stream, err := rewriteStream(strings.NewReader(sse), "gpt-6.1-sol")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(stream.Reader)
	if err != nil {
		t.Fatal(err)
	}
	out := string(body)

	if !strings.Contains(out, `"name":"get_weather"`) {
		t.Fatalf("tool call lost its name: %s", out)
	}
	if !strings.Contains(out, `"id":"call_1"`) {
		t.Fatalf("tool call lost its call id: %s", out)
	}
	if !strings.Contains(out, `"arguments":"{\"city\":\"Tokyo\"}"`) {
		t.Fatalf("tool call lost its arguments: %s", out)
	}
}

// An item that is not a tool call must not consume a tool-call index or emit a
// chunk, otherwise a reasoning item would shift every later call.
func TestRewriteStreamIgnoresNonToolOutputItems(t *testing.T) {
	sse := `event: response.output_item.added
data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[]}}

event: response.output_item.added
data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_2","call_id":"call_2","name":"read_file","arguments":""}}

event: response.function_call_arguments.done
data: {"type":"response.function_call_arguments.done","output_index":1,"item_id":"fc_2","arguments":"{\"path\":\"a.txt\"}"}

event: response.completed
data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}

`

	stream, err := rewriteStream(strings.NewReader(sse), "gpt-6.1-sol")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(stream.Reader)
	if err != nil {
		t.Fatal(err)
	}
	out := string(body)

	if strings.Contains(out, `"name":"rs_1"`) || strings.Contains(out, `"name":""`) {
		t.Fatalf("non-tool item leaked into tool_calls: %s", out)
	}
	if !strings.Contains(out, `"name":"read_file"`) {
		t.Fatalf("tool call lost its name: %s", out)
	}
	if !strings.Contains(out, `"index":0`) {
		t.Fatalf("tool call index should start at 0: %s", out)
	}
}
