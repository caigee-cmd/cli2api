package providers

import (
	"context"
	"strings"
)

type clientUserAgentKey struct{}

type conversationKey struct{}

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

// WithConversation records the gateway session key so a provider can keep one
// upstream conversation id across the turns of a chat. Routing still owns the
// key; this only carries it.
func WithConversation(ctx context.Context, key string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return ctx
	}
	return context.WithValue(ctx, conversationKey{}, key)
}

// ConversationFromContext returns the session key set by WithConversation, or
// empty when the request has no stable conversation.
func ConversationFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	key, _ := ctx.Value(conversationKey{}).(string)
	return strings.TrimSpace(key)
}
