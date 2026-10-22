package executor

import (
	"crypto/x509"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

func TestClassifyErrorQueuedKeepsAccountHealthy(t *testing.T) {
	c := ClassifyError(&providers.Error{Kind: accounts.KindRateLimit, Status: 429, Code: providers.CodeUpstreamQueued, RetryAfter: 7 * time.Second, Message: "queued"})
	if c.Failover || c.Cooldown != 0 || c.RetryAfter != 7*time.Second || c.Status != http.StatusTooManyRequests {
		t.Fatalf("queued classified=%+v", c)
	}
}

func TestClassifyErrorClientVersionTooLowNoRetry(t *testing.T) {
	c := ClassifyError(&providers.Error{Kind: accounts.KindUnavailable, Status: 502, Code: providers.CodeClientVersionTooLow, Message: "too low"})
	if c.Failover || c.Cooldown != 0 {
		t.Fatalf("430 classified=%+v", c)
	}
}

func TestClassifyErrorTLSVerifyNoFailover(t *testing.T) {
	raw := fmt.Errorf("Post https://x: %w", x509.UnknownAuthorityError{})
	c := ClassifyError(raw)
	if c.Failover || c.Cooldown != 0 || c.Code != providers.CodeTLSVerifyFailed {
		t.Fatalf("tls classified=%+v", c)
	}
	c = ClassifyError(fmt.Errorf("connection reset by peer"))
	if !c.Failover {
		t.Fatalf("transient errors must still fail over: %+v", c)
	}
}
