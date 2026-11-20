package minimaxcode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

type memoryStore struct {
	mu      sync.Mutex
	payload []byte
	account accounts.Account
}

func (s *memoryStore) Get(context.Context, string) (accounts.Account, error) { return s.account, nil }
func (s *memoryStore) LoadCredentialPayload(context.Context, string) (string, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return CredentialFormat, append([]byte(nil), s.payload...), nil
}
func (s *memoryStore) SaveCredentialPayload(_ context.Context, _, _ string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.payload = append([]byte(nil), payload...)
	return nil
}
func (s *memoryStore) Observe(context.Context, string, string, string, string, string) error {
	return nil
}

func newTestClient(t *testing.T, accountBase, agentBase string) (*Client, *memoryStore) {
	t.Helper()
	store := &memoryStore{account: accounts.Account{Enabled: true}}
	cred := Credential{Format: CredentialFormat, AccessToken: "tok", Site: "cn", AccountBase: accountBase, AgentBase: agentBase, Region: "cn", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	payload, _ := cred.Encode()
	store.payload = payload
	client := NewClient(store)
	client.http = &http.Client{}
	acceptTestEndpoint = true
	return client, store
}

func TestCredentialRoundTrip(t *testing.T) {
	cred := Credential{AccessToken: "access", RefreshToken: "refresh", Site: "global", ExpiresIn: 3600}
	payload, err := cred.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCredential(payload)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.AccessToken != "access" || decoded.Site != "global" {
		t.Fatalf("decoded = %+v", decoded)
	}
}

func TestDeviceLoginFlow(t *testing.T) {
	deviceCodeIssued := false
	tokenApproved := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case deviceCodePath:
			deviceCodeIssued = true
			_, _ = io.WriteString(w, `{"device_code":"dev-1","user_code":"ABCD-1234","verification_uri_complete":"https://example.com/device","expires_in":600,"interval":1}`)
		case tokenPath:
			if !tokenApproved {
				_, _ = io.WriteString(w, `{"status":"pending"}`)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, _ := newTestClient(t, server.URL, server.URL)
	// Login starts against the credential's account base.
	session, err := client.StartLogin(context.Background(), "acct-1")
	if err != nil {
		t.Fatal(err)
	}
	if session.AuthURL != "https://example.com/device" || session.State != "ABCD-1234" {
		t.Fatalf("session = %+v", session)
	}
	if !deviceCodeIssued {
		t.Fatal("device code endpoint not called")
	}
	done, msg, err := client.PollLogin(context.Background(), "acct-1")
	if err != nil || done {
		t.Fatalf("pending poll: done=%v msg=%q err=%v", done, msg, err)
	}
	if !strings.Contains(msg, "ABCD-1234") {
		t.Fatalf("user code not surfaced: %q", msg)
	}
	tokenApproved = true
	// Force the next poll past the interval gate.
	client.mu.Lock()
	client.logins["acct-1"].lastPolled = time.Time{}
	client.mu.Unlock()
	done, _, err = client.PollLogin(context.Background(), "acct-1")
	if err != nil || !done {
		t.Fatalf("approval poll: done=%v err=%v", done, err)
	}
}

func TestChatNonStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == modelsPath {
			_, _ = io.WriteString(w, `{"models":[{"id":"MiniMax-M3","name":"MiniMax M3"}]}`)
			return
		}
		if r.URL.Path == messagesPath {
			if r.Header.Get("Authorization") != "Bearer tok" {
				t.Error("missing bearer")
			}
			if r.Header.Get("X-Mavis-Agent-Id") != AgentID {
				t.Error("missing mavis agent header")
			}
			body, _ := io.ReadAll(r.Body)
			var anthropic map[string]any
			if json.Unmarshal(body, &anthropic) != nil {
				t.Error("invalid anthropic body")
			}
			if anthropic["model"] != "MiniMax-M3" {
				t.Errorf("model = %v", anthropic["model"])
			}
			if anthropic["system"] != "be brief" {
				t.Errorf("system = %v", anthropic["system"])
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"msg-1","model":"MiniMax-M3","stop_reason":"end_turn","content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":3,"output_tokens":2}}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	client, _ := newTestClient(t, server.URL, server.URL)
	req := translate.ChatRequest{
		Model: "minimaxcode/MiniMax-M3",
		Messages: []translate.ChatMessage{
			{Role: "system", Content: "be brief"},
			{Role: "user", Content: "hi"},
		},
	}
	outcome, err := client.ChatNonStream(context.Background(), "acct-1", req)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Content != "hello" {
		t.Fatalf("content = %q", outcome.Content)
	}
	if outcome.PromptTokens != 3 || outcome.CompletionTokens != 2 {
		t.Fatalf("usage = %+v", outcome)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		status int
		body   string
		kind   string
	}{
		{401, `{"type":"error","error":{"message":"unauthorized"}}`, accounts.KindAuth},
		{402, `{"error":{"message":"insufficient balance"}}`, accounts.KindQuota},
		{429, `{"error":{"message":"rate limit"}}`, accounts.KindRateLimit},
		{500, `boom`, accounts.KindUnavailable},
	}
	for _, tc := range cases {
		got := Classify(tc.status, tc.body)
		if got.Kind != tc.kind {
			t.Errorf("status %d: kind = %q want %q", tc.status, got.Kind, tc.kind)
		}
	}
}

func TestModelsFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"unauthorized"}}`)
	}))
	defer server.Close()
	client, _ := newTestClient(t, server.URL, server.URL)
	models, err := client.Models(context.Background(), "acct-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != len(knownModels) {
		t.Fatalf("fallback models = %d", len(models))
	}
}

var _ = fmt.Sprintf
