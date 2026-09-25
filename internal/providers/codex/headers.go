package codex

import (
	"net/http"
	"strings"
)

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

// applyCodexRequestHeaders adds the request-scoped headers the ChatGPT Codex
// backend reads alongside the body: the routing hint names the resolved model,
// and responses-lite models must be marked or the backend serves the wrong
// protocol variant.
func applyCodexRequestHeaders(h http.Header, model string, lite bool) {
	model = strings.TrimSpace(model)
	if model != "" {
		h.Set(codexRoutingHintHeader, "model="+model)
	}
	if lite {
		h.Set(codexResponsesLiteHeader, "true")
	}
}
