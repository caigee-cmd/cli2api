package codearts

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

func TestOTSessionIDStablePerUser(t *testing.T) {
	a := otSessionID("user-1")
	b := otSessionID("user-1")
	c := otSessionID("user-2")
	if a == "" || len(a) != 32 {
		t.Fatalf("otSessionID length = %d value=%q", len(a), a)
	}
	for _, ch := range a {
		if !strings.ContainsRune("0123456789abcdef", ch) {
			t.Fatalf("non-hex char in %q", a)
		}
	}
	if a != b {
		t.Fatalf("same user must be stable: %q vs %q", a, b)
	}
	if a == c {
		t.Fatalf("different users must differ: %q", a)
	}
	if a != "6c6b39c57d8cffff7601d294f2ad931e" {
		t.Fatalf("must match WorkBuddy tip seed, got %q", a)
	}
}

func TestApplyOTSessionHeadersAfterSignStayOutOfSignedHeaders(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://example.test/api/v2/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	body := []byte(`{"model":"GLM-5.2"}`)
	signRequest(req, body, SignCredential{AccessKeyID: "AK", SecretAccessKey: "SK", SecurityToken: "STS"})
	authBefore := req.Header.Get("Authorization")
	if !strings.Contains(authBefore, "SignedHeaders=") {
		t.Fatalf("missing SignedHeaders in %q", authBefore)
	}
	applyOTSessionHeaders(req.Header, "user-1")
	authAfter := req.Header.Get("Authorization")
	if authAfter != authBefore {
		t.Fatal("Authorization must not change after tip headers are applied")
	}
	session := req.Header.Get("x-ot-session-id")
	if session == "" || session != req.Header.Get("user-session-id") {
		t.Fatalf("session headers = %q / %q", session, req.Header.Get("user-session-id"))
	}
	if req.Header.Get("x-ot-trace-id") == "" {
		t.Fatal("x-ot-trace-id missing")
	}
	signed := signedHeadersList(authAfter)
	for _, name := range []string{"x-ot-session-id", "user-session-id", "x-ot-trace-id"} {
		for _, listed := range signed {
			if listed == name {
				t.Fatalf("%s must not appear in SignedHeaders=%v", name, signed)
			}
		}
	}
}

func TestChatOTSessionHeadersStableAndTraceDiffers(t *testing.T) {
	var mu sync.Mutex
	var captured []http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != chatPath {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		captured = append(captured, r.Header.Clone())
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, w, map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "ok"}}}})
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	client, _ := newTestClient(t, server)

	req := translate.ChatRequest{
		Model:    "GLM-5.2",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}
	if _, err := client.ChatNonStream(context.Background(), "acct-1", req); err != nil {
		t.Fatalf("first chat: %v", err)
	}
	if _, err := client.ChatNonStream(context.Background(), "acct-1", req); err != nil {
		t.Fatalf("second chat: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 2 {
		t.Fatalf("captured %d chat requests", len(captured))
	}
	first, second := captured[0], captured[1]
	session := first.Get("x-ot-session-id")
	if session == "" {
		t.Fatal("x-ot-session-id missing on chat")
	}
	if session != first.Get("user-session-id") {
		t.Fatalf("user-session-id=%q want %q", first.Get("user-session-id"), session)
	}
	if session != second.Get("x-ot-session-id") || session != second.Get("user-session-id") {
		t.Fatalf("session ids not stable across requests: %q/%q vs %q/%q",
			session, first.Get("user-session-id"), second.Get("x-ot-session-id"), second.Get("user-session-id"))
	}
	if want := otSessionID("user-1"); session != want {
		t.Fatalf("session=%q want derived %q", session, want)
	}
	trace1, trace2 := first.Get("x-ot-trace-id"), second.Get("x-ot-trace-id")
	if trace1 == "" || trace2 == "" || trace1 == trace2 {
		t.Fatalf("trace ids must differ per request: %q vs %q", trace1, trace2)
	}
	for i, h := range captured {
		signed := signedHeadersList(h.Get("Authorization"))
		for _, name := range []string{"x-ot-session-id", "user-session-id", "x-ot-trace-id"} {
			for _, listed := range signed {
				if listed == name {
					t.Fatalf("request %d: %s listed in SignedHeaders=%v", i+1, name, signed)
				}
			}
			if h.Get(name) == "" {
				t.Fatalf("request %d: missing %s", i+1, name)
			}
		}
	}
}

func signedHeadersList(authorization string) []string {
	const marker = "SignedHeaders="
	idx := strings.Index(authorization, marker)
	if idx < 0 {
		return nil
	}
	rest := authorization[idx+len(marker):]
	if end := strings.Index(rest, ","); end >= 0 {
		rest = rest[:end]
	}
	parts := strings.Split(rest, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
