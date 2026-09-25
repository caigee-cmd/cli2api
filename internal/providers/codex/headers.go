package codex

import "net/http"

// SetChatHeaders applies the upstream headers the ChatGPT codex backend expects.
// Cloaking (codex-tui UA + Originator) is required: the upstream rejects generic
// clients with Cloudflare 1010 otherwise.
func SetChatHeaders(h http.Header, credential Credential, sessionID string, streaming bool) {
	h.Set("Content-Type", "application/json")
	if credential.AccessToken != "" {
		h.Set("Authorization", "Bearer "+credential.AccessToken)
	}
	if credential.AccountID != "" {
		h.Set("Chatgpt-Account-Id", credential.AccountID)
	}
	h.Set("Originator", Originator)
	h.Set("User-Agent", UserAgent)
	if streaming {
		h.Set("Accept", "text/event-stream")
	} else {
		h.Set("Accept", "application/json")
	}
	h.Set("Connection", "Keep-Alive")
	if sessionID != "" {
		h.Set("Session-Id", sessionID)
	}
}
