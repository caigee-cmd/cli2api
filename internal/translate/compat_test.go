package translate

import (
	"encoding/json"
	"testing"
)

func TestTranslateResponsesUnquotesFunctionCallArguments(t *testing.T) {
	request := ResponsesRequest{
		Model: "qoder/glm-5.2",
		Input: json.RawMessage(`[{
			"type":"function_call",
			"call_id":"call_1",
			"name":"weather",
			"arguments":"{\"city\":\"Shanghai\"}"
		}]`),
	}
	chat, err := TranslateResponses(request)
	if err != nil {
		t.Fatal(err)
	}
	var calls []struct {
		Function struct {
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	if err := json.Unmarshal(chat.Messages[0].ToolCalls, &calls); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Function.Arguments != `{"city":"Shanghai"}` {
		t.Fatalf("calls=%s", chat.Messages[0].ToolCalls)
	}
}

func TestTranslateAnthropicToolResultLiftsImages(t *testing.T) {
	request := AnthropicMessagesRequest{
		Model: "qoder/glm-5.2",
		Messages: []AnthropicMessage{
			{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"toolu_1","name":"inspect","input":{}}]`)},
			{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"text","text":"done"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]}]`)},
		},
	}
	chat, err := TranslateAnthropicMessages(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(chat.Messages) != 3 || chat.Messages[1].Role != "tool" || chat.Messages[2].Role != "user" {
		t.Fatalf("messages=%#v", chat.Messages)
	}
	parts, ok := chat.Messages[2].Content.([]any)
	if !ok || len(parts) != 1 {
		t.Fatalf("image content=%#v", chat.Messages[2].Content)
	}
	image, ok := parts[0].(map[string]any)
	if !ok || image["type"] != "image_url" {
		t.Fatalf("image part=%#v", parts[0])
	}
}
