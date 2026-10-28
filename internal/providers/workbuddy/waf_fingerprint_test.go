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

func TestSanitizeWAFDSHFilerefParagraph(t *testing.T) {
	// Ported from workbuddy2api-hub #684 / v1.9.0 DshFileReferenceParagraphTests.
	a := "Tokens prefixed with @ are paths the user explicitly referenced."
	aFixed := "Tokens prefixed with @ are paths that the user explicitly referenced."
	b := "Relative paths resolve from the workspace root; absolute paths identify files or directories on the host."
	tail := " A trailing slash marks a directory: list it when its contents matter. " +
		"Anything else is a file: use the read tool when its contents are needed, " +
		"and do not claim to have inspected it before reading. " +
		"@\"...\" quotes a path containing spaces."
	para := a + " " + b + tail
	paraFixed := aFixed + " " + b + tail

	if got := sanitizeFingerprintText(para, true); got != paraFixed {
		t.Fatalf("full paragraph = %q want %q", got, paraFixed)
	}
	if got := sanitizeFingerprintText(para, true); !strings.Contains(got, b+tail) {
		t.Fatalf("B+tail must be preserved verbatim: %q", got)
	}
	if strings.Contains(sanitizeFingerprintText(para, true), " are paths the user ") {
		t.Fatal("unsanitized A still present")
	}
	once := sanitizeFingerprintText(para, true)
	if again := sanitizeFingerprintText(once, true); again != once {
		t.Fatalf("not idempotent: %q -> %q", once, again)
	}
	if got := sanitizeFingerprintText(a, true); got != aFixed {
		t.Fatalf("sentence A alone = %q want %q", got, aFixed)
	}
	if got := sanitizeFingerprintText(strings.ToLower(para), true); got != strings.ToLower(paraFixed) {
		t.Fatalf("case-insensitive = %q", got)
	}
	embedded := "You are a coding agent.\n\n" + para + "\n\nMore rules follow."
	wantEmbedded := "You are a coding agent.\n\n" + paraFixed + "\n\nMore rules follow."
	if got := sanitizeFingerprintText(embedded, true); got != wantEmbedded {
		t.Fatalf("embedded = %q", got)
	}
	for _, text := range []string{
		"mail me at foo@bar.com",
		"npm i @deepseek-ai/dsh",
		`see C:\work\@cache for details`,
	} {
		if got := sanitizeFingerprintText(text, true); got != text {
			t.Fatalf("unrelated rewritten: %q -> %q", text, got)
		}
	}

	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": para},
			map[string]any{"role": "developer", "content": para},
			map[string]any{"role": "assistant", "content": para},
			map[string]any{"role": "user", "content": para},
			map[string]any{"role": "tool", "content": para},
		},
	}
	sanitizeWAFKeywords(body)
	msgs := body["messages"].([]any)
	for i, role := range []string{"system", "developer", "assistant"} {
		if got := msgs[i].(map[string]any)["content"]; got != paraFixed {
			t.Fatalf("%s fileref = %q want fixed", role, got)
		}
	}
	if got := msgs[3].(map[string]any)["content"]; got != para {
		t.Fatalf("user must stay untouched: %q", got)
	}
	if got := msgs[4].(map[string]any)["content"]; got != para {
		t.Fatalf("tool must stay untouched: %q", got)
	}
}
