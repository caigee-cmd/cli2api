package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type conversationContextKey struct{}

// WithConversationID records a client-supplied conversation/session id found
// in the decoded request. Headers win; body aliases are the compatibility path
// for clients that never send X-CLI2API-Session.
func WithConversationID(ctx context.Context, id string) context.Context {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 512 {
		return ctx
	}
	return context.WithValue(ctx, conversationContextKey{}, id)
}

func ConversationIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(conversationContextKey{}).(string)
	return strings.TrimSpace(id)
}

var conversationHeaders = []string{
	"X-CLI2API-Session",
	"X-Conversation-Id", "X-Conversation-ID",
	"X-Session-Id", "X-Session-ID",
	"X-Thread-Id", "X-Thread-ID",
}

var conversationBodyKeys = []string{
	"conversation_id", "conversationId", "conversation-id",
	"session_id", "sessionId", "session-id",
	"thread_id", "threadId", "conversation", "session",
}

// conversationIDFromRequest reads the conversation id without consuming the
// body. Callers pass the already-decoded bytes when available.
func conversationIDFromRequest(r *http.Request, body []byte) string {
	if r != nil {
		for _, name := range conversationHeaders {
			if id := cleanID(r.Header.Get(name)); id != "" {
				return id
			}
		}
	}
	if len(body) == 0 || len(body) > 64<<20 {
		return ""
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(body, &top) != nil {
		return ""
	}
	for _, key := range conversationBodyKeys {
		if id, ok := stringField(top[key]); ok {
			return id
		}
	}
	for _, metaKey := range []string{"metadata", "meta", "context"} {
		var meta map[string]json.RawMessage
		if json.Unmarshal(top[metaKey], &meta) != nil {
			continue
		}
		for _, key := range conversationBodyKeys {
			if id, ok := stringField(meta[key]); ok {
				return id
			}
		}
	}
	return ""
}

func stringField(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	if id := cleanID(value); id != "" {
		return id, true
	}
	return "", false
}

func cleanID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 512 {
		return ""
	}
	return value
}

// readConversationBody consumes a request body reader and returns the bytes so
// handlers can both decode JSON and derive the conversation id. It is only used
// for the request body, never the upstream response stream.
func readConversationBody(reader io.Reader, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(reader, limit))
}
