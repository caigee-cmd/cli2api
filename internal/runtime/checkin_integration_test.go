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

func TestCheckinAllRunsWithBoundedConcurrency(t *testing.T) {
	var active atomic.Int32
	var maxActive atomic.Int32
	manager, _ := newCheckinManager(t, checkinFunc(func(context.Context, string) (providers.CheckinResult, error) {
		now := active.Add(1)
		defer active.Add(-1)
		for {
			previous := maxActive.Load()
			if now <= previous || maxActive.CompareAndSwap(previous, now) {
				break
			}
		}
		time.Sleep(40 * time.Millisecond)
		return providers.CheckinResult{Status: "success", Message: "claimed"}, nil
	}))
	enabled := true
	for i := 0; i < 8; i++ {
		if _, err := manager.store.Create(context.Background(), accounts.CreateAccount{
			Name: "batch", Provider: "qoder", Region: "cn", Enabled: true, AutoCheckin: &enabled,
		}); err != nil {
			t.Fatal(err)
		}
	}
	results, err := manager.CheckinAll(context.Background(), false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 9 {
		t.Fatalf("results=%d want 9", len(results))
	}
	for _, result := range results {
		if result.Status != "success" {
			t.Fatalf("result=%+v", result)
		}
	}
	if got := maxActive.Load(); got < 2 || got > manualCheckinConcurrency {
		t.Fatalf("max concurrency=%d want 2..%d", got, manualCheckinConcurrency)
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

func TestCheckinSchedulerRetriesSkippedCampaignAtRetryWindow(t *testing.T) {
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
	// Pin the first attempt to the morning so the 21:00 retry window still
	// applies no matter what time of day the test runs.
	morning := time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, location)
	if err := manager.store.RecordCheckin(context.Background(), account.ID, "skipped", "activity disabled", morning.UTC()); err != nil {
		t.Fatal(err)
	}
	manager.runScheduledCheckins(context.Background(), time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 0, 0, location))
	if calls.Load() != 2 {
		t.Fatalf("skipped campaign should retry at the retry window, calls=%d", calls.Load())
	}
	if _, err := manager.CheckinAccount(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
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
	unsupported, err := manager.store.Create(context.Background(), accounts.CreateAccount{Name: "devin", Provider: "devin", Region: "global", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.CheckinAccount(context.Background(), unsupported.ID); !errors.Is(err, providers.ErrUnsupported) {
		t.Fatalf("err=%v", err)
	}
}

func TestScheduledCheckinCanIncludeDisabledAccountsWhenEnabled(t *testing.T) {
	var calls atomic.Int64
	manager, account := newCheckinManager(t, checkinFunc(func(context.Context, string) (providers.CheckinResult, error) {
		calls.Add(1)
		return providers.CheckinResult{Status: "success", Message: "claimed"}, nil
	}))
	disabled := false
	if err := manager.store.Update(context.Background(), account.ID, accounts.UpdateAccount{Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	if err := manager.store.SetSecret(context.Background(), accounts.CheckinDisabledAccountsSecret, "1"); err != nil {
		t.Fatal(err)
	}

	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(location)
	manager.runScheduledCheckins(context.Background(), time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 0, 0, location))
	if calls.Load() != 1 {
		t.Fatalf("scheduled check-in calls=%d", calls.Load())
	}
}
