package orcarouter

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// fakeStore is the minimal persistence surface the adapter needs. It backs both
// the automated credential tests and the local fake-auth-server PKCE test.
type fakeStore struct {
	mu       sync.Mutex
	format   string
	payload  []byte
	secrets  map[string]string
	observed map[string]string
	kinds    map[string]string
	accounts map[string]accounts.Account
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		secrets:  map[string]string{},
		observed: map[string]string{},
		kinds:    map[string]string{},
		accounts: map[string]accounts.Account{},
	}
}

func (f *fakeStore) Get(_ context.Context, id string) (accounts.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if account, ok := f.accounts[id]; ok {
		return account, nil
	}
	return accounts.Account{ID: id, Provider: ProviderID, ProviderRegion: "global"}, nil
}

func (f *fakeStore) LoadCredentialPayload(context.Context, string) (string, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.format, append([]byte(nil), f.payload...), nil
}

func (f *fakeStore) SaveCredentialPayload(_ context.Context, _ string, format string, payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.format = format
	f.payload = append([]byte(nil), payload...)
	return nil
}

func (f *fakeStore) Observe(_ context.Context, id, _, status, lastError, kind string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.observed[id] = status
	f.kinds[id] = kind
	return nil
}

func (f *fakeStore) GetSecret(_ context.Context, key string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	value, ok := f.secrets[key]
	return value, ok, nil
}

func (f *fakeStore) stored() (string, []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.format, append([]byte(nil), f.payload...)
}

// hitCounter records every request path a test server saw, so a test can assert
// the exact outbound call set (in particular that no refresh/token call exists).
type hitCounter struct {
	mu    sync.Mutex
	paths map[string]int
}

func (h *hitCounter) record(path string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.paths[path]++
}

func (h *hitCounter) snapshot() map[string]int {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]int, len(h.paths))
	for key, value := range h.paths {
		out[key] = value
	}
	return out
}

// newJSONServer serves a canned (status, body) for every path and records hits.
func newJSONServer(t *testing.T, hits *hitCounter, respond func(path string) (int, string)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.record(r.URL.Path)
		status, body := respond(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func mustCredential(t *testing.T, payload []byte) Credential {
	t.Helper()
	credential, err := DecodeCredential(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return credential
}

func chatRequest(model string) translate.ChatRequest {
	return translate.ChatRequest{
		Model:    model,
		Messages: []translate.ChatMessage{{Role: "user", Content: "hello"}},
	}
}
