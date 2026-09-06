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

func TestTranslatedImageOnlyRequestsProduceContentSessionSeed(t *testing.T) {
	anthropic, err := TranslateAnthropicMessages(AnthropicMessagesRequest{
		Model: "glm-5.2",
		Messages: []AnthropicMessage{
			{Role: "user", Content: json.RawMessage(`[{"type":"image","source":{"type":"url","url":"https://example.com/cat.png"}}]`)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	anthropicSeed := ContentSessionSeed(anthropic)
	if anthropicSeed == "" {
		t.Fatalf("anthropic image-only seed empty; content=%#v", anthropic.Messages[0].Content)
	}
	anthropicLater, err := TranslateAnthropicMessages(AnthropicMessagesRequest{
		Model: "glm-5.2",
		Messages: []AnthropicMessage{
			{Role: "user", Content: json.RawMessage(`[{"type":"image","source":{"type":"url","url":"https://example.com/cat.png"}}]`)},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"a cat"}]`)},
			{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"what color?"}]`)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ContentSessionSeed(anthropicLater) != anthropicSeed {
		t.Fatalf("anthropic later turn changed seed:\nfirst=%q\nlater=%q", anthropicSeed, ContentSessionSeed(anthropicLater))
	}

	responses, err := TranslateResponses(ResponsesRequest{
		Model: "glm-5.2",
		Input: json.RawMessage(`[{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/cat.png"}]}]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	responsesSeed := ContentSessionSeed(responses)
	if responsesSeed == "" {
		t.Fatalf("responses image-only seed empty; content=%#v", responses.Messages[0].Content)
	}
	responsesLater, err := TranslateResponses(ResponsesRequest{
		Model: "glm-5.2",
		Input: json.RawMessage(`[
				{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/cat.png"}]},
				{"role":"assistant","content":[{"type":"output_text","text":"a cat"}]},
				{"role":"user","content":[{"type":"input_text","text":"what color?"}]}
			]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if ContentSessionSeed(responsesLater) != responsesSeed {
		t.Fatalf("responses later turn changed seed:\nfirst=%q\nlater=%q", responsesSeed, ContentSessionSeed(responsesLater))
	}

	otherAnthropic, err := TranslateAnthropicMessages(AnthropicMessagesRequest{
		Model: "glm-5.2",
		Messages: []AnthropicMessage{
			{Role: "user", Content: json.RawMessage(`[{"type":"image","source":{"type":"url","url":"https://example.com/dog.png"}}]`)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ContentSessionSeed(otherAnthropic) == anthropicSeed {
		t.Fatal("different anthropic images must not share a seed")
	}
}
