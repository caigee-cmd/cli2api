package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/providers/qoder"
)

// WorkerAdmin coordinates worker readiness and authoritative credential sync.
// HTTP status/body are opaque worker protocol results, not public handlers.
func (m *Manager) WorkerAdmin(ctx context.Context, input providers.AdminRequest) (providers.AdminResponse, error) {
	fail := func(code string, err error) (providers.AdminResponse, error) {
		return providers.AdminResponse{}, &providers.ActionError{Code: code, Err: err}
	}
	spec, ok := qoder.AdminAction(input.Action)
	if !ok {
		return fail("not_found", fmt.Errorf("unknown account action"))
	}
	url, running := m.AccountURL(input.AccountID)
	if !running || strings.TrimSpace(url) == "" {
		return fail("account_not_running", qoder.ErrAccountNotRunning)
	}
	if spec.WaitForAuthManager {
		ready, err := qoder.WaitForAuthManager(ctx, func() (string, bool) { return url, true }, 90*time.Second, 200*time.Millisecond)
		if err != nil {
			return fail("not_ready", err)
		}
		url = ready
	}
	client := qoder.WorkerClient{HTTP: &http.Client{Timeout: 120 * time.Second}, ProxyAPIKey: m.ProxyAPIKey()}
	status, header, body, err := client.Admin(ctx, url, input.Method, spec.Path, input.ContentType, input.Body)
	if err != nil {
		var transport qoder.TransportError
		if errors.As(err, &transport) {
			return fail("worker_unavailable", err)
		}
		return fail("worker_request_failed", err)
	}
	if status < 300 {
		if authType := qoder.LoginCompleteAuthType(spec.SyncAuth, body); authType != "" {
			if err := m.SyncCredential(ctx, input.AccountID, authType); err != nil {
				return fail("credential_sync_failed", err)
			}
		}
	}
	return providers.AdminResponse{Status: status, Header: header, Body: body}, nil
}
