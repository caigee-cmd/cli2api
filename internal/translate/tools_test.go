package translate

import (
	"encoding/json"
	"strings"
	"testing"
)

const namespaceTestTools = `[{"type":"namespace","name":"mcp__fastctx","tools":[{"type":"function","name":"glob","parameters":{"type":"object"}}]}]`

func TestResponsesNamespaceMappingMatchesNormalization(t *testing.T) {
	for _, raw := range []string{
		`[{"type":" NAMESPACE ","name":" mcp__fastctx ","tools":[{"type":" FUNCTION ","function":{"name":"glob"}}]}]`,
		`[{"type":"namespace","name":"mcp__fastctx","tools":null}]`,
		`[{"type":"namespace","name":"mcp__fastctx","tools":{}}]`,
	} {
		names, err := responseToolNames(json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		normalized, err := NormalizeOpenAITools(json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		var tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if len(normalized) > 0 {
			if err := json.Unmarshal(normalized, &tools); err != nil {
				t.Fatal(err)
			}
		}
		if len(names) != len(tools) {
			t.Fatalf("mapping=%v tools=%s", names, normalized)
		}
		for _, tool := range tools {
			if names[tool.Function.Name] != (ResponseToolName{Namespace: "mcp__fastctx", Name: "glob"}) {
				t.Fatal(names)
			}
		}
	}
}

func TestResponsesNamespaceHistoryAndChoice(t *testing.T) {
	var source ResponsesRequest
	if err := json.Unmarshal([]byte(`{"model":"test","input":[{"role":"user","content":"find"},{"type":"function_call","namespace":"mcp__fastctx","name":"glob","call_id":"call_1","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":"found"}],"tool_choice":{"type":"function","namespace":"mcp__fastctx","name":"glob"}}`), &source); err != nil {
		t.Fatal(err)
	}
	source.Tools = json.RawMessage(namespaceTestTools)
	chat, err := TranslateResponses(source)
	if err != nil {
		t.Fatal(err)
	}
	if chat.ResponseToolNames["mcp__fastctx__glob"] != (ResponseToolName{Namespace: "mcp__fastctx", Name: "glob"}) {
		t.Fatal(chat.ResponseToolNames)
	}
	if !strings.Contains(string(chat.Messages[1].ToolCalls), `"name":"mcp__fastctx__glob"`) {
		t.Fatal(string(chat.Messages[1].ToolCalls))
	}
	if !strings.Contains(string(chat.ToolChoice), `"name":"mcp__fastctx__glob"`) {
		t.Fatal(string(chat.ToolChoice))
	}
	if chat.Messages[2].ToolCallID != "call_1" {
		t.Fatal(chat.Messages)
	}
	encoded, _ := json.Marshal(chat)
	if strings.Contains(string(encoded), "ResponseToolNames") || strings.Contains(string(encoded), `"Namespace"`) {
		t.Fatal("internal metadata leaked")
	}
}

func TestResponsesNamespaceAdditionalTools(t *testing.T) {
	var source ResponsesRequest
	if err := json.Unmarshal([]byte(`{"model":"test","input":[{"role":"user","content":"find"},{"type":"additional_tools","tools":`+namespaceTestTools+`}]}`), &source); err != nil {
		t.Fatal(err)
	}
	chat, err := TranslateResponses(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(chat.ResponseToolNames) != 1 {
		t.Fatalf("additional tools mapping=%v", chat.ResponseToolNames)
	}
}

func TestResponsesNamespaceCollision(t *testing.T) {
	for _, extra := range []string{
		`{"type":"function","name":"mcp__fastctx__glob"}`,
		`{"type":"namespace","name":"other","tools":[{"type":"function","name":"mcp__fastctx__glob"}]}`,
	} {
		raw := strings.TrimSuffix(namespaceTestTools, "]") + "," + extra + "]"
		if _, err := responseToolNames(json.RawMessage(raw)); err == nil {
			t.Fatalf("expected collision: %s", raw)
		}
	}
}

func TestRestoreNamespaceUsesDeclarationsOnly(t *testing.T) {
	names, err := responseToolNames(json.RawMessage(`[{"type":"namespace","name":"one","tools":[{"type":"function","name":"lookup"}]},{"type":"namespace","name":"two","tools":[{"type":"function","name":"lookup"}]},{"type":"function","name":"mcp__flat__glob"}]`))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one__lookup", "two__lookup", "mcp__flat__glob"} {
		item := map[string]any{"type": "function_call", "name": name, "arguments": `{"name":"one__lookup"}`}
		RestoreResponseToolNames(item, names)
		if identity, ok := names[name]; ok {
			if item["name"] != identity.Name || item["namespace"] != identity.Namespace {
				t.Fatal(item)
			}
		} else if item["name"] != name || item["namespace"] != nil {
			t.Fatal(item)
		}
		if item["arguments"] != `{"name":"one__lookup"}` {
			t.Fatal("arguments changed")
		}
	}
}

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
		{"type":"custom","name":"ignored"}
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
	want := []string{"lookup", "mcp__computer-use__left_click", "mcp__computer-use__type"}
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
