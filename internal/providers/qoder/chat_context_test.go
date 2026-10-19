package qoder

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

// The gateway resolves the thinking channel and the routed model from
// chat_context.extra.modelConfig. An empty extra block makes it answer with no
// reasoning_content stream even when model_config.is_reasoning and
// parameters.enable_thinking are set. These fields mirror the reference Qoder
// client (and the working Qoder-2API-Go), so guard them against regression.
func TestBuildPlainChatBodyFillsChatContextForReasoning(t *testing.T) {
	raw := `{
		"model":"qwen3.8-flash","max_tokens":40,"reasoning_effort":"xhigh",
		"messages":[
			{"role":"system","content":"be nice"},
			{"role":"user","content":"你好"},
			{"role":"user","content":"再说一次"}
		]
	}`
	var req translate.ChatRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	plain := BuildPlainChatBody(req, time.Now())

	var body struct {
		Source      any    `json:"source"`
		Version     string `json:"version"`
		SessionType string `json:"session_type"`
		ChatContext struct {
			Text struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"text"`
			Extra struct {
				ModelConfig struct {
					Key         string `json:"key"`
					IsReasoning bool   `json:"is_reasoning"`
				} `json:"modelConfig"`
				OriginalContent struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"originalContent"`
			} `json:"extra"`
		} `json:"chat_context"`
		ModelConfig struct {
			Key         string `json:"key"`
			IsReasoning bool   `json:"is_reasoning"`
		} `json:"model_config"`
		Parameters struct {
			EnableThinking bool `json:"enable_thinking"`
		} `json:"parameters"`
	}
	encoded, _ := json.Marshal(plain)
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatal(err)
	}

	// source must be the integer 1 the official client sends, not "cli".
	if num, ok := body.Source.(float64); !ok || num != 1 {
		t.Fatalf("source = %#v, want numeric 1", body.Source)
	}
	if body.Version != "3" {
		t.Fatalf("version = %q, want 3", body.Version)
	}
	if body.SessionType != "qodercli" {
		t.Fatalf("session_type = %q, want qodercli", body.SessionType)
	}

	// chat_context must carry the latest user prompt and the model identity.
	if body.ChatContext.Text.Text != "再说一次" || body.ChatContext.Text.Type != "text" {
		t.Fatalf("chat_context.text = %+v, want the latest user prompt", body.ChatContext.Text)
	}
	if body.ChatContext.Extra.OriginalContent.Text != "再说一次" {
		t.Fatalf("chat_context.extra.originalContent.text = %q", body.ChatContext.Extra.OriginalContent.Text)
	}
	if body.ChatContext.Extra.ModelConfig.Key != "qwen3.8-flash" {
		t.Fatalf("chat_context.extra.modelConfig.key = %q, want qwen3.8-flash", body.ChatContext.Extra.ModelConfig.Key)
	}
	if !body.ChatContext.Extra.ModelConfig.IsReasoning {
		t.Fatal("chat_context.extra.modelConfig.is_reasoning = false, want true for a non-none effort")
	}

	// The top-level copies must agree with the extra block.
	if body.ModelConfig.Key != "qwen3.8-flash" || !body.ModelConfig.IsReasoning {
		t.Fatalf("model_config = %+v, want key + is_reasoning true", body.ModelConfig)
	}
	if !body.Parameters.EnableThinking {
		t.Fatal("parameters.enable_thinking = false, want true")
	}
}

// A "none" effort must flip is_reasoning off in BOTH copies so the gateway does
// not start a thinking stream.
func TestBuildPlainChatBodyNoneEffortDisablesReasoningEverywhere(t *testing.T) {
	raw := `{
		"model":"qwen3.8-flash","reasoning_effort":"none",
		"messages":[{"role":"user","content":"hi"}]
	}`
	var req translate.ChatRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	plain := BuildPlainChatBody(req, time.Now())
	encoded, _ := json.Marshal(plain)

	var body struct {
		ChatContext struct {
			Extra struct {
				ModelConfig struct {
					IsReasoning bool `json:"is_reasoning"`
				} `json:"modelConfig"`
			} `json:"extra"`
		} `json:"chat_context"`
		ModelConfig struct {
			IsReasoning bool `json:"is_reasoning"`
		} `json:"model_config"`
		Parameters struct {
			EnableThinking bool `json:"enable_thinking"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatal(err)
	}
	if body.ChatContext.Extra.ModelConfig.IsReasoning {
		t.Fatal("chat_context.extra.modelConfig.is_reasoning = true, want false for none")
	}
	if body.ModelConfig.IsReasoning {
		t.Fatal("model_config.is_reasoning = true, want false for none")
	}
	if body.Parameters.EnableThinking {
		t.Fatal("parameters.enable_thinking = true, want false for none")
	}
}
