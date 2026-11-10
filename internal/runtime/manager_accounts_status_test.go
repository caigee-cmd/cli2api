package runtime

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	sqlstore "github.com/caigee-cmd/cli2api/internal/store"
)

func TestAccountViewReconcilesExpiredCooldown(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	view := AccountView{Account: Account{
		Status:        "cooling",
		CooldownUntil: &past,
		LastError:     "first byte timeout: no response head within 1m0s",
		LastErrorKind: KindUnavailable,
	}}

	reconcileExpiredCooldown(&view, time.Now())

	if view.Status != "ready" {
		t.Fatalf("status = %q, want ready", view.Status)
	}
	if view.CooldownUntil != nil {
		t.Fatalf("cooldown_until = %v, want nil", view.CooldownUntil)
	}
	if view.LastError != "" || view.LastErrorKind != "" {
		t.Fatalf("stale error survived: %q / %q", view.LastError, view.LastErrorKind)
	}
}

func TestAccountViewKeepsActiveCooldown(t *testing.T) {
	future := time.Now().Add(time.Hour)
	view := AccountView{Account: Account{
		Status:        "cooling",
		CooldownUntil: &future,
		LastError:     "rate limited",
		LastErrorKind: KindUnavailable,
	}}

	reconcileExpiredCooldown(&view, time.Now())

	if view.Status != "cooling" || view.CooldownUntil == nil || !view.CooldownUntil.Equal(future) {
		t.Fatalf("active cooldown was changed: %+v", view.Account)
	}
	if view.LastError != "rate limited" {
		t.Fatalf("active cooldown lost its last error: %q", view.LastError)
	}
}

func TestAccountViewKeepsLivePoolCooldown(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	view := AccountView{Account: Account{
		Status:        "cooling",
		CooldownUntil: &past,
	}}
	view.DownUntil = future.UTC().Format(time.RFC3339)

	reconcileExpiredCooldown(&view, time.Now())

	if view.Status != "cooling" {
		t.Fatalf("live pool cooldown was hidden: status = %q", view.Status)
	}
}

func TestAccountViewLeavesOtherStatusesAlone(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	view := AccountView{Account: Account{
		Status:        "login_required",
		CooldownUntil: &past,
		LastError:     "invalid credential",
		LastErrorKind: KindAuth,
	}}

	reconcileExpiredCooldown(&view, time.Now())

	if view.Status != "login_required" || view.LastError != "invalid credential" {
		t.Fatalf("non-cooling view was changed: %+v", view.Account)
	}
}

type statusTestStarter struct{}

func (statusTestStarter) Start(context.Context, Account, string, int) (ManagedProcess, error) {
	return nil, nil
}

// A stored "cooling" row whose deadline has passed must not reach the console
// as cooling. This reproduces the persisted state that motivated the helper.
func TestAccountsReconcilesExpiredCoolingFromStore(t *testing.T) {
	ctx := context.Background()
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	account, err := store.Create(ctx, accounts.CreateAccount{Name: "Stale cooldown", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordPoolState(ctx, accounts.PoolState{
		ID:            account.ID,
		DownUntil:     time.Now().Add(500 * time.Millisecond),
		LastError:     "first byte timeout: no response head within 1m0s",
		LastErrorKind: KindUnavailable,
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(800 * time.Millisecond)

	manager := NewManager(ManagerConfig{DataDir: t.TempDir(), ProxyAPIKey: "proxy-key"}, store, statusTestStarter{})
	views, err := manager.Accounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 {
		t.Fatalf("views = %d, want 1", len(views))
	}
	view := views[0]
	if view.Status != "ready" {
		t.Fatalf("expired cooling leaked to console: status = %q", view.Status)
	}
	if view.CooldownUntil != nil {
		t.Fatalf("expired cooldown leaked to console: %v", view.CooldownUntil)
	}
	if view.LastError != "" || view.LastErrorKind != "" {
		t.Fatalf("expired cooldown error leaked to console: %q / %q", view.LastError, view.LastErrorKind)
	}
}
