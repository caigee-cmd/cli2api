package doubaowork

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

func sseEvent(name, data string) string {
	return "event: " + name + "\ndata: " + data + "\n\n"
}

func chunkEvent(messageID, blockID string, blockType int, text string, patchType int) string {
	payload := map[string]any{
		"message_id": messageID,
		"patch_op": []any{
			map[string]any{
				"patch_object": 1,
				"patch_type":   patchType,
				"patch_value": map[string]any{
					"content_block": []any{
						map[string]any{
							"block_id":   blockID,
							"block_type": blockType,
							"parent_id":  "",
							"content":    map[string]any{"text_block": map[string]any{"text": text}},
						},
					},
				},
			},
		},
	}
	encoded, _ := json.Marshal(payload)
	return sseEvent("STREAM_CHUNK", string(encoded))
}

func TestReadDoubaoTranslatesContentAndReasoning(t *testing.T) {
	var stream strings.Builder
	stream.WriteString(chunkEvent("m1", "b1", 10000, "Hel", 2))
	stream.WriteString(chunkEvent("m1", "b1", 10000, "Hello", 2))
	stream.WriteString(chunkEvent("m1", "b2", 10040, "why", 2))
	// CHUNK_DELTA extends the last single block (b2, reasoning).
	stream.WriteString(sseEvent("CHUNK_DELTA", `{"text":" not"}`))
	stream.WriteString(sseEvent("SSE_REPLY_END", `{"end_type":3}`))

	var content, reasoning strings.Builder
	err := readDoubaoTranslates(context.Background(), strings.NewReader(stream.String()), nil, nil, func(c, r string) error {
		content.WriteString(c)
		reasoning.WriteString(r)
		return nil
	})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if content.String() != "Hello" {
		t.Fatalf("content = %q", content.String())
	}
	if reasoning.String() != "why not" {
		t.Fatalf("reasoning = %q", reasoning.String())
	}
}

func TestReadDoubaoTranslatesRejectsIncomplete(t *testing.T) {
	stream := chunkEvent("m1", "b1", 10000, "partial", 2) // no SSE_REPLY_END
	err := readDoubaoTranslates(context.Background(), strings.NewReader(stream), nil, nil, func(string, string) error { return nil })
	if err == nil {
		t.Fatal("expected incomplete stream error")
	}
}

// completionHandler builds an httptest handler serving bootstrap, an empty menu
// (baseline fallback), and a chat completion stream.
func completionHandler(completionStream string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/passport/account/info/v2/", func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelope(w, map[string]any{"user_id_str": "42", "screen_name": "Alice"})
	})
	mux.HandleFunc("/alice/user/launch", func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelope(w, map[string]any{"assistant_bot_id": "998877"})
	})
	mux.HandleFunc("/alice/slot/action_bar_v3/brief_list", func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelope(w, map[string]any{}) // empty menu -> baseline fallback
	})
	mux.HandleFunc("/chat/completion", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, completionStream)
	})
	return mux
}

func newChatClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	client, store := newTestClient(t, handler)
	store.payload = credentialPayload(t, "")
	return client
}

func TestChatNonStreamCollectsContent(t *testing.T) {
	var stream strings.Builder
	stream.WriteString(chunkEvent("m1", "b1", 10000, "Hello", 2))
	stream.WriteString(chunkEvent("m1", "b1", 10000, "Hello world", 2))
	stream.WriteString(sseEvent("SSE_REPLY_END", `{"end_type":3}`))

	client := newChatClient(t, completionHandler(stream.String()))
	outcome, err := client.ChatNonStream(context.Background(), "acc1", translate.ChatRequest{
		Model:    "doubao-auto",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if outcome.Content != "Hello world" {
		t.Fatalf("content = %q", outcome.Content)
	}
	if outcome.FinishReason != "stop" {
		t.Fatalf("finish = %q", outcome.FinishReason)
	}
	if outcome.CompletionTokens == 0 {
		t.Fatal("expected estimated completion tokens")
	}
}

func TestChatStreamEmitsOpenAIFrames(t *testing.T) {
	var stream strings.Builder
	stream.WriteString(chunkEvent("m1", "b1", 10000, "Hi", 2))
	stream.WriteString(chunkEvent("m1", "b1", 10000, "Hi there", 2))
	stream.WriteString(sseEvent("SSE_REPLY_END", `{"end_type":3}`))

	client := newChatClient(t, completionHandler(stream.String()))
	resp, _, err := client.ChatStream(context.Background(), "acc1", translate.ChatRequest{
		Model:    "doubao-auto",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("chat stream: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	text := string(body)
	if !strings.Contains(text, `"content":"Hi"`) || !strings.Contains(text, `"content":" there"`) {
		t.Fatalf("stream missing content deltas:\n%s", text)
	}
	if !strings.Contains(text, "data: [DONE]") {
		t.Fatalf("stream missing DONE:\n%s", text)
	}
}

func TestChatStreamRejectsImages(t *testing.T) {
	client := newChatClient(t, completionHandler(""))
	_, _, err := client.ChatStream(context.Background(), "acc1", translate.ChatRequest{
		Model:    "doubao-auto",
		Messages: []translate.ChatMessage{{Role: "user", Content: []any{map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,AAA"}}}}},
	})
	if err == nil {
		t.Fatal("expected image rejection")
	}
}
