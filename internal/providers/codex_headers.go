package providers

import (
	"context"
	"net/http"
	"strings"
)

type codexClientHeadersKey struct{}

// CodexPassthroughHeaderNames are inbound headers CLIProxyAPI EnsureHeader
// forwards onto ChatGPT Codex OAuth requests for turn continuity. Kept
// narrow: Routing-Hint / Responses-Lite are set by the adapter itself;
// Session-Id is managed locally; SIWC does not receive these.
var CodexPassthroughHeaderNames = []string{
	"X-Codex-Turn-Metadata",
	"X-Codex-Turn-State",
	"X-Codex-Window-Id",
}

// WithCodexClientHeaders records selected inbound Codex headers so the
// Codex adapter can pass them through on non-SIWC upstream requests.
func WithCodexClientHeaders(ctx context.Context, src http.Header) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if src == nil {
		return ctx
	}
	out := http.Header{}
	for _, name := range CodexPassthroughHeaderNames {
		if v := strings.TrimSpace(src.Get(name)); v != "" {
			out.Set(name, v)
		}
	}
	if len(out) == 0 {
		return ctx
	}
	return context.WithValue(ctx, codexClientHeadersKey{}, out)
}

// CodexClientHeadersFromContext returns the inbound Codex passthrough
// headers set by WithCodexClientHeaders, or nil when unset.
func CodexClientHeadersFromContext(ctx context.Context) http.Header {
	if ctx == nil {
		return nil
	}
	h, _ := ctx.Value(codexClientHeadersKey{}).(http.Header)
	return h
}
