package trae

import (
	"encoding/json"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func TestNormalizeReasoningLevelAliases(t *testing.T) {
	cases := map[string]string{
		"extra_high": "xhigh",
		"Extra high": "xhigh",
		"light":      "low",
		"off":        "none",
		"HIGH":       "high",
	}
	for input, want := range cases {
		if got := normalizeReasoningLevel(input); got != want {
			t.Fatalf("normalizeReasoningLevel(%q)=%q want %q", input, got, want)
		}
	}
}

func TestClampReasoningLevelDropsUnknownAndFallsBack(t *testing.T) {
	caps := providers.ModelCapabilities{ReasoningOptions: []string{"low", "medium"}, ReasoningDefault: "low"}
	if got := clampReasoningLevel("xhigh", caps); got != "medium" {
		t.Fatalf("clamp=%q", got)
	}
	if got := clampReasoningLevel("high", providers.ModelCapabilities{}); got != "" {
		t.Fatalf("no options should drop: %q", got)
	}
}

func TestApplySoloChatFieldsMapsOpenAIAndStripsQoderKeys(t *testing.T) {
	on := true
	req := translate.ChatRequest{
		EnableThinking:  &on,
		ReasoningEffort: json.RawMessage(`"extra_high"`),
	}
	obj := map[string]any{
		"context_length": 500000, "max_input_tokens": 500000, "enable_thinking": true,
	}
	caps := providers.ModelCapabilities{
		MaxMode: true, ReasoningOptions: []string{"low", "medium", "high", "xhigh"}, ReasoningDefault: "medium",
	}
	applySoloChatFields(obj, req, true, "", caps, "", "")
	if obj["is_max_mode"] != 1 {
		t.Fatalf("is_max_mode=%v", obj["is_max_mode"])
	}
	if obj["reasoning_effort_level"] != "xhigh" {
		t.Fatalf("level=%v", obj["reasoning_effort_level"])
	}
	for _, key := range []string{"context_length", "max_input_tokens", "enable_thinking", "reasoning_effort"} {
		if _, ok := obj[key]; ok {
			t.Fatalf("leaked %s", key)
		}
	}
}

func TestApplySoloChatFieldsOmitsMaxWhenUnsupported(t *testing.T) {
	obj := map[string]any{}
	applySoloChatFields(obj, translate.ChatRequest{}, true, "", providers.ModelCapabilities{}, "", "")
	if _, ok := obj["is_max_mode"]; ok {
		t.Fatal("unsupported max mode should not be sent")
	}
}

func TestApplySoloChatFieldsUsesStoredLevelWhenRequestOmitsEffort(t *testing.T) {
	obj := map[string]any{}
	caps := providers.ModelCapabilities{ReasoningOptions: []string{"low", "high", "xhigh"}, ReasoningDefault: "high"}
	applySoloChatFields(obj, translate.ChatRequest{}, false, "xhigh", caps, "", "")
	if obj["reasoning_effort_level"] != "xhigh" {
		t.Fatalf("stored level=%v", obj["reasoning_effort_level"])
	}
	applySoloChatFields(obj, translate.ChatRequest{}, false, "", caps, "", "")
	if obj["reasoning_effort_level"] != "high" {
		t.Fatalf("catalog default=%v", obj["reasoning_effort_level"])
	}
}

func TestConversationIDReusedForOneAccountAndSession(t *testing.T) {
	first := conversationIDFor("u1", "session-a")
	second := conversationIDFor("u1", "session-a")
	if first != second {
		t.Fatalf("one account and session must reuse its id: %s vs %s", first, second)
	}
	if len(first) != 32 {
		t.Fatalf("conversation id must be 32 hex, got %q", first)
	}
	if conversationIDFor("u2", "session-a") == first {
		t.Fatal("two accounts must not share a conversation id")
	}
	if conversationIDFor("u1", "session-b") == first {
		t.Fatal("two sessions must not share a conversation id")
	}
	fresh := conversationIDFor("u1", "")
	again := conversationIDFor("u1", "  ")
	if fresh == again {
		t.Fatal("a request without a session must get a new conversation id")
	}
}

func TestApplySoloChatFieldsPinsConversationToSession(t *testing.T) {
	obj := map[string]any{}
	applySoloChatFields(obj, translate.ChatRequest{}, false, "", providers.ModelCapabilities{}, "u1", "session-a")
	again := map[string]any{}
	applySoloChatFields(again, translate.ChatRequest{}, false, "", providers.ModelCapabilities{}, "u1", "session-a")
	if obj["conversation_id"] == "" || obj["conversation_id"] != obj["session_id"] {
		t.Fatalf("conversation and session must be one id: %v", obj)
	}
	if obj["conversation_id"] != again["conversation_id"] {
		t.Fatal("the same chat must keep its conversation id")
	}
	if obj["user_id"] != "u1" {
		t.Fatalf("user_id=%v", obj["user_id"])
	}
	other := map[string]any{}
	applySoloChatFields(other, translate.ChatRequest{}, false, "", providers.ModelCapabilities{}, "u1", "session-b")
	if other["conversation_id"] == obj["conversation_id"] {
		t.Fatal("a different session must not reuse the conversation id")
	}
}
