package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	sqlstore "github.com/caigee-cmd/cli2api/internal/store"
)

type checkinFunc func(context.Context, string) (providers.CheckinResult, error)

func (checkin checkinFunc) Checkin(ctx context.Context, accountID string) (providers.CheckinResult, error) {
	return checkin(ctx, accountID)
}

func newCheckinManager(t *testing.T, checkiner providers.AccountCheckiner) (*Manager, Account) {
	t.Helper()
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "checkin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, nil)
	t.Cleanup(func() { manager.Close() })
	registry := providers.NewRegistry()
	registry.Register(providers.Adapter{ID: "qoder", Checkin: checkiner})
	manager.SetProviders(registry)
	enabled := true
	account, err := store.Create(context.Background(), accounts.CreateAccount{Name: "CN", Provider: "qoder", Region: "cn", Enabled: true, AutoCheckin: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	return manager, account
}

func TestCheckinCapabilityRecordsAndDeduplicates(t *testing.T) {
	var calls atomic.Int64
	manager, account := newCheckinManager(t, checkinFunc(func(context.Context, string) (providers.CheckinResult, error) {
		calls.Add(1)
		return providers.CheckinResult{Status: "success", Message: "claimed"}, nil
	}))
	for range 2 {
		updated, err := manager.CheckinAccount(context.Background(), account.ID)
		if err != nil || updated.LastCheckinStatus != "success" {
			t.Fatalf("updated=%+v err=%v", updated, err)
		}
	}
	records, err := manager.store.ListCheckinRecords(context.Background(), account.ID, 20)
	if err != nil || len(records) != 1 || calls.Load() != 1 {
		t.Fatalf("records=%+v calls=%d err=%v", records, calls.Load(), err)
	}
}

func TestCheckinConcurrentManualAndScheduledDoNotOverlap(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	manager, account := newCheckinManager(t, checkinFunc(func(context.Context, string) (providers.CheckinResult, error) {
		close(entered)
		<-release
		return providers.CheckinResult{Status: "already"}, nil
	}))
	done := make(chan error, 1)
	go func() { _, err := manager.CheckinAccount(context.Background(), account.ID); done <- err }()
	<-entered
	_, err := manager.CheckinAccount(context.Background(), account.ID)
	close(release)
	if err == nil {
		t.Error("overlapping check-in accepted")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCheckinCanceledAttemptIsPersistedWithoutChatCooldown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager, account := newCheckinManager(t, checkinFunc(func(ctx context.Context, _ string) (providers.CheckinResult, error) {
		cancel()
		return providers.CheckinResult{}, ctx.Err()
	}))
	_, err := manager.CheckinAccount(ctx, account.ID)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	updated, err := manager.store.Get(context.Background(), account.ID)
	if err != nil || updated.LastCheckinStatus != "error" || updated.LastErrorKind != "" || updated.CooldownUntil != nil {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
}

func TestCheckinSchedulerSkipsDisabledCampaignButManualCanRecheck(t *testing.T) {
	var calls atomic.Int64
	manager, account := newCheckinManager(t, checkinFunc(func(context.Context, string) (providers.CheckinResult, error) {
		calls.Add(1)
		return providers.CheckinResult{Status: "skipped", Message: "activity disabled"}, nil
	}))
	if _, err := manager.CheckinAccount(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	location, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Now().In(location)
	manager.runScheduledCheckins(context.Background(), time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 0, 0, location))
	if calls.Load() != 1 {
		t.Fatal("disabled campaign ran repeatedly")
	}
	if _, err := manager.CheckinAccount(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("manual recheck must remain available")
	}
}

func TestCheckinRejectsDisabledAndUnsupportedAccounts(t *testing.T) {
	manager, account := newCheckinManager(t, checkinFunc(func(context.Context, string) (providers.CheckinResult, error) {
		t.Error("unexpected upstream request")
		return providers.CheckinResult{}, nil
	}))
	disabled := false
	if err := manager.store.Update(context.Background(), account.ID, accounts.UpdateAccount{Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.CheckinAccount(context.Background(), account.ID); err == nil {
		t.Fatal("disabled account accepted")
	}
	global, err := manager.store.Create(context.Background(), accounts.CreateAccount{Name: "global", Provider: "qoder", Region: "global", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.CheckinAccount(context.Background(), global.ID); !errors.Is(err, providers.ErrUnsupported) {
		t.Fatalf("err=%v", err)
	}
}
