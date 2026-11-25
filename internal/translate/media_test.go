package translate

import (
	"strings"
	"testing"
)

func TestValidateChatRequestRejectsUnsupportedMedia(t *testing.T) {
	request := ChatRequest{
		Model: "glm-5.2",
		Messages: []ChatMessage{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "text", "text": "what happens in this clip?"},
				map[string]any{"type": "video_url", "video_url": map[string]any{"url": "https://example.com/clip.mp4"}},
			},
		}},
	}
	err := ValidateChatRequest(&request)
	if err == nil {
		t.Fatal("a video part must be refused, not silently dropped")
	}
	if !strings.Contains(err.Error(), "video_url") || !strings.Contains(err.Error(), "messages[0].content[1]") {
		t.Fatalf("error must name the offending part, got %v", err)
	}
}

func TestValidateChatRequestRejectsAudioAndFileParts(t *testing.T) {
	for _, partType := range []string{"input_audio", "input_file"} {
		request := ChatRequest{
			Model: "glm-5.2",
			Messages: []ChatMessage{{
				Role:    "user",
				Content: []any{map[string]any{"type": partType}},
			}},
		}
		if err := ValidateChatRequest(&request); err == nil {
			t.Fatalf("%s must be refused", partType)
		}
	}
}

func TestValidateChatRequestKeepsImageParts(t *testing.T) {
	request := ChatRequest{
		Model: "glm-5.2",
		Messages: []ChatMessage{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "text", "text": "describe this"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/cat.png"}},
			},
		}},
	}
	if err := ValidateChatRequest(&request); err != nil {
		t.Fatalf("image parts must stay accepted: %v", err)
	}
}

func TestValidateChatRequestKeepsStringContent(t *testing.T) {
	request := ChatRequest{
		Model:    "glm-5.2",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	}
	if err := ValidateChatRequest(&request); err != nil {
		t.Fatalf("plain text content must stay accepted: %v", err)
	}
}
