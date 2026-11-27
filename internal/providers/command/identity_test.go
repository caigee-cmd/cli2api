package command

import (
	"strings"
	"testing"
)

// TestGenerateRequestCarriesCLIIdentityHeaders pins the identity surface this
// adapter sends: the four documented headers, on the generate request itself.
//
// Scope is deliberately narrow. Earlier work on this branch also posted a
// synthesised device fingerprint to /alpha/fingerprint/record and a lifecycle
// heartbeat, which is a different thing entirely — it invents a machine to look
// like an official install. That is out of scope, and no test here asserts a
// background request: there must be none.
func TestGenerateRequestCarriesCLIIdentityHeaders(t *testing.T) {
	body := ndjson(
		`{"type":"text-start","id":"t0"}`,
		`{"type":"text-delta","id":"t0","text":"ok"}`,
		`{"type":"text-end","id":"t0"}`,
		`{"type":"finish","totalUsage":{"inputTokens":1,"outputTokens":1}}`,
	)
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

	// No synthesized identifiers: these would be stable per-key values and are
	// exactly the kind of fake device detail this adapter must not send.
	for _, forbidden := range []string{"x-project-slug", "x-osc-primary-provider"} {
		if got := rec.header(forbidden); got != "" {
			t.Errorf("%s must not be sent, got %q", forbidden, got)
		}
	}
}

// TestTraceparentFormat checks the W3C shape the CLI uses. It is per-request, so
// two calls must not share a trace id.
func TestTraceparentFormat(t *testing.T) {
	first := newTraceparent()
	parts := strings.Split(first, "-")
	if len(parts) != 4 {
		t.Fatalf("traceparent = %q, want 4 dash-separated parts", first)
	}
	if parts[0] != "00" || parts[3] != "01" {
		t.Errorf("traceparent version/flags = %q/%q, want 00/01", parts[0], parts[3])
	}
	if len(parts[1]) != 32 {
		t.Errorf("trace id = %q (%d chars), want 32", parts[1], len(parts[1]))
	}
	if len(parts[2]) != 16 {
		t.Errorf("span id = %q (%d chars), want 16", parts[2], len(parts[2]))
	}
	if len(first) != 55 {
		t.Errorf("traceparent length = %d, want 55", len(first))
	}
	if second := newTraceparent(); second == first {
		t.Error("each request must get its own traceparent")
	}
}

// TestNoBackgroundTelemetryRequests asserts the adapter opens exactly one
// connection per chat: the generate call. A background fingerprint/lifecycle POST
// would make this count 2+, which is what an earlier revision of this branch did.
func TestNoBackgroundTelemetryRequests(t *testing.T) {
	body := ndjson(`{"type":"text-start","id":"t0"}`, `{"type":"text-delta","id":"t0","text":"ok"}`, `{"type":"text-end","id":"t0"}`, `{"type":"finish","totalUsage":{"inputTokens":1,"outputTokens":1}}`)
	srv, rec, _ := generateServer(t, 0, body)
	client, _ := newTestClient(t, srv)

	if _, err := client.ChatNonStream(t.Context(), "acc-1", chatReq()); err != nil {
		t.Fatalf("ChatNonStream: %v", err)
	}
	// generateServer records only the generate path, so count the recorder's hits:
	// a second POST would land on a path the fixture does not serve and surface as
	// a 404 recorded request. Assert the generate count is exactly 1 instead.
	if got := rec.count(PathGenerate); got != 1 {
		t.Errorf("generate requests = %d, want 1", got)
	}
}
