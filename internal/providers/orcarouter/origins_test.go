package orcarouter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// The capability query must be forwarded on the wire so a workspace-scoped
// catalog can be requested for a non-chat entry point.
func TestCatalogSendsCapabilityQuery(t *testing.T) {
	var mu sync.Mutex
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.RawQuery)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fixtureCatalog))
	}))
	t.Cleanup(srv.Close)

	payload, err := Credential{Format: CredentialFormat, APIKey: "sk-orca-CAP00000000000000", APIBase: srv.URL}.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	client := NewClient(newFakeStore())
	credential := mustCredential(t, payload)
	if _, err := client.fetchCatalog(context.Background(), credential, CapabilityEmbedding); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(queries) != 1 {
		t.Fatalf("expected one catalog call, got %d", len(queries))
	}
	if !strings.Contains(queries[0], "capability=embedding") {
		t.Fatalf("capability filter must be sent on the wire, got %q", queries[0])
	}
}

// The auth origin must never receive an inference call and vice versa. This is
// asserted at the adapter level: the chat path only ever addresses the API base.
func TestInferenceNeverTargetsAuthOrigin(t *testing.T) {
	authHits := &hitCounter{paths: map[string]int{}}
	authSrv := newJSONServer(t, authHits, func(string) (int, string) { return 200, `{}` })
	apiHits := &hitCounter{paths: map[string]int{}}
	apiSrv := newJSONServer(t, apiHits, func(string) (int, string) {
		return 200, `{"model":"deepseek/deepseek-v4-pro","choices":[{"finish_reason":"stop","message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`
	})
	store := newFakeStore()
	store.secrets[SecretAuthBase] = authSrv.URL
	store.secrets[SecretAPIBase] = apiSrv.URL
	payload, _ := Credential{Format: CredentialFormat, APIKey: "sk-orca-SPLIT000000000000"}.Encode()
	store.format, store.payload = CredentialFormat, payload
	client := NewClient(store)

	if _, err := client.ChatNonStream(context.Background(), "acc-1", chatRequest("deepseek/deepseek-v4-pro")); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if len(authHits.snapshot()) != 0 {
		t.Fatalf("inference must never touch the auth origin, saw %v", authHits.snapshot())
	}
	paths := apiHits.snapshot()
	if len(paths) != 1 {
		t.Fatalf("inference must hit exactly one API-origin path, saw %v", paths)
	}
	for path := range paths {
		if !strings.HasSuffix(path, ChatPath) {
			t.Fatalf("inference must hit the chat path, saw %q", path)
		}
	}
}

// A split-origin self-hosted deployment resolves the two origins independently.
func TestResolveOriginsUsesSeparateOverrides(t *testing.T) {
	origins, err := ResolveOrigins("", "https://auth.example.com", "https://api.example.com/v1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if origins.AuthBase != "https://auth.example.com" {
		t.Fatalf("auth base: %q", origins.AuthBase)
	}
	if origins.APIBase != "https://api.example.com/v1" {
		t.Fatalf("api base: %q", origins.APIBase)
	}

	// A shared self-hosted base covers both when no explicit override is given.
	shared, err := ResolveOrigins("https://gw.example.com", "", "")
	if err != nil {
		t.Fatalf("resolve shared: %v", err)
	}
	if shared.AuthBase != "https://gw.example.com" {
		t.Fatalf("shared auth base: %q", shared.AuthBase)
	}
	// The API origin gains the /v1 the inference API needs, and is never derived
	// from the auth origin by string surgery.
	if shared.APIBase != "https://gw.example.com/v1" {
		t.Fatalf("shared api base: %q", shared.APIBase)
	}

	// Explicit overrides win over the shared base.
	explicit, err := ResolveOrigins("https://gw.example.com", "https://a.example.com", "https://b.example.com/v1")
	if err != nil {
		t.Fatalf("resolve explicit: %v", err)
	}
	if explicit.AuthBase != "https://a.example.com" || explicit.APIBase != "https://b.example.com/v1" {
		t.Fatalf("explicit overrides must win: %+v", explicit)
	}

	// Plain HTTP is only allowed for loopback.
	if _, err := ResolveOrigins("", "http://auth.example.com", ""); err == nil {
		t.Fatal("a remote http origin must be refused")
	}
	if _, err := ResolveOrigins("", "http://127.0.0.1:8080", "http://localhost:9090/v1"); err != nil {
		t.Fatalf("loopback http must be allowed: %v", err)
	}

	// The public defaults are the documented pair.
	defaults, err := ResolveOrigins("", "", "")
	if err != nil {
		t.Fatalf("defaults: %v", err)
	}
	if defaults.AuthBase != DefaultAuthBase || defaults.APIBase != DefaultAPIBase {
		t.Fatalf("unexpected defaults: %+v", defaults)
	}
}
