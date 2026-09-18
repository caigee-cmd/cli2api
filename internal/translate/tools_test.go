package translate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeOpenAIToolsExpandsNamespaceAndDropsHostedShells(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"function","name":"lookup","description":"lookup","parameters":{"type":"object"}},
		{"type":"namespace","name":"mcp__computer-use","tools":[
			{"type":"function","name":"left_click","description":"click","parameters":{"type":"object","properties":{"x":{"type":"number"}}}},
			{"type":"function","function":{"name":"mcp__computer-use__type","description":"type","parameters":{"type":"object"}}}
		]},
		{"type":"mcp","server_label":"computer-use"},
		{"type":"web_search"},
		{"type":"web_search_preview"},
		{"type":"namespace","name":"mcp__empty","tools":[]},
		{"type":"custom","name":"exec","description":"run code","format":{"type":"grammar","syntax":"lark","definition":"start: \"x\""}}
	]`)
	got, err := NormalizeOpenAITools(raw)
	if err != nil {
		t.Fatal(err)
	}
	var tools []struct {
		Type     string `json:"type"`
		Function struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		} `json:"function"`
	}
	if err := json.Unmarshal(got, &tools); err != nil {
		t.Fatal(err)
	}
	want := []string{"lookup", "mcp__computer-use__left_click", "mcp__computer-use__type", "__codex_custom__exec"}
	if len(tools) != len(want) {
		t.Fatalf("tools=%v want %v", toolNames(tools), want)
	}
	for i, name := range want {
		if tools[i].Type != "function" || tools[i].Function.Name != name {
			t.Fatalf("tools[%d]=%#v want %q", i, tools[i], name)
		}
	}
	if string(tools[1].Function.Parameters) == "" || string(tools[1].Function.Parameters) == "null" {
		t.Fatalf("left_click parameters missing: %s", tools[1].Function.Parameters)
	}
}

func TestNormalizeOpenAIToolsExpandsNamespacedCustomTools(t *testing.T) {
	got, err := NormalizeOpenAITools(json.RawMessage(`[
		{"type":"namespace","name":"functions","tools":[
			{"type":"custom","name":"apply_patch","description":"Apply a patch","format":{"type":"grammar","syntax":"lark","definition":"start: patch"}}
		]}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	var tools []struct {
		Function struct {
			Name        string          `json:"name"`
			Parameters  json.RawMessage `json:"parameters"`
			Description string          `json:"description"`
		} `json:"function"`
	}
	if err := json.Unmarshal(got, &tools); err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Function.Name != "functions__codex_custom__apply_patch" {
		t.Fatalf("tools=%s", got)
	}
	if !strings.Contains(tools[0].Function.Description, "start: patch") {
		t.Fatalf("description=%q", tools[0].Function.Description)
	}
	namespace, name, ok := DecodeCustomToolName(tools[0].Function.Name)
	if !ok || namespace != "functions" || name != "apply_patch" {
		t.Fatalf("decoded=(%q,%q,%v)", namespace, name, ok)
	}
}

func TestTranslateResponsesAcceptsCustomToolChoice(t *testing.T) {
	chat, err := TranslateResponses(ResponsesRequest{
		Model:      "workbuddy/glm-5.2",
		Input:      json.RawMessage(`"hi"`),
		Tools:      json.RawMessage(`[{"type":"custom","name":"exec","format":{"type":"grammar","syntax":"lark","definition":"start: \"x\""}}]`),
		ToolChoice: json.RawMessage(`{"type":"custom","name":"exec"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(chat.Tools), `"name":"__codex_custom__exec"`) {
		t.Fatalf("tools=%s", chat.Tools)
	}
	if !strings.Contains(string(chat.ToolChoice), `"__codex_custom__exec"`) {
		t.Fatalf("tool_choice=%s", chat.ToolChoice)
	}
}

func TestTranslateResponsesDropsAutoChoiceWhenToolsNormalizeEmpty(t *testing.T) {
	chat, err := TranslateResponses(ResponsesRequest{
		Model:      "workbuddy/glm-5.2",
		Input:      json.RawMessage(`"hi"`),
		Tools:      json.RawMessage(`[{"type":"mcp","server_label":"codex_apps"}]`),
		ToolChoice: json.RawMessage(`"auto"`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(chat.Tools) != 0 || len(chat.ToolChoice) != 0 {
		t.Fatalf("tools=%q choice=%q", chat.Tools, chat.ToolChoice)
	}
	if err := ValidateChatRequest(&chat); err != nil {
		t.Fatal(err)
	}
}

func TestTranslateResponsesAcceptsNamespaceTools(t *testing.T) {
	chat, err := TranslateResponses(ResponsesRequest{
		Model: "workbuddy/glm-5.2",
		Input: json.RawMessage(`"hi"`),
		Tools: json.RawMessage(`[
			{"type":"namespace","name":"mcp__computer-use","tools":[
				{"type":"function","name":"left_click","parameters":{"type":"object"}}
			]},
			{"type":"web_search"}
		]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var tools []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(chat.Tools, &tools); err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Function.Name != "mcp__computer-use__left_click" {
		t.Fatalf("tools=%s", chat.Tools)
	}
}

func TestTranslateResponsesAttachesReasoningToAssistant(t *testing.T) {
	chat, err := TranslateResponses(ResponsesRequest{
		Model: "workbuddy/glm-5.2",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},
			{"type":"reasoning","id":"rs_1","status":"completed","summary":[{"type":"summary_text","text":"think"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"again"}]}
		]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(chat.Messages) != 3 {
		t.Fatalf("messages=%#v", chat.Messages)
	}
	if chat.Messages[0].Role != "user" || ContentToString(chat.Messages[0].Content) != "hi" {
		t.Fatalf("first=%#v", chat.Messages[0])
	}
	if chat.Messages[1].Role != "assistant" || ContentToString(chat.Messages[1].Content) != "hello" || chat.Messages[1].ReasoningContent != "think" {
		t.Fatalf("second=%#v", chat.Messages[1])
	}
	if chat.Messages[2].Role != "user" || ContentToString(chat.Messages[2].Content) != "again" {
		t.Fatalf("third=%#v", chat.Messages[2])
	}
}

func TestTranslateResponsesAttachesReasoningToFunctionCall(t *testing.T) {
	chat, err := TranslateResponses(ResponsesRequest{
		Model: "deepseek-v4.1-flash",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},
			{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"need tool"}]},
			{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"}
		]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(chat.Messages) != 2 {
		t.Fatalf("messages=%#v", chat.Messages)
	}
	if chat.Messages[1].Role != "assistant" || chat.Messages[1].ReasoningContent != "need tool" || len(chat.Messages[1].ToolCalls) == 0 {
		t.Fatalf("assistant=%#v", chat.Messages[1])
	}
}

func TestTranslateResponsesLiftsFunctionCallOutputImages(t *testing.T) {
	chat, err := TranslateResponses(ResponsesRequest{
		Model: "deepseek-v4.1-flash",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"look"}]},
			{"type":"function_call","call_id":"call_1","name":"screenshot","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":[
				{"type":"input_text","text":"captured"},
				{"type":"input_image","image_url":"data:image/png;base64,AAAA"}
			]}
		]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(chat.Messages) != 4 {
		t.Fatalf("messages=%#v", chat.Messages)
	}
	if chat.Messages[2].Role != "tool" || ContentToString(chat.Messages[2].Content) != "captured" || chat.Messages[2].ToolCallID != "call_1" {
		t.Fatalf("tool=%#v", chat.Messages[2])
	}
	if chat.Messages[3].Role != "user" {
		t.Fatalf("image lift=%#v", chat.Messages[3])
	}
	parts, ok := chat.Messages[3].Content.([]any)
	if !ok || len(parts) != 1 {
		t.Fatalf("image content=%#v", chat.Messages[3].Content)
	}
	image, ok := parts[0].(map[string]any)
	if !ok || image["type"] != "image_url" {
		t.Fatalf("image part=%#v", parts[0])
	}
}

func TestTranslateResponsesMergesReasoningMessageAndFunctionCall(t *testing.T) {
	chat, err := TranslateResponses(ResponsesRequest{
		Model: "deepseek-v4.1-flash",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},
			{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"plan tool use"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"checking"}]},
			{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"q\":\"x\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"result"},
			{"type":"reasoning","id":"rs_2","summary":[{"type":"summary_text","text":"answer now"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}
		]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(chat.Messages) != 4 {
		t.Fatalf("messages=%#v", chat.Messages)
	}
	first := chat.Messages[1]
	if first.Role != "assistant" || first.ReasoningContent != "plan tool use" || ContentToString(first.Content) != "checking" || len(first.ToolCalls) == 0 {
		t.Fatalf("tool turn=%#v tools=%s", first, first.ToolCalls)
	}
	if chat.Messages[2].Role != "tool" {
		t.Fatalf("tool result=%#v", chat.Messages[2])
	}
	second := chat.Messages[3]
	if second.Role != "assistant" || second.ReasoningContent != "answer now" || ContentToString(second.Content) != "done" {
		t.Fatalf("final=%#v", second)
	}
}

func toolNames(tools []struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Function.Name)
	}
	return names
}
