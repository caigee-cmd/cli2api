package accounts

import "testing"

func TestEarliestCheckinClockIsRegionAware(t *testing.T) {
	tests := []struct {
		provider string
		region   string
		want     string
	}{
		{"qoder", "global", "10:00"},
		{"qoder", "cn", "00:00"},
		{"workbuddy", "cn", "00:00"},
		{"trae", "cn", "00:00"},
		{"qwenwork", "global", "00:00"},
		{"qwenwork", "cn", "00:00"},
		{"codearts", "cn-north-4", "00:00"},
		{"doubaowork", "cn", "00:00"},
		{"minimaxcode", "cn", "00:00"},
		{"unknown-provider", "cn", "00:00"},
	}
	for _, test := range tests {
		if got := EarliestCheckinClockFor(test.provider, test.region); got != test.want {
			t.Errorf("EarliestCheckinClockFor(%s,%s)=%q want %q", test.provider, test.region, got, test.want)
		}
	}
}

func TestDefaultCheckinAutoTimeUsesRegionWindow(t *testing.T) {
	tests := []struct {
		provider string
		region   string
		want     string
	}{
		{"qoder", "global", "11:00"},
		{"qoder", "cn", "01:00"},
		{"trae", "cn", "01:00"},
	}
	for _, test := range tests {
		if got := DefaultCheckinAutoTimeFor(test.provider, test.region); got != test.want {
			t.Errorf("DefaultCheckinAutoTimeFor(%s,%s)=%q want %q", test.provider, test.region, got, test.want)
		}
	}
}

func TestEarliestCheckinClockDefaultsToProviderDefaultRegion(t *testing.T) {
	if got := EarliestCheckinClock("qoder"); got != "10:00" {
		t.Fatalf("qoder default region clock=%q want 10:00", got)
	}
	if got := DefaultCheckinAutoTime("qoder"); got != "11:00" {
		t.Fatalf("qoder default region auto=%q want 11:00", got)
	}
}

func TestAutoCheckinTimeFromEarliestClampsLateWindows(t *testing.T) {
	tests := map[string]string{
		"00:00": "01:00",
		"10:00": "11:00",
		"23:00": "23:59",
		"23:59": "23:59",
		"":      "",
		"bad":   "",
	}
	for earliest, want := range tests {
		if got := AutoCheckinTimeFromEarliest(earliest); got != want {
			t.Errorf("AutoCheckinTimeFromEarliest(%q)=%q want %q", earliest, got, want)
		}
	}
}
