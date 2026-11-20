package kiro

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/gateway"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// sseLine builds one CreateResponse event the way the Kiro runtime writes it.
func sseLine(t *testing.T, event map[string]any) string {
	t.Helper()
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	return "data: " + string(encoded) + "\n"
}

// TestCreateResponseToolStreamMatchesAnthropicMessages is the contract for
// /v1/messages tool calls. Two parallel calls arrive interleaved: each
// function_call opens by item id, argument deltas name that item, and only
// the done event closes the call. The Anthropic relay must see one tool_use
// block per call, with the argument JSON split into input_json_delta pieces
// that concatenate back to the upstream object.
func TestCreateResponseToolStreamMatchesAnthropicMessages(t *testing.T) {
	var raw strings.Builder
	raw.WriteString(sseLine(t, map[string]any{"type": "response.output_text.delta", "delta": "looking"}))
	raw.WriteString(sseLine(t, map[string]any{
		"type": "response.output_item.added",
		"item": map[string]any{"type": "function_call", "id": "fc_read", "call_id": "call_read", "name": "Read"},
	}))
	raw.WriteString(sseLine(t, map[string]any{
		"type": "response.output_item.added",
		"item": map[string]any{"type": "function_call", "id": "fc_grep", "call_id": "call_grep", "name": "Grep"},
	}))
	// Argument fragments are interleaved and one of them carries no item id,
	// so it must continue the call that started last rather than the first.
	raw.WriteString(sseLine(t, map[string]any{"type": "response.function_call_arguments.delta", "item_id": "fc_read", "delta": `{"path":`}))
	raw.WriteString(sseLine(t, map[string]any{"type": "response.function_call_arguments.delta", "item_id": "fc_grep", "delta": `{"pattern":"tool`}))
	raw.WriteString(sseLine(t, map[string]any{"type": "response.function_call_arguments.delta", "delta": `Use"}`}))
	raw.WriteString(sseLine(t, map[string]any{"type": "response.function_call_arguments.delta", "item_id": "fc_read", "delta": `"/tmp/a"}`}))
	raw.WriteString(sseLine(t, map[string]any{"type": "response.output_item.done", "item": map[string]any{"type": "function_call", "id": "fc_read", "call_id": "call_read", "name": "Read", "status": "completed"}}))
	raw.WriteString(sseLine(t, map[string]any{"type": "response.output_item.done", "item": map[string]any{"type": "function_call", "id": "fc_grep", "call_id": "call_grep", "name": "Grep", "status": "completed"}}))
	raw.WriteString(sseLine(t, map[string]any{
		"type":     "response.completed",
		"response": map[string]any{"usage": map[string]any{"input_tokens": 11, "output_tokens": 7}},
	}))

	resp := writeOpenAIStream(io.NopCloser(strings.NewReader(raw.String())), "claude-sonnet-4.5", nil, nil, nil)
	defer resp.Body.Close()
	openai, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(openai), "data: [DONE]") {
		t.Fatalf("openai stream missing [DONE]:\n%s", openai)
	}

	var anthropic strings.Builder
	if _, err := gateway.RelayAnthropicStream(&anthropic, strings.NewReader(string(openai)), "req1", "claude-sonnet-4.5"); err != nil {
		t.Fatal(err)
	}
	events := parseSSE(t, anthropic.String())

	var starts []map[string]any
	indexToID := map[float64]string{}
	partials := map[string]string{}
	var stopReason string
	for _, event := range events {
		switch event["type"] {
		case "content_block_start":
			block, _ := event["content_block"].(map[string]any)
			if block["type"] != "tool_use" {
				continue
			}
			starts = append(starts, block)
			id := block["id"].(string)
			indexToID[event["index"].(float64)] = id
			partials[id] = ""
		case "content_block_delta":
			delta, _ := event["delta"].(map[string]any)
			if delta["type"] != "input_json_delta" {
				continue
			}
			id := indexToID[event["index"].(float64)]
			partials[id] += delta["partial_json"].(string)
		case "message_delta":
			messageDelta, _ := event["delta"].(map[string]any)
			stopReason, _ = messageDelta["stop_reason"].(string)
		}
	}
	if len(starts) != 2 {
		t.Fatalf("tool_use blocks = %d, want 2\n%s", len(starts), anthropic.String())
	}
	if starts[0]["id"] != "call_read" || starts[0]["name"] != "Read" {
		t.Fatalf("first tool = %#v", starts[0])
	}
	if starts[1]["id"] != "call_grep" || starts[1]["name"] != "Grep" {
		t.Fatalf("second tool = %#v", starts[1])
	}
	assertJSON(t, "Read", partials["call_read"], map[string]any{"path": "/tmp/a"})
	assertJSON(t, "Grep", partials["call_grep"], map[string]any{"pattern": "toolUse"})
	if stopReason != "tool_use" {
		t.Fatalf("stop_reason = %q, want tool_use", stopReason)
	}
}

// TestIncompleteToolCallIsNotEmitted: a call Kiro marks incomplete must not
// reach the client. Running a write tool with a partial argument object is worse
// than dropping the call.
func TestIncompleteToolCallIsNotEmitted(t *testing.T) {
	var raw strings.Builder
	raw.WriteString(sseLine(t, map[string]any{
		"type": "response.output_item.added",
		"item": map[string]any{"type": "function_call", "id": "fc_bad", "call_id": "call_bad", "name": "Write"},
	}))
	raw.WriteString(sseLine(t, map[string]any{"type": "response.function_call_arguments.delta", "item_id": "fc_bad", "delta": `{"path":"/tmp`}))
	raw.WriteString(sseLine(t, map[string]any{"type": "response.output_item.done", "item": map[string]any{"type": "function_call", "id": "fc_bad", "status": "incomplete"}}))
	raw.WriteString(sseLine(t, map[string]any{"type": "response.completed"}))

	resp := writeOpenAIStream(io.NopCloser(strings.NewReader(raw.String())), "claude-sonnet-4.5", nil, nil, nil)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "Write") || strings.Contains(string(body), "call_bad") {
		t.Fatalf("incomplete tool leaked into the openai stream:\n%s", body)
	}
}

// TestLegacyEventStreamToolFragments: generateAssistantResponse sends the same
// call as several AWS event-stream frames. Arguments concatenate, and the call
// is emitted only when the JSON is complete.
func TestLegacyEventStreamToolFragments(t *testing.T) {
	frames := []streamEvent{
		{Type: "toolUseEvent", Event: map[string]any{"name": "Read"}},
		{Type: "toolUseEvent", Event: map[string]any{"toolUseId": "call_read", "input": `{"path":`}},
		{Type: "toolUseEvent", Event: map[string]any{"input": `"/tmp/a"}`, "stop": true}},
	}
	var got []toolUse
	err := consumeEvents(sliceSource(frames), &streamCallbacks{OnToolUse: func(tool toolUse) { got = append(got, tool) }})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "call_read" || got[0].Name != "Read" {
		t.Fatalf("tools = %#v", got)
	}
	if got[0].Input["path"] != "/tmp/a" {
		t.Fatalf("input = %#v", got[0].Input)
	}
}

func TestBrokenToolJSONIsAnError(t *testing.T) {
	frames := []streamEvent{{Type: "toolUseEvent", Event: map[string]any{"toolUseId": "call_bad", "name": "Write", "input": `{"path":`, "stop": true}}}
	err := consumeEvents(sliceSource(frames), &streamCallbacks{OnToolUse: func(tool toolUse) { t.Fatalf("emitted %#v", tool) }})
	if err == nil || !strings.Contains(err.Error(), "Write") {
		t.Fatalf("err = %v", err)
	}
}

func TestRequestReplaysToolHistory(t *testing.T) {
	calls, _ := json.Marshal([]map[string]any{{
		"id": "call_read", "type": "function",
		"function": map[string]any{"name": "Read", "arguments": `{"path":"/tmp/a"}`},
	}})
	req := translate.ChatRequest{
		Model: "kiro/claude-sonnet-4.5",
		Messages: []translate.ChatMessage{
			{Role: "user", Content: "read it"},
			{Role: "assistant", ToolCalls: calls},
			{Role: "tool", ToolCallID: "call_read", Content: "file contents"},
		},
		Tools: json.RawMessage(`[{"type":"function","function":{"name":"Read","description":"read a file","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}}]`),
	}
	conv, err := buildConversation(req, Credential{Region: "us-east-1"}, "claude-sonnet-4.5")
	if err != nil {
		t.Fatal(err)
	}
	built := conv.responsesBody(true)
	if built.Model != "claude-sonnet-4.5" {
		t.Fatalf("model = %s", built.Model)
	}
	kinds := make([]string, 0, len(built.Input))
	for _, item := range built.Input {
		if item["type"] != nil {
			kinds = append(kinds, item["type"].(string))
			continue
		}
		kinds = append(kinds, item["role"].(string))
	}
	want := []string{"user", "function_call", "function_call_output"}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Fatalf("input kinds = %v", kinds)
	}
	if built.Input[1]["call_id"] != "call_read" || built.Input[1]["arguments"] != `{"path":"/tmp/a"}` {
		t.Fatalf("function_call = %#v", built.Input[1])
	}
	if built.Input[2]["output"] != "file contents" {
		t.Fatalf("tool output = %#v", built.Input[2])
	}
	if len(built.Tools) != 1 || built.Tools[0].Name != "read" || conv.Tools[0].ClientName != "Read" {
		t.Fatalf("tools = %#v", conv.Tools)
	}
}

func TestUnpairedToolHistoryIsNarrated(t *testing.T) {
	calls, _ := json.Marshal([]map[string]any{{
		"id": "call_old", "type": "function",
		"function": map[string]any{"name": "Read", "arguments": `{"path":"/tmp/a"}`},
	}})
	req := translate.ChatRequest{
		Messages: []translate.ChatMessage{
			{Role: "user", Content: "read it"},
			{Role: "assistant", Content: "looking", ToolCalls: calls},
			{Role: "user", Content: "now write a summary"},
		},
		Tools: json.RawMessage(`[{"type":"function","function":{"name":"Read"}}]`),
	}
	conv, err := buildConversation(req, Credential{}, "claude-sonnet-4.5")
	if err != nil {
		t.Fatal(err)
	}
	for _, turn := range conv.History {
		if turn.Assistant != nil && len(turn.Assistant.ToolUses) > 0 {
			t.Fatalf("unpaired call stayed structured: %#v", turn.Assistant.ToolUses)
		}
	}
	body := conv.responsesBody(false)
	encoded, _ := json.Marshal(body.Input)
	if !strings.Contains(string(encoded), "now write a summary") {
		t.Fatalf("input = %s", encoded)
	}
}

func TestSnakeCaseToolNameIsCamelCased(t *testing.T) {
	req := translate.ChatRequest{
		Messages: []translate.ChatMessage{{Role: "user", Content: "go"}},
		Tools:    json.RawMessage(`[{"type":"function","function":{"name":"read_file","parameters":{"type":"object"}}}]`),
	}
	conv, err := buildConversation(req, Credential{}, "claude-sonnet-4.5")
	if err != nil {
		t.Fatal(err)
	}
	if len(conv.Tools) != 1 || conv.Tools[0].Name != "readFile" || conv.Tools[0].ClientName != "read_file" {
		t.Fatalf("tool = %#v", conv.Tools)
	}
	if conv.clientNames()["readFile"] != "read_file" {
		t.Fatalf("names = %#v", conv.clientNames())
	}
}

func TestEmptyToolSchemaBecomesObject(t *testing.T) {
	req := translate.ChatRequest{
		Messages: []translate.ChatMessage{{Role: "user", Content: "go"}},
		Tools:    json.RawMessage(`[{"type":"function","function":{"name":"Read"}}]`),
	}
	conv, err := buildConversation(req, Credential{}, "claude-sonnet-4.5")
	if err != nil {
		t.Fatal(err)
	}
	params, _ := conv.Tools[0].Parameters.(map[string]any)
	if params["type"] != "object" {
		t.Fatalf("parameters = %#v", conv.Tools[0].Parameters)
	}
	if conv.Tools[0].Description != "Tool: read" {
		t.Fatalf("description = %q", conv.Tools[0].Description)
	}
}

func TestLongToolNameIsMappedBack(t *testing.T) {
	name := "mcp__filesystem__read_file"
	req := translate.ChatRequest{
		Messages: []translate.ChatMessage{{Role: "user", Content: "go"}},
		Tools:    json.RawMessage(`[{"type":"function","function":{"name":"` + name + `"}}]`),
	}
	conv, err := buildConversation(req, Credential{}, "claude-sonnet-4.5")
	if err != nil {
		t.Fatal(err)
	}
	if len(conv.Tools) != 1 || conv.Tools[0].Name == name || conv.Tools[0].ClientName != name {
		t.Fatalf("tool = %#v", conv.Tools)
	}
	if conv.clientNames()[conv.Tools[0].Name] != name {
		t.Fatalf("names = %#v", conv.clientNames())
	}
}

func TestToolChoiceNamesUndeclaredTool(t *testing.T) {
	req := translate.ChatRequest{
		Messages:   []translate.ChatMessage{{Role: "user", Content: "go"}},
		Tools:      json.RawMessage(`[{"type":"function","function":{"name":"Read"}}]`),
		ToolChoice: json.RawMessage(`{"type":"function","function":{"name":"Write"}}`),
	}
	if _, err := buildConversation(req, Credential{}, "claude-sonnet-4.5"); err == nil {
		t.Fatal("undeclared tool_choice was accepted")
	}
}

func TestAnthropicServerWebSearchIsMarkedForProvider(t *testing.T) {
	translated, err := translate.TranslateAnthropicMessages(translate.AnthropicMessagesRequest{
		Model:    "kiro/auto",
		Messages: []translate.AnthropicMessage{{Role: "user", Content: json.RawMessage(`"latest news"`)}},
		Tools:    json.RawMessage(`[{"type":"web_search_20250305","name":"web_search","max_uses":3}]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(translated.AnthropicServerTools) != 1 || translated.AnthropicServerTools[0] != "web_search_20250305" {
		t.Fatalf("server tools = %#v", translated.AnthropicServerTools)
	}
}

func TestAnthropicErrorToolResultAndImagePlaceholder(t *testing.T) {
	translated, err := translate.TranslateAnthropicMessages(translate.AnthropicMessagesRequest{
		Model: "kiro/auto",
		Messages: []translate.AnthropicMessage{
			{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"toolu_1","name":"inspect","input":{}}]`)},
			{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"toolu_1","is_error":true,"content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]}]`)},
		},
		Tools: json.RawMessage(`[{"name":"inspect","input_schema":{"type":"object"}}]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := buildConversation(translated, Credential{}, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if len(conv.Current.Results) != 1 || !conv.Current.Results[0].IsError || !strings.Contains(conv.Current.Results[0].Output, "image") {
		t.Fatalf("results = %#v", conv.Current.Results)
	}
	legacy := conv.legacyBody(Origin, false)
	ctx := legacy.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext
	if ctx == nil || len(ctx.ToolResults) != 1 || ctx.ToolResults[0].Status != "error" {
		t.Fatalf("legacy tool results = %#v", ctx)
	}
	if len(conv.Current.Images) != 1 || conv.Current.Images[0].Format != "png" {
		t.Fatalf("tool result image = %#v", conv.Current.Images)
	}
}

func TestAnthropicDisablesParallelToolsAndCleansSchema(t *testing.T) {
	translated, err := translate.TranslateAnthropicMessages(translate.AnthropicMessagesRequest{
		Model:      "kiro/auto",
		Messages:   []translate.AnthropicMessage{{Role: "user", Content: json.RawMessage(`"go"`)}},
		ToolChoice: json.RawMessage(`{"type":"any","disable_parallel_tool_use":true}`),
		Tools:      json.RawMessage(`[{"name":"read_file","input_schema":{"type":"object","additionalProperties":false,"required":[],"properties":{"path":{"type":"string","additionalProperties":false}}}}]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := buildConversation(translated, Credential{}, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if !conv.DisableParallel || !strings.Contains(conv.Current.Text, "at most one tool call") {
		t.Fatalf("directive = %q", conv.Current.Text)
	}
	params := conv.Tools[0].Parameters.(map[string]any)
	if _, ok := params["additionalProperties"]; ok {
		t.Fatalf("top-level additionalProperties survived: %#v", params)
	}
	if _, ok := params["required"]; ok {
		t.Fatalf("empty required survived: %#v", params)
	}
	path := params["properties"].(map[string]any)["path"].(map[string]any)
	if _, ok := path["additionalProperties"]; ok {
		t.Fatalf("nested additionalProperties survived: %#v", path)
	}
}

func TestMarkupRescueBecomesToolCall(t *testing.T) {
	filter := armMarkupFilter([]toolSpec{{Name: "Read", Parameters: map[string]any{
		"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}},
	}}})
	var raw strings.Builder
	raw.WriteString(sseLine(t, map[string]any{"type": "response.output_text.delta", "delta": "calling "}))
	raw.WriteString(sseLine(t, map[string]any{"type": "response.output_text.delta", "delta": `<function_calls><invoke name="Read"><parameter name="path">/tmp/a</parameter></invoke></function_calls>`}))
	raw.WriteString(sseLine(t, map[string]any{"type": "response.completed", "response": map[string]any{"usage": map[string]any{"input_tokens": 1, "output_tokens": 1}}}))
	resp := writeOpenAIStream(io.NopCloser(strings.NewReader(raw.String())), "claude-sonnet-4.5", nil, filter, nil)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"name":"Read"`) || strings.Contains(string(body), "function_calls") {
		t.Fatalf("markup was not rescued:\n%s", body)
	}
}

func sliceSource(frames []streamEvent) eventSource {
	return func() (streamEvent, error) {
		if len(frames) == 0 {
			return streamEvent{}, io.EOF
		}
		next := frames[0]
		frames = frames[1:]
		return next, nil
	}
}

func parseSSE(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var events []map[string]any
	scanner := bufio.NewScanner(strings.NewReader(raw))
	var data strings.Builder
	flush := func() {
		payload := strings.TrimSpace(data.String())
		data.Reset()
		if payload == "" {
			return
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			t.Fatalf("sse json: %v\n%s", err, payload)
		}
		events = append(events, event)
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		if value, ok := strings.CutPrefix(line, "data: "); ok {
			data.WriteString(value)
		}
	}
	flush()
	return events
}

func assertJSON(t *testing.T, name, raw string, want map[string]any) {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("%s arguments %q: %v", name, raw, err)
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("%s %s = %#v, want %#v (%s)", name, key, got[key], value, raw)
		}
	}
}
