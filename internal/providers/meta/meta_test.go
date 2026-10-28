package meta

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func TestMain(m *testing.M) {
	acceptTestEndpoint = true
	m.Run()
}

type fakeStore struct {
	mu       sync.Mutex
	accounts map[string]accounts.Account
	creds    map[string][]byte
	secrets  map[string]string
	settings map[string]accounts.ProviderModelSetting
	observed []string
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		accounts: map[string]accounts.Account{},
		creds:    map[string][]byte{},
		secrets:  map[string]string{},
		settings: map[string]accounts.ProviderModelSetting{},
	}
}

func (s *fakeStore) addAccount(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accounts[id] = accounts.Account{ID: id, Provider: ProviderID, ProviderRegion: "global", Enabled: true}
}

func (s *fakeStore) setCredential(id string, credential Credential) {
	payload, err := credential.Encode()
	if err != nil {
		panic(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.creds[id] = payload
}

func (s *fakeStore) Get(_ context.Context, id string) (accounts.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.accounts[id]
	if !ok {
		return accounts.Account{}, accounts.ErrAccountNotFound
	}
	return account, nil
}

func (s *fakeStore) LoadCredentialPayload(_ context.Context, id string) (string, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, ok := s.creds[id]
	if !ok {
		return "", nil, accounts.ErrAccountNotFound
	}
	out := make([]byte, len(payload))
	copy(out, payload)
	var format string
	var peek struct {
		Format string `json:"format"`
	}
	_ = json.Unmarshal(payload, &peek)
	format = peek.Format
	return format, out, nil
}

func (s *fakeStore) SaveCredentialPayload(_ context.Context, id, format string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]byte, len(payload))
	copy(out, payload)
	s.creds[id] = out
	_ = format
	return nil
}

func (s *fakeStore) Observe(_ context.Context, accountID, uid, status, lastError, lastKind string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observed = append(s.observed, accountID+":"+status+":"+uid)
	_ = lastError
	_ = lastKind
	return nil
}

func (s *fakeStore) GetSecret(_ context.Context, key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.secrets[key]
	return value, ok, nil
}

func (s *fakeStore) GetProviderModelSetting(_ context.Context, provider, model string) (accounts.ProviderModelSetting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	setting, ok := s.settings[provider+"/"+model]
	if !ok {
		return accounts.ProviderModelSetting{}, errors.New("missing")
	}
	return setting, nil
}

func TestDecodeCredentialCPAShape(t *testing.T) {
	raw := []byte(`{
		"type":"meta",
		"api_key":"mk-test",
		"dca_token":"dca:abc",
		"email":"user@example.com",
		"base_url":"https://api.meta.ai/v1"
	}`)
	cred, err := DecodeCredential(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cred.APIKey != "mk-test" || cred.DCAToken != "dca:abc" || cred.Email != "user@example.com" {
		t.Fatalf("decoded=%+v", cred)
	}
	if !cred.Ready() {
		t.Fatal("expected ready")
	}
	if err := ValidateCredential(raw); err != nil {
		t.Fatal(err)
	}
}

func TestClassifyQuotaVsRateLimit(t *testing.T) {
	quotaBody := `{"error":{"code":"rate_limit_exceeded","message":"subscription quota","resets_at":"2026-10-28T12:00:00Z"}}`
	got := Classify(429, quotaBody)
	if got.Kind != accounts.KindQuota {
		t.Fatalf("quota kind=%s", got.Kind)
	}
	plain := Classify(429, `{"error":{"message":"too many requests"}}`)
	if plain.Kind != accounts.KindRateLimit {
		t.Fatalf("rate limit kind=%s", plain.Kind)
	}
}

func validEncryptedContent(payloadLen int) string {
	decoded := make([]byte, metaReasoningSignatureOverheadLen+payloadLen)
	copy(decoded, metaReasoningSignatureHeader)
	for i := len(metaReasoningSignatureHeader); i < len(decoded); i++ {
		decoded[i] = byte('x')
	}
	return base64.RawURLEncoding.EncodeToString(decoded)
}

func TestSanitizeEncryptedContentPassthrough(t *testing.T) {
	enc := validEncryptedContent(1024) // k=0 bucket
	if !IsValidMetaReasoningSignature(enc) {
		t.Fatalf("fixture invalid: %s", enc[:32])
	}
	body, _ := json.Marshal(map[string]any{
		"model": "muse-spark-1.3",
		"input": []any{
			map[string]any{
				"type":              "reasoning",
				"encrypted_content": enc,
			},
		},
	})
	out := sanitizeEncryptedContent(body)
	if !strings.Contains(string(out), enc) {
		t.Fatalf("expected passthrough, got %s", out)
	}
	badBody, _ := json.Marshal(map[string]any{
		"input": []any{map[string]any{"type": "reasoning", "encrypted_content": "not-valid"}},
	})
	cleaned := sanitizeEncryptedContent(badBody)
	if strings.Contains(string(cleaned), "not-valid") {
		t.Fatalf("expected strip, got %s", cleaned)
	}
}

func TestCatalogHasCPAModels(t *testing.T) {
	models := catalogModelInfos()
	if len(models) != 9 {
		t.Fatalf("len=%d", len(models))
	}
	want := map[string]bool{
		"muse-wonder-1.0": true, "muse-spark-1.3": true, "muse-spark-1.1": true,
	}
	for _, m := range models {
		delete(want, m.NativeModel)
	}
	if len(want) != 0 {
		t.Fatalf("missing %#v", want)
	}
}

func TestChatStreamHeadersAndPath(t *testing.T) {
	var gotUA, gotClient, gotAuth, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotClient = r.Header.Get("X-Client-Id")
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hi\"}]}]}}\n\n"))
	}))
	defer server.Close()

	store := newFakeStore()
	store.addAccount("a1")
	store.setCredential("a1", Credential{
		Format:  CredentialFormatKey,
		APIKey:  "mk-secret",
		BaseURL: server.URL,
	})
	client := NewClient(store)

	resp, _, err := client.ChatStream(context.Background(), "a1", translate.ChatRequest{
		Model:    "muse-spark-1.3",
		Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if gotPath != "/responses" {
		t.Fatalf("path=%s", gotPath)
	}
	if gotUA != ChatUserAgent || gotClient != ClientIDHeader || gotAuth != "Bearer mk-secret" {
		t.Fatalf("ua=%q client=%q auth=%q", gotUA, gotClient, gotAuth)
	}
}

func TestDeviceLoginMintFlow(t *testing.T) {
	var mu sync.Mutex
	step := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/oidc/device/authorization/"):
			_ = r.ParseForm()
			if r.Form.Get("client_id") != ClientID {
				t.Errorf("client_id=%s", r.Form.Get("client_id"))
			}
			if r.Header.Get("User-Agent") != AuthUserAgent {
				t.Errorf("auth ua=%s", r.Header.Get("User-Agent"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"user_code":"ABCD","device_code":"dev","verification_uri":"https://auth.meta.com/device","verification_uri_complete":"https://auth.meta.com/device?user_code=ABCD","expires_in":600,"interval":1}`))
		case strings.HasSuffix(r.URL.Path, "/oidc/device/token/"):
			step++
			w.Header().Set("Content-Type", "application/json")
			if step < 2 {
				_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"dca:token","token_type":"bearer","expires_in":3600}`))
		case strings.HasSuffix(r.URL.Path, "/muse-code/key"):
			if r.Header.Get("Authorization") != "Bearer dca:token" {
				t.Errorf("mint auth=%s", r.Header.Get("Authorization"))
			}
			if r.Header.Get("User-Agent") != AuthUserAgent {
				t.Errorf("mint ua=%s", r.Header.Get("User-Agent"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"api_key":"mk-minted","base_url":"https://api.meta.ai/v1","user_email":"u@x.com","user_full_name":"U","subs_tier_name":"pro","is_subs_active":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	store := newFakeStore()
	store.addAccount("a1")
	client := NewClient(store)
	client.authHost = server.URL
	client.mintURL = server.URL + "/muse-code/key"

	now := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	client.nowFn = func() time.Time { return now }

	session, err := client.StartLogin(context.Background(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	if session.State != "ABCD" || session.AuthURL == "" {
		t.Fatalf("session=%+v", session)
	}
	// First poll: pending
	done, msg, err := client.PollLogin(context.Background(), "a1")
	if err != nil || done {
		t.Fatalf("pending: done=%v err=%v msg=%s", done, err, msg)
	}
	// Advance past pollFloor so the second poll hits the token endpoint.
	now = now.Add(6 * time.Second)
	done, _, err = client.PollLogin(context.Background(), "a1")
	if err != nil || !done {
		t.Fatalf("success: done=%v err=%v", done, err)
	}
	cred, err := client.loadCredential(context.Background(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	if cred.APIKey != "mk-minted" || cred.DCAToken != "dca:token" || cred.Email != "u@x.com" {
		t.Fatalf("cred=%+v", cred)
	}
}

func TestAdapterSurface(t *testing.T) {
	client := NewClient(newFakeStore())
	adapter := client.Adapter()
	if adapter.ID != ProviderID {
		t.Fatalf("id=%s", adapter.ID)
	}
	if adapter.StreamFormat != providers.StreamFormatResponses {
		t.Fatalf("stream format=%v", adapter.StreamFormat)
	}
	if adapter.NativeResponses == nil || adapter.Login == nil || adapter.Chat == nil {
		t.Fatal("missing capabilities")
	}
}

func TestMuseAliasResolves(t *testing.T) {
	if got := accounts.NormalizeProviderFamily("muse"); got != "meta" {
		t.Fatalf("normalize=%s", got)
	}
	d, ok := providers.Get("muse")
	if !ok || d.ID != "meta" {
		t.Fatalf("get muse=%v ok=%v", d.ID, ok)
	}
}


func TestAccountProxyOverridesGlobal(t *testing.T) {
	store := newFakeStore()
	store.addAccount("a1")
	store.mu.Lock()
	account := store.accounts["a1"]
	account.ProxyURL = "http://account-proxy.example:8080"
	store.accounts["a1"] = account
	store.secrets["proxy_url"] = "http://global-proxy.example:8080"
	store.mu.Unlock()

	client := NewClient(store)
	got, err := client.effectiveProxy(context.Background(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://account-proxy.example:8080" {
		t.Fatalf("effectiveProxy=%q", got)
	}

	httpClient, err := client.httpClient(context.Background(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok || transport.Proxy == nil {
		t.Fatalf("transport=%#v", httpClient.Transport)
	}
	req, _ := http.NewRequest(http.MethodGet, "https://api.meta.ai/", nil)
	proxyURL, err := transport.Proxy(req)
	if err != nil || proxyURL == nil || proxyURL.String() != "http://account-proxy.example:8080" {
		t.Fatalf("proxy=%v err=%v", proxyURL, err)
	}
}

func TestEffectiveProxyGetFailureNotSwallowed(t *testing.T) {
	store := newFakeStore()
	client := NewClient(store)
	if _, err := client.effectiveProxy(context.Background(), "missing"); err == nil {
		t.Fatal("expected account lookup error")
	}
}
