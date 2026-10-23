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

func TestMaintenanceAccountGapForStableJitter(t *testing.T) {
	base := 2 * time.Second
	a := maintenanceAccountGapFor("acct-a", base)
	b := maintenanceAccountGapFor("acct-a", base)
	c := maintenanceAccountGapFor("acct-b", base)
	if a != b {
		t.Fatalf("gap not stable for same id: %v vs %v", a, b)
	}
	if a < base || a >= base+time.Duration(maintenanceGapJitterMS)*time.Millisecond {
		t.Fatalf("gap out of range: %v", a)
	}
	if c < base || c >= base+time.Duration(maintenanceGapJitterMS)*time.Millisecond {
		t.Fatalf("gap out of range for other id: %v", c)
	}
}

func TestSleepMaintenanceGapHonorsCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if sleepMaintenanceGap(ctx, "acct", time.Hour) {
		t.Fatal("canceled ctx should not sleep successfully")
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("canceled gap slept too long")
	}
}

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

func TestRunScheduledCheckinsCapsPerTick(t *testing.T) {
	prevCap, prevGap := maintenancePerTickCap, maintenanceAccountGap
	maintenancePerTickCap = 2
	maintenanceAccountGap = 0
	t.Cleanup(func() {
		maintenancePerTickCap = prevCap
		maintenanceAccountGap = prevGap
	})

	var mu sync.Mutex
	var calls []string
	manager, _ := newCheckinManager(t, checkinFunc(func(_ context.Context, accountID string) (providers.CheckinResult, error) {
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

	manager.runScheduledCheckins(context.Background(), dueAt)
	mu.Lock()
	first := append([]string(nil), calls...)
	mu.Unlock()
	if len(first) != 2 {
		t.Fatalf("first tick calls=%d want 2 ids=%v", len(first), first)
	}

	manager.runScheduledCheckins(context.Background(), dueAt)
	mu.Lock()
	second := append([]string(nil), calls...)
	mu.Unlock()
	if len(second) != 4 {
		t.Fatalf("second tick cumulative calls=%d want 4 ids=%v", len(second), second)
	}

	manager.runScheduledCheckins(context.Background(), dueAt)
	manager.runScheduledCheckins(context.Background(), dueAt)
	mu.Lock()
	total := len(calls)
	mu.Unlock()
	if total != 6 {
		t.Fatalf("after draining calls=%d want 6", total)
	}
	manager.runScheduledCheckins(context.Background(), dueAt)
	mu.Lock()
	after := len(calls)
	mu.Unlock()
	if after != 6 {
		t.Fatalf("already-checked accounts re-ran: calls=%d", after)
	}
}

func TestKeepaliveWorkBuddyCapsPerTickAndResumes(t *testing.T) {
	prevCap, prevGap := maintenancePerTickCap, maintenanceAccountGap
	maintenancePerTickCap = 2
	maintenanceAccountGap = 0
	t.Cleanup(func() {
		maintenancePerTickCap = prevCap
		maintenanceAccountGap = prevGap
	})

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
	if !more {
		t.Fatal("expected more accounts after first capped tick")
	}
	if got := rec.snapshot(); len(got) != 2 {
		t.Fatalf("first tick keepalive calls=%d want 2 ids=%v", len(got), got)
	}
	if len(done) != 2 {
		t.Fatalf("done map size=%d want 2", len(done))
	}

	more = manager.KeepaliveWorkBuddy(context.Background(), true, done)
	if !more {
		t.Fatal("expected more after second tick")
	}
	if got := rec.snapshot(); len(got) != 4 {
		t.Fatalf("second tick cumulative=%d want 4", len(got))
	}

	more = manager.KeepaliveWorkBuddy(context.Background(), true, done)
	if more {
		t.Fatal("expected no more after final tick")
	}
	if got := rec.snapshot(); len(got) != 5 {
		t.Fatalf("final cumulative=%d want 5", len(got))
	}

	more = manager.KeepaliveWorkBuddy(context.Background(), true, done)
	if more {
		t.Fatal("repeat with full done should report no more")
	}
	if got := rec.snapshot(); len(got) != 5 {
		t.Fatalf("repeat re-ran keepalive: %v", got)
	}
}
