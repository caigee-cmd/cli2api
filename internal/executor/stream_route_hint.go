package executor

import (
	"context"
	"sync"
)

type streamRouteHintKey struct{}

// StreamRouteHint holds the account/provider the executor has already picked
// for an in-flight stream attempt. The gateway reads it from setStreamHeaders
// so a heartbeat that commits before ChatStreamProxy returns can still send
// X-CLI2API-Account and X-CLI2API-Provider.
type StreamRouteHint struct {
	mu        sync.Mutex
	accountID string
	provider  string
}

func (h *StreamRouteHint) Set(accountID, provider string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.accountID = accountID
	h.provider = provider
	h.mu.Unlock()
}

func (h *StreamRouteHint) Get() (accountID, provider string) {
	if h == nil {
		return "", ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.accountID, h.provider
}

func WithStreamRouteHint(ctx context.Context, hint *StreamRouteHint) context.Context {
	if ctx == nil || hint == nil {
		return ctx
	}
	return context.WithValue(ctx, streamRouteHintKey{}, hint)
}

func noteStreamRoute(ctx context.Context, accountID, provider string) {
	if ctx == nil {
		return
	}
	hint, _ := ctx.Value(streamRouteHintKey{}).(*StreamRouteHint)
	if hint == nil {
		return
	}
	hint.Set(accountID, provider)
}
