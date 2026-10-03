package orcarouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAuthServer is a local stand-in for the OrcaRouter consent + exchange
// endpoints. It records every authorize URL it is handed and every exchange body
// it receives, so the test can assert the exact PKCE contract without a browser.
type fakeAuthServer struct {
	mu            sync.Mutex
	authorizeURL  string
	exchangeBodys []map[string]string
	capability    string
	scope         string
	status        int
	errorBody     string
	key           string
	exchangePath  string
}

func newFakeAuthServer(t *testing.T) (*httptest.Server, *fakeAuthServer) {
	t.Helper()
	fake := &fakeAuthServer{scope: "api", status: 200, key: "sk-orca-FAKEKEY0000000000"}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		fake.authorizeURL = r.URL.String()
		fake.capability = r.URL.Query().Get("capability")
		fake.mu.Unlock()
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "consent")
	})
	mux.HandleFunc(ExchangePath, func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		fake.exchangePath = r.URL.Path
		fake.mu.Unlock()
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		body := map[string]string{}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
			if parsed, err := url.ParseQuery(string(raw)); err == nil {
				for key := range parsed {
					body[key] = parsed.Get(key)
				}
			}
		} else {
			_ = json.Unmarshal(raw, &body)
		}
		fake.mu.Lock()
		fake.exchangeBodys = append(fake.exchangeBodys, body)
		status, errorBody, scope, key := fake.status, fake.errorBody, fake.scope, fake.key
		fake.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if status >= 300 {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, errorBody)
			return
		}
		w.WriteHeader(200)
		_, _ = io.WriteString(w, fmt.Sprintf(`{"key":%q,"user_id":"12345","scope":%q}`, key, scope))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, fake
}

func newLoginClient(srv *httptest.Server) (*Client, *fakeStore) {
	store := newFakeStore()
	store.secrets[SecretAuthBase] = srv.URL
	store.secrets[SecretAPIBase] = srv.URL
	return NewClient(store), store
}

// The full loopback path: start → authorize URL → state → callback → exchange →
// persist, driven by the project's own connect adapter (not a hash helper).
func TestPKCELoopbackRoundTrip(t *testing.T) {
	srv, fake := newFakeAuthServer(t)
	client, store := newLoginClient(srv)

	session, err := client.StartLogin(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	if session.AuthURL == "" || session.State == "" {
		t.Fatal("login session must carry an auth URL and state")
	}
	authURL, err := url.Parse(session.AuthURL)
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}
	if authURL.Path != AuthorizePath {
		t.Fatalf("authorize path must be %q, got %q", AuthorizePath, authURL.Path)
	}
	if authURL.Host != strings.TrimPrefix(srv.URL, "http://") {
		t.Fatalf("authorize must go to the auth origin, got %q", authURL.Host)
	}
	query := authURL.Query()
	if query.Get("code_challenge_method") != "S256" {
		t.Fatalf("S256 is mandatory, got %q", query.Get("code_challenge_method"))
	}
	if query.Get("scope") != ScopeAPI {
		t.Fatalf("scope must be %q, got %q", ScopeAPI, query.Get("scope"))
	}
	if query.Get("app_name") == "" {
		t.Fatal("app_name must be sent so the consent screen can name the app")
	}
	if !strings.HasPrefix(query.Get("callback_url"), "http://127.0.0.1:") {
		t.Fatalf("expected a loopback callback_url, got %q", query.Get("callback_url"))
	}
	if strings.Contains(session.AuthURL, "code_verifier") {
		t.Fatal("the verifier must never appear on the authorize URL")
	}

	// The redirect the browser would deliver: the absolute URL, exactly as the
	// loopback server receives it on the request line.
	callbackAbsolute := fmt.Sprintf("%s?code=FAKECODE123&state=%s", query.Get("callback_url"), url.QueryEscape(session.State))
	if err := client.acceptCallback(context.Background(), callbackAbsolute); err != nil {
		t.Fatalf("loopback callback rejected: %v", err)
	}

	done, message, err := client.PollLogin(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("poll login: %v", err)
	}
	if !done {
		t.Fatalf("login must complete, got %q", message)
	}

	format, payload := store.stored()
	if format != CredentialFormatOAuth {
		t.Fatalf("expected oauth format, got %q", format)
	}
	credential, err := DecodeCredential(payload)
	if err != nil {
		t.Fatalf("decode persisted credential: %v", err)
	}
	if credential.APIKey != fake.key {
		t.Fatalf("persisted key mismatch: %q", credential.APIKey)
	}
	if credential.UserID != "12345" || credential.Scope != "api" {
		t.Fatalf("identity/scope not persisted: %+v", credential)
	}
	if !credential.Ready() {
		t.Fatal("a freshly exchanged credential must be ready")
	}

	// The exchange must have used the auth origin, the right body, and no secret.
	fake.mu.Lock()
	exchanges := append([]map[string]string(nil), fake.exchangeBodys...)
	fake.mu.Unlock()
	if len(exchanges) != 1 {
		t.Fatalf("expected exactly one exchange, got %d", len(exchanges))
	}
	body := exchanges[0]
	if body["code"] != "FAKECODE123" {
		t.Fatalf("exchange code mismatch: %+v", body)
	}
	if body["code_challenge_method"] != "S256" {
		t.Fatalf("exchange must declare S256: %+v", body)
	}
	if body[ClientSecretParam] != "" {
		t.Fatal("the exchange must never carry a client secret")
	}
	if body["code_verifier"] == "" {
		t.Fatal("the exchange must present the verifier")
	}
	// The verifier's S256 hash must equal the challenge that rode on the
	// authorize URL the user actually opened.
	authorizeParams, err := url.ParseQuery(authURL.RawQuery)
	if err != nil {
		t.Fatalf("parse authorize query: %v", err)
	}
	if pkceChallenge(body["code_verifier"]) != authorizeParams.Get("code_challenge") {
		t.Fatal("exchange verifier does not match the authorize challenge")
	}
}

// A fresh verifier and state per attempt, and no verifier leakage in the URL.
func TestPKCEFreshPerAttemptAndNoVerifierLeak(t *testing.T) {
	srv, fake := newFakeAuthServer(t)
	client, _ := newLoginClient(srv)

	first, err := client.StartLogin(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("start 1: %v", err)
	}
	client.mu.Lock()
	firstVerifier := client.pending["acc-1"].verifier
	client.mu.Unlock()

	second, err := client.StartLogin(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("start 2: %v", err)
	}
	client.mu.Lock()
	secondVerifier := client.pending["acc-1"].verifier
	client.mu.Unlock()

	if first.State == second.State {
		t.Fatal("state must be fresh for every attempt")
	}
	if firstVerifier == secondVerifier {
		t.Fatal("verifier must be fresh for every attempt")
	}
	if strings.Contains(second.AuthURL, secondVerifier) || strings.Contains(first.AuthURL, firstVerifier) {
		t.Fatal("the verifier must never be placed on the authorize URL")
	}
	_ = fake
}

// A state mismatch must abort and must never exchange the code.
func TestPKCEStateMismatchAborts(t *testing.T) {
	srv, fake := newFakeAuthServer(t)
	client, store := newLoginClient(srv)

	if _, err := client.StartLogin(context.Background(), "acc-1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	err := client.acceptCallback(context.Background(), AuthorizePath+"?code=CODE&state=forged")
	if err == nil {
		t.Fatal("a state mismatch must fail the callback")
	}
	if !strings.Contains(err.Error(), "state") {
		t.Fatalf("expected a state error, got %q", err)
	}
	fake.mu.Lock()
	exchanges := len(fake.exchangeBodys)
	fake.mu.Unlock()
	if exchanges != 0 {
		t.Fatal("the code must never be exchanged after a state mismatch")
	}
	if format, _ := store.stored(); format != "" {
		t.Fatal("no credential may be persisted on a state mismatch")
	}
}

// Denial must end the flow cleanly with an actionable message.
func TestPKCEDenialEndsCleanly(t *testing.T) {
	srv, _ := newFakeAuthServer(t)
	client, store := newLoginClient(srv)

	session, err := client.StartLogin(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	err = client.acceptCallback(context.Background(), AuthorizePath+"?error=access_denied&state="+url.QueryEscape(session.State))
	if err == nil {
		t.Fatal("denial must surface as an error")
	}
	if !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("denial message must name the cause, got %q", err)
	}
	done, _, pollErr := client.PollLogin(context.Background(), "acc-1")
	if pollErr == nil || done {
		t.Fatal("a denied login must not poll as complete")
	}
	if format, _ := store.stored(); format != "" {
		t.Fatal("denial must not persist a credential")
	}
}

// 403 (unknown/expired/used code, or verifier mismatch) must be terminal.
func TestPKCEExchange403IsTerminal(t *testing.T) {
	srv, fake := newFakeAuthServer(t)
	client, store := newLoginClient(srv)
	fake.mu.Lock()
	fake.status = 403
	fake.errorBody = `{"error":"invalid_grant"}`
	fake.mu.Unlock()

	session, err := client.StartLogin(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	authURL, _ := url.Parse(session.AuthURL)
	callback := fmt.Sprintf("%s?code=USEDCODE&state=%s", authURL.Query().Get("callback_url"), url.QueryEscape(session.State))
	err = client.acceptCallback(context.Background(), strings.TrimPrefix(callback, authURL.Query().Get("callback_url")))
	if err == nil {
		t.Fatal("a rejected code must fail")
	}
	if format, _ := store.stored(); format != "" {
		t.Fatal("no credential may be persisted on a rejected code")
	}
	// A second poll must report the failure rather than hang.
	if _, _, pollErr := client.PollLogin(context.Background(), "acc-1"); pollErr == nil {
		t.Fatal("the failed attempt must be reported to the poller")
	}
}

// 400 (code_challenge_method downgrade defence) must be terminal.
func TestPKCEExchange400IsTerminal(t *testing.T) {
	srv, fake := newFakeAuthServer(t)
	client, _ := newLoginClient(srv)
	fake.mu.Lock()
	fake.status = 400
	fake.errorBody = `{"error":"invalid_request","error_description":"code_challenge_method"}`
	fake.mu.Unlock()

	session, err := client.StartLogin(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	authURL, _ := url.Parse(session.AuthURL)
	callback := fmt.Sprintf("%s?code=CODE&state=%s", authURL.Query().Get("callback_url"), url.QueryEscape(session.State))
	if err := client.acceptCallback(context.Background(), strings.TrimPrefix(callback, authURL.Query().Get("callback_url"))); err == nil {
		t.Fatal("a downgrade-defence 400 must fail")
	}
}

// 429 (the 10-keys-per-24h cap) must produce an actionable message, not a hang.
func TestPKCEExchange429IsActionable(t *testing.T) {
	srv, fake := newFakeAuthServer(t)
	client, _ := newLoginClient(srv)
	fake.mu.Lock()
	fake.status = 429
	fake.errorBody = `{"error":"rate_limited"}`
	fake.mu.Unlock()

	session, err := client.StartLogin(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	authURL, _ := url.Parse(session.AuthURL)
	callback := fmt.Sprintf("%s?code=CODE&state=%s", authURL.Query().Get("callback_url"), url.QueryEscape(session.State))
	err = client.acceptCallback(context.Background(), strings.TrimPrefix(callback, authURL.Query().Get("callback_url")))
	if err == nil {
		t.Fatal("429 must surface")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Fatalf("429 must be named in the message, got %q", err)
	}
}

// A granted scope narrower than requested must be recorded, not assumed away.
func TestPKCEScopeDowngradeIsRecorded(t *testing.T) {
	srv, fake := newFakeAuthServer(t)
	client, store := newLoginClient(srv)
	fake.mu.Lock()
	fake.scope = "read"
	fake.mu.Unlock()

	session, err := client.StartLogin(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	authURL, _ := url.Parse(session.AuthURL)
	callback := fmt.Sprintf("%s?code=CODE&state=%s", authURL.Query().Get("callback_url"), url.QueryEscape(session.State))
	if err := client.acceptCallback(context.Background(), strings.TrimPrefix(callback, authURL.Query().Get("callback_url"))); err != nil {
		t.Fatalf("callback: %v", err)
	}
	_, payload := store.stored()
	credential, err := DecodeCredential(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if credential.Scope != "read" {
		t.Fatalf("granted scope must be recorded verbatim, got %q", credential.Scope)
	}
}

// The verifier must never appear in an error message.
func TestPKCEVerifierNeverLeaksInErrors(t *testing.T) {
	srv, fake := newFakeAuthServer(t)
	client, _ := newLoginClient(srv)
	fake.mu.Lock()
	fake.status = 403
	fake.errorBody = `{"error":"invalid_grant","error_description":"code_verifier mismatch"}`
	fake.mu.Unlock()

	session, err := client.StartLogin(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	client.mu.Lock()
	verifier := client.pending["acc-1"].verifier
	client.mu.Unlock()

	authURL, _ := url.Parse(session.AuthURL)
	callback := fmt.Sprintf("%s?code=CODE&state=%s", authURL.Query().Get("callback_url"), url.QueryEscape(session.State))
	err = client.acceptCallback(context.Background(), strings.TrimPrefix(callback, authURL.Query().Get("callback_url")))
	if err == nil {
		t.Fatal("expected failure")
	}
	if strings.Contains(err.Error(), verifier) {
		t.Fatalf("error leaked the verifier: %q", err.Error())
	}
}

// The out-of-band completion path (Flow B) must work with a pasted code.
func TestPKCEOutOfBandCompletion(t *testing.T) {
	srv, fake := newFakeAuthServer(t)
	client, store := newLoginClient(srv)

	session, err := client.StartLogin(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := client.CompleteLogin(context.Background(), "acc-1", "OOBCODE999"); err != nil {
		t.Fatalf("oob completion: %v", err)
	}
	format, payload := store.stored()
	if format != CredentialFormatOAuth {
		t.Fatalf("expected oauth format, got %q", format)
	}
	credential, err := DecodeCredential(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if credential.APIKey != fake.key {
		t.Fatalf("unexpected key: %q", credential.APIKey)
	}
	_ = session
}

// An expired attempt must be refused with instructions, not an indefinite poll.
func TestPKCEExpiredAttemptIsRefused(t *testing.T) {
	srv, _ := newFakeAuthServer(t)
	client, _ := newLoginClient(srv)
	if _, err := client.StartLogin(context.Background(), "acc-1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	client.mu.Lock()
	client.pending["acc-1"].createdAt = time.Now().Add(-2 * loginPendingTTL)
	client.mu.Unlock()
	if _, _, err := client.PollLogin(context.Background(), "acc-1"); err == nil {
		t.Fatal("an expired attempt must be reported")
	}
}

// A complete attempt reuses the stored key instead of minting another one.
func TestSuccessfulLoginIsReusedNotRepeated(t *testing.T) {
	srv, fake := newFakeAuthServer(t)
	client, _ := newLoginClient(srv)

	session, err := client.StartLogin(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	authURL, _ := url.Parse(session.AuthURL)
	callback := fmt.Sprintf("%s?code=CODE&state=%s", authURL.Query().Get("callback_url"), url.QueryEscape(session.State))
	if err := client.acceptCallback(context.Background(), strings.TrimPrefix(callback, authURL.Query().Get("callback_url"))); err != nil {
		t.Fatalf("callback: %v", err)
	}
	// A later call resolves the stored credential without a second exchange.
	credential, err := client.credential(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("credential: %v", err)
	}
	if credential.APIKey != fake.key {
		t.Fatalf("stored key not reused: %q", credential.APIKey)
	}
	fake.mu.Lock()
	count := len(fake.exchangeBodys)
	fake.mu.Unlock()
	if count != 1 {
		t.Fatalf("expected exactly one exchange, got %d", count)
	}
}
