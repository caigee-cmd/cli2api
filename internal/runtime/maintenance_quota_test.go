package runtime

import (
	"testing"
	"time"
)

func TestQuotaSnapshotStale(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		q     *QuotaSnapshot
		delay time.Duration
		want  bool
	}{
		{
			name:  "nil snapshot is not stale (caller skips)",
			q:     nil,
			delay: 5 * time.Minute,
			want:  false,
		},
		{
			name:  "empty FetchedAt is stale",
			q:     &QuotaSnapshot{Exceeded: true},
			delay: 5 * time.Minute,
			want:  true,
		},
		{
			name:  "fetched under the delay is fresh",
			q:     &QuotaSnapshot{Exceeded: true, FetchedAt: now.Add(-1 * time.Minute).Format(time.RFC3339)},
			delay: 5 * time.Minute,
			want:  false,
		},
		{
			name:  "fetched past the delay is stale",
			q:     &QuotaSnapshot{Exceeded: true, FetchedAt: now.Add(-(5*time.Minute + time.Minute)).Format(time.RFC3339)},
			delay: 5 * time.Minute,
			want:  true,
		},
		{
			name:  "unparseable FetchedAt is stale",
			q:     &QuotaSnapshot{Exceeded: true, FetchedAt: "yesterday"},
			delay: 5 * time.Minute,
			want:  true,
		},
		{
			name:  "staleness ignores the Exceeded flag (caller gates on it)",
			q:     &QuotaSnapshot{Exceeded: false, FetchedAt: now.Add(-2 * time.Hour).Format(time.RFC3339)},
			delay: 5 * time.Minute,
			want:  true,
		},
		{
			name:  "a long delay holds a two-hour-old snapshot as fresh",
			q:     &QuotaSnapshot{Exceeded: true, FetchedAt: now.Add(-2 * time.Hour).Format(time.RFC3339)},
			delay: 3 * time.Hour,
			want:  false,
		},
		{
			name:  "a long delay still expires an old snapshot",
			q:     &QuotaSnapshot{Exceeded: true, FetchedAt: now.Add(-7 * time.Hour).Format(time.RFC3339)},
			delay: 6 * time.Hour,
			want:  true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := quotaSnapshotStale(tc.q, now, tc.delay); got != tc.want {
				t.Fatalf("quotaSnapshotStale = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestQuotaProbeDelay(t *testing.T) {
	tests := []struct {
		level int
		want  time.Duration
	}{
		{level: -1, want: 5 * time.Minute},
		{level: 0, want: 5 * time.Minute},
		{level: 2, want: 5 * time.Minute},
		{level: 3, want: 15 * time.Minute},
		{level: 4, want: 30 * time.Minute},
		{level: 5, want: time.Hour},
		{level: 6, want: 3 * time.Hour},
		{level: 7, want: 6 * time.Hour},
		{level: 8, want: 12 * time.Hour},
		{level: 9, want: 24 * time.Hour},
		{level: 100, want: 24 * time.Hour},
	}
	for _, tc := range tests {
		if got := quotaProbeDelay(tc.level); got != tc.want {
			t.Fatalf("quotaProbeDelay(%d) = %v, want %v", tc.level, got, tc.want)
		}
	}
}

func TestQuotaProbeDelayIsMonotonic(t *testing.T) {
	// The ladder must never shrink: an account that keeps failing should not
	// suddenly be probed more often.
	previous := time.Duration(0)
	for level := 0; level < 12; level++ {
		delay := quotaProbeDelay(level)
		if delay < previous {
			t.Fatalf("quotaProbeDelay(%d) = %v shrank below previous %v", level, delay, previous)
		}
		previous = delay
	}
}

func TestQuotaProbeLevelLifecycle(t *testing.T) {
	manager := &Manager{}
	if got := manager.quotaProbeLevel("acc"); got != 0 {
		t.Fatalf("initial level = %d, want 0", got)
	}
	manager.bumpQuotaProbeLevel("acc")
	manager.bumpQuotaProbeLevel("acc")
	if got := manager.quotaProbeLevel("acc"); got != 2 {
		t.Fatalf("after two bumps level = %d, want 2", got)
	}
	manager.resetQuotaProbeLevel("acc")
	if got := manager.quotaProbeLevel("acc"); got != 0 {
		t.Fatalf("after reset level = %d, want 0", got)
	}
	// prune drops accounts no longer seen and keeps the ones still in the pool.
	manager.bumpQuotaProbeLevel("kept")
	manager.bumpQuotaProbeLevel("gone")
	manager.pruneQuotaProbeLevels(map[string]struct{}{"kept": {}})
	if _, ok := manager.quotaProbeLevels["gone"]; ok {
		t.Fatalf("pruned account still present")
	}
	if got := manager.quotaProbeLevel("kept"); got != 1 {
		t.Fatalf("kept account level = %d, want 1", got)
	}
}
