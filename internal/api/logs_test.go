package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/config"
)

func TestListRequestLogsFiltersAndPagination(t *testing.T) {
	dir := t.TempDir()
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: dir, DataDir: dir,
	})
	defer srv.Close()

	ctx := context.Background()
	base := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Second)
	for i := 0; i < 5; i++ {
		account := "acc_a"
		model := "glm-5.3"
		stream := false
		if i%2 == 1 {
			account = "acc_b"
			model = "qwen3.7-plus"
			stream = true
		}
		if err := srv.Recorder.Store().InsertRequestLog(ctx, accounts.RequestLog{
			// Distinct eight-character prefixes keep the prefix-query assertion deterministic.
			ID: fmt.Sprintf("req_%04d-fixture", i), CreatedAt: base.Add(time.Duration(i) * time.Minute),
			Status: accounts.RequestStatusOK, RequestedModel: model, AccountID: account, Stream: stream,
		}); err != nil {
			t.Fatal(err)
		}
	}
	srv.Ring.Append("[account=acc_b] warn line")
	srv.Ring.Append("plain info")

	h := srv.Handler()
	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer secret")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	page := get("/api/logs/requests?limit=2&offset=2")
	if page.Code != http.StatusOK {
		t.Fatalf("page status=%d body=%s", page.Code, page.Body.String())
	}
	var listed accounts.RequestLogList
	if err := json.Unmarshal(page.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Total != 5 || listed.Limit != 2 || listed.Offset != 2 || len(listed.Items) != 2 {
		t.Fatalf("page = %+v", listed)
	}

	filtered := get("/api/logs/requests?account=acc_b&model=qwen3.7-plus&stream=1&from=" + base.Format(time.RFC3339) + "&to=" + base.Add(4*time.Minute).Format(time.RFC3339))
	if filtered.Code != http.StatusOK {
		t.Fatalf("filter status=%d body=%s", filtered.Code, filtered.Body.String())
	}
	if err := json.Unmarshal(filtered.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Total != 2 {
		t.Fatalf("filtered total=%d items=%+v", listed.Total, listed.Items)
	}

	exactID := listed.Items[0].ID
	byID := get("/api/logs/requests?id=" + exactID)
	if byID.Code != http.StatusOK {
		t.Fatalf("id filter status=%d body=%s", byID.Code, byID.Body.String())
	}
	if err := json.Unmarshal(byID.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Total != 1 || listed.Items[0].ID != exactID {
		t.Fatalf("id filter = %+v", listed)
	}

	prefix := get("/api/logs/requests?q=" + exactID[:8])
	if prefix.Code != http.StatusOK {
		t.Fatalf("id prefix status=%d body=%s", prefix.Code, prefix.Body.String())
	}
	if err := json.Unmarshal(prefix.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Total != 1 || listed.Items[0].ID != exactID {
		t.Fatalf("id prefix = %+v", listed)
	}

	runtime := get("/api/logs/runtime?account=acc_b")
	if runtime.Code != http.StatusOK {
		t.Fatalf("runtime status=%d body=%s", runtime.Code, runtime.Body.String())
	}
	var snapshot struct {
		Items []struct {
			AccountID string `json:"account_id"`
			Message   string `json:"message"`
		} `json:"items"`
		Count  int `json:"count"`
		Total  int `json:"total"`
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	}
	if err := json.Unmarshal(runtime.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Count != 1 || snapshot.Total != 1 || snapshot.Items[0].AccountID != "acc_b" {
		t.Fatalf("runtime = %+v", snapshot)
	}

	srv.Ring.Append("line one")
	srv.Ring.Append("line two")
	srv.Ring.Append("line three")
	paged := get("/api/logs/runtime?limit=2&offset=2")
	if paged.Code != http.StatusOK {
		t.Fatalf("runtime page status=%d body=%s", paged.Code, paged.Body.String())
	}
	if err := json.Unmarshal(paged.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Total != 5 || snapshot.Count != 2 || snapshot.Limit != 2 || snapshot.Offset != 2 || len(snapshot.Items) != 2 {
		t.Fatalf("runtime page = %+v", snapshot)
	}
	if !strings.Contains(snapshot.Items[0].Message, "line one") || !strings.Contains(snapshot.Items[1].Message, "plain info") {
		t.Fatalf("expected older runtime page, got %+v", snapshot.Items)
	}
}

func TestRequestStatsWindow(t *testing.T) {
	dir := t.TempDir()
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: dir, DataDir: dir,
	})
	defer srv.Close()

	ctx := context.Background()
	base := time.Now().UTC().Add(-40 * time.Minute).Truncate(time.Second)
	latency := 180
	prompt, completion := 11, 22
	if err := srv.Recorder.Store().InsertRequestLog(ctx, accounts.RequestLog{
		ID: accounts.NewRequestID(), CreatedAt: base, Status: accounts.RequestStatusOK,
		RequestedModel: "glm-5.3", AccountID: "acc_a", LatencyMs: &latency,
		PromptTokens: &prompt, CompletionTokens: &completion,
	}); err != nil {
		t.Fatal(err)
	}
	if err := srv.Recorder.Store().InsertRequestLog(ctx, accounts.RequestLog{
		ID: accounts.NewRequestID(), CreatedAt: base.Add(30 * time.Minute), Status: accounts.RequestStatusError,
		RequestedModel: "qwen3.7-plus", AccountID: "acc_b", ErrorKind: accounts.KindUnavailable,
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/logs/stats?from="+base.Add(-10*time.Minute).Format(time.RFC3339)+"&to="+base.Add(50*time.Minute).Format(time.RFC3339), nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var stats accounts.RequestStats
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.Totals.Requests != 2 || stats.Totals.OK != 1 || stats.Totals.Error != 1 {
		t.Fatalf("stats = %+v", stats.Totals)
	}
	if stats.Tokens.Total != 33 {
		t.Fatalf("tokens = %+v", stats.Tokens)
	}
	if len(stats.Errors) != 1 || stats.Errors[0].Key != accounts.KindUnavailable {
		t.Fatalf("errors = %+v", stats.Errors)
	}

	second := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/logs/stats?from="+base.Add(-10*time.Minute).Format(time.RFC3339)+"&to="+base.Add(50*time.Minute).Format(time.RFC3339), nil)
	req.Header.Set("Authorization", "Bearer secret")
	srv.Handler().ServeHTTP(second, req)
	if second.Code != http.StatusOK {
		t.Fatalf("cached status=%d body=%s", second.Code, second.Body.String())
	}
	cacheEntries := srv.consoleHandler().StatsCacheSize()
	if cacheEntries != 1 {
		t.Fatalf("stats cache entries=%d, want 1", cacheEntries)
	}
}

func TestParseQueryTimeDateOnlyEndOfDay(t *testing.T) {
	from := parseQueryTime("2026-08-28", false)
	to := parseQueryTime("2026-08-28", true)
	if from == nil || to == nil {
		t.Fatal("expected parsed times")
	}
	if from.UTC().Format(time.RFC3339) != "2026-08-28T00:00:00Z" {
		t.Fatalf("from=%s", from.UTC())
	}
	if to.UTC().Format(time.RFC3339Nano) != "2026-08-28T23:59:59.999999999Z" {
		t.Fatalf("to=%s", to.UTC())
	}
}

func TestGetRequestLogExposesUsageDetail(t *testing.T) {
	dir := t.TempDir()
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: dir, DataDir: dir,
	})
	defer srv.Close()

	ctx := context.Background()
	id := accounts.NewRequestID()
	created := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	if err := srv.Recorder.Store().InsertRequestLog(ctx, accounts.RequestLog{
		ID: id, CreatedAt: created, Status: accounts.RequestStatusOK,
		RequestedModel: "hy3", AccountID: "acc_wb", Provider: "workbuddy",
	}); err != nil {
		t.Fatal(err)
	}
	consumed := 0.75
	if err := srv.Recorder.Store().InsertRequestUsageDetail(ctx, accounts.RequestUsageDetail{
		RequestID: id, CreatedAt: created, Provider: "workbuddy", Credit: &consumed, Unit: "credits",
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/logs/requests/"+id, nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		UsageDetail *accounts.RequestUsageDetail `json:"usage_detail"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.UsageDetail == nil || got.UsageDetail.Credit == nil || *got.UsageDetail.Credit != 0.75 ||
		got.UsageDetail.Unit != "credits" || got.UsageDetail.Provider != "workbuddy" {
		t.Fatalf("usage_detail = %+v", got.UsageDetail)
	}
}

func TestRequestStatsDimLimitAndProviderFilter(t *testing.T) {
	dir := t.TempDir()
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: dir, DataDir: dir,
	})
	defer srv.Close()

	ctx := context.Background()
	base := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	for i, row := range []struct{ model, provider string }{{"glm", "qoder"}, {"glm", "qoder"}, {"hy3", "workbuddy"}} {
		latency := 100 * (i + 1)
		if err := srv.Recorder.Store().InsertRequestLog(ctx, accounts.RequestLog{
			ID: accounts.NewRequestID(), CreatedAt: base.Add(time.Duration(i) * time.Minute), Status: accounts.RequestStatusOK,
			RequestedModel: row.model, AccountID: "acc_" + row.provider, Provider: row.provider, LatencyMs: &latency,
		}); err != nil {
			t.Fatal(err)
		}
	}
	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer secret")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		return rec
	}
	window := "from=" + base.Add(-time.Minute).Format(time.RFC3339) + "&to=" + base.Add(10*time.Minute).Format(time.RFC3339)

	var stats accounts.RequestStats
	if err := json.Unmarshal(get("/api/logs/stats?dim=model&limit=1&"+window).Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if len(stats.Models) != 1 || stats.Models[0].Key != "glm" || len(stats.Accounts) != 0 || len(stats.Providers) != 0 {
		t.Fatalf("dim=model&limit=1 → models %+v accounts %+v providers %+v", stats.Models, stats.Accounts, stats.Providers)
	}
	if stats.Models[0].LatencyP95Ms == nil || *stats.Models[0].LatencyP95Ms != 200 {
		t.Fatalf("glm p95 = %+v", stats.Models[0].LatencyP95Ms)
	}

	// No dim/limit: unchanged overview contract (all three lists).
	if err := json.Unmarshal(get("/api/logs/stats?"+window).Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if len(stats.Models) != 2 || len(stats.Accounts) != 2 || len(stats.Providers) != 2 {
		t.Fatalf("default lists = %+v / %+v / %+v", stats.Models, stats.Accounts, stats.Providers)
	}

	var listed accounts.RequestLogList
	if err := json.Unmarshal(get("/api/logs/requests?provider=workbuddy").Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Total != 1 || listed.Items[0].Provider != "workbuddy" {
		t.Fatalf("provider filter = %+v", listed)
	}
}

func TestRequestUsageEndpointCredits(t *testing.T) {
	dir := t.TempDir()
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: dir, DataDir: dir,
	})
	defer srv.Close()

	ctx := context.Background()
	base := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	fptr := func(v float64) *float64 { return &v }
	iptr := func(v int) *int { return &v }

	accStore := srv.Manager.Store()
	globalAcc, err := accStore.Create(ctx, accounts.CreateAccount{Name: "G", Provider: "qoder", Region: "global"})
	if err != nil {
		t.Fatal(err)
	}
	cnAcc, err := accStore.Create(ctx, accounts.CreateAccount{Name: "C", Provider: "workbuddy", Region: "cn"})
	if err != nil {
		t.Fatal(err)
	}
	store := srv.Recorder.Store()

	if err := store.InsertRequestLog(ctx, accounts.RequestLog{
		ID: accounts.NewRequestID(), CreatedAt: base, Status: accounts.RequestStatusOK,
		RequestedModel: "glm", AccountID: globalAcc.ID, Provider: "qoder",
		PromptTokens: iptr(100_000), CompletionTokens: iptr(0), Credits: fptr(1.0),
	}); err != nil {
		t.Fatal(err)
	}
	fallbackID := accounts.NewRequestID()
	if err := store.InsertRequestLog(ctx, accounts.RequestLog{
		ID: fallbackID, CreatedAt: base.Add(time.Minute), Status: accounts.RequestStatusOK,
		RequestedModel: "hy3", AccountID: cnAcc.ID, Provider: "workbuddy",
		PromptTokens: iptr(50_000), CompletionTokens: iptr(0),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertRequestUsageDetail(ctx, accounts.RequestUsageDetail{
		RequestID: fallbackID, CreatedAt: base.Add(time.Minute), Provider: "workbuddy",
		Credit: fptr(0.5), Unit: "credits",
	}); err != nil {
		t.Fatal(err)
	}
	// Dual NULL — excluded from credit samples / denominator.
	if err := store.InsertRequestLog(ctx, accounts.RequestLog{
		ID: accounts.NewRequestID(), CreatedAt: base.Add(2 * time.Minute), Status: accounts.RequestStatusOK,
		RequestedModel: "hy3", AccountID: cnAcc.ID, Provider: "workbuddy",
		PromptTokens: iptr(999_000), CompletionTokens: iptr(0),
	}); err != nil {
		t.Fatal(err)
	}

	window := "from=" + base.Add(-time.Minute).Format(time.RFC3339) + "&to=" + base.Add(10*time.Minute).Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, "/api/logs/usage?"+window, nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var usage accounts.RequestUsage
	if err := json.Unmarshal(rec.Body.Bytes(), &usage); err != nil {
		t.Fatal(err)
	}
	if usage.Totals.Credits != 1.5 || usage.Totals.CreditSamples != 2 || usage.Totals.CreditTokens != 150_000 {
		t.Fatalf("totals = %+v", usage.Totals)
	}
	if usage.Totals.CreditsPer1MTokens == nil {
		t.Fatal("expected credits_per_1m_tokens")
	}
	if len(usage.ByRealm) < 2 {
		t.Fatalf("by_realm = %+v", usage.ByRealm)
	}
	realms := map[string]int{}
	for _, row := range usage.ByRealm {
		realms[row.Key] = row.Requests
	}
	if realms["global"] != 1 || realms["cn"] != 2 {
		t.Fatalf("by_realm counts = %+v", realms)
	}

	// Cache hit: second call must reuse the same entry.
	second := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/logs/usage?"+window, nil)
	req.Header.Set("Authorization", "Bearer secret")
	srv.Handler().ServeHTTP(second, req)
	if second.Code != http.StatusOK {
		t.Fatalf("cached status=%d body=%s", second.Code, second.Body.String())
	}
}

func TestUsageCostEndpointPricesDeepSeek(t *testing.T) {
	dir := t.TempDir()
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: dir, DataDir: dir,
	})
	defer srv.Close()

	ctx := context.Background()
	// Keep the fixture inside retention even when the asynchronous pruner runs.
	// Noon UTC is 20:00 Beijing and always off-peak.
	yesterday := time.Now().UTC().Add(-24 * time.Hour)
	at := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 12, 0, 0, 0, time.UTC)
	prompt, completion, cacheRead := 1_000_000, 1_000_000, 500_000
	if err := srv.Recorder.Store().InsertRequestLog(ctx, accounts.RequestLog{
		ID: accounts.NewRequestID(), CreatedAt: at, Status: accounts.RequestStatusOK,
		RequestedModel: "deepseek-v4.1-flash", AccountID: "acc_wb", Provider: "workbuddy",
		PromptTokens: &prompt, CompletionTokens: &completion, CacheReadTokens: &cacheRead,
	}); err != nil {
		t.Fatal(err)
	}

	from := at.Truncate(24 * time.Hour)
	to := from.Add(24 * time.Hour)
	req := httptest.NewRequest(http.MethodGet, "/api/usage/cost?from="+from.Format(time.RFC3339)+"&to="+to.Format(time.RFC3339), nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got accounts.CostBreakdown
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Totals.Requests != 1 || len(got.Models) != 1 {
		t.Fatalf("breakdown = %+v", got.Totals)
	}
	// Off-peak 500k hit*0.02 + 500k miss*1 + 1M out*4 = 0.01 + 0.5 + 4 = 4.51
	if math.Abs(got.Totals.Cost-4.51) > 1e-6 {
		t.Fatalf("cost = %v, want 4.51", got.Totals.Cost)
	}
	if got.Currency != "CNY" {
		t.Fatalf("currency = %q", got.Currency)
	}
}

func TestUsageCostAndPricingRequireConsoleKey(t *testing.T) {
	dir := t.TempDir()
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: dir, DataDir: dir,
	})
	defer srv.Close()
	for _, path := range []string{"/api/usage/cost", "/api/usage/pricing"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s without key status=%d, want 401", path, rec.Code)
		}
	}
}
