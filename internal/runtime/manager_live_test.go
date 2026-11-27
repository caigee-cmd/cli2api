package runtime

import (
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func liveView(id string, quota *accounts.QuotaSnapshot) AccountView {
	return AccountView{Account: accounts.Account{ID: id}, Quota: quota}
}

func TestApplyLiveReadOutcomeDropsFailedQuota(t *testing.T) {
	quota := &accounts.QuotaSnapshot{Remaining: 1804, Total: 2500, FetchedAt: "2026-11-27T04:11:48Z"}
	views := []AccountView{
		liveView("acc_ok", quota),
		liveView("acc_fail", quota),
		liveView("acc_unread", nil),
	}
	failures := map[string]string{
		"acc_fail": "fetch quota account=acc_fail: user-resource status=401: 401 Authorization Required",
	}
	out := applyLiveReadOutcome(views, failures)

	if out[0].Quota != quota || out[0].QuotaError != "" {
		t.Fatalf("healthy account changed: quota=%+v error=%q", out[0].Quota, out[0].QuotaError)
	}
	if out[1].Quota != nil {
		t.Fatalf("failed account kept a stale quota: %+v", out[1].Quota)
	}
	if out[1].QuotaError == "" {
		t.Fatalf("failed account lost the upstream failure")
	}
	if out[2].QuotaError != "" {
		t.Fatalf("untouched account gained an error: %q", out[2].QuotaError)
	}
}

func TestLiveReadFailureMessageFallsBack(t *testing.T) {
	if got := liveReadFailureMessage(accounts.AccountRefreshResult{Code: "quota_refresh_failed"}); got != "quota_refresh_failed" {
		t.Fatalf("code fallback = %q", got)
	}
	if got := liveReadFailureMessage(accounts.AccountRefreshResult{Kind: "auth"}); got != "auth" {
		t.Fatalf("kind fallback = %q", got)
	}
	if got := liveReadFailureMessage(accounts.AccountRefreshResult{}); got == "" {
		t.Fatalf("empty result should still carry a reason")
	}
}

func TestReserveLiveReadCoalesces(t *testing.T) {
	manager := &Manager{}
	if !manager.reserveLiveRead() {
		t.Fatal("first read must own the fan-out")
	}
	if manager.reserveLiveRead() {
		t.Fatal("second read inside the window must reuse the first")
	}
	manager.mu.Lock()
	manager.lastLiveReadAt = time.Now().Add(-2 * liveReadCoalesce)
	manager.mu.Unlock()
	if !manager.reserveLiveRead() {
		t.Fatal("a read after the window must probe again")
	}
}
