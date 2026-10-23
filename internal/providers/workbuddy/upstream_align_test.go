package workbuddy

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func newAlignClient(t *testing.T, cred Credential, handler http.HandlerFunc) (*Client, *memStore) {
	t.Helper()
	payload, _ := cred.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}
	return client, store
}

func catalogOK(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
		"models": []map[string]any{{"id": "glm-5.2", "name": "GLM"}},
		"agents": []map[string]any{{"name": "cli", "models": []string{"glm-5.2"}}},
	}})
}

var alignReq = translate.ChatRequest{Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}}

func TestChat401RefreshesOnceAndRetriesSameAccount(t *testing.T) {
	var chats, refreshes int32
	client, store := newAlignClient(t, Credential{AccessToken: "old", RefreshToken: "rt", UID: "u1", Domain: DomainCN, ExpiresAt: 4102444800},
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case pathProductConfig:
				catalogOK(w)
			case pathTokenRefresh:
				atomic.AddInt32(&refreshes, 1)
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"accessToken": "new", "expiresIn": 3600}})
			case pathChat:
				atomic.AddInt32(&chats, 1)
				if r.Header.Get("Authorization") != "Bearer new" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(chatSSE))
			}
		})
	out, err := client.ChatNonStream(context.Background(), "acc1", alignReq)
	if err != nil {
		t.Fatalf("expected success after refresh, got %v", err)
	}
	if out.Content != "OK" || chats != 2 || refreshes != 1 {
		t.Fatalf("content=%q chats=%d refreshes=%d", out.Content, chats, refreshes)
	}
	if store.lastKind == accounts.KindAuth {
		t.Fatal("recovered account must not be marked auth-dead")
	}
}

func TestChat401RefreshFailureSurfacesAuthForFailover(t *testing.T) {
	var chats, refreshes int32
	client, store := newAlignClient(t, Credential{AccessToken: "old", RefreshToken: "rt", UID: "u1", Domain: DomainCN, ExpiresAt: 4102444800},
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case pathProductConfig:
				catalogOK(w)
			case pathTokenRefresh:
				atomic.AddInt32(&refreshes, 1)
				w.WriteHeader(http.StatusUnauthorized)
			case pathChat:
				atomic.AddInt32(&chats, 1)
				w.WriteHeader(http.StatusUnauthorized)
			}
		})
	_, _, err := client.ChatStream(context.Background(), "acc1", alignReq)
	var pe *providers.Error
	if !errors.As(err, &pe) || pe.Kind != accounts.KindAuth {
		t.Fatalf("want KindAuth after failed refresh, got %v", err)
	}
	if pe.Message != "token refresh after 401 failed; re-login required" {
		t.Fatalf("auth message = %q", pe.Message)
	}
	if chats != 1 || refreshes != 1 {
		t.Fatalf("chats=%d refreshes=%d; refresh must run once and not retry chat", chats, refreshes)
	}
	// Refresh Observe(login_required); sendChat must not Observe unavailable.
	if store.lastStatus != "login_required" || store.lastKind != accounts.KindAuth {
		t.Fatalf("want Refresh Observe(login_required/auth), got status=%q kind=%q", store.lastStatus, store.lastKind)
	}
}

func TestChat401RefreshNonAuthSurfacesUnavailable(t *testing.T) {
	var chats, refreshes int32
	client, store := newAlignClient(t, Credential{AccessToken: "old", RefreshToken: "rt", UID: "u1", Domain: DomainCN, ExpiresAt: 4102444800},
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case pathProductConfig:
				catalogOK(w)
			case pathTokenRefresh:
				atomic.AddInt32(&refreshes, 1)
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`upstream boom`))
			case pathChat:
				atomic.AddInt32(&chats, 1)
				w.WriteHeader(http.StatusUnauthorized)
			}
		})
	_, _, err := client.ChatStream(context.Background(), "acc1", alignReq)
	var pe *providers.Error
	if !errors.As(err, &pe) || pe.Kind != accounts.KindUnavailable {
		t.Fatalf("want KindUnavailable after non-auth refresh failure, got %v", err)
	}
	if strings.Contains(pe.Message, "re-login required") {
		t.Fatalf("non-auth refresh must not surface re-login message: %q", pe.Message)
	}
	if !strings.Contains(pe.Message, "refresh") {
		t.Fatalf("want refresh failure summary in message, got %q", pe.Message)
	}
	if chats != 1 || refreshes != 1 {
		t.Fatalf("chats=%d refreshes=%d; refresh must run once and not retry chat", chats, refreshes)
	}
	if store.lastKind == accounts.KindAuth || store.lastStatus == "login_required" {
		t.Fatalf("non-auth refresh must not Observe login_required/auth: status=%q kind=%q", store.lastStatus, store.lastKind)
	}
	if store.lastKind != accounts.KindUnavailable || store.lastStatus != "error" {
		t.Fatalf("want Observe(error/unavailable) diagnostic, got status=%q kind=%q", store.lastStatus, store.lastKind)
	}
}

func TestChat401RefreshSessionDeadBodySurfacesAuth(t *testing.T) {
	var chats, refreshes int32
	client, store := newAlignClient(t, Credential{AccessToken: "old", RefreshToken: "rt", UID: "u1", Domain: DomainCN, ExpiresAt: 4102444800},
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case pathProductConfig:
				catalogOK(w)
			case pathTokenRefresh:
				atomic.AddInt32(&refreshes, 1)
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code": sessionDeadCode,
					"msg":  sessionDeadText,
				})
			case pathChat:
				atomic.AddInt32(&chats, 1)
				w.WriteHeader(http.StatusUnauthorized)
			}
		})
	_, _, err := client.ChatStream(context.Background(), "acc1", alignReq)
	var pe *providers.Error
	if !errors.As(err, &pe) || pe.Kind != accounts.KindAuth {
		t.Fatalf("want KindAuth for session-dead refresh body, got %v", err)
	}
	if store.lastStatus != "login_required" || store.lastKind != accounts.KindAuth {
		t.Fatalf("want Refresh Observe(login_required/auth), got status=%q kind=%q", store.lastStatus, store.lastKind)
	}
	if chats != 1 || refreshes != 1 {
		t.Fatalf("chats=%d refreshes=%d", chats, refreshes)
	}
}

func TestTLSVerifyErrorIsWrappedNotRetried(t *testing.T) {
	raw := &url_error{err: x509.UnknownAuthorityError{}}
	err := wrapTransportError(raw)
	var pe *providers.Error
	if !errors.As(err, &pe) || pe.Code != providers.CodeTLSVerifyFailed {
		t.Fatalf("want tls_verify_failed, got %v", err)
	}
	plain := errors.New("connection reset by peer")
	if wrapTransportError(plain) != plain {
		t.Fatal("non-TLS transport errors must pass through")
	}
}

type url_error struct{ err error }

func (e *url_error) Error() string { return "Post: " + e.err.Error() }
func (e *url_error) Unwrap() error { return e.err }

func TestPrivateDataHeaderModes(t *testing.T) {
	personal := Credential{Domain: DomainCN}
	enterprise := Credential{Domain: DomainCN, EnterpriseID: "e1"}
	global := Credential{Domain: DomainGlobal}
	cases := []struct {
		identity, mode string
		cred           Credential
		want           string
		send           bool
	}{
		{identityCLI, "auto", personal, "false", true},
		{identityCLI, "auto", enterprise, "true", true},
		{identityWorkBuddy, "auto", personal, "true", true},
		{identityVSCode, "auto", personal, "", false},
		{identityVSCode, "auto", global, "true", true},
		{identityVSCode, "true", personal, "true", true},
		{identityVSCode, "false", personal, "false", true},
		{identityCLI, "true", personal, "true", true},
	}
	for _, c := range cases {
		got, send := privateDataHeader(c.identity, c.mode, c.cred)
		if got != c.want || send != c.send {
			t.Errorf("%s/%s: got %q,%v want %q,%v", c.identity, c.mode, got, send, c.want, c.send)
		}
	}
	for raw, want := range map[string]string{"": "auto", "AUTO": "auto", "True": "true", "off": "false", "junk": "auto"} {
		if got := normalizePrivateDataMode(raw); got != want {
			t.Errorf("normalize(%q)=%q want %q", raw, got, want)
		}
	}
}

func TestChatHeadersHonorPrivateDataEnv(t *testing.T) {
	t.Setenv(PrivateDataEnv, "true")
	h := http.Header{}
	SetChatHeaders(h, Credential{Domain: DomainCN})
	if h.Get("X-Private-Data") != "true" {
		t.Fatalf("X-Private-Data=%q", h.Get("X-Private-Data"))
	}
	t.Setenv(PrivateDataEnv, "")
	h = http.Header{}
	SetChatHeaders(h, Credential{Domain: DomainCN})
	if h.Get("X-Private-Data") != "false" {
		t.Fatalf("auto personal CLI should stay false, got %q", h.Get("X-Private-Data"))
	}
}
