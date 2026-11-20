package doubaowork

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

type fakeStore struct {
	mu       sync.Mutex
	payload  []byte
	observed string
}

func (f *fakeStore) Get(context.Context, string) (accounts.Account, error) {
	return accounts.Account{ID: "acc1", Provider: "doubaowork", ProviderRegion: "cn"}, nil
}

func (f *fakeStore) LoadCredentialPayload(context.Context, string) (string, []byte, error) {
	return CredentialFormat, f.payload, nil
}

func (f *fakeStore) SaveCredentialPayload(context.Context, string, string, []byte) error { return nil }

func (f *fakeStore) Observe(_ context.Context, _ string, remoteUID, status, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.observed = status + ":" + remoteUID
	return nil
}

func credentialPayload(t *testing.T, uidHint string) []byte {
	t.Helper()
	cred := Credential{
		Cookies: []Cookie{{Name: "sessionid", Value: "abc"}},
		UID:     uidHint,
	}
	encoded, err := cred.Encode()
	if err != nil {
		t.Fatalf("encode credential: %v", err)
	}
	return encoded
}

// newTestClient wires a client against a test server with the given handlers.
func newTestClient(t *testing.T, handler http.Handler) (*Client, *fakeStore) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	store := &fakeStore{}
	client := NewClient(store)
	client.baseURL = server.URL
	return client, store
}

func identityHandler(identity map[string]any, launch map[string]any) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/passport/account/info/v2/", func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelope(w, identity)
	})
	mux.HandleFunc("/alice/user/launch", func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelope(w, launch)
	})
	return mux
}

func writeEnvelope(w http.ResponseWriter, data map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "", "data": data})
}

func TestBootstrapSuccess(t *testing.T) {
	client, store := newTestClient(t, identityHandler(
		map[string]any{"user_id_str": "42", "screen_name": "Alice"},
		map[string]any{"assistant_bot_id": "998877", "country": "CN", "config": map[string]any{"web_id": "555", "ttwid": "tw-1"}},
	))
	store.payload = credentialPayload(t, "")
	sess := client.sessionFor("acc1")
	if err := sess.ready(context.Background()); err != nil {
		t.Fatalf("ready: %v", err)
	}
	if sess.uid != "42" {
		t.Fatalf("uid = %q", sess.uid)
	}
	if sess.bot != "998877" {
		t.Fatalf("bot = %q", sess.bot)
	}
	if sess.clientInfo.WebID != "555" || sess.clientInfo.Ttwid != "tw-1" {
		t.Fatalf("device identifiers not filled from launch: %+v", sess.clientInfo)
	}

	health, err := client.Probe(context.Background(), "acc1")
	if err != nil || !health.Ready || health.UID != "42" {
		t.Fatalf("probe: %+v, %v", health, err)
	}
	if store.observed != "ready:42" {
		t.Fatalf("observe not called with ready: %q", store.observed)
	}
}

func TestBootstrapRejectsVisitor(t *testing.T) {
	client, store := newTestClient(t, identityHandler(
		map[string]any{"user_id_str": "0", "is_visitor_account": true},
		map[string]any{},
	))
	store.payload = credentialPayload(t, "")
	err := client.sessionFor("acc1").ready(context.Background())
	if err == nil {
		t.Fatal("expected visitor rejection")
	}
}

func TestBootstrapUsesFallbackBotWhenLaunchHasNone(t *testing.T) {
	client, store := newTestClient(t, identityHandler(
		map[string]any{"user_id_str": "42", "screen_name": "Alice"},
		map[string]any{"assistant_bot_id": ""},
	))
	store.payload = credentialPayload(t, "")
	sess := client.sessionFor("acc1")
	if err := sess.ready(context.Background()); err != nil {
		t.Fatalf("ready: %v", err)
	}
	if sess.bot != FallbackBot {
		t.Fatalf("expected fallback bot, got %q", sess.bot)
	}
}

func TestBootstrapRejectsUIDMismatch(t *testing.T) {
	client, store := newTestClient(t, identityHandler(
		map[string]any{"user_id_str": "99", "screen_name": "Mallory"},
		map[string]any{},
	))
	store.payload = credentialPayload(t, "42") // stored uid differs from upstream
	err := client.sessionFor("acc1").ready(context.Background())
	if err == nil {
		t.Fatal("expected uid mismatch rejection")
	}
}

func TestSerialAcquireRelease(t *testing.T) {
	sess := &session{client: &Client{}, accountID: "acc1"}
	ctx := context.Background()
	if err := sess.acquire(ctx); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	blocked := make(chan struct{})
	acquired := make(chan struct{})
	go func() {
		close(blocked)
		_ = sess.acquire(context.Background())
		close(acquired)
	}()
	<-blocked
	select {
	case <-acquired:
		t.Fatal("second acquire should block while the first holds the queue")
	case <-time.After(80 * time.Millisecond):
	}
	sess.release()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("second acquire did not proceed after release")
	}
	sess.release()
}

func TestSerialAcquireHonorsContextCancel(t *testing.T) {
	sess := &session{client: &Client{}, accountID: "acc1"}
	_ = sess.acquire(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := sess.acquire(ctx); err == nil {
		t.Fatal("expected context cancellation while queue is held")
	}
	sess.release()
}

func TestLeaseObserveAndCleanup(t *testing.T) {
	mux := http.NewServeMux()
	deletes := 0
	mux.HandleFunc("/im/conversation/batch_del_user_conv", func(w http.ResponseWriter, _ *http.Request) {
		deletes++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status_code":0,"downlink_body":{}}`)
	})
	client, store := newTestClient(t, mux)
	store.payload = credentialPayload(t, "")
	sess := client.sessionFor("acc1")

	l := newLease(sess, "local_abc")
	if err := l.begin(context.Background()); err != nil {
		t.Fatalf("begin: %v", err)
	}
	// Mismatched ack is rejected.
	if err := l.observe("SSE_ACK", map[string]any{"ack_client_meta": map[string]any{"local_conversation_id": "other", "conversation_id": "1"}}); err == nil {
		t.Fatal("expected ack mismatch rejection")
	}
	// Matching ack claims the id.
	if err := l.observe("SSE_ACK", map[string]any{"ack_client_meta": map[string]any{"local_conversation_id": "local_abc", "conversation_id": "123"}}); err != nil {
		t.Fatalf("observe: %v", err)
	}
	if l.conversationID != "123" {
		t.Fatalf("conversation id = %q", l.conversationID)
	}
	if err := l.finish(context.Background(), nil); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if deletes != 1 {
		t.Fatalf("expected one delete call, got %d", deletes)
	}
}

func TestLeaseCleanupFailureDoesNotBreakSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/im/conversation/batch_del_user_conv", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status_code":500,"status_desc":"boom"}`)
	})
	client, store := newTestClient(t, mux)
	store.payload = credentialPayload(t, "")
	sess := client.sessionFor("acc1")
	l := newLease(sess, "local_abc")
	_ = l.begin(context.Background())
	_ = l.observe("SSE_ACK", map[string]any{"ack_client_meta": map[string]any{"local_conversation_id": "local_abc", "conversation_id": "123"}})
	if err := l.finish(context.Background(), nil); err != nil {
		t.Fatalf("cleanup failure must not break a successful reply: %v", err)
	}
}
