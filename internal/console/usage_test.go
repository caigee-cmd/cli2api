package console

import "testing"

func TestExportWindowHoursFollowsTotalsRetention(t *testing.T) {
	t.Setenv("CLI2API_LOG_RETENTION_DAYS", "")
	t.Setenv("CLI2API_LOG_TOTALS_RETENTION_DAYS", "")
	if got := exportWindowHours(); got != 24*730 {
		t.Fatalf("default export window = %dh, want %dh", got, 24*730)
	}
	// A raised detail window raises the totals window with it.
	t.Setenv("CLI2API_LOG_RETENTION_DAYS", "90")
	if got := exportWindowHours(); got != 24*730 {
		t.Fatalf("export window with 90d detail retention = %dh, want 730d", got)
	}
	t.Setenv("CLI2API_LOG_RETENTION_DAYS", "")
	t.Setenv("CLI2API_LOG_TOTALS_RETENTION_DAYS", "365")
	if got := exportWindowHours(); got != 24*365 {
		t.Fatalf("export window with 365d totals retention = %dh, want %dh", got, 24*365)
	}
	t.Setenv("CLI2API_LOG_TOTALS_RETENTION_DAYS", "not-a-number")
	if got := exportWindowHours(); got != 24*730 {
		t.Fatalf("export window with invalid totals retention = %dh, want default %dh", got, 24*730)
	}
}
