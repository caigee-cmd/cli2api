package runtime

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	sqlstore "github.com/caigee-cmd/cli2api/internal/store"
)

type recordingKeepalive struct {
	mu    sync.Mutex
	calls []string
}

func (r *recordingKeepalive) DailyCheckin(context.Context, string) (string, error) {
	return "", nil
}

func (r *recordingKeepalive) Keepalive(_ context.Context, accountID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, accountID)
	return nil
}

func (r *recordingKeepalive) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.calls))
	copy(out, r.calls)
	return out
}

// One maintenance tick must fan out every due account concurrently instead of
// queuing them across ticks: all 6 opted-in accounts check in on the first
// tick, and a repeat tick does not re-run already-checked accounts.
func TestRunScheduledCheckinsFansOutAllDueAccounts(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	var active, maxActive int32
	var activeMu sync.Mutex
	manager, _ := newCheckinManager(t, checkinFunc(func(_ context.Context, accountID string) (providers.CheckinResult, error) {
		activeMu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		activeMu.Unlock()
		time.Sleep(30 * time.Millisecond)
		activeMu.Lock()
		active--
		activeMu.Unlock()
		mu.Lock()
		calls = append(calls, accountID)
		mu.Unlock()
		return providers.CheckinResult{Status: "success", Message: "ok"}, nil
	}))
	enabled := true
	for i := 0; i < 5; i++ {
		if _, err := manager.store.Create(context.Background(), accounts.CreateAccount{
			Name: "due", Provider: "qoder", Region: "cn", Enabled: true, AutoCheckin: &enabled,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// newCheckinManager already created one opted-in account → 6 due total.
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(location)
	dueAt := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 0, 0, location)

	start := time.Now()
	manager.runScheduledCheckins(context.Background(), dueAt)
	elapsed := time.Since(start)
	mu.Lock()
	first := append([]string(nil), calls...)
	mu.Unlock()
	if len(first) != 6 {
		t.Fatalf("first tick calls=%d want 6 ids=%v", len(first), first)
	}
	if maxActive < 6 {
		t.Fatalf("scheduled check-ins were not fully concurrent: maxActive=%d want 6", maxActive)
	}
	if elapsed > time.Second {
		t.Fatalf("full fan-out took %v; accounts were queued", elapsed)
	}

	manager.runScheduledCheckins(context.Background(), dueAt)
	mu.Lock()
	after := len(calls)
	mu.Unlock()
	if after != 6 {
		t.Fatalf("already-checked accounts re-ran: calls=%d", after)
	}
}

// Keepalive must refresh every eligible account in one concurrent call and
// report no remaining work; the done map still deduplicates across ticks.
func TestKeepaliveWorkBuddyFansOutAndDeduplicates(t *testing.T) {
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "keepalive.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, nil)
	t.Cleanup(func() { manager.Close() })
	rec := &recordingKeepalive{}
	manager.SetWorkBuddy(rec)

	enabled := true
	for i := 0; i < 5; i++ {
		if _, err := store.Create(context.Background(), accounts.CreateAccount{
			Name: "wb", Provider: "workbuddy", Region: "cn", Enabled: true, WorkBuddyAutoCheckin: &enabled,
		}); err != nil {
			t.Fatal(err)
		}
	}

	done := map[string]struct{}{}
	more := manager.KeepaliveWorkBuddy(context.Background(), true, done)
	if more {
		t.Fatal("full fan-out must finish every pending account in one call")
	}
	if got := rec.snapshot(); len(got) != 5 {
		t.Fatalf("keepalive calls=%d want 5 ids=%v", len(got), got)
	}
	if len(done) != 5 {
		t.Fatalf("done map size=%d want 5", len(done))
	}

	more = manager.KeepaliveWorkBuddy(context.Background(), true, done)
	if more {
		t.Fatal("repeat with full done should report no more")
	}
	if got := rec.snapshot(); len(got) != 5 {
		t.Fatalf("repeat re-ran keepalive: %v", got)
	}
}
