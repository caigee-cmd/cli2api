package command

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestDeviceRegistrationHitsBothEndpoints pins the anti-detection contract: the
// first use of a key replays the CLI's two startup telemetry calls, with the
// CLI's identity headers and a body shaped like the official client. Without
// this the adapter looks like a proxy (which upstream bans) instead of a CLI.
func TestDeviceRegistrationHitsBothEndpoints(t *testing.T) {
	var mu sync.Mutex
	type hit struct {
		Path   string
		Header http.Header
		Body   map[string]any
	}
	var hits []hit

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		hits = append(hits, hit{Path: r.URL.Path, Header: r.Header.Clone(), Body: body})
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	t.Cleanup(srv.Close)

	client, _ := newTestClient(t, srv)
	cred := Credential{APIKey: "user_telemetry_key"}

	// Record once, then assert the memo only fires a single round.
	client.recordFingerprint(t.Context(), cred)
	client.recordLifecycle(t.Context(), cred)
	client.recordFingerprint(t.Context(), cred)

	mu.Lock()
	got := append([]hit(nil), hits...)
	mu.Unlock()

	byPath := map[string][]hit{}
	for _, h := range got {
		byPath[h.Path] = append(byPath[h.Path], h)
	}

	fp := byPath[PathFingerprint]
	if len(fp) != 1 {
		t.Fatalf("fingerprint hits = %d, want exactly 1 (it is memoized per key)", len(fp))
	}
	if c := fp[0].Body["components"]; c == nil {
		t.Error("fingerprint body missing components")
	}
	if tm, ok := fp[0].Body["thumbmark"].(string); !ok || len(tm) != 64 {
		t.Errorf("thumbmark = %v, want 64-char hex", fp[0].Body["thumbmark"])
	}
	if acts := fp[0].Header.Get("Authorization"); acts != "Bearer user_telemetry_key" {
		t.Errorf("authorization = %q", acts)
	}
	if ua := fp[0].Header.Get("User-Agent"); ua != UserAgent {
		t.Errorf("User-Agent = %q, want %q", ua, UserAgent)
	}
	if v := fp[0].Header.Get("x-command-code-version"); v != PinnedCLIVersion {
		t.Errorf("x-command-code-version = %q, want %q", v, PinnedCLIVersion)
	}

	lc := byPath[PathLifecycle]
	if len(lc) != 1 {
		t.Fatalf("lifecycle hits = %d, want 1", len(lc))
	}
	if lc[0].Body["eventType"] != "cli_session_exists" {
		t.Errorf("eventType = %v", lc[0].Body["eventType"])
	}
	meta, ok := lc[0].Body["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata = %v", lc[0].Body["metadata"])
	}
	for _, key := range []string{"sessionId", "cliVersion", "mode", "os"} {
		if meta[key] == nil || meta[key] == "" {
			t.Errorf("metadata.%s missing", key)
		}
	}
}

// TestGenerateRequestCarriesCLIIdentityHeaders asserts the generate call itself
// carries the CLI identity headers, not just the telemetry calls.
func TestGenerateRequestCarriesCLIIdentityHeaders(t *testing.T) {
	body := ndjson(`{"type":"text-start","id":"t0"}`, `{"type":"text-delta","id":"t0","text":"ok"}`, `{"type":"text-end","id":"t0"}`, `{"type":"finish","totalUsage":{"inputTokens":1,"outputTokens":1}}`)
	srv, rec, _ := generateServer(t, 0, body)
	client, _ := newTestClient(t, srv)

	if _, err := client.ChatNonStream(t.Context(), "acc-1", chatReq()); err != nil {
		t.Fatalf("ChatNonStream: %v", err)
	}

	if got := rec.header("User-Agent"); got != UserAgent {
		t.Errorf("User-Agent = %q, want %q", got, UserAgent)
	}
	if got := rec.header("x-cli-surface"); got != "cli" {
		t.Errorf("x-cli-surface = %q", got)
	}
	if got := rec.header("x-taste-learning"); got != "false" {
		t.Errorf("x-taste-learning = %q", got)
	}
	if got := rec.header("x-project-slug"); got == "" {
		t.Error("x-project-slug missing")
	}
	if tp := rec.header("traceparent"); len(tp) != 55 {
		t.Errorf("traceparent = %q, want 55 chars", tp)
	}
}
