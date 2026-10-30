package console

import (
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/executor"
)

func TestBuildLiveSnapshot(t *testing.T) {
	// AccountView.InFlight is the pool's current counter (Acquire/Release),
	// not a probe snapshot; /api/live just sums those live values.
	now := time.Unix(1_700_000_000, 0).UTC()
	coolingUntil := now.Add(90 * time.Second).Format(time.RFC3339)
	modelUntil := now.Add(45 * time.Second).Format(time.RFC3339)
	expiredUntil := now.Add(-time.Minute).Format(time.RFC3339)

	views := []accounts.AccountView{
		{
			Account:        accounts.Account{ID: "a", Name: "wb-a1", Provider: "workbuddy", Enabled: true, MaxInFlight: 4},
			InFlight:       4,
			InFlightSince:  now.Add(-12 * time.Second).Format(time.RFC3339),
		},
		{
			Account:         accounts.Account{ID: "b", Name: "wb-a2", Provider: "workbuddy", Enabled: true},
			InFlight:        1, // default max 4
			DownUntil:       coolingUntil,
			ModelCooldowns:  map[string]string{"glm-5": modelUntil, "stale": expiredUntil},
		},
		{Account: accounts.Account{ID: "c", Enabled: false, MaxInFlight: 8}},
	}
	sticky := executor.SessionAffinityStats{Bindings: 3, Hits: 12, Misses: 2, Capacity: 64, TTLSeconds: 600}

	got := buildLiveSnapshot(views, 17, sticky, now)
	if got.InFlight != 5 || got.Capacity != 8 || got.RPM != 17 {
		t.Fatalf("snapshot = %+v", got)
	}
	if len(got.Accounts) != 3 || got.Accounts[1].MaxInFlight != accounts.DefaultMaxInFlight || got.Accounts[2].Enabled {
		t.Fatalf("accounts = %+v", got.Accounts)
	}
	if got.Sticky.Bindings != 3 || got.Sticky.Hits != 12 {
		t.Fatalf("sticky = %+v", got.Sticky)
	}
	if len(got.Workers) != 3 {
		t.Fatalf("workers len = %d", len(got.Workers))
	}
	w0 := got.Workers[0]
	if !w0.Busy || w0.RateLimited || w0.Progress != 1 || w0.Name != "wb-a1" || w0.Provider != "workbuddy" {
		t.Fatalf("worker[0] = %+v", w0)
	}
	if w0.StartedAt == "" || w0.StartedAt != now.Add(-12*time.Second).Format(time.RFC3339) {
		t.Fatalf("worker[0].StartedAt = %q", w0.StartedAt)
	}
	if got.Workers[2].StartedAt != "" {
		t.Fatalf("idle worker must omit started_at, got %q", got.Workers[2].StartedAt)
	}
	w1 := got.Workers[1]
	if !w1.Busy || !w1.RateLimited || w1.Progress != 0.25 {
		t.Fatalf("worker[1] = %+v", w1)
	}
	w2 := got.Workers[2]
	if w2.Busy || w2.RateLimited || w2.Name != "c" {
		t.Fatalf("worker[2] = %+v", w2)
	}

	// Active: account-wide b + model glm-5. Expired stale model is dropped.
	if len(got.RateLimits) != 2 {
		t.Fatalf("rate_limits = %+v", got.RateLimits)
	}
	// Sorted by remaining_ms desc: account 90s, then model 45s.
	if got.RateLimits[0].AccountID != "b" || got.RateLimits[0].Model != "" || got.RateLimits[0].RemainingMs != 90_000 {
		t.Fatalf("rate_limits[0] = %+v", got.RateLimits[0])
	}
	if got.RateLimits[1].Model != "glm-5" || got.RateLimits[1].RemainingMs != 45_000 {
		t.Fatalf("rate_limits[1] = %+v", got.RateLimits[1])
	}
	if got.RateLimits[0].Label != "wb-a2" || got.RateLimits[1].Label != "wb-a2 · glm-5" {
		t.Fatalf("labels = %q / %q", got.RateLimits[0].Label, got.RateLimits[1].Label)
	}
	if got.Queue == nil {
		t.Fatalf("queue must be non-nil empty slice")
	}
	if len(got.Queue) != 0 {
		t.Fatalf("queue = %+v", got.Queue)
	}
	if got.Occupancy == nil {
		t.Fatalf("occupancy must be non-nil empty slice")
	}
	if len(got.Occupancy) != 0 {
		t.Fatalf("occupancy = %+v", got.Occupancy)
	}
}

func TestBuildLiveSnapshotEmpty(t *testing.T) {
	got := buildLiveSnapshot(nil, 0, executor.SessionAffinityStats{}, time.Unix(0, 0).UTC())
	if got.InFlight != 0 || got.Capacity != 0 || got.Workers == nil || got.RateLimits == nil || got.Accounts == nil {
		t.Fatalf("empty snapshot should use non-nil slices: %+v", got)
	}
	if len(got.Workers) != 0 || len(got.RateLimits) != 0 || len(got.Accounts) != 0 {
		t.Fatalf("empty lengths: %+v", got)
	}
}

func TestBuildLiveSnapshotLastErrorKind(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	views := []accounts.AccountView{
		{
			Account: accounts.Account{
				ID: "err1", Name: "fail-slot", Provider: "qoder", Enabled: true,
				LastErrorKind: accounts.KindUnavailable,
			},
			InFlight: 0,
		},
		{
			Account: accounts.Account{
				ID: "ok1", Name: "ok-slot", Provider: "qoder", Enabled: true,
			},
			InFlight: 1,
			InFlightSince: now.Add(-2 * time.Second).Format(time.RFC3339),
		},
	}
	got := buildLiveSnapshot(views, 0, executor.SessionAffinityStats{}, now)
	if got.Workers[0].LastErrorKind != accounts.KindUnavailable {
		t.Fatalf("worker[0].LastErrorKind = %q", got.Workers[0].LastErrorKind)
	}
	if got.Workers[1].LastErrorKind != "" {
		t.Fatalf("busy-ok worker must omit last_error_kind, got %q", got.Workers[1].LastErrorKind)
	}
}
