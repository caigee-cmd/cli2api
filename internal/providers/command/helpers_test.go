package command

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

// fakeStore is the minimal persistence surface the adapter needs.
type fakeStore struct {
	format   string
	payload  []byte
	observed map[string]string
}

func (f *fakeStore) Get(context.Context, string) (accounts.Account, error) {
	return accounts.Account{ID: "acc-1", Provider: "command", ProviderRegion: "global"}, nil
}

func (f *fakeStore) LoadCredentialPayload(context.Context, string) (string, []byte, error) {
	return f.format, f.payload, nil
}

func (f *fakeStore) SaveCredentialPayload(_ context.Context, _ string, format string, payload []byte) error {
	f.format = format
	f.payload = payload
	return nil
}

func (f *fakeStore) Observe(_ context.Context, id, _, status, _, _ string) error {
	if f.observed == nil {
		f.observed = map[string]string{}
	}
	f.observed[id] = status
	return nil
}

// newTestClient returns a Client pointed at a test server with a stored key.
func newTestClient(t *testing.T, srv *httptest.Server) (*Client, *fakeStore) {
	t.Helper()
	cred := Credential{APIKey: "user_test_key_1234567890", BaseURL: srv.URL}
	payload, err := cred.Encode()
	if err != nil {
		t.Fatalf("encode credential: %v", err)
	}
	store := &fakeStore{format: CredentialFormat, payload: payload}
	client := NewClient(store)
	client.SetBase(srv.URL)
	return client, store
}

// recorder captures the last request headers and a per-path hit count.
type recorder struct {
	mu      sync.Mutex
	headers http.Header
	hits    map[string]int
}

func newRecorder() *recorder { return &recorder{hits: map[string]int{}} }

func (r *recorder) record(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.headers = req.Header.Clone()
	r.hits[req.URL.Path]++
}

func (r *recorder) count(path string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hits[path]
}

func (r *recorder) header(key string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.headers == nil {
		return ""
	}
	return r.headers.Get(key)
}

// catalogServer serves the fixture at PathModels and records requests.
func catalogServer(t *testing.T) (*httptest.Server, *recorder) {
	t.Helper()
	raw, err := os.ReadFile("testdata/command_models.sample.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	rec := newRecorder()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		if r.URL.Path != PathModels {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

// generateServer serves a canned NDJSON response for /alpha/generate and records
// the request body so tests can assert the envelope shape.
func generateServer(t *testing.T, status int, body string) (*httptest.Server, *recorder, *string) {
	t.Helper()
	rec := newRecorder()
	var requestBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		raw, _ := io.ReadAll(r.Body)
		requestBody = string(raw)
		if r.URL.Path != PathGenerate {
			http.NotFound(w, r)
			return
		}
		if status != 0 {
			w.WriteHeader(status)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, rec, &requestBody
}

func ndjson(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

// asProviderError unwraps a *providers.Error, reporting whether it matched.
func asProviderError(err error, target **providers.Error) bool {
	return errors.As(err, target)
}

func newPathServer(t *testing.T, path string, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newWhoamiServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return newPathServer(t, PathWhoami, status, body)
}

func newCreditsServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return newPathServer(t, PathBillingCredits, status, body)
}
