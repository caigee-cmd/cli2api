package trae

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

type memStore struct {
	// mu guards every field below. Redemption runs on a background goroutine
	// after the loopback callback, so the store is reached concurrently.
	mu       sync.Mutex
	items    map[string][]byte
	region   string
	settings map[string]accounts.ProviderModelSetting
	lookups  []string

	accountProxyURL string
	secretValue     string
	secretFound     bool
	secretErr       error
	secretCalls     int
}

func (s *memStore) Get(ctx context.Context, id string) (accounts.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	region := s.region
	if region == "" {
		region = "cn"
	}
	return accounts.Account{ID: id, Provider: "trae", ProviderRegion: region, ProxyURL: s.accountProxyURL}, nil
}
func (s *memStore) GetSecret(ctx context.Context, key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secretCalls++
	if s.secretErr != nil {
		return "", false, s.secretErr
	}
	return s.secretValue, s.secretFound, nil
}
func (s *memStore) LoadCredentialPayload(ctx context.Context, accountID string) (string, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, ok := s.items[accountID]
	if !ok {
		return "", nil, accounts.ErrAccountNotFound
	}
	return CredentialFormat, payload, nil
}
func (s *memStore) SaveCredentialPayload(ctx context.Context, accountID, format string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		s.items = map[string][]byte{}
	}
	s.items[accountID] = payload
	return nil
}
func (s *memStore) Observe(ctx context.Context, id, remoteUID, status, lastError, lastKind string) error {
	return nil
}
func (s *memStore) GetProviderModelSetting(ctx context.Context, provider, modelID string) (accounts.ProviderModelSetting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lookups = append(s.lookups, provider+"/"+modelID)
	if s.settings == nil {
		return accounts.ProviderModelSetting{}, nil
	}
	return s.settings[modelID], nil
}

func newTestClient(t *testing.T, handler http.Handler) (*Client, *memStore) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	store := &memStore{}
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}
	return client, store
}

type rewriteTransport struct {
	server string
	round  http.RoundTripper
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(strings.TrimPrefix(t.server, "http://"), "https://")
	if t.round == nil {
		return http.DefaultTransport.RoundTrip(req)
	}
	return t.round.RoundTrip(req)
}

const soloSSE = "event: metadata\ndata: {\"model\":\"glm-5.2\"}\n\n" +
	"event: output\ndata: {\"reasoning_content\":\"think\"}\n\n" +
	"event: output\ndata: {\"response\":\"O\"}\n\n" +
	"event: output\ndata: {\"response\":\"K\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function_call\":{\"name\":\"get_time\",\"arguments\":\"{}\"}}]}\n\n" +
	"event: token_usage\ndata: {\"prompt_tokens\":3,\"completion_tokens\":2}\n\n" +
	"event: done\ndata: {\"finish_reason\":\"tool_calls\"}\n\n"

func TestDecodeCredentialNestedAndFlat(t *testing.T) {
	nested := []byte(`{"account":{"uid":"u1","enterpriseId":"e1","nickname":"N"},"auth":{"accessToken":"at","refreshToken":"rt","expiresAt":4102444800000,"domain":"trae.cn","machineId":"m1","deviceId":"d1"}}`)
	credential, err := DecodeCredential(nested)
	if err != nil || credential.UID != "u1" || credential.RefreshToken != "rt" || credential.ExpiresAt != 4102444800 {
		t.Fatalf("nested=%+v err=%v", credential, err)
	}
	flat, err := DecodeCredential([]byte(`{"access_token":"at","refresh_token":"rt","uid":"u1"}`))
	if err != nil || !flat.Ready() {
		t.Fatalf("flat=%+v err=%v", flat, err)
	}
}

func TestLoginCallbackStoresDeviceAndToken(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathExchange:
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
				"Token": "at", "RefreshToken": "rt2", "TokenExpireAt": time.Now().Add(time.Hour).Unix(),
			}})
		case pathUserInfo:
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
				"UserID": "u1", "ScreenName": "Tester", "EnterpriseID": "e1",
			}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	session, err := client.StartLogin(context.Background(), "acc1")
	if err != nil || session.AuthURL == "" || !strings.Contains(session.AuthURL, "auth_from=trae") {
		t.Fatalf("session=%+v err=%v", session, err)
	}
	if !strings.Contains(session.AuthURL, "code_challenge=") || !strings.Contains(session.AuthURL, "client_id="+ClientID) {
		t.Fatalf("login must carry PKCE challenge + IDE client: %s", session.AuthURL)
	}
	if !strings.Contains(session.AuthURL, "127.0.0.1") {
		t.Fatalf("callback must be loopback: %s", session.AuthURL)
	}
	client.mu.Lock()
	pending := client.pending["acc1"]
	pending.done = true
	pending.credential = Credential{RefreshToken: "rt", MachineID: pending.machineID, DeviceID: pending.deviceID}
	client.mu.Unlock()
	done, _, err := client.PollLogin(context.Background(), "acc1")
	if err != nil || !done {
		t.Fatalf("done=%v err=%v", done, err)
	}
	_, payload, err := store.LoadCredentialPayload(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := DecodeCredential(payload)
	if err != nil || credential.UID != "u1" || credential.AccessToken != "at" || credential.MachineID == "" || credential.DeviceID == "" {
		t.Fatalf("credential=%+v err=%v", credential, err)
	}
}

func TestCompleteLoginAcceptsPastedCallbackURL(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathExchange:
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
				"Token": "at", "RefreshToken": "rt2", "TokenExpireAt": time.Now().Add(time.Hour).Unix(),
			}})
		case pathUserInfo:
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
				"UserID": "u1", "ScreenName": "Tester", "EnterpriseID": "e1",
			}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	if _, err := client.StartLogin(context.Background(), "acc1"); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	machine, device := client.pending["acc1"].machineID, client.pending["acc1"].deviceID
	client.mu.Unlock()
	err := client.CompleteLogin(context.Background(), "acc1",
		`http://127.0.0.1:9/authorize?refreshToken=rt&userInfo={"UserID":"u1","ScreenName":"N"}`)
	if err != nil {
		t.Fatal(err)
	}
	_, payload, err := store.LoadCredentialPayload(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := DecodeCredential(payload)
	if err != nil || credential.UID != "u1" || credential.AccessToken != "at" || credential.MachineID != machine || credential.DeviceID != device {
		t.Fatalf("credential=%+v err=%v want machine=%s device=%s", credential, err, machine, device)
	}
}

func TestCompleteLoginIsIdempotentWhenAlreadyReady(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", ExpiresAt: 4102444800, MachineID: "m1", DeviceID: "d1"}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	client := NewClient(store)
	client.http = (&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("already-ready complete login must not hit upstream")
		return nil, nil
	})})
	if err := client.CompleteLogin(context.Background(), "acc1", `http://127.0.0.1:9/authorize?refreshToken=rt`); err != nil {
		t.Fatal(err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// mustKey returns a fresh EC device private key, the shape a real login stores
// and the code exchange signs with.
func mustKey(t *testing.T) string {
	t.Helper()
	priv, _, err := generateDeviceKey()
	if err != nil {
		t.Fatal(err)
	}
	return priv
}

// A callback captured by the loopback listener but never redeemed by the poll
// loop (the console's poll window can expire first) must still be exchangeable
// through a pasted callback URL. Reporting success without storing a credential
// left the account permanently at "account not found".
func TestCompleteLoginRedeemsCodeCapturedByLoopback(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathExchangeCode:
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
				"Token": "at", "RefreshToken": "rt2", "TokenExpireAt": time.Now().Add(time.Hour).Unix(),
			}})
		case pathUserInfo:
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
				"UserID": "u1", "ScreenName": "Tester", "EnterpriseID": "e1",
			}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	session, err := client.StartLogin(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	// The browser hits the loopback listener: the callback is parked as done
	// with the code attached, exactly as acceptCallback leaves it.
	client.mu.Lock()
	pending := client.pending["acc1"]
	pending.done = true
	pending.authCode = "ABC-123"
	pending.credential = Credential{
		UID: "u1", Nickname: "Tester", EnterpriseID: "e1",
		MachineID: pending.machineID, DeviceID: pending.deviceID,
	}
	machine, device := pending.machineID, pending.deviceID
	client.mu.Unlock()

	// The operator pastes the callback URL afterwards; the poll loop never ran.
	callback := "http://127.0.0.1:49519/authorize?loginTraceID=" + session.State +
		"&authCodeInfo=%7B%22AuthCode%22%3A%22ABC-123%22%7D"
	if err := client.CompleteLogin(context.Background(), "acc1", callback); err != nil {
		t.Fatal(err)
	}
	_, payload, err := store.LoadCredentialPayload(context.Background(), "acc1")
	if err != nil {
		t.Fatalf("credential must be stored: %v", err)
	}
	credential, err := DecodeCredential(payload)
	if err != nil {
		t.Fatal(err)
	}
	if credential.UID != "u1" || credential.AccessToken != "at" || credential.RefreshToken != "rt2" {
		t.Fatalf("credential=%+v", credential)
	}
	if credential.MachineID != machine || credential.DeviceID != device {
		t.Fatalf("device identity must survive: got machine=%s device=%s want machine=%s device=%s",
			credential.MachineID, credential.DeviceID, machine, device)
	}
	if credential.DevicePrivateKey == "" {
		t.Fatal("device key must be stored for later code exchanges")
	}
}

// The pasted URL may carry nothing but the trace id; the code parked by the
// loopback listener is the only source of the authorization code.
func TestCompleteLoginFallsBackToCapturedAuthCode(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathExchangeCode:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["AuthCode"] != "PARKED-CODE" {
				t.Fatalf("exchange must redeem the parked code, body=%v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
				"Token": "at", "RefreshToken": "rt2", "TokenExpireAt": time.Now().Add(time.Hour).Unix(),
			}})
		case pathUserInfo:
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{"UserID": "u1"}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	session, err := client.StartLogin(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	pending := client.pending["acc1"]
	pending.done = true
	pending.authCode = "PARKED-CODE"
	pending.credential = Credential{MachineID: pending.machineID, DeviceID: pending.deviceID}
	client.mu.Unlock()

	callback := "http://127.0.0.1:49519/authorize?loginTraceID=" + session.State
	if err := client.CompleteLogin(context.Background(), "acc1", callback); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.LoadCredentialPayload(context.Background(), "acc1"); err != nil {
		t.Fatalf("credential must be stored: %v", err)
	}
}

// A callback with no code and no token must fail loudly rather than persist an
// empty credential the probe would then report as ready.
func TestCompleteLoginRejectsCallbackWithoutCodeOrToken(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("no upstream call without a code or token")
	}))
	if _, err := client.StartLogin(context.Background(), "acc1"); err != nil {
		t.Fatal(err)
	}
	err := client.CompleteLogin(context.Background(), "acc1", "http://127.0.0.1:49519/authorize?isRedirect=true")
	if err == nil {
		t.Fatal("expected an error for a callback with neither code nor token")
	}
	if _, _, loadErr := store.LoadCredentialPayload(context.Background(), "acc1"); loadErr == nil {
		t.Fatal("no credential may be stored for an empty callback")
	}
}

// A redeemed login must not leave a trace index behind for a later callback to
// match against a credential that was already exchanged.
func TestPollLoginClearsTraceIndexAfterRedeem(t *testing.T) {
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathExchangeCode:
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
				"Token": "at", "RefreshToken": "rt2", "TokenExpireAt": time.Now().Add(time.Hour).Unix(),
			}})
		case pathUserInfo:
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{"UserID": "u1"}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	session, err := client.StartLogin(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	pending := client.pending["acc1"]
	pending.done = true
	pending.authCode = "ABC-123"
	pending.credential = Credential{MachineID: pending.machineID, DeviceID: pending.deviceID}
	client.mu.Unlock()

	if done, _, err := client.PollLogin(context.Background(), "acc1"); err != nil || !done {
		t.Fatalf("done=%v err=%v", done, err)
	}
	client.mu.Lock()
	_, stillPending := client.pending["acc1"]
	_, stillTraced := client.pendingTraces[session.State]
	client.mu.Unlock()
	if stillPending || stillTraced {
		t.Fatalf("redeemed login must clear pending (pending=%v trace=%v)", stillPending, stillTraced)
	}
}

// After a pasted callback completes the login, a poll still in flight from the
// browser flow must report success rather than "login not started".
func TestPollLoginReportsReadyAfterPastedCallback(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathExchangeCode:
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
				"Token": "at", "RefreshToken": "rt2", "TokenExpireAt": time.Now().Add(time.Hour).Unix(),
			}})
		case pathUserInfo:
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{"UserID": "u1"}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	session, err := client.StartLogin(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	pending := client.pending["acc1"]
	pending.done = true
	pending.authCode = "ABC-123"
	pending.credential = Credential{MachineID: pending.machineID, DeviceID: pending.deviceID}
	client.mu.Unlock()

	callback := "http://127.0.0.1:49519/authorize?loginTraceID=" + session.State
	if err := client.CompleteLogin(context.Background(), "acc1", callback); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.LoadCredentialPayload(context.Background(), "acc1"); err != nil {
		t.Fatalf("credential must be stored: %v", err)
	}
	done, message, err := client.PollLogin(context.Background(), "acc1")
	if err != nil {
		t.Fatalf("poll after paste must not fail: %v", err)
	}
	if !done {
		t.Fatalf("poll after paste must report done, message=%q", message)
	}
}

// The loopback callback must redeem the code on its own. The console's poll
// window is much shorter than a real browser login, so waiting for a poller
// left the code unexchanged and the account at "account not found".
func TestAcceptCallbackRedeemsWithoutPolling(t *testing.T) {
	var exchanges int32
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathExchangeCode:
			atomic.AddInt32(&exchanges, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
				"Token": "at", "RefreshToken": "rt2", "TokenExpireAt": time.Now().Add(time.Hour).Unix(),
			}})
		case pathUserInfo:
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
				"UserID": "u1", "ScreenName": "Tester", "EnterpriseID": "e1",
			}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	session, err := client.StartLogin(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	// The browser lands on the loopback listener; no poll ever runs.
	callback := "http://127.0.0.1:9/authorize?loginTraceID=" + session.State +
		"&authCodeInfo=%7B%22AuthCode%22%3A%22ABC-123%22%7D"
	if err := client.acceptCallback(context.Background(), callback); err != nil {
		t.Fatal(err)
	}
	// The exchange runs in the background, so wait for the credential.
	var credential Credential
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, payload, err := store.LoadCredentialPayload(context.Background(), "acc1"); err == nil {
			if decoded, err := DecodeCredential(payload); err == nil && decoded.Ready() {
				credential = decoded
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !credential.Ready() {
		t.Fatal("callback must redeem the code and store a credential without any poll")
	}
	if credential.UID != "u1" || credential.AccessToken != "at" || credential.RefreshToken != "rt2" {
		t.Fatalf("credential=%+v", credential)
	}
	if credential.DevicePrivateKey == "" {
		t.Fatal("device key must be stored for later code exchanges")
	}
	if got := atomic.LoadInt32(&exchanges); got != 1 {
		t.Fatalf("code must be exchanged exactly once, got %d", got)
	}
	// A poll racing the background exchange must report the same success, not
	// "login not started".
	done, _, err := client.PollLogin(context.Background(), "acc1")
	if err != nil || !done {
		t.Fatalf("poll after callback done=%v err=%v", done, err)
	}
}

// The authorization code is single-use, so overlapping redeemers must produce
// exactly one exchange and share its result.
func TestRedeemExchangesCodeOnceUnderConcurrency(t *testing.T) {
	var exchanges int32
	release := make(chan struct{})
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathExchangeCode:
			atomic.AddInt32(&exchanges, 1)
			<-release // hold the exchange open so both callers overlap
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
				"Token": "at", "RefreshToken": "rt2", "TokenExpireAt": time.Now().Add(time.Hour).Unix(),
			}})
		case pathUserInfo:
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{"UserID": "u1"}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	if _, err := client.StartLogin(context.Background(), "acc1"); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	pending := client.pending["acc1"]
	pending.done = true
	pending.authCode = "ABC-123"
	pending.credential = Credential{MachineID: pending.machineID, DeviceID: pending.deviceID}
	credential, authCode, verifier := pending.credential, pending.authCode, pending.codeVerifier
	client.mu.Unlock()

	errs := make(chan error, 2)
	go func() {
		errs <- client.redeem(context.Background(), "acc1", pending, credential, authCode, verifier)
	}()
	// Wait for the first exchange to be in flight before starting the second.
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt32(&exchanges) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	go func() {
		errs <- client.redeem(context.Background(), "acc1", pending, credential, authCode, verifier)
	}()
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent redeem failed: %v", err)
		}
	}
	if got := atomic.LoadInt32(&exchanges); got != 1 {
		t.Fatalf("single-use code must be exchanged once, got %d exchanges", got)
	}
}

// A failed exchange must not burn the guard, so the operator can retry the
// same callback instead of restarting the whole login.
func TestRedeemAllowsRetryAfterFailure(t *testing.T) {
	var exchanges int32
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathExchangeCode:
			if atomic.AddInt32(&exchanges, 1) == 1 {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"error":"upstream blip"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
				"Token": "at", "RefreshToken": "rt2", "TokenExpireAt": time.Now().Add(time.Hour).Unix(),
			}})
		case pathUserInfo:
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{"UserID": "u1"}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	if _, err := client.StartLogin(context.Background(), "acc1"); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	pending := client.pending["acc1"]
	pending.done = true
	pending.authCode = "ABC-123"
	pending.credential = Credential{MachineID: pending.machineID, DeviceID: pending.deviceID}
	credential, authCode, verifier := pending.credential, pending.authCode, pending.codeVerifier
	client.mu.Unlock()

	if err := client.redeem(context.Background(), "acc1", pending, credential, authCode, verifier); err == nil {
		t.Fatal("first exchange must surface the upstream failure")
	}
	if err := client.redeem(context.Background(), "acc1", pending, credential, authCode, verifier); err != nil {
		t.Fatalf("retry after a transient failure must succeed: %v", err)
	}
	if _, _, err := store.LoadCredentialPayload(context.Background(), "acc1"); err != nil {
		t.Fatalf("credential must be stored after the retry: %v", err)
	}
}

// The loopback listener is shared across accounts, so a callback must be paired
// with the login whose challenge it echoes, not with whichever entry the map
// happens to yield first.
func TestAcceptCallbackPairsByTraceAcrossAccounts(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathExchangeCode:
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
				"Token": "at", "RefreshToken": "rt2", "TokenExpireAt": time.Now().Add(time.Hour).Unix(),
			}})
		case pathUserInfo:
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{"UserID": "u1"}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	first, err := client.StartLogin(context.Background(), "acc-first")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.StartLogin(context.Background(), "acc-second"); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	wantMachine := client.pending["acc-first"].machineID
	client.mu.Unlock()

	// The callback echoes the first login's trace id.
	callback := "http://127.0.0.1:9/authorize?loginTraceID=" + first.State +
		"&authCodeInfo=%7B%22AuthCode%22%3A%22ABC-123%22%7D"
	if err := client.acceptCallback(context.Background(), callback); err != nil {
		t.Fatal(err)
	}
	var credential Credential
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, payload, err := store.LoadCredentialPayload(context.Background(), "acc-first"); err == nil {
			if decoded, err := DecodeCredential(payload); err == nil && decoded.Ready() {
				credential = decoded
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !credential.Ready() {
		t.Fatal("the traced account must receive the credential")
	}
	if credential.MachineID != wantMachine {
		t.Fatalf("credential machine=%s want %s", credential.MachineID, wantMachine)
	}
	if _, _, err := store.LoadCredentialPayload(context.Background(), "acc-second"); err == nil {
		t.Fatal("the other account's login must stay untouched")
	}
}

// The CN front doors disagree about which host mints a code: the official
// client exchanges at the host product.json configures (api.trae.cn) while the
// callback advertises api.trae.com.cn. A code minted by one must be redeemable
// even when the other host rejects it as an invalid parameter.
func TestExchangeAuthCodeFallsBackAcrossCNHosts(t *testing.T) {
	var hosts []string
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hosts = append(hosts, r.Host)
		// The first host (api.trae.cn) rejects the code; the callback's host accepts it.
		if len(hosts) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ResponseMetadata":{"Error":{"Code":"10101","Message":"无效参数"}}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
			"Token": "at", "RefreshToken": "rt2", "TokenExpireAt": time.Now().Add(time.Hour).Unix(),
		}})
	}))
	credential := Credential{
		MachineID: "m1", DeviceID: "1234567890123456",
		DevicePrivateKey: mustKey(t), APIHost: "https://api.trae.com.cn", Domain: DomainCN,
	}
	refreshed, err := client.ExchangeAuthCode(context.Background(), "acc1", credential, "CODE-XYZ", "verifier")
	if err != nil {
		t.Fatalf("a code rejected by the configured host must still be tried on the callback host: %v", err)
	}
	if refreshed.AccessToken != "at" {
		t.Fatalf("credential=%+v", refreshed)
	}
	if len(hosts) != 2 {
		t.Fatalf("expected two host attempts, got %v", hosts)
	}
}

// A transient upstream failure must surface, not be masked by a second host
// attempt that happens to succeed.
func TestExchangeAuthCodeSurfacesTransientFailure(t *testing.T) {
	var attempts int32
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"upstream blip"}`))
	}))
	credential := Credential{
		MachineID: "m1", DeviceID: "1234567890123456",
		DevicePrivateKey: mustKey(t), APIHost: "https://api.trae.com.cn", Domain: DomainCN,
	}
	if _, err := client.ExchangeAuthCode(context.Background(), "acc1", credential, "CODE-XYZ", "verifier"); err == nil {
		t.Fatal("a 502 must surface as an error")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("a transient failure must not be retried on another host, got %d attempts", got)
	}
}

func TestChatNonStreamAggregatesToolsAndReasoning(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", Domain: DomainCN, ExpiresAt: 4102444800, MachineID: "m1", DeviceID: "d1"}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pathModels {
			_ = json.NewEncoder(w).Encode(map[string]any{"config_info_list": []map[string]any{{
				"config_name": "glm-5.2", "display_config": map[string]any{"display_name": "GLM-5.2"},
			}}})
			return
		}
		if r.URL.Path != pathChat {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Cloud-IDE-JWT at" || r.Header.Get("X-Uid") != "u1" ||
			r.Header.Get("X-Ide-Version") != IdeVersion || r.Header.Get("X-Machine-Id") != "m1" {
			t.Fatalf("headers missing: %+v", r.Header)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] != true || body["function"] != PrimaryScene || body["config_name"] != "glm-5.2" {
			t.Fatalf("body=%v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(soloSSE))
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}

	out, err := client.ChatNonStream(context.Background(), "acc1", translate.ChatRequest{
		Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Content != "OK" || out.Reasoning != "think" || out.FinishReason != "tool_calls" || out.PromptTokens != 3 {
		t.Fatalf("outcome=%+v", out)
	}
	if !strings.Contains(string(out.ToolCalls), "get_time") {
		t.Fatalf("tool calls=%s", out.ToolCalls)
	}
}

// Solo answers 200 and only then fails inside the stream, so the quota error
// cannot surface as a ChatStream return value: bytes are already committed.
// The rewritten body must report it as a read error once drained.
func TestChatStreamQuotaErrorAfterContentIsReadable(t *testing.T) {
	stream := "event: metadata\ndata: {\"model\":\"deepseek-v4-flash\"}\n\n" +
		"event: output\ndata: {\"response\":\"partial\"}\n\n" +
		"event: error\ndata: {\"code\":4008,\"message\":\"Your requests have exceeded the quota.\"}\n\n"
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pathModels {
			_ = json.NewEncoder(w).Encode(map[string]any{"config_info_list": []map[string]any{{
				"config_name": "deepseek-v4-flash", "display_config": map[string]any{"display_name": "DS"},
			}}})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(stream))
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}

	resp, _, err := client.ChatStream(context.Background(), "acc1", translate.ChatRequest{Model: "deepseek-v4-flash"})
	if err != nil {
		t.Fatalf("upstream 200 must not fail up front: %v", err)
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "partial") {
		t.Fatalf("buffered content must still reach the client: %s", body)
	}
	var classified *providers.Error
	if !errors.As(readErr, &classified) {
		t.Fatalf("readErr=%v", readErr)
	}
	if classified.Kind != accounts.KindQuota {
		t.Fatalf("kind=%s", classified.Kind)
	}
	if classified.RetryAfter != 0 {
		t.Fatalf("retry_after=%v", classified.RetryAfter)
	}
}

func TestChatStreamQuotaErrorBeforeContentStillFailsUpFront(t *testing.T) {
	stream := "event: metadata\ndata: {\"model\":\"deepseek-v4-flash\"}\n\n" +
		"event: error\ndata: {\"code\":4008,\"message\":\"Your requests have exceeded the quota.\"}\n\n"
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pathModels {
			_ = json.NewEncoder(w).Encode(map[string]any{"config_info_list": []map[string]any{{
				"config_name": "deepseek-v4-flash", "display_config": map[string]any{"display_name": "DS"},
			}}})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(stream))
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}

	resp, _, err := client.ChatStream(context.Background(), "acc1", translate.ChatRequest{Model: "deepseek-v4-flash"})
	if resp != nil {
		resp.Body.Close()
	}
	var classified *providers.Error
	if !errors.As(err, &classified) || classified.Kind != accounts.KindQuota {
		t.Fatalf("err=%v classified=%+v", err, classified)
	}
}

func TestChatStreamRewritesOpenAIChunks(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pathModels {
			_ = json.NewEncoder(w).Encode(map[string]any{"config_info_list": []map[string]any{{
				"config_name": "glm-5.2", "display_config": map[string]any{"display_name": "GLM-5.2"},
			}}})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(soloSSE))
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}
	resp, _, err := client.ChatStream(context.Background(), "acc1", translate.ChatRequest{Model: "glm-5.2"})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if !strings.Contains(text, `"object":"chat.completion.chunk"`) || !strings.Contains(text, "data: [DONE]") {
		t.Fatalf("stream=%s", text)
	}
	if !strings.Contains(text, `"finish_reason":"tool_calls"`) || !strings.Contains(text, `"prompt_tokens":3`) {
		t.Fatalf("terminal chunk missing finish_reason or usage: %s", text)
	}
	if strings.Contains(text, "event: output") {
		t.Fatalf("must rewrite solo events, got %s", text)
	}
}

const soloSSEStopWithTools = "event: metadata\ndata: {\"model\":\"glm-5.2\"}\n\n" +
	"event: output\ndata: {\"response\":\"Yes\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function_call\":{\"name\":\"get_time\",\"arguments\":\"{}\"}}]}\n\n" +
	"event: token_usage\ndata: {\"prompt_tokens\":3,\"completion_tokens\":2}\n\n" +
	"event: done\ndata: {\"finish_reason\":\"stop\"}\n\n"

func TestChatNonStreamSalvagesStopFinishReasonWithToolCalls(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", Domain: DomainCN, ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pathModels {
			_ = json.NewEncoder(w).Encode(map[string]any{"config_info_list": []map[string]any{{
				"config_name": "glm-5.2", "display_config": map[string]any{"display_name": "GLM-5.2"},
			}}})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(soloSSEStopWithTools))
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}
	out, err := client.ChatNonStream(context.Background(), "acc1", translate.ChatRequest{Model: "glm-5.2"})
	if err != nil {
		t.Fatal(err)
	}
	if out.FinishReason != "tool_calls" {
		t.Fatalf("finish=%q", out.FinishReason)
	}
	if !strings.Contains(string(out.ToolCalls), "get_time") {
		t.Fatalf("tool calls=%s", out.ToolCalls)
	}
}

func TestChatStreamSalvagesStopFinishReasonWithToolCalls(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pathModels {
			_ = json.NewEncoder(w).Encode(map[string]any{"config_info_list": []map[string]any{{
				"config_name": "glm-5.2", "display_config": map[string]any{"display_name": "GLM-5.2"},
			}}})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(soloSSEStopWithTools))
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}
	resp, _, err := client.ChatStream(context.Background(), "acc1", translate.ChatRequest{Model: "glm-5.2"})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"finish_reason":"tool_calls"`) {
		t.Fatalf("stream=%s", body)
	}
	if strings.Contains(string(body), `"finish_reason":"stop"`) {
		t.Fatalf("stop must not win over tool_calls: %s", body)
	}
}

func TestChatStreamSalvagesToolCallsWhenDoneMissing(t *testing.T) {
	stream := "event: metadata\ndata: {\"model\":\"glm-5.2\"}\n\n" +
		"event: output\ndata: {\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function_call\":{\"name\":\"get_time\",\"arguments\":\"{}\"}}]}\n\n"
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pathModels {
			_ = json.NewEncoder(w).Encode(map[string]any{"config_info_list": []map[string]any{{
				"config_name": "glm-5.2", "display_config": map[string]any{"display_name": "GLM-5.2"},
			}}})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(stream))
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}
	resp, _, err := client.ChatStream(context.Background(), "acc1", translate.ChatRequest{Model: "glm-5.2"})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"finish_reason":"tool_calls"`) {
		t.Fatalf("missing done after tools must salvage tool_calls: %s", body)
	}
}

func TestChatRequestForwardsResponseFormat(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", Domain: DomainCN, ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pathModels {
			_ = json.NewEncoder(w).Encode(map[string]any{"config_info_list": []map[string]any{{
				"config_name": "glm-5.2", "display_config": map[string]any{"display_name": "GLM-5.2"},
			}}})
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(soloSSE))
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}
	_, err := client.ChatNonStream(context.Background(), "acc1", translate.ChatRequest{
		Model:          "glm-5.2",
		ResponseFormat: json.RawMessage(`{"type":"json_object"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	format, _ := got["response_format"].(map[string]any)
	if format["type"] != "json_object" {
		t.Fatalf("response_format=%v", got["response_format"])
	}
}

func TestNormalizeFinishReason(t *testing.T) {
	if got := normalizeFinishReason("stop", true); got != "tool_calls" {
		t.Fatalf("stop+tools=%q", got)
	}
	if got := normalizeFinishReason("", true); got != "tool_calls" {
		t.Fatalf("empty+tools=%q", got)
	}
	if got := normalizeFinishReason("length", true); got != "length" {
		t.Fatalf("length must stay: %q", got)
	}
	if got := normalizeFinishReason("stop", false); got != "stop" {
		t.Fatalf("stop without tools=%q", got)
	}
}

func TestChatStreamFirstErrorFailsBeforeOpenAIChunks(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pathModels {
			_ = json.NewEncoder(w).Encode(map[string]any{"config_info_list": []map[string]any{{
				"config_name": "Doubao-Seed-Evolving", "display_config": map[string]any{"display_name": "Seed-Evolving"},
			}}})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: metadata\ndata: {\"model\":\"Doubao-Seed-Evolving\"}\n\nevent: error\ndata: {\"code\":1005,\"message\":\"\"}\n\n"))
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}
	resp, _, err := client.ChatStream(context.Background(), "acc1", translate.ChatRequest{Model: "Doubao-Seed-Evolving"})
	if resp != nil {
		resp.Body.Close()
	}
	var classified *providers.Error
	if err == nil || !errors.As(err, &classified) || classified.Kind != accounts.KindQuota {
		t.Fatalf("err=%v classified=%+v", err, classified)
	}
}

func TestModelsFromConfigInfoList(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathModels {
			t.Fatalf("path=%s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"config_info_list": []map[string]any{
				{"config_name": "glm-5.2", "display_config": map[string]any{"display_name": "GLM 5.2"}},
				{"config_name": "glm-5.3", "display_config": map[string]any{"display_name": "GLM 5.3"}},
			},
		})
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}
	models, err := client.Models(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].NativeModel != "glm-5.2" || models[1].DisplayName != "GLM 5.3" {
		t.Fatalf("models=%+v", models)
	}
}

func TestChatRequestMapsOpenAIReasoningAndMaxMode(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", Domain: DomainCN, ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pathModels {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"config_info_list": []map[string]any{{
					"config_name":           "DeepSeek-V4-Pro-Official",
					"context_window_tokens": map[string]any{"dev": 200000, "max": 1000000},
					"display_config":        map[string]any{"display_name": "DeepSeek-V4-Pro 正式版"},
					"reasoning_effort_config": map[string]any{
						"support_thinking": true,
						"options":          []string{"low", "medium", "high", "xhigh"},
						"default_level":    "medium",
					},
				}},
			})
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(soloSSE))
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}
	if _, err := client.Models(context.Background(), "acc1"); err != nil {
		t.Fatal(err)
	}
	max := true
	_, err := client.ChatNonStream(context.Background(), "acc1", translate.ChatRequest{
		Model:           "DeepSeek-V4-Pro-Official",
		IsMaxMode:       &max,
		ReasoningEffort: json.RawMessage(`"extra_high"`),
		ContextLength:   json.RawMessage(`500000`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["is_max_mode"] != float64(1) || got["reasoning_effort_level"] != "xhigh" {
		t.Fatalf("body=%v", got)
	}
	if _, ok := got["context_length"]; ok {
		t.Fatalf("qoder context leaked: %v", got)
	}
}

// Regression: settings saved by the console are keyed by canonicalModelID
// (lowercase, _ and space folded to -). The chat-time lookup must use the
// same form, otherwise max_mode never reaches the upstream payload.
func TestChatRequestFindsStoredMaxModeByCanonicalKey(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", Domain: DomainCN, ExpiresAt: 4102444800}.Encode()
	store := &memStore{
		items: map[string][]byte{"acc1": payload},
		// Console saved "deepseek_v4_pro_official" (underscore form).
		settings: map[string]accounts.ProviderModelSetting{
			"deepseek-v4-pro-official": {MaxMode: true},
		},
	}
	var got map[string]any
	modelsServed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pathModels {
			modelsServed = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"config_info_list": []map[string]any{{
					"config_name":           "DeepSeek-V4-Pro-Official",
					"context_window_tokens": map[string]any{"dev": 200000, "max": 1000000},
					"display_config":        map[string]any{"display_name": "DeepSeek-V4-Pro 正式版"},
				}},
			})
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(soloSSE))
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}

	// No IsMaxMode in the request and no warm catalog: the stored setting
	// must still turn on max mode, and the cold-catalog refresh must run so
	// caps say the model supports it.
	if _, err := client.ChatNonStream(context.Background(), "acc1", translate.ChatRequest{
		Model: "DeepSeek-V4-Pro-Official",
	}); err != nil {
		t.Fatal(err)
	}
	if got["is_max_mode"] != float64(1) {
		t.Fatalf("stored max mode not applied: %v", got)
	}
	if len(store.lookups) == 0 || store.lookups[0] != "trae/deepseek-v4-pro-official" {
		t.Fatalf("lookup key mismatch: %v", store.lookups)
	}
	if !modelsServed {
		t.Fatal("cold catalog should be refreshed before deciding max mode")
	}
}

// When max mode is on but the model has no max-mode capability, the field is
// dropped and the request still goes out (with a server-side log line).
func TestChatRequestDropsMaxModeWhenCatalogSaysUnsupported(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", Domain: DomainCN, ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pathModels {
			// Model exists but no max window, no max-mode flags.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"config_info_list": []map[string]any{{
					"config_name":    "glm-5.2",
					"display_config": map[string]any{"display_name": "GLM 5.2"},
				}},
			})
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(soloSSE))
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}
	if _, err := client.Models(context.Background(), "acc1"); err != nil {
		t.Fatal(err)
	}
	max := true
	if _, err := client.ChatNonStream(context.Background(), "acc1", translate.ChatRequest{
		Model:     "glm-5.2",
		IsMaxMode: &max,
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["is_max_mode"]; ok {
		t.Fatalf("unsupported max mode must not be sent: %v", got)
	}
}

func TestSettingModelKeyMatchesConsoleCanonicalForm(t *testing.T) {
	cases := map[string]string{
		"DeepSeek-V4-Pro-Official": "deepseek-v4-pro-official",
		"deepseek_v4_pro_official": "deepseek-v4-pro-official",
		"GLM 5.3":                  "glm-5.3",
		"glm-5.2":                  "glm-5.2",
	}
	for input, want := range cases {
		if got := settingModelKey(input); got != want {
			t.Fatalf("settingModelKey(%q)=%q want %q", input, got, want)
		}
	}
}

func TestModelsEmptyIsError(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"config_info_list": []any{}})
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}
	if _, err := client.Models(context.Background(), "acc1"); err == nil {
		t.Fatal("expected empty catalog error")
	}
}

func TestErrorMappingAndCooldown(t *testing.T) {
	cases := []struct {
		status int
		body   string
		kind   string
		code   string
	}{
		{401, `{"code":1001}`, "auth", "1001"},
		{200, `{"code":1005,"message":"plan"}`, "quota", "1005"},
		{200, `{"code":4008}`, "quota", "4008"},
		{200, `{"code":4001}`, "invalid_request", "4001"},
		{429, `{"code":"insufficient_quota","message":"token-limit"}`, "invalid_request", "insufficient_quota"},
		{200, `{"code":4011}`, "rate_limit", "4011"},
		{429, "too many requests", "rate_limit", ""},
		{500, "boom", "unavailable", ""},
	}
	for _, c := range cases {
		got := Classify(c.status, c.body)
		if got.Kind != c.kind {
			t.Fatalf("Classify(%d,%s)=%+v want %s", c.status, c.body, got, c.kind)
		}
		err := wrapClassified(got, extractCode(c.body))
		var classified *providers.Error
		if !errors.As(err, &classified) {
			t.Fatalf("wrapClassified type %T", err)
		}
		if classified.Kind != c.kind {
			t.Fatalf("kind(%s)=%s want %s", c.body, classified.Kind, c.kind)
		}
		if classified.Code != c.code {
			t.Fatalf("code(%s)=%s want %s", c.body, classified.Code, c.code)
		}
		if classified.RetryAfter != 0 {
			t.Fatalf("provider must not set retry_after(%s)=%s", c.body, classified.RetryAfter)
		}
	}
}

func TestExtractCodeOmitsMissingAndNull(t *testing.T) {
	for _, body := range []string{
		`{"message":"plan limit"}`,
		`{"code":null,"message":"plan limit"}`,
		`{"code":"","message":"plan limit"}`,
		"not json",
	} {
		if got := extractCode(body); got != "" {
			t.Fatalf("extractCode(%s)=%q want empty", body, got)
		}
		err := wrapClassified(Classify(429, body), extractCode(body))
		var classified *providers.Error
		if !errors.As(err, &classified) {
			t.Fatalf("wrapClassified type %T", err)
		}
		if classified.Code != "" {
			t.Fatalf("missing code must stay empty, body=%s code=%q", body, classified.Code)
		}
	}
}

func TestPrepareBodyForcesSoloShape(t *testing.T) {
	out := PrepareBody([]byte(`{"model":"glm-5.3","stream":false,"messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"t","parameters":{"type":"object"}}}],"tool_choice":{"type":"function","function":{"name":"t"}}}`), PrimaryScene)
	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatal(err)
	}
	if body["stream"] != true || body["function"] != PrimaryScene || body["config_name"] != "glm-5.3" {
		t.Fatalf("body=%v", body)
	}
	messages, _ := body["messages"].([]any)
	first, _ := messages[0].(map[string]any)
	content, _ := first["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content=%v", first["content"])
	}
	if body["tool_choice"] != "t" {
		t.Fatalf("tool_choice=%v", body["tool_choice"])
	}
	tools, _ := body["tools"].([]any)
	tool, _ := tools[0].(map[string]any)
	fn, _ := tool["function"].(map[string]any)
	if _, ok := fn["parameters"].(string); !ok {
		t.Fatalf("parameters should be JSON string: %v", fn["parameters"])
	}
}

func TestPrepareBodyExpandsNamespaceTools(t *testing.T) {
	out := PrepareBody([]byte(`{"model":"glm-5.3","messages":[{"role":"user","content":"hi"}],"tools":[
		{"type":"namespace","name":"mcp__computer-use","tools":[
			{"type":"function","name":"left_click","parameters":{"type":"object","properties":{"x":{"type":"number"}}}}
		]},
		{"type":"mcp","server_label":"computer-use"}
	]}`), PrimaryScene)
	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatal(err)
	}
	tools, _ := body["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools=%v", body["tools"])
	}
	tool, _ := tools[0].(map[string]any)
	fn, _ := tool["function"].(map[string]any)
	if fn["name"] != "mcp__computer-use__left_click" {
		t.Fatalf("name=%v", fn["name"])
	}
	if _, ok := fn["parameters"].(string); !ok {
		t.Fatalf("parameters should remain Trae JSON string: %v", fn["parameters"])
	}
}

func TestProbeReadyWithCredential(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("probe should not hit network when credential is fresh: %s", r.URL.Path)
	}))
	payload, _ := json.Marshal(Credential{
		AccessToken: "at", RefreshToken: "rt", ExpiresAt: 4102444800, Domain: DomainCN, UID: "u1",
	})
	_ = store.SaveCredentialPayload(context.Background(), "acc1", CredentialFormat, payload)
	health, err := client.Probe(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if !health.Ready || !health.Hot || health.UID != "u1" {
		t.Fatalf("health=%+v", health)
	}
}

func TestQuotaUsesEntitlementPackUnit(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathEntUsage {
			t.Fatalf("path=%s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"user_entitlement_pack_list": []map[string]any{
				{"entitlement_base_info": map[string]any{"quota": map[string]any{"credits_limit": 2000}}, "usage": map[string]any{"credits_amount": 500}},
			},
		})
	}))
	payload, _ := json.Marshal(Credential{AccessToken: "at", RefreshToken: "rt", ExpiresAt: 4102444800, UID: "u1"})
	_ = store.SaveCredentialPayload(context.Background(), "acc1", CredentialFormat, payload)
	info, err := client.Quota(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if info.Remaining != 1500 || info.Total != 2000 || info.Used != 500 || info.Unit != QuotaUnit {
		t.Fatalf("quota=%+v", info)
	}
}

func TestParseEntitlementUsageUnwrapsResultEnvelope(t *testing.T) {
	remain, used, total := parseEntitlementUsage([]byte(`{"Result":{"user_entitlement_pack_list":[{"entitlement_base_info":{"quota":{"credits_limit":100}},"usage":{"credits_amount":25}}]}}`))
	if remain != 75 || used != 25 || total != 100 {
		t.Fatalf("remain=%d used=%d total=%d", remain, used, total)
	}
}

func TestAdapterWiresCapabilities(t *testing.T) {
	adapter := NewClient(&memStore{}).Adapter()
	for _, cap := range []string{"credential", "login", "chat", "models", "classifier", "prober", "import_export"} {
		if !adapter.Supports(cap) {
			t.Fatalf("missing %s", cap)
		}
	}
}

func TestBuildLoginURLIsLoopbackPKCE(t *testing.T) {
	u := buildLoginURL("m", "d", "http://127.0.0.1:9/authorize", "trace", "")
	if strings.Contains(u, "0.0.0.0") || !strings.Contains(u, "127.0.0.1") ||
		!strings.Contains(u, "auth_from=trae") || !strings.Contains(u, "client_id="+ClientID) ||
		!strings.Contains(u, "code_challenge=") || !strings.Contains(u, "code_challenge_method=S256") {
		t.Fatalf("url=%s", u)
	}
}

func TestParseCallbackReadsRefreshAndUserInfo(t *testing.T) {
	info, err := ParseCallback(`http://127.0.0.1:18080/authorize?refreshToken=rt&userInfo={"UserID":"u1","ScreenName":"N"}`)
	if err != nil || info.RefreshToken != "rt" || info.UID != "u1" {
		t.Fatalf("info=%+v err=%v", info, err)
	}
}

func TestHTTPClientFailsClosedOnSecretError(t *testing.T) {
	store := &memStore{secretErr: errors.New("db down")}
	client := NewClient(store)
	if _, err := client.httpClient(context.Background(), "acc1"); err == nil {
		t.Fatal("httpClient succeeded despite a global proxy read error")
	}
}

func TestHTTPClientAccountDirectSkipsGlobalRead(t *testing.T) {
	store := &memStore{accountProxyURL: "direct", secretErr: errors.New("db down")}
	client := NewClient(store)
	if _, err := client.httpClient(context.Background(), "acc1"); err != nil {
		t.Fatalf("account direct must not read the global proxy: %v", err)
	}
	if store.secretCalls != 0 {
		t.Fatalf("global proxy was read %d times for an account with a proxy override", store.secretCalls)
	}
}

func TestHTTPClientUsesGlobalProxyWhenAccountIsBlank(t *testing.T) {
	store := &memStore{secretValue: "http://global.example:8080", secretFound: true}
	client := NewClient(store)
	httpClient, err := client.httpClient(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatalf("transport = %#v", httpClient.Transport)
	}
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	proxyURL, err := transport.Proxy(req)
	if err != nil || proxyURL == nil || proxyURL.String() != "http://global.example:8080" {
		t.Fatalf("transport proxy = %v err=%v", proxyURL, err)
	}
}

func TestHTTPClientKeepsInjectedTransportWhenUnconfigured(t *testing.T) {
	store := &memStore{}
	client := NewClient(store)
	custom := rewriteTransport{}
	client.http.Transport = custom
	httpClient, err := client.httpClient(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if httpClient.Transport != custom {
		t.Fatalf("unconfigured client replaced the injected transport: %#v", httpClient.Transport)
	}
}

func TestHTTPClientReusesTransportByProxyURL(t *testing.T) {
	store := &memStore{secretValue: "http://global.example:8080", secretFound: true}
	client := NewClient(store)

	first, err := client.httpClient(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.httpClient(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if first.Transport == nil || first.Transport != second.Transport {
		t.Fatal("transport was not reused across requests")
	}
}

func TestHTTPClientDifferentProxiesUseDifferentTransports(t *testing.T) {
	store := &memStore{secretValue: "http://global.example:8080", secretFound: true}
	client := NewClient(store)

	inherited, err := client.httpClient(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}

	store.accountProxyURL = "socks5://proxy.example:1080"
	overridden, err := client.httpClient(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if inherited.Transport == overridden.Transport {
		t.Fatal("different proxy URLs shared a transport")
	}

	store.accountProxyURL = "direct"
	direct, err := client.httpClient(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if direct.Transport == inherited.Transport || direct.Transport == overridden.Transport {
		t.Fatal("direct did not get its own transport")
	}
}

func TestParseEntitlementBucketsSplitsGeneralAndWork(t *testing.T) {
	body := []byte(`{"Result":{"user_entitlement_pack_list":[
		{"entitlement_base_info":{"quota":{"credits_limit":2000},"available_endpoint":0},"usage":{"credits_amount":500}},
		{"entitlement_base_info":{"quota":{"credits_limit":600},"available_endpoint":1},"usage":{"credits_amount":100}}
	]}}`)
	general, work := parseEntitlementBuckets(body)
	if general.total != 2000 || general.used != 500 || general.remain != 1500 {
		t.Fatalf("general=%+v", general)
	}
	if work.total != 600 || work.used != 100 || work.remain != 500 {
		t.Fatalf("work=%+v", work)
	}
	// The flat helper still sums both buckets.
	remain, used, total := parseEntitlementUsage(body)
	if remain != 2000 || used != 600 || total != 2600 {
		t.Fatalf("flat remain=%d used=%d total=%d", remain, used, total)
	}
}

func TestQuotaShowsOnlyGeneralBucket(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"user_entitlement_pack_list": []map[string]any{
				{"entitlement_base_info": map[string]any{"quota": map[string]any{"credits_limit": 2000}, "available_endpoint": 0}, "usage": map[string]any{"credits_amount": 500}},
				{"entitlement_base_info": map[string]any{"quota": map[string]any{"credits_limit": 600}, "available_endpoint": 1}, "usage": map[string]any{"credits_amount": 600}},
			},
		})
	}))
	payload, _ := json.Marshal(Credential{AccessToken: "at", RefreshToken: "rt", ExpiresAt: 4102444800, UID: "u1"})
	_ = store.SaveCredentialPayload(context.Background(), "acc1", CredentialFormat, payload)
	info, err := client.Quota(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	// Work bucket (600/600, exhausted) must NOT be shown.
	if info.Total != 2000 || info.Remaining != 1500 || info.Used != 500 {
		t.Fatalf("quota must be General-only: %+v", info)
	}
	if info.Exceeded || len(info.Windows) != 0 {
		t.Fatalf("work bucket leaked: %+v", info)
	}
}

// A chat must be sent under the scene that actually serves the model: a model
// the primary scene lists goes through it, while a model only the secondary
// scene carries keeps the secondary scene, regardless of the max-mode toggle.
func TestChatRoutesByModelScene(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", Domain: DomainCN, ExpiresAt: 4102444800}.Encode()
	store := &memStore{items: map[string][]byte{"acc1": payload}}
	captured := map[string]map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pathModels {
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			fn, _ := req["function"].(string)
			name := "glm-5.3"
			if fn != PrimaryScene {
				name = "kimi-k2.6"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"config_info_list": []map[string]any{{
				"config_name": name, "display_config": map[string]any{"display_name": name},
			}}})
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		m, _ := body["config_name"].(string)
		captured[m] = body
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(soloSSE))
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}
	if _, err := client.Models(context.Background(), "acc1"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ChatNonStream(context.Background(), "acc1", translate.ChatRequest{
		Model: "glm-5.3", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ChatNonStream(context.Background(), "acc1", translate.ChatRequest{
		Model: "kimi-k2.6", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatal(err)
	}
	if captured["glm-5.3"]["function"] != PrimaryScene {
		t.Fatalf("primary-scene model function=%v", captured["glm-5.3"]["function"])
	}
	if captured["kimi-k2.6"]["function"] != SecondaryScene {
		t.Fatalf("secondary-scene model function=%v", captured["kimi-k2.6"]["function"])
	}
}
