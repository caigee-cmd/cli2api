package command

import (
	"net/http"
	"strings"
	"testing"
)

// newProbeRequest makes an unattached request suitable for header assertions.
func newProbeRequest(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return req
}

// TestDeviceFingerprintIsStablePerKey pins the property that makes the
// fingerprint safe to send: one key reports one device forever, never a
// different "machine" per call or per process restart.
func TestDeviceFingerprintIsStablePerKey(t *testing.T) {
	a := Credential{APIKey: "user_alpha"}
	b := Credential{APIKey: "user_alpha"}
	c := Credential{APIKey: "user_beta"}

	first, comps := buildDeviceFingerprint(a)
	second, _ := buildDeviceFingerprint(b)
	if first != second {
		t.Fatalf("same key produced different fingerprints: %s vs %s", first, second)
	}
	if other, _ := buildDeviceFingerprint(c); other == first {
		t.Fatal("different keys must not share a fingerprint")
	}
	if len(first) != 64 {
		t.Errorf("thumbmark = %q, want a 64-char sha256 hex", first)
	}
	if len(comps.MacHashes) != 2 || comps.MacHashes[0] == comps.MacHashes[1] {
		t.Errorf("mac hashes = %v", comps.MacHashes)
	}
	for _, h := range append([]string{comps.MachineIDHash, comps.OSUserHash}, comps.MacHashes...) {
		if h != "" && len(h) != 64 {
			t.Errorf("hashed signal = %q, want 64-char hex or empty", h)
		}
	}
	if comps.Runtime != "cli" {
		t.Errorf("runtime = %q, want cli", comps.Runtime)
	}
}

// TestIdentityHeadersMatchCLI asserts the identity headers the CLI always sends
// are present and stable, and that the opaque trace spans look well formed.
func TestIdentityHeadersMatchCLI(t *testing.T) {
	cred := Credential{APIKey: "user_alpha"}
	probe := newProbeRequest(t, "https://api.commandcode.ai/alpha/generate")
	setIdentityHeaders(probe, cred)

	if got := probe.Header.Get("x-cli-surface"); got != "cli" {
		t.Errorf("x-cli-surface = %q", got)
	}
	if got := probe.Header.Get("x-taste-learning"); got != "false" {
		t.Errorf("x-taste-learning = %q", got)
	}
	slug := probe.Header.Get("x-project-slug")
	if !strings.HasPrefix(slug, "proj-") {
		t.Errorf("x-project-slug = %q", slug)
	}
	// Stable per key: the slug must not change between calls.
	again := newProbeRequest(t, "https://api.commandcode.ai/alpha/generate")
	setIdentityHeaders(again, cred)
	if again.Header.Get("x-project-slug") != slug {
		t.Error("project slug must be stable per key")
	}

	if sid := newSessionID(); sid == "" {
		t.Error("session id must not be empty")
	}
	parts := strings.Split(newTraceparent(), "-")
	if len(parts) != 4 || parts[0] != "00" || parts[3] != "01" || len(parts[1]) != 32 || len(parts[2]) != 16 {
		t.Errorf("traceparent = %q", strings.Join(parts, "-"))
	}
}
