package codex

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// roundTripFunc serves Quota's probe request from a test closure without a
// network round-trip; the client keeps its real base URLs, so the test also
// pins the endpoint the probe calls.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func quotaProbeClient(t *testing.T, transport roundTripFunc) *Client {
	t.Helper()
	client := NewClient(testStore{items: map[string][]byte{"a1": credentialPayload()}})
	client.http.Transport = transport
	return client
}

func TestQuotaProbesBackendApiUsage(t *testing.T) {
	var gotURL, gotAuth, gotOriginator string
	client := quotaProbeClient(t, func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		gotAuth = r.Header.Get("Authorization")
		gotOriginator = r.Header.Get("Originator")
		body := `{"plan_type":"plus","rate_limit":{
			"primary_window":{"used_percent":0,"limit_window_seconds":18000,"reset_after_seconds":900},
			"secondary_window":{"used_percent":54,"limit_window_seconds":604800,"reset_after_seconds":2263}}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})
	info, err := client.Quota(context.Background(), "a1")
	if err != nil {
		t.Fatalf("quota: %v", err)
	}
	// The probe lives on the backend-api root, not under the /codex chat
	// prefix: the /codex path answers with the Cloudflare 403 interstitial.
	if gotURL != "https://chatgpt.com/backend-api/wham/usage" {
		t.Fatalf("probe url = %q", gotURL)
	}
	if strings.Contains(gotURL, "/codex/") {
		t.Fatalf("probe url must not use the chat prefix: %q", gotURL)
	}
	if gotAuth != "Bearer at" || gotOriginator != Originator {
		t.Fatalf("probe headers auth=%q originator=%q", gotAuth, gotOriginator)
	}
	if info == nil || len(info.Windows) != 2 || info.Percentage != 54 || info.Exceeded {
		t.Fatalf("quota info = %+v", info)
	}
}

func TestQuotaReportsProbeFailureWithoutCachedWindows(t *testing.T) {
	client := quotaProbeClient(t, func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader("blocked")), Header: http.Header{}}, nil
	})
	info, err := client.Quota(context.Background(), "a1")
	if info != nil || err == nil {
		t.Fatalf("expected probe failure, got info=%+v err=%v", info, err)
	}
	if !strings.Contains(err.Error(), "403") {
		t.Fatalf("error = %v", err)
	}
}

func TestQuotaProbeFailureSurfacesErrorNotCachedWindows(t *testing.T) {
	client := quotaProbeClient(t, func(*http.Request) (*http.Response, error) {
		return nil, io.ErrUnexpectedEOF
	})
	header := http.Header{}
	header.Set("X-Codex-Primary-Used-Percent", "12")
	header.Set("X-Codex-Primary-Reset-After-Seconds", "600")
	client.observeQuotaHeaders("a1", header)
	// A failed live probe must surface the error rather than reusing a cached
	// (stale) window, so only real upstream data is shown.
	info, err := client.Quota(context.Background(), "a1")
	if info != nil || err == nil {
		t.Fatalf("expected probe failure, got info=%+v err=%v", info, err)
	}
}

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
