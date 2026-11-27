package command

import (
	"encoding/json"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

func rawMessage(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

func TestBuildGenerateRequestEnvelopeAndMessages(t *testing.T) {
	req := translate.ChatRequest{
		Model:     "deepseek/deepseek-v4-pro",
		MaxTokens: rawMessage(4096),
		Messages: []translate.ChatMessage{
			{Role: "system", Content: "be terse"},
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "sure", ToolCalls: rawMessage([]map[string]any{{
				"id": "call_1", "type": "function",
				"function": map[string]any{"name": "read_file", "arguments": `{"path":"a"}`},
			}})},
			{Role: "tool", ToolCallID: "call_1", Content: "file body"},
		},
		Tools: rawMessage([]map[string]any{{
			"type": "function",
			"function": map[string]any{
				"name": "read_file", "description": "Read a file.",
				"parameters": map[string]any{"type": "object", "properties": map[string]any{}},
			},
		}}),
	}

	payload, err := buildGenerateRequest(req, "deepseek/deepseek-v4-pro")
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	var envelope struct {
		Config         map[string]any `json:"config"`
		Memory         any            `json:"memory"`
		Taste          any            `json:"taste"`
		Skills         any            `json:"skills"`
		PermissionMode string         `json:"permissionMode"`
		Params         struct {
			Model string `json:"model"`
			// System is the CLI's text-block array, not a bare string.
			System []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"system"`
			Stream    bool `json:"stream"`
			MaxTokens int  `json:"max_tokens"`
			Tools     []struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				InputSchema json.RawMessage `json:"input_schema"`
			} `json:"tools"`
			Messages []struct {
				Role    string           `json:"role"`
				Content []map[string]any `json:"content"`
			} `json:"messages"`
		} `json:"params"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, payload)
	}

	// Strict config envelope: every field must be present.
	for _, key := range []string{"workingDir", "date", "environment", "structure", "isGitRepo", "currentBranch", "mainBranch", "gitStatus", "recentCommits"} {
		if _, ok := envelope.Config[key]; !ok {
			t.Errorf("config missing required field %q", key)
		}
	}
	if envelope.Memory != "" {
		t.Errorf("memory = %v, want empty string", envelope.Memory)
	}
	if envelope.Taste != nil || envelope.Skills != nil {
		t.Error("taste/skills must be null")
	}
	if envelope.PermissionMode != "standard" {
		t.Errorf("permissionMode = %q", envelope.PermissionMode)
	}
	if !envelope.Params.Stream {
		t.Error("stream must always be true (the gateway rejects stream:false)")
	}
	if len(envelope.Params.System) != 1 || envelope.Params.System[0].Text != "be terse" {
		t.Errorf("system = %+v", envelope.Params.System)
	}
	if envelope.Params.MaxTokens != 4096 {
		t.Errorf("max_tokens = %d", envelope.Params.MaxTokens)
	}

	// Tools use snake_case input_schema.
	if len(envelope.Params.Tools) != 1 || envelope.Params.Tools[0].Name != "read_file" {
		t.Fatalf("tools = %+v", envelope.Params.Tools)
	}
	if len(envelope.Params.Tools[0].InputSchema) == 0 {
		t.Error("input_schema missing")
	}

	// Roles: system folds into params.system; assistant tool-call uses camelCase
	// keys; tool result is a separate role:"tool" message.
	var sawAssistantCall, sawToolResult bool
	for _, msg := range envelope.Params.Messages {
		for _, part := range msg.Content {
			switch part["type"] {
			case "tool-call":
				sawAssistantCall = true
				if _, ok := part["toolCallId"]; !ok {
					t.Error("tool-call must use camelCase toolCallId")
				}
				if _, ok := part["toolName"]; !ok {
					t.Error("tool-call must carry toolName")
				}
				if _, ok := part["input"]; !ok {
					t.Error("tool-call must carry input object")
				}
			case "tool-result":
				if msg.Role != "tool" {
					t.Errorf("tool-result must live in a role:\"tool\" message, got %q", msg.Role)
				}
				output, _ := part["output"].(map[string]any)
				if output["type"] != "text" || output["value"] != "file body" {
					t.Errorf("tool-result output = %+v", output)
				}
				sawToolResult = true
			}
		}
	}
	if !sawAssistantCall || !sawToolResult {
		t.Errorf("tool roundtrip missing: call=%v result=%v", sawAssistantCall, sawToolResult)
	}

	// No system role should survive in messages.
	for _, msg := range envelope.Params.Messages {
		if msg.Role == "system" {
			t.Error("system role leaked into messages")
		}
	}
}

func TestBuildMessagesMergesConsecutiveToolResults(t *testing.T) {
	_, messages := buildMessages([]translate.ChatMessage{
		{Role: "assistant", ToolCalls: rawMessage([]map[string]any{
			{"id": "c1", "type": "function", "function": map[string]any{"name": "a", "arguments": "{}"}},
			{"id": "c2", "type": "function", "function": map[string]any{"name": "b", "arguments": "{}"}},
		})},
		{Role: "tool", ToolCallID: "c1", Content: "one"},
		{Role: "tool", ToolCallID: "c2", Content: "two"},
	})
	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2 (assistant + merged tool)", len(messages))
	}
	if messages[1].Role != "tool" || len(messages[1].Content) != 2 {
		t.Fatalf("consecutive tool results must merge: %+v", messages[1])
	}
}

func TestBuildToolsDropsHostedShells(t *testing.T) {
	tools := buildTools(rawMessage([]map[string]any{
		{"type": "web_search"},
		{"type": "function", "function": map[string]any{"name": "ok", "parameters": map[string]any{"type": "object"}}},
	}))
	if len(tools) != 1 || tools[0].Name != "ok" {
		t.Fatalf("tools = %+v", tools)
	}
}

func TestBuildToolsAlwaysArray(t *testing.T) {
	if tools := buildTools(nil); tools == nil || len(tools) != 0 {
		t.Fatalf("no tools must serialize as an empty array, got %#v", tools)
	}
}

func TestParseReasoningEffort(t *testing.T) {
	cases := map[string]string{
		`"high"`:             "high",
		`"none"`:             "",
		`{"type":"medium"}`:  "medium",
		`{"effort":"xhigh"}`: "xhigh",
		`"nonsense"`:         "",
	}
	for raw, want := range cases {
		req := translate.ChatRequest{}
		if len(raw) > 0 && raw[0] == '{' {
			req.Thinking = json.RawMessage(raw)
		} else {
			req.ReasoningEffort = json.RawMessage(raw)
		}
		if got := parseReasoningEffort(req); got != want {
			t.Errorf("parseReasoningEffort(%s) = %q, want %q", raw, got, want)
		}
	}
}

// TestGenerateBodyOmitsUnprovenEnvelopeFields locks the encoded body to exactly
// what main sent before the version bump, apart from x-command-code-version.
//
// CodeArts/Command's /alpha/generate has no public contract and upstream keys it
// on x-command-code-version, so every body field is a behavioural claim. The
// struct keeps mode/threadId/promptCache/subagent for a future capture, but until
// there is a capture of the official CLI's *non-interactive* call, none of them
// may be set: guessing "agent" from the interactive TUI default would invent a
// mode we cannot justify.
//
// Decoding into map[string]json.RawMessage rather than a struct is deliberate:
// omitempty on a struct would conflate "sent empty" with "not sent".
func TestGenerateBodyOmitsUnprovenEnvelopeFields(t *testing.T) {
	raw, err := buildGenerateRequest(chatRequestWithSystem("be terse"), "deepseek/deepseek-v4.1-flash")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, raw)
	}
	for _, field := range []string{"mode", "threadId", "promptCache", "subagent"} {
		if _, present := top[field]; present {
			t.Errorf("envelope must not send %q: %s", field, raw)
		}
	}
	// The required config envelope is unchanged; a missing path is a 400 upstream.
	var config map[string]json.RawMessage
	if err := json.Unmarshal(top["config"], &config); err != nil {
		t.Fatalf("config: %v", err)
	}
	for _, key := range []string{"workingDir", "date", "environment", "structure", "isGitRepo", "currentBranch", "mainBranch", "gitStatus", "recentCommits"} {
		if _, ok := config[key]; !ok {
			t.Errorf("config missing required field %q", key)
		}
	}
	if got := string(top["permissionMode"]); got != `"standard"` {
		t.Errorf("permissionMode = %s, want \"standard\"", got)
	}
	// And the version header the bump actually changes is not in the body at all.
	if _, present := top["x-command-code-version"]; present {
		t.Error("version belongs in the request header, not the body")
	}
}
