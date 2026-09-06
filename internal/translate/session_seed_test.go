package translate

import (
	"encoding/json"
	"testing"
)

func TestContentSessionSeedStableAcrossLaterTurns(t *testing.T) {
	first := ChatRequest{
		Model: "GLM-5.2",
		Tools: json.RawMessage(`[{"type":"function","function":{"name":"search"}}]`),
		Messages: []ChatMessage{
			{Role: "system", Content: "you are a bot"},
			{Role: "user", Content: "plan the refactor"},
		},
	}
	second := ChatRequest{
		Model: "glm-5.2",
		Tools: json.RawMessage(`[{"type": "function", "function": {"name": "search"}}]`),
		Messages: []ChatMessage{
			{Role: "system", Content: "you are a bot"},
			{Role: "user", Content: "plan the refactor"},
			{Role: "assistant", Content: "here is a plan"},
			{Role: "user", Content: "do step two"},
		},
	}
	got := ContentSessionSeed(first)
	if got == "" {
		t.Fatal("expected content seed")
	}
	if ContentSessionSeed(second) != got {
		t.Fatalf("later turn changed seed:\nfirst=%q\nsecond=%q", got, ContentSessionSeed(second))
	}
}

func TestContentSessionSeedRequiresFirstUserAnchor(t *testing.T) {
	if got := ContentSessionSeed(ChatRequest{Model: "glm-5.2"}); got != "" {
		t.Fatalf("model-only seed = %q", got)
	}
	if got := ContentSessionSeed(ChatRequest{
		Model:    "glm-5.2",
		Messages: []ChatMessage{{Role: "system", Content: "identity"}},
	}); got != "" {
		t.Fatalf("system-only seed = %q", got)
	}
	if got := ContentSessionSeed(ChatRequest{
		Model:    "glm-5.2",
		Messages: []ChatMessage{{Role: "user", Content: "   "}},
	}); got != "" {
		t.Fatalf("blank user seed = %q", got)
	}
	if got := ContentSessionSeed(ChatRequest{
		Model: "glm-5.2",
		Messages: []ChatMessage{
			{Role: "user", Content: "   "},
			{Role: "assistant", Content: "ok"},
			{Role: "user", Content: "real question"},
		},
	}); got != "" {
		t.Fatalf("blank first user must not fall through to a later user: %q", got)
	}
}

func TestContentSessionSeedKeepsPrefixRoleBoundaries(t *testing.T) {
	systemThenUser := ContentSessionSeed(ChatRequest{
		Model: "glm-5.2",
		Messages: []ChatMessage{
			{Role: "system", Content: "shared"},
			{Role: "user", Content: "hi"},
		},
	})
	developerThenUser := ContentSessionSeed(ChatRequest{
		Model: "glm-5.2",
		Messages: []ChatMessage{
			{Role: "developer", Content: "shared"},
			{Role: "user", Content: "hi"},
		},
	})
	if systemThenUser == "" || developerThenUser == "" || systemThenUser == developerThenUser {
		t.Fatalf("system and developer prefixes must not collide: system=%q developer=%q", systemThenUser, developerThenUser)
	}
	oneSystem := ContentSessionSeed(ChatRequest{
		Model: "glm-5.2",
		Messages: []ChatMessage{
			{Role: "system", Content: "a"},
			{Role: "system", Content: "b"},
			{Role: "user", Content: "hi"},
		},
	})
	mergedSystem := ContentSessionSeed(ChatRequest{
		Model: "glm-5.2",
		Messages: []ChatMessage{
			{Role: "system", Content: "ab"},
			{Role: "user", Content: "hi"},
		},
	})
	if oneSystem == "" || mergedSystem == "" || oneSystem == mergedSystem {
		t.Fatalf("consecutive prefix messages must keep boundaries: split=%q merged=%q", oneSystem, mergedSystem)
	}
}

func TestContentSessionSeedIgnoresSystemAfterFirstUser(t *testing.T) {
	base := ChatRequest{
		Messages: []ChatMessage{
			{Role: "system", Content: "keep"},
			{Role: "user", Content: "hi"},
		},
	}
	injected := ChatRequest{
		Messages: []ChatMessage{
			{Role: "system", Content: "keep"},
			{Role: "user", Content: "hi"},
			{Role: "system", Content: "late"},
			{Role: "user", Content: "next"},
		},
	}
	if ContentSessionSeed(base) != ContentSessionSeed(injected) {
		t.Fatal("system messages after the first user must not change the seed")
	}
}

func TestContentSessionSeedUsesStructuredFieldsNotDelimiters(t *testing.T) {
	spoofed := ContentSessionSeed(ChatRequest{
		Model: "glm-5.2",
		Messages: []ChatMessage{
			{Role: "system", Content: "x|first_user=y"},
			{Role: "user", Content: "   "},
		},
	})
	if spoofed != "" {
		t.Fatalf("delimiter in system plus empty user must not create a seed: %q", spoofed)
	}
	real := ContentSessionSeed(ChatRequest{
		Model: "glm-5.2",
		Messages: []ChatMessage{
			{Role: "system", Content: "x"},
			{Role: "user", Content: "y"},
		},
	})
	if real == "" {
		t.Fatal("expected seed for a real first user message")
	}
	shifted := ContentSessionSeed(ChatRequest{
		Model: "glm-5.2",
		Messages: []ChatMessage{
			{Role: "system", Content: "x|first_user=y"},
			{Role: "user", Content: "other"},
		},
	})
	if shifted == "" || shifted == real {
		t.Fatalf("system delimiter must not collide with a real user field: real=%q shifted=%q", real, shifted)
	}
}

func TestContentSessionSeedUsesMultimodalText(t *testing.T) {
	textOnly := ContentSessionSeed(ChatRequest{
		Model:    "glm-5.2",
		Messages: []ChatMessage{{Role: "user", Content: "describe this"}},
	})
	got := ContentSessionSeed(ChatRequest{
		Model: "glm-5.2",
		Messages: []ChatMessage{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "text", "text": "describe this"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/cat.png"}},
			},
		}},
	})
	if got == "" {
		t.Fatal("expected multimodal seed")
	}
	if got == textOnly {
		t.Fatal("image part must change the first-user fingerprint")
	}
}

func TestContentSessionSeedImageOnlyStableAcrossLaterTurns(t *testing.T) {
	image := []any{
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/cat.png"}},
	}
	first := ChatRequest{
		Model:    "glm-5.2",
		Messages: []ChatMessage{{Role: "user", Content: image}},
	}
	later := ChatRequest{
		Model: "glm-5.2",
		Messages: []ChatMessage{
			{Role: "user", Content: image},
			{Role: "assistant", Content: "a cat sitting on a fence"},
			{Role: "user", Content: "what color is it?"},
		},
	}
	got := ContentSessionSeed(first)
	if got == "" {
		t.Fatal("image-only first user must produce a session seed")
	}
	if ContentSessionSeed(later) != got {
		t.Fatalf("image-only later turn changed seed:\nfirst=%q\nlater=%q", got, ContentSessionSeed(later))
	}
	other := ContentSessionSeed(ChatRequest{
		Model: "glm-5.2",
		Messages: []ChatMessage{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/dog.png"}},
			},
		}},
	})
	if other == "" || other == got {
		t.Fatalf("different image must not share a seed: got=%q other=%q", got, other)
	}
}

func TestContentSessionSeedImageOnlyBase64AndAnthropicSource(t *testing.T) {
	dataURI := ChatRequest{
		Model: "glm-5.2",
		Messages: []ChatMessage{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,abc"}},
			},
		}},
	}
	anthropic := ChatRequest{
		Model: "glm-5.2",
		Messages: []ChatMessage{{
			Role: "user",
			Content: map[string]any{
				"type": "image",
				"source": map[string]any{
					"type":       "base64",
					"media_type": "image/png",
					"data":       "abc",
				},
			},
		}},
	}
	if got := ContentSessionSeed(dataURI); got == "" {
		t.Fatal("data URI image-only request must produce a session seed")
	}
	if got := ContentSessionSeed(anthropic); got == "" {
		t.Fatal("Anthropic image source must produce a session seed")
	}
	if ContentSessionSeed(dataURI) == ContentSessionSeed(ChatRequest{
		Model: "glm-5.2",
		Messages: []ChatMessage{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,def"}},
			},
		}},
	}) {
		t.Fatal("different image payloads must not share a seed")
	}
}
