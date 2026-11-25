package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	sqlstore "github.com/caigee-cmd/cli2api/internal/store"
)

type checkinStatusFunc func(context.Context, string) (providers.CheckinResult, error)

func (read checkinStatusFunc) CheckinStatus(ctx context.Context, accountID string) (providers.CheckinResult, error) {
	return read(ctx, accountID)
}

func newStatusManager(t *testing.T, read providers.AccountCheckinStatusReader) (*Manager, Account) {
	t.Helper()
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "checkin-status.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, nil)
	t.Cleanup(func() { manager.Close() })
	registry := providers.NewRegistry()
	registry.Register(providers.Adapter{
		ID: "qoder",
		Checkin: checkinFunc(func(context.Context, string) (providers.CheckinResult, error) {
			return providers.CheckinResult{Status: "skipped"}, nil
		}),
		CheckinStatus: read,
	})
	manager.SetProviders(registry)
	enabled := true
	account, err := store.Create(context.Background(), accounts.CreateAccount{Name: "CN", Provider: "qoder", Region: "cn", Enabled: true, AutoCheckin: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	return manager, account
}

func TestSyncCheckinStatusPersistsUpstreamAlreadyAndAutoTime(t *testing.T) {
	manager, account := newStatusManager(t, checkinStatusFunc(func(context.Context, string) (providers.CheckinResult, error) {
		return providers.CheckinResult{Status: "already", Message: "今日已签到", EarliestCheckin: "10:00"}, nil
	}))
	manager.SyncCheckinStatuses(context.Background())
	updated, err := manager.store.Get(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastCheckinStatus != "already" || updated.LastCheckinSource != "upstream" ||
		updated.LastCheckinSyncedAt == "" || updated.CheckinAutoTime != "09:00" ||
		updated.LastCheckinAt == "" {
		t.Fatalf("synced=%+v", updated)
	}
	if len(mustCheckinRecords(t, manager, account.ID)) != 0 {
		t.Fatal("status sync must not add a check-in history record")
	}
}

func TestSyncCheckinStatusPendingKeepsLastAttemptAndDerivesOneHour(t *testing.T) {
	manager, account := newStatusManager(t, checkinStatusFunc(func(context.Context, string) (providers.CheckinResult, error) {
		return providers.CheckinResult{Status: "pending", Message: "上游显示今日未签到", EarliestCheckin: "00:00"}, nil
	}))
	if err := manager.store.RecordCheckin(context.Background(), account.ID, "success", "claimed", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	manager.SyncCheckinStatuses(context.Background())
	updated, err := manager.store.Get(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastCheckinStatus != "success" {
		// Within the post-check-in grace window the fresh local success stands.
		t.Fatalf("grace clobbered=%+v", updated)
	}
	if updated.CheckinAutoTime != "09:00" {
		t.Fatalf("auto time=%q want 09:00", updated.CheckinAutoTime)
	}
	if updated.LastCheckinSource != "local" {
		t.Fatalf("grace must keep the local source, got %q", updated.LastCheckinSource)
	}
}

func TestSyncCheckinStatusPendingWinsAfterGrace(t *testing.T) {
	manager, account := newStatusManager(t, checkinStatusFunc(func(context.Context, string) (providers.CheckinResult, error) {
		return providers.CheckinResult{Status: "pending", Message: "上游显示今日未签到", EarliestCheckin: "00:00"}, nil
	}))
	old := time.Now().UTC().Add(-time.Hour)
	if err := manager.store.RecordCheckin(context.Background(), account.ID, "success", "claimed", old); err != nil {
		t.Fatal(err)
	}
	manager.SyncCheckinStatuses(context.Background())
	updated, err := manager.store.Get(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastCheckinStatus != "pending" || updated.LastCheckinSource != "upstream" || updated.CheckinAutoTime != "09:00" {
		t.Fatalf("pending=%+v", updated)
	}
	if updated.LastCheckinAt == "" {
		t.Fatal("pending sync erased the last real attempt timestamp")
	}
}

func TestSyncCheckinStatusErrorMarksUnknownWithoutFakeCheckin(t *testing.T) {
	manager, account := newStatusManager(t, checkinStatusFunc(func(context.Context, string) (providers.CheckinResult, error) {
		return providers.CheckinResult{}, errors.New("upstream unavailable")
	}))
	manager.SyncCheckinStatuses(context.Background())
	updated, err := manager.store.Get(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastCheckinStatus != "unknown" || updated.LastCheckinSource != "upstream" ||
		updated.LastCheckinAt != "" || updated.CheckinAutoTime != "09:00" {
		t.Fatalf("unknown=%+v", updated)
	}
}

func TestSyncCheckinStatusDoesNotOverwriteCheckinFinishedDuringProbe(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	manager, account := newStatusManager(t, checkinStatusFunc(func(context.Context, string) (providers.CheckinResult, error) {
		close(started)
		<-release
		return providers.CheckinResult{Status: "pending", Message: "上游显示今日未签到", EarliestCheckin: "10:00"}, nil
	}))
	done := make(chan struct{})
	go func() {
		manager.SyncCheckinStatuses(context.Background())
		close(done)
	}()
	<-started
	// The probe is still in flight when a real check-in lands.
	if err := manager.store.RecordCheckin(context.Background(), account.ID, "success", "claimed while probing", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	close(release)
	<-done

	updated, err := manager.store.Get(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastCheckinStatus != "success" || updated.LastCheckinSource != "local" {
		t.Fatalf("a stale probe overwrote the check-in: %+v", updated)
	}
	if updated.LastCheckinMsg != "claimed while probing" {
		t.Fatalf("message=%q", updated.LastCheckinMsg)
	}
}

func TestSyncCheckinStatusUnknownKeepsRealLocalSuccess(t *testing.T) {
	manager, account := newStatusManager(t, checkinStatusFunc(func(context.Context, string) (providers.CheckinResult, error) {
		// A provisional rollover answer: the provider cannot yet confirm today.
		return providers.CheckinResult{Status: "unknown", Message: "上游日界尚未确认，稍后复核", EarliestCheckin: "00:00"}, nil
	}))
	old := time.Now().UTC().Add(-2 * time.Hour)
	if err := manager.store.RecordCheckin(context.Background(), account.ID, "success", "claimed earlier", old); err != nil {
		t.Fatal(err)
	}
	manager.SyncCheckinStatuses(context.Background())
	updated, err := manager.store.Get(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastCheckinStatus != "success" || updated.LastCheckinSource != "local" {
		t.Fatalf("unknown must not erase a successful local day: %+v", updated)
	}
}

func TestSyncCheckinStatusAppliesProviderSettingWhenProbeFails(t *testing.T) {
	manager, account := newStatusManager(t, checkinStatusFunc(func(context.Context, string) (providers.CheckinResult, error) {
		return providers.CheckinResult{}, errors.New("upstream unavailable")
	}))
	ctx := context.Background()
	if err := manager.store.SetSecret(ctx, accounts.CheckinTimeSecret("qoder"), "10:30"); err != nil {
		t.Fatal(err)
	}
	if err := manager.store.RecordCheckinStatus(ctx, account.ID, accounts.CheckinStatusUpdate{Status: "pending", AutoTime: "11:00"}); err != nil {
		t.Fatal(err)
	}
	manager.SyncCheckinStatuses(ctx)
	updated, err := manager.store.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.CheckinAutoTime != "10:30" {
		t.Fatalf("the provider setting did not reach the account: %q", updated.CheckinAutoTime)
	}
}

func TestSyncCheckinStatusRewritesStoredAutoTimeFromSetting(t *testing.T) {
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "checkin-window.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, nil)
	t.Cleanup(func() { manager.Close() })
	registry := providers.NewRegistry()
	registry.Register(providers.Adapter{
		ID: "trae",
		Checkin: checkinFunc(func(context.Context, string) (providers.CheckinResult, error) {
			return providers.CheckinResult{Status: "skipped"}, nil
		}),
		CheckinStatus: checkinStatusFunc(func(context.Context, string) (providers.CheckinResult, error) {
			// Trae only reports the day flag, never a window start.
			return providers.CheckinResult{Status: "pending", Message: "上游显示今日未签到"}, nil
		}),
	})
	manager.SetProviders(registry)
	enabled := true
	account, err := store.Create(context.Background(), accounts.CreateAccount{Name: "TRAE", Provider: "trae", Region: "cn", Enabled: true, AutoCheckin: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.RecordCheckinStatus(ctx, account.ID, accounts.CheckinStatusUpdate{Status: "pending", AutoTime: "05:00"}); err != nil {
		t.Fatal(err)
	}
	manager.SyncCheckinStatuses(ctx)
	updated, err := store.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.CheckinAutoTime != accounts.DefaultCheckinTime {
		t.Fatalf("default setting not applied: %q", updated.CheckinAutoTime)
	}
}

func mustCheckinRecords(t *testing.T, manager *Manager, id string) []accounts.CheckinRecord {
	t.Helper()
	records, err := manager.store.ListCheckinRecords(context.Background(), id, 20)
	if err != nil {
		t.Fatal(err)
	}
	return records
}
