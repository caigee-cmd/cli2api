package workbuddy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizeWAFIdentityAndGUIRules(t *testing.T) {
	guiA := "You are interacting with the user through the DeepSeek Harness Web GUI whose URL the runtime context names."
	guiB := `When the user refers to "this page", "this GUI", or "this app" without naming another target, they mean this GUI.`
	gui := guiA + " " + guiB
	wantGUI := "You are interacting with the user through the DeepSeek Web GUI whose URL is named in the runtime context. " + guiB

	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "You are an AI agent powered by DeepSeek Harness."},
			map[string]any{"role": "developer", "content": "You are Codex Agent, built by OpenAI."},
			map[string]any{"role": "assistant", "content": "You are a coding agent powered by the deepseek-v4.1-flash model."},
			map[string]any{"role": "user", "content": "You are an AI agent powered by DeepSeek Harness."},
			map[string]any{"role": "tool", "content": gui},
			map[string]any{"role": "system", "content": gui},
		},
	}
	sanitizeWAFKeywords(body)
	msgs := body["messages"].([]any)

	if got := msgs[0].(map[string]any)["content"]; got != "You are a helpful AI assistant." {
		t.Fatalf("system identity = %q", got)
	}
	if got := msgs[1].(map[string]any)["content"]; got != "You are a helpful AI assistant." {
		t.Fatalf("developer identity = %q", got)
	}
	if got := msgs[2].(map[string]any)["content"]; got != "You are a helpful AI assistant." {
		t.Fatalf("assistant identity = %q", got)
	}
	if got := msgs[3].(map[string]any)["content"]; got != "You are an AI agent powered by DeepSeek Harness." {
		t.Fatalf("user must stay untouched: %q", got)
	}
	if got := msgs[4].(map[string]any)["content"]; got != gui {
		t.Fatalf("tool must stay untouched: %q", got)
	}
	if got := msgs[5].(map[string]any)["content"]; got != wantGUI {
		t.Fatalf("system GUI = %q want %q", got, wantGUI)
	}
}

func TestSanitizeWAFBrandLeavesHyphenatedIdentifiers(t *testing.T) {
	for _, text := range []string{
		"github.com/deepseek-ai/deepseek-harness/tree/main",
		"import deepseek_harness as dsh",
		"/opt/deepseek-harness/app",
	} {
		if got := sanitizeFingerprintText(text, true); got != text {
			t.Fatalf("identifier rewritten: %q -> %q", text, got)
		}
	}
	if got := sanitizeFingerprintText("the DeepSeek Harness project", true); got != "the DeepSeek project" {
		t.Fatalf("brand = %q", got)
	}
}

func TestSanitizeWAFIdentityDoesNotOverMatch(t *testing.T) {
	for _, text := range []string{
		"You are an AI agent.",
		"You are free to use tools built by the user.",
	} {
		if got := sanitizeFingerprintText(text, true); got != text {
			t.Fatalf("over-matched %q -> %q", text, got)
		}
	}
}

func TestPrepareBodyAppliesWAFIdentityRules(t *testing.T) {
	src, _ := json.Marshal(map[string]any{
		"model": "wb",
		"messages": []any{
			map[string]any{"role": "system", "content": "You are an AI assistant built on DeepSeek."},
			map[string]any{"role": "user", "content": "hi"},
		},
	})
	out := PrepareBody(src)
	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatal(err)
	}
	msgs := body["messages"].([]any)
	if got := msgs[0].(map[string]any)["content"].(string); got != "You are a helpful AI assistant." {
		t.Fatalf("prepared system = %q", got)
	}
	if got := msgs[1].(map[string]any)["content"].(string); got != "hi" {
		t.Fatalf("prepared user = %q", got)
	}
	if strings.Contains(string(out), "built on DeepSeek") {
		t.Fatalf("identity fingerprint survived: %s", out)
	}
}
