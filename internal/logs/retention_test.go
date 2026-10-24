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
