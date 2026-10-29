package executor

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

// qoderHandshakeTimeout is the error shape http.Client returns when the TCP
// connect succeeded but the TLS handshake never completed: *url.Error wrapping
// a plain message, exactly what api1.qoder.sh produced in the field.
func qoderHandshakeTimeout() error {
	return &url.Error{
		Op:  "Post",
		URL: "https://api1.qoder.sh/algo/api/v2/service/pro/sse/agent_chat_generation?FetchKeys=llm_model_result&AgentId=agent_common&Encode=1",
		Err: errors.New("net/http: TLS handshake timeout"),
	}
}

func TestClassifyErrorTransportFailureKeepsAccountRoutable(t *testing.T) {
	c := ClassifyError(qoderHandshakeTimeout())
	if c.Kind != accounts.KindUnavailable || c.Status != 502 || c.Code != "upstream_unreachable" {
		t.Fatalf("transport classified=%+v", c)
	}
	if c.Cooldown != 0 || c.RetryAfter != 0 || c.Failover {
		t.Fatalf("transport failure must not cool or fail over: %+v", c)
	}
	if !strings.Contains(c.Message, "TLS handshake timeout") {
		t.Fatalf("message must keep the transport cause: %q", c.Message)
	}

	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "qoder-global", Provider: "qoder", Region: "global"})
	executor := NewChatExecutor(pool, "")
	executor.markClassified("qoder-global", c, "deepseek-flash")

	item, ok := pool.ByID("qoder-global")
	if !ok {
		t.Fatal("account missing from pool")
	}
	if !item.DownUntil.IsZero() {
		t.Fatalf("transport failure parked the account until %v", item.DownUntil)
	}
	if len(item.ModelDownUntil) != 0 {
		t.Fatalf("transport failure recorded a model cooldown: %v", item.ModelDownUntil)
	}
	if _, ok := pool.PickRoute(RouteQuery{PublicModel: "deepseek-flash"}); !ok {
		t.Fatal("account must stay routable after a transport failure")
	}
}

// A 502 whose *body* quotes the same text is an answer from the upstream (or
// from something in front of it), not a transport failure on our side: the
// generic unavailable cooldown still applies. Detection is by error shape, not
// by message text.
func TestClassifyErrorUpstreamQuotedHandshakeTimeoutStillCools(t *testing.T) {
	quoted := &providers.Error{
		Kind:    accounts.KindUnavailable,
		Status:  502,
		Message: qoderHandshakeTimeout().Error(),
	}
	c := ClassifyError(quoted)
	if !c.Failover {
		t.Fatalf("upstream 502 must still fail over: %+v", c)
	}
	if c.Cooldown != 15*time.Second {
		t.Fatalf("upstream 502 keeps the unavailable cooldown, got %v", c.Cooldown)
	}
}

// Adapters wrap transport errors (qoder.TransportError, fmt.Errorf("%w")); the
// policy must survive the wrapping.
func TestClassifyErrorWrappedTransportFailure(t *testing.T) {
	wrapped := fmt.Errorf("qoder chat: %w", qoderHandshakeTimeout())
	c := ClassifyError(wrapped)
	if c.Cooldown != 0 || c.Failover || c.Code != "upstream_unreachable" {
		t.Fatalf("wrapped transport classified=%+v", c)
	}
}
