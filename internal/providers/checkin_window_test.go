package providers

import (
	"testing"
	"time"
)

func mustParse(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestCheckinClockFromTextUsesProviderCalendar(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{"naive wall clock", "2026-11-24 00:00:00", "00:00"},
		{"utc midnight is beijing morning", "2026-11-23T16:00:00Z", "00:00"},
		{"explicit beijing midnight", "2026-11-24T00:00:00+08:00", "00:00"},
		{"utc ten is beijing eighteen", "2026-11-24T10:00:00Z", "18:00"},
		{"minute precision", "2026-11-24 09:59", "09:59"},
		{"empty", "", ""},
		{"garbage", "not-a-time", ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := CheckinClockFromText(test.value); got != test.want {
				t.Fatalf("CheckinClockFromText(%q)=%q want %q", test.value, got, test.want)
			}
		})
	}
}

func TestCheckinAnswerProvisionalAtDayRollover(t *testing.T) {
	if !CheckinAnswerProvisional(mustParse(t, "2026-11-23T17:59:59Z")) { // 01:59 Beijing
		t.Fatal("01:59 Beijing must still be provisional")
	}
	if CheckinAnswerProvisional(mustParse(t, "2026-11-23T18:00:00Z")) { // 02:00 Beijing
		t.Fatal("02:00 Beijing must be settled")
	}
}

func TestCheckinClaimProvisionalOnlyAroundMidnight(t *testing.T) {
	if !CheckinClaimProvisional(mustParse(t, "2026-11-23T16:30:00Z")) { // 00:30 Beijing
		t.Fatal("00:30 claim answer must be provisional")
	}
	if !CheckinClaimProvisional(mustParse(t, "2026-11-23T17:59:59Z")) { // 01:59 Beijing
		t.Fatal("01:59 claim answer must be provisional")
	}
	if CheckinClaimProvisional(mustParse(t, "2026-11-23T18:00:00Z")) { // 02:00 Beijing
		t.Fatal("02:00 claim answer must be settled")
	}
}
