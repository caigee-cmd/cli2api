package accounts

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestLookupModelPriceNormalizesIDs(t *testing.T) {
	cases := map[string]string{
		"deepseek-v4.1-flash":           "deepseek-v4.1-flash",
		"DeepSeek-V4.1-Flash":           "deepseek-v4.1-flash",
		"workbuddy/deepseek-v4.1-flash": "deepseek-v4.1-flash",
		"deepseek-v4.1-flash-sg":        "deepseek-v4.1-flash",
		"deepseek-v4-flash":             "deepseek-v4-flash",
		"deepseek-v4-pro":               "deepseek-v4-pro",
	}
	for in, want := range cases {
		price, ok := LookupModelPrice(in)
		if !ok {
			t.Fatalf("LookupModelPrice(%q) not found", in)
		}
		if price.ID != want {
			t.Fatalf("LookupModelPrice(%q).ID = %q, want %q", in, price.ID, want)
		}
	}
	if _, ok := LookupModelPrice("glm-5.3-flash"); ok {
		t.Fatal("glm-5.3-flash must not be priced as DeepSeek")
	}
	if _, ok := LookupModelPrice(""); ok {
		t.Fatal("empty id must not resolve")
	}
}

func TestPeakHourBeijingWindow(t *testing.T) {
	price, _ := LookupModelPrice("deepseek-v4.1-flash")
	// Beijing is UTC+8. Monday 2026-10-12.
	mk := func(beijingHour, minute int) time.Time {
		return time.Date(2026, 10, 12, beijingHour, minute, 0, 0, chinaLocation).UTC()
	}
	peaks := []time.Time{mk(9, 0), mk(11, 59), mk(14, 0), mk(17, 59)}
	for _, at := range peaks {
		if !price.isPeakHour(at) {
			t.Fatalf("%s should be peak", at.In(chinaLocation).Format("Mon 15:04"))
		}
	}
	offpeak := []time.Time{mk(8, 59), mk(12, 0), mk(13, 59), mk(18, 0), mk(3, 0)}
	for _, at := range offpeak {
		if price.isPeakHour(at) {
			t.Fatalf("%s should be off-peak", at.In(chinaLocation).Format("Mon 15:04"))
		}
	}
	// Weekend day is off-peak regardless of hour.
	sat := time.Date(2026, 10, 10, 10, 0, 0, 0, chinaLocation).UTC()
	if price.isPeakHour(sat) {
		t.Fatal("Saturday 10:00 Beijing must be off-peak")
	}
}

func TestModelPriceCost(t *testing.T) {
	price, _ := LookupModelPrice("deepseek-v4.1-flash")
	usage := TokenUsage{CacheHit: 1_000_000, CacheMiss: 1_000_000, Output: 1_000_000}
	// Off-peak: 0.02 + 1 + 4 = 5.02
	if got := price.Cost(usage, false); math.Abs(got-5.02) > 1e-9 {
		t.Fatalf("off-peak cost = %v, want 5.02", got)
	}
	// Peak is exactly double.
	if got := price.Cost(usage, true); math.Abs(got-10.04) > 1e-9 {
		t.Fatalf("peak cost = %v, want 10.04", got)
	}
}

func TestBuildCostBreakdownSplitsPeakAndOffPeak(t *testing.T) {
	// 2026-10-12 02:00 UTC = 10:00 Beijing (peak). 2026-10-12 20:00 UTC = 04:00
	// Beijing next day (off-peak) — both same UTC day so the daily series merges.
	peakHour := time.Date(2026, 10, 12, 2, 0, 0, 0, time.UTC)
	offHour := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	rows := []CostUsageRow{
		{Model: "deepseek-v4.1-flash", HourStart: peakHour, Requests: 3, Usage: TokenUsage{CacheHit: 1_000_000, Output: 1_000_000}},
		{Model: "workbuddy/deepseek-v4.1-flash", HourStart: offHour, Requests: 2, Usage: TokenUsage{CacheMiss: 1_000_000, Output: 1_000_000}},
		{Model: "glm-5.3-flash", HourStart: peakHour, Requests: 9, Usage: TokenUsage{Output: 5_000_000}}, // unpriced
	}
	from := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	got := BuildCostBreakdown(rows, from, to, true)

	// Every model that carried usage is reported; glm-5.3-flash has no list
	// price, so it appears with Priced=false and zero cost while still
	// contributing its tokens.
	if len(got.Models) != 2 {
		t.Fatalf("models = %d, want 2 (1 priced + 1 unpriced)", len(got.Models))
	}
	var m ModelCost
	for _, candidate := range got.Models {
		if candidate.Priced {
			m = candidate
		} else if !strings.Contains(candidate.ID, "glm") {
			t.Fatalf("unpriced model = %q, want glm-5.3-flash", candidate.ID)
		}
	}
	if m.Requests != 5 {
		t.Fatalf("requests = %d, want 5", m.Requests)
	}
	// Peak hour: 1M hit*0.04 + 1M out*8 = 8.04
	// Off hour:  1M miss*1 + 1M out*4 = 5.0
	wantPeak, wantOff := 8.04, 5.0
	if math.Abs(m.CostPeak-wantPeak) > 1e-9 || math.Abs(m.CostOffPeak-wantOff) > 1e-9 {
		t.Fatalf("peak/off = %v/%v, want %v/%v", m.CostPeak, m.CostOffPeak, wantPeak, wantOff)
	}
	if math.Abs(m.Cost-(wantPeak+wantOff)) > 1e-9 {
		t.Fatalf("total cost = %v, want %v", m.Cost, wantPeak+wantOff)
	}
	if m.PeakRequests != 3 {
		t.Fatalf("peak requests = %d, want 3", m.PeakRequests)
	}
	if len(got.Unpriced) != 1 || got.Unpriced[0] != "glm-5.3-flash" {
		t.Fatalf("unpriced = %v, want [glm-5.3-flash]", got.Unpriced)
	}
	// Both hours fall on the same UTC day, so the daily series has one point.
	if len(got.Series) != 1 {
		t.Fatalf("series points = %d, want 1", len(got.Series))
	}
	if math.Abs(got.Series[0].PerModel["deepseek-v4.1-flash"]-(wantPeak+wantOff)) > 1e-9 {
		t.Fatalf("series bucket = %v, want %v", got.Series[0].PerModel["deepseek-v4.1-flash"], wantPeak+wantOff)
	}
}

func TestBuildCostBreakdownReportsCreditsForUnpricedModels(t *testing.T) {
	// Subscription accounts consume quota even for models with no official list
	// price, so those rows must still surface tokens and credits with zero cost.
	peakHour := time.Date(2026, 10, 12, 2, 0, 0, 0, time.UTC)
	rows := []CostUsageRow{
		{Model: "deepseek-v4.1-flash", HourStart: peakHour, Requests: 2,
			Usage: TokenUsage{CacheHit: 1_000_000, Output: 1_000_000}, Credits: 1.5},
		{Model: "glm-5.3-flash", HourStart: peakHour, Requests: 4,
			Usage: TokenUsage{CacheMiss: 2_000_000, Output: 3_000_000}, Credits: 7.25},
	}
	from := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	got := BuildCostBreakdown(rows, from, to, true)

	var priced, unpriced *ModelCost
	for i := range got.Models {
		if got.Models[i].Priced {
			priced = &got.Models[i]
		} else {
			unpriced = &got.Models[i]
		}
	}
	if priced == nil || unpriced == nil {
		t.Fatalf("models = %d, want one priced and one unpriced", len(got.Models))
	}
	if unpriced.Credits != 7.25 {
		t.Fatalf("unpriced credits = %v, want 7.25", unpriced.Credits)
	}
	if unpriced.TotalTokens != 5_000_000 {
		t.Fatalf("unpriced tokens = %d, want 5000000", unpriced.TotalTokens)
	}
	if unpriced.Cost != 0 {
		t.Fatalf("unpriced cost = %v, want 0", unpriced.Cost)
	}
	if priced.Credits != 1.5 {
		t.Fatalf("priced credits = %v, want 1.5", priced.Credits)
	}
	// Totals keep both kinds of cost unit so the console can show quota consumed
	// across every provider next to the DeepSeek-equivalent money.
	if got.Totals.Credits != 8.75 {
		t.Fatalf("total credits = %v, want 8.75", got.Totals.Credits)
	}
	if got.Totals.Requests != 6 {
		t.Fatalf("total requests = %d, want 6", got.Totals.Requests)
	}
	if len(got.Series) != 1 || got.Series[0].Credits != 8.75 {
		t.Fatalf("series credits = %+v, want one point with 8.75", got.Series)
	}
	// Per-model credit series must include unpriced models so the primary
	// chart is not DeepSeek-only.
	if got.Series[0].PerModelCredits["glm-5.3-flash"] != 7.25 {
		t.Fatalf("per_model_credits glm = %v, want 7.25", got.Series[0].PerModelCredits["glm-5.3-flash"])
	}
	if got.Series[0].PerModelCredits["deepseek-v4.1-flash"] != 1.5 {
		t.Fatalf("per_model_credits deepseek = %v, want 1.5", got.Series[0].PerModelCredits["deepseek-v4.1-flash"])
	}
	if got.Series[0].PerModelTokens["glm-5.3-flash"] != 5_000_000 {
		t.Fatalf("per_model_tokens glm = %v, want 5000000", got.Series[0].PerModelTokens["glm-5.3-flash"])
	}
	// Models with higher quota should sort ahead of CNY-only ordering.
	if got.Models[0].ID != "glm-5.3-flash" {
		t.Fatalf("first model = %q, want glm-5.3-flash (highest credits)", got.Models[0].ID)
	}
}
