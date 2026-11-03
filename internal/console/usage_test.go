package console

import "testing"

func TestExportWindowHoursFollowsRetention(t *testing.T) {
	t.Setenv("CLI2API_LOG_RETENTION_DAYS", "")
	if got := exportWindowHours(); got != 24*7 {
		t.Fatalf("default export window = %dh, want %dh", got, 24*7)
	}
	t.Setenv("CLI2API_LOG_RETENTION_DAYS", "90")
	if got := exportWindowHours(); got != 24*90 {
		t.Fatalf("export window with 90d retention = %dh, want %dh", got, 24*90)
	}
	t.Setenv("CLI2API_LOG_RETENTION_DAYS", "not-a-number")
	if got := exportWindowHours(); got != 24*7 {
		t.Fatalf("export window with invalid retention = %dh, want default %dh", got, 24*7)
	}
}
