package logs

import (
	"testing"
)

func TestRetentionDaysDefaultsAndOverride(t *testing.T) {
	t.Setenv(retentionDaysEnv, "")
	if got := RetentionDays(); got != defaultRetentionDays {
		t.Fatalf("default retention = %d, want %d", got, defaultRetentionDays)
	}

	t.Setenv(retentionDaysEnv, "90")
	if got := RetentionDays(); got != 90 {
		t.Fatalf("override retention = %d, want 90", got)
	}

	// Invalid values must not silently disable pruning.
	for _, bad := range []string{"0", "-3", "abc", "  "} {
		t.Setenv(retentionDaysEnv, bad)
		if got := RetentionDays(); got != defaultRetentionDays {
			t.Fatalf("retention for %q = %d, want default %d", bad, got, defaultRetentionDays)
		}
	}
}

func TestMaxLogRowsDefaultsAndOverride(t *testing.T) {
	t.Setenv(maxLogRowsEnv, "")
	if got := MaxLogRows(); got != defaultMaxLogRows {
		t.Fatalf("default max rows = %d, want %d", got, defaultMaxLogRows)
	}

	t.Setenv(maxLogRowsEnv, "200000")
	if got := MaxLogRows(); got != 200000 {
		t.Fatalf("override max rows = %d, want 200000", got)
	}

	for _, bad := range []string{"0", "-1", "lots"} {
		t.Setenv(maxLogRowsEnv, bad)
		if got := MaxLogRows(); got != defaultMaxLogRows {
			t.Fatalf("max rows for %q = %d, want default %d", bad, got, defaultMaxLogRows)
		}
	}
}

func TestTotalsRetentionDaysDefaultsAndOverride(t *testing.T) {
	t.Setenv(retentionDaysEnv, "")
	t.Setenv(totalsRetentionDaysEnv, "")
	if got := TotalsRetentionDays(); got != defaultTotalsRetentionDays {
		t.Fatalf("default totals retention = %d, want %d", got, defaultTotalsRetentionDays)
	}

	t.Setenv(totalsRetentionDaysEnv, "365")
	if got := TotalsRetentionDays(); got != 365 {
		t.Fatalf("override totals retention = %d, want 365", got)
	}

	// The totals window can never be shorter than the detail window: deleting
	// totals before their detail would lose data the detail view still shows.
	t.Setenv(retentionDaysEnv, "90")
	t.Setenv(totalsRetentionDaysEnv, "30")
	if got := TotalsRetentionDays(); got != 90 {
		t.Fatalf("totals retention below detail = %d, want detail window 90", got)
	}

	for _, bad := range []string{"0", "-5", "abc"} {
		t.Setenv(retentionDaysEnv, "")
		t.Setenv(totalsRetentionDaysEnv, bad)
		if got := TotalsRetentionDays(); got != defaultTotalsRetentionDays {
			t.Fatalf("totals retention for %q = %d, want default %d", bad, got, defaultTotalsRetentionDays)
		}
	}
}
