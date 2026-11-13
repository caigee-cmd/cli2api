package executor

import (
	"context"
	"net/http"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// WithClientUserAgent records the inbound User-Agent for provider routing.
func WithClientUserAgent(ctx context.Context, ua string) context.Context {
	return providers.WithClientUserAgent(ctx, ua)
}

// ClientUserAgentFromContext returns the inbound User-Agent, or empty when unset.
func ClientUserAgentFromContext(ctx context.Context) string {
	return providers.ClientUserAgentFromContext(ctx)
}

// WithCodexClientHeaders records inbound Codex turn/window headers for the
// Codex provider to pass through on OAuth (non-SIWC) upstream requests.
func WithCodexClientHeaders(ctx context.Context, h http.Header) context.Context {
	return providers.WithCodexClientHeaders(ctx, h)
}

// CodexClientHeadersFromContext returns inbound Codex passthrough headers.
func CodexClientHeadersFromContext(ctx context.Context) http.Header {
	return providers.CodexClientHeadersFromContext(ctx)
}

// WithConversation records the resolved session key for providers that keep an
// upstream conversation id stable across turns.
func WithConversation(ctx context.Context, key string) context.Context {
	return providers.WithConversation(ctx, key)
}
