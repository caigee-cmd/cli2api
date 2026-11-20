package doubaowork

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

func chatRequest(model string, messages []translate.ChatMessage) translate.ChatRequest {
	return translate.ChatRequest{Model: model, Messages: messages}
}

func TestPrepareSeparatesSystemAndHistory(t *testing.T) {
	req := chatRequest("doubao-auto", []translate.ChatMessage{
		{Role: "system", Content: "be concise"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
		{Role: "user", Content: "again"},
	})
	prepared, err := prepareChatRequest(req)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !prepared.SystemSeen {
		t.Fatal("system prompt must be recorded")
	}
	if !strings.Contains(prepared.Prompt, "instructions") {
		t.Fatal("system/developer instructions must be encoded separately")
	}
	if !strings.Contains(prepared.Prompt, "again") || !strings.Contains(prepared.Prompt, "hi") {
		t.Fatal("conversation history must be encoded")
	}
}

func TestPrepareEncodesTools(t *testing.T) {
	tools := json.RawMessage(`[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object"}}}]`)
	req := chatRequest("doubao-auto", []translate.ChatMessage{{Role: "user", Content: "weather?"}})
	req.Tools = tools
	req.ToolChoice = json.RawMessage(`"auto"`)
	prepared, err := prepareChatRequest(req)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !prepared.HasTools {
		t.Fatal("tools should be enabled")
	}
	if _, ok := prepared.ToolNames["get_weather"]; !ok {
		t.Fatal("tool name not recorded")
	}
	if !strings.Contains(prepared.Prompt, "工具传输协议") {
		t.Fatal("tool protocol instruction missing")
	}
}

func TestPrepareStructured(t *testing.T) {
	req := chatRequest("doubao-auto", []translate.ChatMessage{{Role: "user", Content: "give json"}})
	req.ResponseFormat = json.RawMessage(`{"type":"json_object"}`)
	prepared, err := prepareChatRequest(req)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !prepared.Structured || !prepared.JSONObject {
		t.Fatalf("structured/json_object flags wrong: %+v", prepared)
	}
}

func preparedWithTools(t *testing.T, required bool, parallel bool) preparedRequest {
	t.Helper()
	tools := json.RawMessage(`[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object"}}}]`)
	req := chatRequest("doubao-auto", []translate.ChatMessage{{Role: "user", Content: "weather?"}})
	req.Tools = tools
	if required {
		req.ToolChoice = json.RawMessage(`"required"`)
	}
	parallelVal := parallel
	req.ParallelToolCalls = &parallelVal
	prepared, err := prepareChatRequest(req)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	return prepared
}

func TestDecodeToolFenced(t *testing.T) {
	prepared := preparedWithTools(t, false, true)
	text := "好的，我来调用\n```json\n{\"content\":\"\",\"tool_calls\":[{\"name\":\"get_weather\",\"arguments\":{\"city\":\"SF\"}}]}\n```"
	content, calls, err := decodeReply(text, prepared)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if content != "" {
		t.Fatalf("content should be empty, got %q", content)
	}
	assertToolCall(t, calls, "get_weather", "SF")
}

func TestDecodeToolTruncatedSalvage(t *testing.T) {
	prepared := preparedWithTools(t, false, true)
	// Outer object/array never close, but the first call is complete.
	text := `{"content":"working","tool_calls":[{"name":"get_weather","arguments":{"city":"LA"}}`
	content, calls, err := decodeReply(text, prepared)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if content != "working" {
		t.Fatalf("salvaged content = %q", content)
	}
	assertToolCall(t, calls, "get_weather", "LA")
}

func TestDecodeToolNonForcedFallsBackToText(t *testing.T) {
	prepared := preparedWithTools(t, false, true)
	content, calls, err := decodeReply("The weather is nice today.", prepared)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("expected no tool calls, got %s", calls)
	}
	if content != "The weather is nice today." {
		t.Fatalf("content = %q", content)
	}
}

func TestDecodeToolRequiredMissingFails(t *testing.T) {
	prepared := preparedWithTools(t, true, true)
	if _, _, err := decodeReply("just chatting", prepared); err == nil {
		t.Fatal("expected error when required tool call is missing")
	}
}

func assertToolCall(t *testing.T, calls json.RawMessage, wantName, wantArg string) {
	t.Helper()
	if len(calls) == 0 {
		t.Fatal("no tool calls decoded")
	}
	var decoded []map[string]any
	if err := json.Unmarshal(calls, &decoded); err != nil {
		t.Fatalf("tool calls not json: %v", err)
	}
	fn, _ := decoded[0]["function"].(map[string]any)
	if fn["name"] != wantName {
		t.Fatalf("tool name = %v", fn["name"])
	}
	if !strings.Contains(fn["arguments"].(string), wantArg) {
		t.Fatalf("arguments missing %q: %v", wantArg, fn["arguments"])
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"9":                                "auto",
		"5":                                "pro",
		"person-kimi-k3-1-pro-261111":      "kimi-k3-1-pro",
		"person-deepseek-v4-pro-ga-260813": "deepseek-v4-pro-ga",
	}
	for key, want := range cases {
		if got := slugify("Some Name", key); got != want {
			t.Fatalf("slugify(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestReadRate(t *testing.T) {
	for _, text := range []string{"0.01x", "限时优惠 0.47X", "0.80×", "1.48 倍"} {
		if _, _, got := readRate([]string{text}); got == "" {
			t.Fatalf("rate not parsed from %q", text)
		}
	}
}
