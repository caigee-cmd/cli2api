package command

import (
	"crypto/rand"
	"encoding/hex"
)

// newTraceparent returns a W3C trace-context value in the shape the CLI sends:
// 00-<32 hex trace id>-<16 hex span id>-01.
//
// It is generated per request and carries no state, so it only tells upstream
// "this call is one trace" — it does not identify the host, the account or the
// deployment, and nothing about it is retained between calls.
func newTraceparent() string {
	var trace [24]byte
	if _, err := rand.Read(trace[:]); err != nil {
		// No trace header is better than a malformed one.
		return ""
	}
	return "00-" + hex.EncodeToString(trace[:16]) + "-" + hex.EncodeToString(trace[16:]) + "-01"
}
