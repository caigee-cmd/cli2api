package codex

import (
	"net/http"
	"testing"
)

func TestQuotaFromUsageReadsCurrentWindows(t *testing.T) {
	body := []byte(`{
		"plan_type":"plus",
		"rate_limit":{
			"allowed":true,
			"limit_reached":false,
			"primary_window":{"used_percent":0,"limit_window_seconds":18000,"reset_after_seconds":10800,"reset_at":1893456000},
			"secondary_window":{"used_percent":7,"limit_window_seconds":604800,"reset_after_seconds":432000,"reset_at":1894000000}
		}
	}`)
	info := quotaFromUsage(body)
	if info == nil || len(info.Windows) != 2 {
		t.Fatalf("windows = %+v", info)
	}
	if info.Windows[0].ID != "fiveHour" || info.Windows[0].Percentage != 0 || info.Windows[0].ResetAt == "" {
		t.Fatalf("primary = %+v", info.Windows[0])
	}
	if info.Windows[1].ID != "weeklyLimit" || info.Windows[1].Percentage != 7 || info.Percentage != 7 {
		t.Fatalf("secondary = %+v info=%+v", info.Windows[1], info)
	}
	if info.Plan != "plus" {
		t.Fatalf("plan = %q", info.Plan)
	}
}

func TestQuotaFromUsageReadsRateLimitWindows(t *testing.T) {
	body := []byte(`{
		"plan_type":"plus",
		"rate_limits":{
			"primary":{"used_percent":12.5,"window_minutes":300,"reset_after_seconds":600},
			"secondary":{"used_percent":40,"window_minutes":10080,"reset_at":1893456000}
		}
	}`)
	info := quotaFromUsage(body)
	if info == nil || len(info.Windows) != 2 {
		t.Fatalf("windows = %+v", info)
	}
	if info.Windows[0].ID != "fiveHour" || info.Windows[0].Percentage != 12.5 || info.Windows[0].ResetAt == "" {
		t.Fatalf("primary = %+v", info.Windows[0])
	}
	if info.Windows[1].ID != "weeklyLimit" || info.Windows[1].Percentage != 40 || info.Percentage != 40 {
		t.Fatalf("secondary = %+v info=%+v", info.Windows[1], info)
	}
}

func TestQuotaFromUsageRejectsIncompleteWindows(t *testing.T) {
	if info := quotaFromUsage([]byte(`{"rate_limits":{"primary":{"used_percent":0}}}`)); info != nil {
		t.Fatalf("incomplete window accepted: %+v", info)
	}
	if info := quotaFromUsage([]byte(`not json`)); info != nil {
		t.Fatal("invalid json accepted")
	}
}

func TestQuotaFromHeadersKeepsZeroUsedWindow(t *testing.T) {
	header := http.Header{}
	header.Set("X-Codex-Primary-Used-Percent", "0")
	header.Set("X-Codex-Primary-Window-Minutes", "300")
	header.Set("X-Codex-Primary-Reset-After-Seconds", "1000")
	info := quotaFromHeaders(header)
	if info == nil || len(info.Windows) != 1 || info.Windows[0].ID != "fiveHour" || info.Windows[0].Percentage != 0 {
		t.Fatalf("zero usage dropped: %+v", info)
	}
}
