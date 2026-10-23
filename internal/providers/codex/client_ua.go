package codex

import (
	"context"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// IsCodexClientUserAgent reports whether userAgent identifies an official Codex
// client. Aligned with CLIProxyAPI IsCodexClientUserAgent (d946c4f).
func IsCodexClientUserAgent(userAgent string) bool {
	ua := strings.ToLower(strings.TrimSpace(userAgent))
	if ua == "" {
		return false
	}
	return strings.Contains(ua, "codex-desktop") ||
		strings.Contains(ua, "codex desktop") ||
		strings.Contains(ua, "codex-rs") ||
		strings.Contains(ua, "codex_cli_rs") ||
		strings.Contains(ua, "codex-tui") ||
		strings.Contains(ua, "codex_exec")
}

// ClientUserAgentFromContext returns the inbound User-Agent recorded by the
// gateway, or empty when unset.
func ClientUserAgentFromContext(ctx context.Context) string {
	return providers.ClientUserAgentFromContext(ctx)
}
