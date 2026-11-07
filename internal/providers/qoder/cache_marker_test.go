package qoder

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

type plainMessage struct {
	Role     string          `json:"role"`
	Content  json.RawMessage `json:"content"`
	Contents []struct {
		Type         string `json:"type"`
		Text         string `json:"text"`
		CacheControl *struct {
			Type string `json:"type"`
		} `json:"cache_control"`
	} `json:"contents"`
}

func decodePlainMessages(t *testing.T, body map[string]any) []plainMessage {
	t.Helper()
	encoded, err := json.Marshal(body["messages"])
	if err != nil {
		t.Fatal(err)
	}
	var msgs []plainMessage
	if err := json.Unmarshal(encoded, &msgs); err != nil {
		t.Fatal(err)
	}
	return msgs
}

func countMarkers(msgs []plainMessage) int {
	marked := 0
	for _, msg := range msgs {
		for _, part := range msg.Contents {
			if part.CacheControl != nil {
				marked++
			}
		}
	}
	return marked
}

// The gateway discounts prefix tokens only when the request carries an
// ephemeral cache_control on the last cacheable part of the last non-tool
// message. Without the marker the CN gateway bills the whole context at
// uncached rates on every call, even when the prefix repeats turn over turn.
func TestBuildPlainChatBodyMarksLastCacheablePart(t *testing.T) {
	raw := `{
		"model":"qoder-cn/deepseek-flash","max_tokens":40,
		"messages":[
			{"role":"system","content":"you are an agent"},
			{"role":"user","content":"list the files"},
			{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"bash","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"file1.txt"},
			{"role":"user","content":[{"type":"text","text":"now summarize"}]}
		]
	}`
	var req translate.ChatRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	plain := BuildPlainChatBody(req, time.Now())
	msgs := decodePlainMessages(t, plain)

	if got := countMarkers(msgs); got != 1 {
		t.Fatalf("cache markers = %d, want exactly 1", got)
	}
	last := msgs[len(msgs)-1]
	if last.Role != "user" {
		t.Fatalf("marked message role = %s, want user", last.Role)
	}
	if len(last.Contents) != 1 || last.Contents[0].Type != "text" || last.Contents[0].Text != "now summarize" {
		t.Fatalf("contents did not mirror the content parts: %+v", last.Contents)
	}
	if last.Contents[0].CacheControl == nil || last.Contents[0].CacheControl.Type != "ephemeral" {
		t.Fatalf("last part cache_control = %+v, want ephemeral", last.Contents[0].CacheControl)
	}
	// Tool results never carry contents, so they can never be marked.
	for _, msg := range msgs {
		if msg.Role == "tool" && len(msg.Contents) != 0 {
			t.Fatalf("tool message carried contents: %+v", msg)
		}
	}
	// The assistant tool-call turn keeps its rewritten empty string content
	// and gains no contents of its own.
	if string(msgs[2].Content) != `""` {
		t.Fatalf("assistant content = %s, want an empty string", msgs[2].Content)
	}
	if len(msgs[2].Contents) != 0 {
		t.Fatalf("assistant empty-content message carried contents: %+v", msgs[2].Contents)
	}
}

// A client that sends its own cache marker controls caching; the builder must
// not add a second one.
func TestBuildPlainChatBodyRespectsExistingCacheMarker(t *testing.T) {
	raw := `{
		"model":"qoder-cn/deepseek-flash","max_tokens":40,
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"stable prefix"},
				{"type":"text","text":"newest turn","cache_control":{"type":"ephemeral"}}
			]}
		]
	}`
	var req translate.ChatRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	plain := BuildPlainChatBody(req, time.Now())
	msgs := decodePlainMessages(t, plain)

	if got := countMarkers(msgs); got != 1 {
		t.Fatalf("cache markers = %d, want the client's 1", got)
	}
	parts := msgs[0].Contents
	if len(parts) != 2 || parts[0].CacheControl != nil {
		t.Fatalf("existing marker moved: %+v", parts)
	}
	if parts[1].CacheControl == nil || parts[1].CacheControl.Type != "ephemeral" {
		t.Fatalf("client marker lost: %+v", parts[1].CacheControl)
	}
}

// The walk skips role:"tool" messages, so a request ending in a tool result
// marks the last non-tool message instead.
func TestBuildPlainChatBodyMarkerSkipsToolMessages(t *testing.T) {
	raw := `{
		"model":"qoder-cn/deepseek-flash","max_tokens":40,
		"messages":[
			{"role":"user","content":"run it"},
			{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"bash","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"done"}
		]
	}`
	var req translate.ChatRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	plain := BuildPlainChatBody(req, time.Now())
	msgs := decodePlainMessages(t, plain)

	if got := countMarkers(msgs); got != 1 {
		t.Fatalf("cache markers = %d, want exactly 1", got)
	}
	if msgs[0].Contents[0].CacheControl == nil {
		t.Fatalf("marker should land on the first user message: %+v", msgs[0].Contents)
	}
}
