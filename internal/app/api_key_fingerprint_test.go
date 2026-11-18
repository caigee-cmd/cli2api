package app

import (
	"strings"
	"testing"
)

// TestAPIKeyFingerprintNeverLeaksSecret guards invariant B2: startup logging
// must identify a freshly generated admin key without printing the secret
// itself into stderr/container logs.
func TestAPIKeyFingerprintNeverLeaksSecret(t *testing.T) {
	const secret = "sk_live_super_secret_value_1234567890"
	fp := apiKeyFingerprint(secret)

	if strings.Contains(fp, secret) {
		t.Fatalf("fingerprint leaked secret: %q", fp)
	}
	if len(secret) > 8 && strings.Contains(fp, secret[:8]) {
		t.Fatalf("fingerprint leaked secret prefix: %q", fp)
	}
	if fp == "" {
		t.Fatal("fingerprint must not be empty")
	}
	if got := apiKeyFingerprint(secret); got != fp {
		t.Fatalf("fingerprint not deterministic: %q then %q", fp, got)
	}
	if apiKeyFingerprint(secret) == apiKeyFingerprint("a totally different key") {
		t.Fatal("distinct keys must have distinct fingerprints")
	}
}
