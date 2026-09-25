package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func TestBuildNativeBodyPreservesFieldsAndAppliesAccountPolicy(t *testing.T) {
	req, err := translate.ParseNativeResponses([]byte(`{
		"model":"codex/gpt-5.5",
		"stream":false,
		"instructions":"keep?",
		"temperature":0.2,"max_output_tokens":100,"user":"someone","context_management":{"compaction":{}},
		"input":[
			{"type":"message","role":"system","content":"drop"},
			{"type":"message","role":"user","content":"keep","x_item":7},
			{"type":"reasoning","id":"rs_1","encrypted_content":"opaque","summary":[]},
			{"type":"function_call","id":"fc_1","call_id":"call_1","namespace":"ns","name":"tool","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":"ok"}
		],
		"tools":[{"type":"namespace","name":"ns","tools":[{"type":"function","name":"tool"}]}],
		"tool_choice":"auto","text":{"format":{"type":"text"}},"include":["file_search_call.results"],"store":true,
		"reasoning":{"effort":"max","summary":"auto"},"x_future":{"n":2.50}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	body, resolved, err := buildNativeBody(req, providers.RequestOptions{Model: "gpt-5.5", DropSystemPrompt: true}, providers.ModelCapabilities{ReasoningOptions: []string{"low", "medium", "high"}}, "session-key")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if string(got["model"]) != `"gpt-5.5"` || string(got["stream"]) != "true" || string(got["instructions"]) != `""` || string(got["store"]) != "false" {
		t.Fatalf("rewrites = model:%s stream:%s instructions:%s store:%s", got["model"], got["stream"], got["instructions"], got["store"])
	}
	if string(got["include"]) != `["reasoning.encrypted_content"]` {
		t.Fatalf("include = %s", got["include"])
	}
	for _, field := range []string{"temperature", "max_output_tokens", "user", "context_management"} {
		if _, ok := got[field]; ok {
			t.Errorf("rejected field %q was forwarded", field)
		}
	}
	if resolved.ReasoningLevel != "low" {
		t.Fatalf("reasoning level = %q", resolved.ReasoningLevel)
	}
	input := string(got["input"])
	if strings.Contains(input, `"role":"system"`) || !strings.Contains(input, `"encrypted_content":"opaque"`) || !strings.Contains(input, `"x_item":7`) {
		t.Fatalf("input policy/preservation failed: %s", input)
	}
	for _, field := range []string{"tools", "tool_choice", "text", "x_future"} {
		if _, ok := got[field]; !ok {
			t.Errorf("field %q dropped", field)
		}
	}
	if !strings.Contains(string(got["x_future"]), "2.50") && !strings.Contains(string(got["x_future"]), "2.5") {
		t.Fatalf("unknown member lost: %s", got["x_future"])
	}
	if string(got["prompt_cache_key"]) != `"session-key"` {
		t.Fatalf("prompt cache key = %s", got["prompt_cache_key"])
	}
}

func TestBuildNativeBodyCallerPromptCacheKeyWins(t *testing.T) {
	req, err := translate.ParseNativeResponses([]byte(`{"model":"m","input":"hi","prompt_cache_key":"caller"}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _, err := buildNativeBody(req, providers.RequestOptions{}, providers.ModelCapabilities{}, "generated")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if json.Unmarshal(body, &got) != nil || got["prompt_cache_key"] != "caller" {
		t.Fatalf("prompt cache key = %v", got["prompt_cache_key"])
	}
}

func TestBuildNativeBodyAppliesCodexUpstreamConstraints(t *testing.T) {
	req, err := translate.ParseNativeResponses([]byte(`{
		"model":"gpt-5.5","input":[
			{"type":"message","id":"item_74ec","role":"system","content":[{"type":"input_text","text":"rules","prompt_cache_breakpoint":{"ttl":"1h"}}]},
			{"type":"reasoning","id":"rs_orphan","summary":[]},
			{"type":"reasoning","id":"` + strings.Repeat("r", 70) + `","encrypted_content":"opaque","summary":[]},
			{"type":"function_call","id":"` + strings.Repeat("c", 70) + `","call_id":"call_1","name":"lookup","arguments":"{}"},
			{"role":"user","content":"continue"}
		],
		"tools":[{"type":"web_search_preview"},{"type":"function","name":"lookup","parameters":{"type":"object","properties":{"q":{"type":"string","pattern":"\\p{L}+"}}}}],
		"tool_choice":{"type":"web_search_preview"},
		"service_tier":"default","parallel_tool_calls":false
	}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _, err := buildNativeBody(req, providers.RequestOptions{Model: "not-a-catalog-model"}, providers.ModelCapabilities{}, "generated")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["service_tier"]; ok {
		t.Fatalf("unsupported service tier forwarded: %v", got["service_tier"])
	}
	if got["parallel_tool_calls"] != true {
		t.Fatalf("parallel_tool_calls = %v", got["parallel_tool_calls"])
	}
	tools, _ := json.Marshal(got["tools"])
	if !strings.Contains(string(tools), `"type":"web_search"`) || strings.Contains(string(tools), "web_search_preview") || strings.Contains(string(tools), `\p{L}`) {
		t.Fatalf("tools = %s", tools)
	}
	choice, _ := json.Marshal(got["tool_choice"])
	if string(choice) != `{"type":"web_search"}` {
		t.Fatalf("tool_choice = %s", choice)
	}
	input, _ := json.Marshal(got["input"])
	text := string(input)
	if strings.Contains(text, `"role":"system"`) || strings.Contains(text, "prompt_cache_breakpoint") || strings.Contains(text, "rs_orphan") || strings.Contains(text, strings.Repeat("r", 70)) {
		t.Fatalf("input constraints missed: %s", text)
	}
	if !strings.Contains(text, `"role":"developer"`) || !strings.Contains(text, `"id":"msg_item_74ec"`) {
		t.Fatalf("message item was not normalized: %s", text)
	}
	if !strings.Contains(text, `"call_id":"call_1"`) {
		t.Fatalf("call linkage lost: %s", text)
	}
}
