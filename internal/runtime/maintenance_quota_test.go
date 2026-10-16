package runtime

import (
	"testing"
	"time"
)

func TestQuotaSnapshotStale(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		q    *QuotaSnapshot
		want bool
	}{
		{
			name: "nil snapshot is not stale (caller skips)",
			q:    nil,
			want: false,
		},
		{
			name: "empty FetchedAt is stale",
			q:    &QuotaSnapshot{Exceeded: true},
			want: true,
		},
		{
			name: "fetched under the threshold is fresh",
			q:    &QuotaSnapshot{Exceeded: true, FetchedAt: now.Add(-1 * time.Minute).Format(time.RFC3339)},
			want: false,
		},
		{
			name: "fetched past the threshold is stale",
			q:    &QuotaSnapshot{Exceeded: true, FetchedAt: now.Add(-(staleQuotaRefreshAfter + time.Minute)).Format(time.RFC3339)},
			want: true,
		},
		{
			name: "unparseable FetchedAt is stale",
			q:    &QuotaSnapshot{Exceeded: true, FetchedAt: "yesterday"},
			want: true,
		},
		{
			name: "staleness ignores the Exceeded flag (caller gates on it)",
			q:    &QuotaSnapshot{Exceeded: false, FetchedAt: now.Add(-2 * time.Hour).Format(time.RFC3339)},
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := quotaSnapshotStale(tc.q, now); got != tc.want {
				t.Fatalf("quotaSnapshotStale = %v, want %v", got, tc.want)
			}
		})
	}
}
