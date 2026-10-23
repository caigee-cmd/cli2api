package executor

import (
	"context"

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
