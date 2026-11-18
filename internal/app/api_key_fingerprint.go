package app

import (
	"crypto/sha256"
	"encoding/hex"
)

// apiKeyFingerprint returns a short, non-reversible identifier for a freshly
// generated admin key. It lets operators correlate startup logs with the key
// shown in the console without ever writing the secret itself to logs (B2).
func apiKeyFingerprint(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])[:12]
}
