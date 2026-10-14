package qoder

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

// The gateway pairs a role:"tool" message with the tool_calls of the assistant
// turn ahead of it, and only registers that pairing when the assistant
// message's content is a string. OpenAI clients send `"content": null` for a
// pure tool-call turn, which leaves the call unregistered and the gateway
// rejects the follow-up tool message:
//
//	messages with role "tool" must be a response to a preceeding message
//	with "tool_calls"
//
// An empty string pairs correctly, so a nil content on a tool-call turn is
// rewritten to "".
func TestBuildPlainChatBodyNullContentWithToolCallsBecomesEmptyString(t *testing.T) {
	raw := `{
		"model":"qwen3.8-flash","stream":false,"max_tokens":40,
		"tools":[{"type":"function","function":{"name":"bash","description":"run","parameters":{"type":"object"}}}],
		"messages":[
			{"role":"user","content":"run ls"},
			{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"bash","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"file1.txt"},
			{"role":"user","content":"say OK"}
		]
	}`
	var req translate.ChatRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	plain := BuildPlainChatBody(req, time.Now())
	encoded, _ := json.Marshal(plain["messages"])
	t.Logf("messages = %s", encoded)

	var msgs []struct {
		Role      string          `json:"role"`
		Content   json.RawMessage `json:"content"`
		ToolCalls json.RawMessage `json:"tool_calls"`
	}
	if err := json.Unmarshal(encoded, &msgs); err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 4 {
		t.Fatalf("message count = %d, want 4", len(msgs))
	}
	if string(msgs[1].Content) != `""` {
		t.Fatalf("assistant content = %s, want an empty string", msgs[1].Content)
	}
	// The tool call and its id must survive the rewrite untouched.
	if len(msgs[1].ToolCalls) == 0 {
		t.Fatal("assistant tool_calls dropped")
	}
}

// A plain assistant message with null content and no tool calls keeps its
// original shape: the pairing rule only applies to tool-call turns.
func TestBuildPlainChatBodyKeepsNullContentWithoutToolCalls(t *testing.T) {
	raw := `{
		"model":"qwen3.8-flash","max_tokens":40,
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":null}
		]
	}`
	var req translate.ChatRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	plain := BuildPlainChatBody(req, time.Now())
	encoded, _ := json.Marshal(plain["messages"])

	var msgs []struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(encoded, &msgs); err != nil {
		t.Fatal(err)
	}
	if string(msgs[1].Content) != "null" {
		t.Fatalf("content = %s, want null (no tool_calls to pair)", msgs[1].Content)
	}
}

// Non-empty content and empty-string content both pass through unchanged.
func TestBuildPlainChatBodyKeepsExistingContent(t *testing.T) {
	for _, want := range []string{`""`, `"planning the call"`} {
		raw := `{
			"model":"qwen3.8-flash","max_tokens":40,
			"messages":[
				{"role":"user","content":"hi"},
				{"role":"assistant","content":` + want + `,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"bash","arguments":"{}"}}]}
			]
		}`
		var req translate.ChatRequest
		if err := json.Unmarshal([]byte(raw), &req); err != nil {
			t.Fatal(err)
		}
		plain := BuildPlainChatBody(req, time.Now())
		encoded, _ := json.Marshal(plain["messages"])
		var msgs []struct {
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(encoded, &msgs); err != nil {
			t.Fatal(err)
		}
		if string(msgs[1].Content) != want {
			t.Fatalf("content = %s, want %s", msgs[1].Content, want)
		}
	}
}
