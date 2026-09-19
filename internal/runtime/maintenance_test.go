package runtime

import (
	"testing"
	"time"
)

func TestCheckinSchedule(t *testing.T) {
	location := time.FixedZone("CST", 8*3600)
	base := time.Date(2026, 9, 19, 8, 0, 0, 0, location)
	account := Account{ID: "account", Enabled: true, AutoCheckin: true}
	if checkinDue(account, "09:00", base) {
		t.Fatal("ran before configured time")
	}
	if !checkinDue(account, "09:00", base.Add(2*time.Hour)) {
		t.Fatal("did not catch up after scheduled time")
	}
	account.LastCheckinAt = base.Add(2 * time.Hour).Format(time.RFC3339)
	for _, status := range []string{"success", "already", "skipped"} {
		account.LastCheckinStatus = status
		if checkinDue(account, "09:00", base.Add(14*time.Hour)) {
			t.Fatalf("repeated terminal status %s", status)
		}
	}
	account.LastCheckinStatus = "error"
	if checkinDue(account, "09:00", base.Add(3*time.Hour)) {
		t.Fatal("retried a failure before evening slot")
	}
	if !checkinDue(account, "09:00", base.Add(14*time.Hour)) {
		t.Fatal("did not retry morning failure")
	}
	account.LastCheckinAt = base.Add(14 * time.Hour).Format(time.RFC3339)
	if checkinDue(account, "09:00", base.Add(15*time.Hour)) {
		t.Fatal("retried evening failure repeatedly")
	}
	if !checkinDue(account, "09:00", base.Add(26*time.Hour)) {
		t.Fatal("previous day suppressed next day")
	}
}

func TestCheckinScheduleSameTimeAndDisabled(t *testing.T) {
	now := time.Date(2026, 9, 19, 21, 30, 0, 0, time.UTC)
	account := Account{ID: "account", Enabled: true, AutoCheckin: true, LastCheckinAt: now.Add(-10 * time.Minute).Format(time.RFC3339), LastCheckinStatus: "error"}
	if checkinDue(account, "21:00", now) {
		t.Fatal("scheduled time and retry slot must not run twice")
	}
	account.LastCheckinAt = ""
	account.AutoCheckin = false
	if checkinDue(account, "09:00", now) {
		t.Fatal("auto-checkin is opt-in")
	}
	account.AutoCheckin = true
	account.Enabled = false
	if checkinDue(account, "09:00", now) {
		t.Fatal("disabled account was scheduled")
	}
}

func TestCheckinDayUsesProviderTimezone(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 20, 0, 1, 0, 0, location)
	if CheckedInLocalDay("2026-09-19T15:59:00Z", "success", now) {
		t.Fatal("yesterday in China was treated as today")
	}
	if !CheckedInLocalDay("2026-09-19T16:00:00Z", "already", now) {
		t.Fatal("today in China was missed")
	}
}
