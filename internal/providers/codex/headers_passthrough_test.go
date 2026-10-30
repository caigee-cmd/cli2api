package codex

import (
	"net/http"
	"strings"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

func TestApplyCodexPassthroughHeaders(t *testing.T) {
	src := http.Header{}
	src.Set("X-Codex-Turn-State", " opaque-state ")
	src.Set("X-Codex-Turn-Metadata", `{"turn_id":"t1"}`)
	src.Set("X-Codex-Window-Id", "win-1")
	src.Set("X-Codex-Routing-Hint", "must-not-passthrough")
	src.Set("Authorization", "secret")

	dst := http.Header{}
	applyCodexPassthroughHeaders(dst, src)

	if got := dst.Get("X-Codex-Turn-State"); got != "opaque-state" {
		t.Fatalf("Turn-State=%q", got)
	}
	if got := dst.Get("X-Codex-Turn-Metadata"); got != `{"turn_id":"t1"}` {
		t.Fatalf("Turn-Metadata=%q", got)
	}
	if got := dst.Get("X-Codex-Window-Id"); got != "win-1" {
		t.Fatalf("Window-Id=%q", got)
	}
	if dst.Get("X-Codex-Routing-Hint") != "" || dst.Get("Authorization") != "" {
		t.Fatal("must not widen passthrough scope")
	}
}

func TestApplyCodexPassthroughHeadersSkipsEmpty(t *testing.T) {
	dst := http.Header{}
	applyCodexPassthroughHeaders(dst, http.Header{"X-Codex-Turn-State": {"  "}})
	if dst.Get("X-Codex-Turn-State") != "" {
		t.Fatal("blank values must be skipped")
	}
	applyCodexPassthroughHeaders(dst, nil)
	applyCodexPassthroughHeaders(nil, http.Header{"X-Codex-Turn-State": {"x"}})
}

func TestWithCodexClientHeadersFilters(t *testing.T) {
	src := http.Header{}
	src.Set("X-Codex-Turn-State", "s")
	src.Set("X-Other", "nope")
	ctx := providers.WithCodexClientHeaders(nil, src)
	got := providers.CodexClientHeadersFromContext(ctx)
	if got.Get("X-Codex-Turn-State") != "s" || got.Get("X-Other") != "" {
		t.Fatalf("got=%v", got)
	}
	if providers.CodexClientHeadersFromContext(nil) != nil {
		t.Fatal("nil ctx")
	}
}

func TestUserAgentPinnedToOfficialStable(t *testing.T) {
	if !strings.Contains(UserAgent, "0.166.0") {
		t.Fatalf("UserAgent=%q want 0.166.0", UserAgent)
	}
}
