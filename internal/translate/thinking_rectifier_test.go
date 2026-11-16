package translate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRectifyAnthropicRequestDropsHistoricalThinking(t *testing.T) {
	body := []byte(`{"model":"hy3","thinking":{"type":"enabled","budget_tokens":1024},"max_tokens":64,
		"messages":[
			{"role":"user","content":[{"type":"text","text":"hello"}]},
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"secret chain","signature":"sig-from-other-provider"},
				{"type":"text","text":"answer"},
				{"type":"tool_use","id":"call_1","name":"Read","input":{}}
			]}
		]}`)
	out, result, err := RectifyAnthropicRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied || result.RemovedThinkingBlocks != 1 || !result.RemovedTopLevelThinking {
		t.Fatalf("result = %+v", result)
	}
	if strings.Contains(string(out), "thinking") && strings.Contains(string(out), "secret chain") {
		t.Fatalf("thinking content leaked: %s", out)
	}
	if !strings.Contains(string(out), "tool_use") || !strings.Contains(string(out), "answer") {
		t.Fatalf("normal content lost: %s", out)
	}
	var source AnthropicMessagesRequest
	if err := json.Unmarshal(out, &source); err != nil {
		t.Fatal(err)
	}
	chat, err := TranslateAnthropicMessages(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(chat.Messages) != 2 {
		t.Fatalf("messages = %d", len(chat.Messages))
	}
}

func TestRectifyAnthropicRequestIsIdempotentAndPreservesPlainBodies(t *testing.T) {
	plain := []byte(`{"model":"hy3","messages":[{"role":"user","content":"hi"}]}`)
	out, result, err := RectifyAnthropicRequest(plain)
	if err != nil || result.Applied || string(out) != string(plain) {
		t.Fatalf("plain body changed: %+v %s", result, out)
	}
	withSignature := []byte(`{"model":"hy3","messages":[{"role":"assistant","content":[{"type":"text","text":"ok","signature":"stale"}]}]}`)
	first, r1, err := RectifyAnthropicRequest(withSignature)
	if err != nil || !r1.Applied || r1.RemovedSignatureFields != 1 {
		t.Fatalf("first = %+v", r1)
	}
	second, r2, err := RectifyAnthropicRequest(first)
	if err != nil || r2.Applied || string(second) != string(first) {
		t.Fatalf("not idempotent: %+v", r2)
	}
}
