package providers

import (
	"context"
	"strings"
)

type clientUserAgentKey struct{}

// WithClientUserAgent records the inbound User-Agent so providers can branch
// on the calling client (for example Codex SIWC vs outer OAuth).
func WithClientUserAgent(ctx context.Context, ua string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return ctx
	}
	return context.WithValue(ctx, clientUserAgentKey{}, ua)
}

// ClientUserAgentFromContext returns the inbound User-Agent set by
// WithClientUserAgent, or empty when unset.
func ClientUserAgentFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	ua, _ := ctx.Value(clientUserAgentKey{}).(string)
	return strings.TrimSpace(ua)
}
