package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/events"
	"github.com/caigee-cmd/cli2api/internal/providers"
	sqlstore "github.com/caigee-cmd/cli2api/internal/store"
)

func openEventTestManager(t *testing.T) (*Manager, *sqlstore.Store) {
	t.Helper()
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, nil)
	t.Cleanup(func() { manager.Close() })
	return manager, store
}

// persistQuota must publish the exact snapshot it persisted, never a copy or
// a synthesized value.
func TestPersistQuotaPublishesRealSnapshot(t *testing.T) {
	manager, _ := openEventTestManager(t)
	stream, cancel := manager.SubscribeEvents(0)
	defer cancel()

	quota := &accounts.QuotaSnapshot{Total: 1000, Remaining: 240, Percentage: 76, FetchedAt: time.Now().UTC().Format(time.RFC3339)}
	manager.persistQuota(context.Background(), "acc-quota", quota)

	event := <-stream
	if event.Type != events.TypeQuota {
		t.Fatalf("event = %+v", event)
	}
	state, ok := event.Data.(events.QuotaState)
	if !ok || state.Quota == nil || state.Quota.Remaining != 240 {
		t.Fatalf("data = %#v", event.Data)
	}
}

// One-click check-in must publish start, per-account, and done frames carrying
// the real provider outcome and reward.
func TestCheckinAllPublishesBulkProgressWithRealReward(t *testing.T) {
	manager, _ := openEventTestManager(t)
	registry := providers.NewRegistry()
	registry.Register(providers.Adapter{ID: "qoder", Checkin: checkinFunc(func(context.Context, string) (providers.CheckinResult, error) {
		return providers.CheckinResult{Status: "success", Message: "claimed", RewardCredits: 150}, nil
	})})
	manager.SetProviders(registry)
	enabled := true
	for i := 0; i < 2; i++ {
		if _, err := manager.store.Create(context.Background(), accounts.CreateAccount{
			Name: "batch", Provider: "qoder", Region: "cn", Enabled: true, AutoCheckin: &enabled,
		}); err != nil {
			t.Fatal(err)
		}
	}
	stream, cancel := manager.SubscribeEvents(0)
	defer cancel()

	results, err := manager.CheckinAll(context.Background(), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results=%d", len(results))
	}

	var sawStart, sawDone, sawReward bool
	deadline := time.After(2 * time.Second)
	for !(sawStart && sawDone && sawReward) {
		select {
		case <-deadline:
			t.Fatalf("missing bulk frames start=%v done=%v reward=%v", sawStart, sawDone, sawReward)
		case event := <-stream:
			if event.Type != events.TypeBulk {
				continue
			}
			progress, ok := event.Data.(events.BulkProgress)
			if !ok {
				continue
			}
			switch progress.Phase {
			case "start":
				sawStart = progress.Total == 2
			case "done":
				sawDone = progress.Done == 2
			case "account":
				if result, ok := progress.Result.(accounts.CheckinAllResult); ok && result.RewardCredits == 150 {
					sawReward = true
				}
			}
		}
	}
}

// The force-ready override must affect routing only: SQLite keeps the real
// snapshot, the view shows the real snapshot, and the override is badged.
func TestForceReadyOverrideNeverFabricatesStorage(t *testing.T) {
	manager, store := openEventTestManager(t)
	account, err := store.Create(context.Background(), accounts.CreateAccount{
		Name: "forced", Provider: "qoder", Region: "cn", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(manager.config.DataDir, "force-ready"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manager.config.DataDir, "force-ready", account.ID), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager.pool.Upsert(Item{ID: account.ID, Provider: "qoder", Region: "cn", RuntimeState: "ready"})

	realQuota := &accounts.QuotaSnapshot{Total: 100, Remaining: 0, Percentage: 100, Exceeded: true, FetchedAt: time.Now().UTC().Format(time.RFC3339)}
	manager.persistQuota(context.Background(), account.ID, realQuota)

	stored, err := store.Get(context.Background(), account.ID)
	if err != nil || stored.Quota == nil {
		t.Fatalf("stored=%+v err=%v", stored.Quota, err)
	}
	if !stored.Quota.Exceeded || stored.Quota.Remaining != 0 {
		t.Fatalf("storage must hold the real exhausted snapshot, got %+v", stored.Quota)
	}
	item, ok := manager.pool.ByID(account.ID)
	if !ok || item.Quota == nil || item.Quota.Exceeded || item.Quota.Remaining <= 0 {
		t.Fatalf("routing copy must be overridden, got %+v", item.Quota)
	}
	view, err := manager.AccountView(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !view.ForceReadyOverride {
		t.Fatal("view must badge the local override")
	}
	if view.Quota == nil || !view.Quota.Exceeded || view.Quota.Remaining != 0 {
		t.Fatalf("view must show the real snapshot, got %+v", view.Quota)
	}
}
