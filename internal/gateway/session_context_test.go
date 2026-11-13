package gateway

import (
	"net/http/httptest"
	"testing"
)

func TestConversationIDFromHeaderAliases(t *testing.T) {
	for _, name := range []string{"X-CLI2API-Session", "X-Conversation-Id", "X-Session-ID", "X-Thread-Id"} {
		req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
		req.Header.Set(name, "chat-42")
		if got := conversationIDFromRequest(req, nil); got != "chat-42" {
			t.Fatalf("%s = %q", name, got)
		}
	}
}

func TestConversationIDFromBodyAliases(t *testing.T) {
	for _, body := range []string{
		`{"conversation_id":"a"}`,
		`{"conversationId":"b"}`,
		`{"session_id":"c"}`,
		`{"sessionId":"d"}`,
		`{"thread_id":"e"}`,
		`{"metadata":{"conversation_id":"f"}}`,
		`{"meta":{"conversationId":"g"}}`,
		`{"context":{"sessionId":"h"}}`,
	} {
		req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
		got := conversationIDFromRequest(req, []byte(body))
		if got == "" {
			t.Fatalf("no id from %s", body)
		}
	}
}

func TestConversationIDHeaderWinsAndBoundsValue(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("X-Conversation-ID", "header-wins")
	if got := conversationIDFromRequest(req, []byte(`{"metadata":{"conversation_id":"body"}}`)); got != "header-wins" {
		t.Fatalf("got %q", got)
	}
	if got := conversationIDFromRequest(req, []byte(`{"conversation_id":"`+string(make([]byte, 600))+`"}`)); got != "header-wins" {
		t.Fatalf("oversize value leaked: %q", got)
	}
}

func TestWithConversationIDRoundTrip(t *testing.T) {
	ctx := WithConversationID(httptest.NewRequest("GET", "/", nil).Context(), " abc ")
	if got := ConversationIDFromContext(ctx); got != "abc" {
		t.Fatalf("got %q", got)
	}
}
