package store

import (
	"context"
	"github.com/caigee-cmd/cli2api/internal/accounts"
	"math"
	"path/filepath"
	"testing"
	"time"
)

func TestRequestLogsInsertListGetAndPurge(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().UTC()
	if _, err := store.Create(ctx, accounts.CreateAccount{Name: "A", Provider: "qoder", Region: "global"}); err != nil {
		t.Fatal(err)
	}
	wb, err := store.Create(ctx, accounts.CreateAccount{Name: "WB", Provider: "workbuddy", Region: "cn"})
	if err != nil {
		t.Fatal(err)
	}
	parentID := accounts.NewRequestID()
	prompt, completion := 12, 34
	if err := store.InsertRequestLog(ctx, accounts.RequestLog{
		ID: parentID, CreatedAt: now, Stream: false, Status: accounts.RequestStatusStarted,
		RequestedModel: "glm-5.3", RequestedReasoning: "high", AccountID: wb.ID, AttemptCount: 0,
	}); err != nil {
		t.Fatal(err)
	}
	finished := now.Add(120 * time.Millisecond)
	latency := 120
	if err := store.UpdateRequestLog(ctx, accounts.RequestLog{
		ID: parentID, CreatedAt: now, FinishedAt: &finished, Stream: false, Status: accounts.RequestStatusOK,
		RequestedModel: "glm-5.3", RequestedReasoning: "high", ResolvedReasoning: "medium", AccountID: wb.ID, PromptTokens: &prompt, CompletionTokens: &completion,
		UsageSource: "upstream", LatencyMs: &latency, AttemptCount: 2,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertRequestAttempt(ctx, accounts.RequestAttempt{
		ID: accounts.NewAttemptID(), RequestID: parentID, AttemptIndex: 0, AccountID: "acc_a",
		StartedAt: now, FinishedAt: &finished, Status: accounts.AttemptStatusFailover,
		ErrorKind: accounts.KindRateLimit, ErrorMessage: "too many requests",
	}); err != nil {
		t.Fatal(err)
	}
	upstreamStatus := 200
	if err := store.InsertRequestStreamDiagnostic(ctx, accounts.RequestStreamDiagnostic{
		RequestID: parentID, CreatedAt: now, FinishedAt: &finished, UpstreamStatus: &upstreamStatus,
		ContextErr: "context canceled", CancellationSource: "request_context_canceled",
		RelayError: "stream read error: context canceled", SSEEventCount: 4, BytesRead: 512,
		ContentLength: -1, LastEvent: "message", SawDone: false,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertRequestAttempt(ctx, accounts.RequestAttempt{
		ID: accounts.NewAttemptID(), RequestID: parentID, AttemptIndex: 1, AccountID: "acc_b",
		StartedAt: now, FinishedAt: &finished, Status: accounts.AttemptStatusOK,
		PromptTokens: &prompt, CompletionTokens: &completion, UsageSource: "upstream",
	}); err != nil {
		t.Fatal(err)
	}
	consumed := 0.75
	if err := store.InsertRequestUsageDetail(ctx, accounts.RequestUsageDetail{
		RequestID: parentID, CreatedAt: now, Provider: "workbuddy", Credit: &consumed, Unit: "credits",
	}); err != nil {
		t.Fatal(err)
	}

	list, err := store.ListRequestLogs(ctx, accounts.RequestLogFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || len(list.Items) != 1 || list.Items[0].Status != accounts.RequestStatusOK || list.Items[0].Provider != "workbuddy" {
		t.Fatalf("list = %+v", list)
	}
	if list.Items[0].RequestedReasoning != "high" || list.Items[0].ResolvedReasoning != "medium" {
		t.Fatalf("list reasoning = %+v", list.Items[0])
	}
	if list.Items[0].Credits == nil || *list.Items[0].Credits != 0.75 {
		t.Fatalf("list credits fallback = %+v", list.Items[0].Credits)
	}
	if list.Items[0].UsageDetail != nil {
		t.Fatalf("list should not carry usage detail: %+v", list.Items[0].UsageDetail)
	}

	got, err := store.GetRequestLog(ctx, parentID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccountID != wb.ID || got.Provider != "workbuddy" || len(got.Attempts) != 2 || got.Attempts[0].Status != accounts.AttemptStatusFailover {
		t.Fatalf("detail = %+v", got)
	}
	if got.RequestedReasoning != "high" || got.ResolvedReasoning != "medium" {
		t.Fatalf("detail reasoning = %+v", got)
	}
	if got.StreamDiagnostic == nil || got.StreamDiagnostic.UpstreamStatus == nil || *got.StreamDiagnostic.UpstreamStatus != 200 ||
		got.StreamDiagnostic.CancellationSource != "request_context_canceled" || got.StreamDiagnostic.SSEEventCount != 4 || got.StreamDiagnostic.SawDone {
		t.Fatalf("stream diagnostic = %+v", got.StreamDiagnostic)
	}
	if got.UsageDetail == nil || got.UsageDetail.Credit == nil || *got.UsageDetail.Credit != 0.75 ||
		got.UsageDetail.Unit != "credits" || got.UsageDetail.Provider != "workbuddy" {
		t.Fatalf("usage detail = %+v", got.UsageDetail)
	}

	filtered, err := store.ListRequestLogs(ctx, accounts.RequestLogFilter{AccountID: wb.ID, Status: accounts.RequestStatusOK})
	if err != nil || filtered.Total != 1 {
		t.Fatalf("filtered = %+v err=%v", filtered, err)
	}
	none, err := store.ListRequestLogs(ctx, accounts.RequestLogFilter{ErrorKind: accounts.KindQuota})
	if err != nil || none.Total != 0 {
		t.Fatalf("error filter = %+v err=%v", none, err)
	}

	from := now.Add(-time.Second)
	to := now.Add(time.Second)
	timed, err := store.ListRequestLogs(ctx, accounts.RequestLogFilter{From: &from, To: &to, Model: "glm-5.3"})
	if err != nil || timed.Total != 1 {
		t.Fatalf("time/model filter = %+v err=%v", timed, err)
	}
	tooNew := now.Add(time.Hour)
	emptyTime, err := store.ListRequestLogs(ctx, accounts.RequestLogFilter{From: &tooNew})
	if err != nil || emptyTime.Total != 0 {
		t.Fatalf("future from filter = %+v err=%v", emptyTime, err)
	}

	page, err := store.ListRequestLogs(ctx, accounts.RequestLogFilter{Limit: 1, Offset: 0})
	if err != nil || page.Total != 1 || page.Limit != 1 || page.Offset != 0 || len(page.Items) != 1 {
		t.Fatalf("page = %+v err=%v", page, err)
	}

	oldID := accounts.NewRequestID()
	if err := store.InsertRequestLog(ctx, accounts.RequestLog{
		ID: oldID, CreatedAt: now.Add(-10 * 24 * time.Hour), Status: accounts.RequestStatusError, RequestedModel: "old",
	}); err != nil {
		t.Fatal(err)
	}
	deleted, err := store.PurgeRequestLogs(ctx, 7*24*time.Hour, 20_000)
	if err != nil || deleted < 1 {
		t.Fatalf("purge deleted=%d err=%v", deleted, err)
	}
	if _, err := store.GetRequestLog(ctx, oldID); err != accounts.ErrRequestLogNotFound {
		t.Fatalf("expected old log gone, got %v", err)
	}

	cleared, err := store.ClearRequestLogs(ctx)
	if err != nil || cleared < 1 {
		t.Fatalf("clear = %d err=%v", cleared, err)
	}
	var usageRows int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM request_usage_details`).Scan(&usageRows); err != nil {
		t.Fatal(err)
	}
	if usageRows != 0 {
		t.Fatalf("usage detail rows survived clear: %d", usageRows)
	}
}

func TestUsageDetailBackfillUsesProviderThenAccountFallback(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	account, err := store.Create(ctx, accounts.CreateAccount{Name: "WB", Provider: "workbuddy", Region: "cn"})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	explicit, fallback, missing := 1.5, 2.5, 3.5
	rows := []accounts.RequestLog{
		{ID: accounts.NewRequestID(), CreatedAt: now, Status: accounts.RequestStatusOK, RequestedModel: "m1", Provider: "trae", AccountID: account.ID, Credits: &explicit},
		{ID: accounts.NewRequestID(), CreatedAt: now, Status: accounts.RequestStatusOK, RequestedModel: "m2", AccountID: account.ID, Credits: &fallback},
		{ID: accounts.NewRequestID(), CreatedAt: now, Status: accounts.RequestStatusOK, RequestedModel: "m3", AccountID: "ghost", Credits: &missing},
	}
	for _, row := range rows {
		if err := store.InsertRequestLog(ctx, row); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := store.DB().ExecContext(ctx, `DROP TABLE IF EXISTS request_usage_details`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM schema_migrations WHERE filename = '020_request_usage_details.sql'`); err != nil {
		t.Fatal(err)
	}
	if err := store.runMigrations(ctx); err != nil {
		t.Fatal(err)
	}

	explicitDetail, err := store.getRequestUsageDetail(ctx, rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if explicitDetail == nil || explicitDetail.Provider != "trae" {
		t.Fatalf("explicit provider row = %+v, want trae", explicitDetail)
	}

	fallbackDetail, err := store.getRequestUsageDetail(ctx, rows[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if fallbackDetail == nil || fallbackDetail.Provider != "workbuddy" {
		t.Fatalf("account fallback row = %+v, want workbuddy", fallbackDetail)
	}
	if fallbackDetail.Credit == nil || *fallbackDetail.Credit != fallback || fallbackDetail.Unit != "credits" {
		t.Fatalf("account fallback credit/unit = %+v", fallbackDetail)
	}

	missingDetail, err := store.getRequestUsageDetail(ctx, rows[2].ID)
	if err != nil {
		t.Fatal(err)
	}
	if missingDetail == nil || missingDetail.Provider != "" {
		t.Fatalf("unreachable account row = %+v, want empty provider", missingDetail)
	}
}

func TestRequestLogsPaginationAndTimeFilter(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		account := "acc_a"
		model := "glm-5.3"
		if i%2 == 1 {
			account = "acc_b"
			model = "qwen3.7-plus"
		}
		if err := store.InsertRequestLog(ctx, accounts.RequestLog{
			ID: accounts.NewRequestID(), CreatedAt: base.Add(time.Duration(i) * time.Minute),
			Status: accounts.RequestStatusOK, RequestedModel: model, AccountID: account,
		}); err != nil {
			t.Fatal(err)
		}
	}

	page1, err := store.ListRequestLogs(ctx, accounts.RequestLogFilter{Limit: 2, Offset: 0})
	if err != nil || page1.Total != 5 || page1.Limit != 2 || page1.Offset != 0 || len(page1.Items) != 2 {
		t.Fatalf("page1 = %+v err=%v", page1, err)
	}
	page3, err := store.ListRequestLogs(ctx, accounts.RequestLogFilter{Limit: 2, Offset: 4})
	if err != nil || page3.Total != 5 || len(page3.Items) != 1 {
		t.Fatalf("page3 = %+v err=%v", page3, err)
	}

	from := base.Add(2 * time.Minute)
	to := base.Add(3 * time.Minute)
	window, err := store.ListRequestLogs(ctx, accounts.RequestLogFilter{From: &from, To: &to, Limit: 10})
	if err != nil || window.Total != 2 {
		t.Fatalf("window = %+v err=%v", window, err)
	}
	accountB, err := store.ListRequestLogs(ctx, accounts.RequestLogFilter{AccountID: "acc_b", Limit: 10})
	if err != nil || accountB.Total != 2 {
		t.Fatalf("account = %+v err=%v", accountB, err)
	}
	model, err := store.ListRequestLogs(ctx, accounts.RequestLogFilter{Model: "qwen3.7-plus", Limit: 10})
	if err != nil || model.Total != 2 {
		t.Fatalf("model = %+v err=%v", model, err)
	}
	exactID := page1.Items[0].ID
	byID, err := store.ListRequestLogs(ctx, accounts.RequestLogFilter{ID: exactID, Limit: 10})
	if err != nil || byID.Total != 1 || byID.Items[0].ID != exactID {
		t.Fatalf("id = %+v err=%v", byID, err)
	}
	prefix, err := store.ListRequestLogs(ctx, accounts.RequestLogFilter{Query: exactID[:8], Limit: 10})
	if err != nil || prefix.Total != 1 || prefix.Items[0].ID != exactID {
		t.Fatalf("id prefix = %+v err=%v", prefix, err)
	}
	noneQuery, err := store.ListRequestLogs(ctx, accounts.RequestLogFilter{Query: "glm-5.3", Limit: 10})
	if err != nil || noneQuery.Total != 0 {
		t.Fatalf("query should only match ids, got %+v err=%v", noneQuery, err)
	}
}

func TestRequestLogsCapPurgeKeepsNewest(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Now().UTC()
	for i := 0; i < 5; i++ {
		if err := store.InsertRequestLog(ctx, accounts.RequestLog{
			ID: accounts.NewRequestID(), CreatedAt: base.Add(time.Duration(i) * time.Second),
			Status: accounts.RequestStatusOK, RequestedModel: "m",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.PurgeRequestLogs(ctx, 30*24*time.Hour, 3); err != nil {
		t.Fatal(err)
	}
	list, err := store.ListRequestLogs(ctx, accounts.RequestLogFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != 3 {
		t.Fatalf("expected 3 rows after cap purge, got %d", list.Total)
	}
}

func TestSummarizeRequestLogs(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Date(2026, 8, 28, 10, 15, 0, 0, time.UTC)
	insert := func(at time.Time, status, model, account, provider, kind string, latency, prompt, completion int, stream bool) {
		t.Helper()
		log := accounts.RequestLog{
			ID: accounts.NewRequestID(), CreatedAt: at, Status: status, RequestedModel: model, AccountID: account,
			Provider: provider, ErrorKind: kind, Stream: stream, AttemptCount: 1,
		}
		if latency > 0 {
			log.LatencyMs = &latency
		}
		if prompt > 0 {
			log.PromptTokens = &prompt
		}
		if completion > 0 {
			log.CompletionTokens = &completion
		}
		if err := store.InsertRequestLog(ctx, log); err != nil {
			t.Fatal(err)
		}
	}
	insert(base, accounts.RequestStatusOK, "glm-5.3", "acc_a", "qoder", "", 100, 12, 34, false)
	insert(base.Add(20*time.Minute), accounts.RequestStatusOK, "glm-5.3", "acc_a", "qoder", "", 200, 10, 20, true)
	insert(base.Add(90*time.Minute), accounts.RequestStatusError, "qwen3.7-plus", "acc_b", "workbuddy", accounts.KindRateLimit, 400, 8, 0, false)
	insert(base.Add(2*time.Hour), accounts.RequestStatusCanceled, "glm-5.3", "acc_a", "qoder", "", 0, 0, 0, false)
	insert(base.Add(2*time.Hour+15*time.Minute), accounts.RequestStatusIncomplete, "glm-5.3", "acc_a", "qoder", "", 0, 0, 0, false)
	insert(base.Add(-30*time.Hour), accounts.RequestStatusOK, "old", "acc_a", "qoder", "", 50, 1, 1, false)

	from := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 28, 13, 0, 0, 0, time.UTC)
	stats, err := store.SummarizeRequestLogs(ctx, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Totals.Requests != 5 || stats.Totals.OK != 2 || stats.Totals.Incomplete != 1 || stats.Totals.Error != 1 || stats.Totals.Canceled != 1 || stats.Totals.Streaming != 1 {
		t.Fatalf("totals = %+v", stats.Totals)
	}
	if stats.Totals.SuccessRate != 0.4 {
		t.Fatalf("success rate = %v", stats.Totals.SuccessRate)
	}
	if stats.Tokens.Prompt != 30 || stats.Tokens.Completion != 54 || stats.Tokens.Total != 84 {
		t.Fatalf("tokens = %+v", stats.Tokens)
	}
	if stats.Latency.AvgMs == nil || *stats.Latency.AvgMs != 233 {
		t.Fatalf("avg latency = %+v", stats.Latency.AvgMs)
	}
	if stats.Latency.P50Ms == nil || *stats.Latency.P50Ms != 200 {
		t.Fatalf("p50 = %+v", stats.Latency.P50Ms)
	}
	if stats.Latency.P95Ms == nil || *stats.Latency.P95Ms != 400 {
		t.Fatalf("p95 = %+v", stats.Latency.P95Ms)
	}
	if len(stats.Errors) != 1 || stats.Errors[0].Key != accounts.KindRateLimit || stats.Errors[0].Count != 1 {
		t.Fatalf("errors = %+v", stats.Errors)
	}
	if len(stats.Models) == 0 || stats.Models[0].Key != "glm-5.3" || stats.Models[0].Count != 4 {
		t.Fatalf("models = %+v", stats.Models)
	}
	if len(stats.Accounts) == 0 || stats.Accounts[0].Key != "acc_a" || stats.Accounts[0].Count != 4 {
		t.Fatalf("accounts = %+v", stats.Accounts)
	}
	if len(stats.Providers) == 0 || stats.Providers[0].Key != "qoder" || stats.Providers[0].Count != 4 {
		t.Fatalf("providers = %+v", stats.Providers)
	}
	if len(stats.Series) != 3 {
		t.Fatalf("series len = %d %+v", len(stats.Series), stats.Series)
	}
	if stats.Series[0].Requests != 2 || stats.Series[1].Requests != 1 || stats.Series[2].Requests != 2 {
		t.Fatalf("series = %+v", stats.Series)
	}

	empty, err := store.SummarizeRequestLogs(ctx, to.Add(time.Hour), to.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if empty.Totals.Requests != 0 || empty.Latency.AvgMs != nil || len(empty.Models) != 0 {
		t.Fatalf("empty stats = %+v", empty)
	}

	hourFrom := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	hourTo := hourFrom.Add(time.Hour)
	hourStats, err := store.SummarizeRequestLogs(ctx, hourFrom, hourTo)
	if err != nil {
		t.Fatal(err)
	}
	if len(hourStats.Series) != 4 {
		t.Fatalf("1h series len = %d %+v", len(hourStats.Series), hourStats.Series)
	}
	if hourStats.Series[1].Requests != 1 || hourStats.Series[2].Requests != 1 {
		t.Fatalf("1h series = %+v", hourStats.Series)
	}
}

func TestRequestLogPersistsRouting(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	id := accounts.NewRequestID()
	if err := store.InsertRequestLog(ctx, accounts.RequestLog{
		ID: id, CreatedAt: time.Now().UTC(), Status: accounts.RequestStatusOK,
		RequestedModel: "glm-5.2", AccountID: "account-b", Routing: "sticky_escape",
	}); err != nil {
		t.Fatal(err)
	}
	listed, err := store.ListRequestLogs(ctx, accounts.RequestLogFilter{Limit: 10})
	if err != nil || len(listed.Items) != 1 || listed.Items[0].Routing != "sticky_escape" {
		t.Fatalf("listed = %+v, err=%v", listed, err)
	}
	detail, err := store.GetRequestLog(ctx, id)
	if err != nil || detail.Routing != "sticky_escape" {
		t.Fatalf("detail = %+v, err=%v", detail, err)
	}
}

func TestSummarizeRequestLogsUsageAndSeriesMetrics(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Date(2026, 9, 1, 10, 5, 0, 0, time.UTC)
	ptr := func(v int) *int { return &v }
	fptr := func(v float64) *float64 { return &v }
	insert := func(at time.Time, model, account, provider string, latency, prompt, completion, cacheRead, cacheWrite int, credits *float64) {
		t.Helper()
		log := accounts.RequestLog{
			ID: accounts.NewRequestID(), CreatedAt: at, Status: accounts.RequestStatusOK, RequestedModel: model,
			AccountID: account, Provider: provider, AttemptCount: 1,
			LatencyMs: ptr(latency), PromptTokens: ptr(prompt), CompletionTokens: ptr(completion),
			CacheReadTokens: ptr(cacheRead), CacheWriteTokens: ptr(cacheWrite), Credits: credits,
		}
		if err := store.InsertRequestLog(ctx, log); err != nil {
			t.Fatal(err)
		}
	}
	// Hour 10: three glm calls (latency 100/200/1000) on qoder.
	insert(base, "glm", "acc_a", "qoder", 100, 10, 1, 4, 2, fptr(0.5))
	insert(base.Add(10*time.Minute), "glm", "acc_a", "qoder", 200, 20, 2, 6, 0, fptr(0.25))
	insert(base.Add(20*time.Minute), "glm", "acc_b", "qoder", 1000, 30, 3, 0, 1, nil)
	// Hour 11: one hy3 call on workbuddy.
	insert(base.Add(time.Hour), "hy3", "acc_wb", "workbuddy", 300, 5, 5, 0, 7, fptr(2))

	from := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 1, 13, 0, 0, 0, time.UTC) // >2h → hourly buckets

	stats, err := store.SummarizeRequestLogs(ctx, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Tokens.CacheWrite != 10 || stats.Totals.Credits != 2.75 {
		t.Fatalf("cache_write/credits totals = %+v %+v", stats.Tokens, stats.Totals)
	}
	if len(stats.Series) != 3 {
		t.Fatalf("series = %+v", stats.Series)
	}
	h10 := stats.Series[0]
	if h10.PromptTokens != 60 || h10.CompletionTokens != 6 || h10.CacheReadTokens != 10 {
		t.Fatalf("hour-10 tokens = %+v", h10)
	}
	if h10.LatencyP50Ms == nil || *h10.LatencyP50Ms != 200 || h10.LatencyP95Ms == nil || *h10.LatencyP95Ms != 1000 {
		t.Fatalf("hour-10 latency = %+v / %+v", h10.LatencyP50Ms, h10.LatencyP95Ms)
	}
	if h11 := stats.Series[1]; h11.LatencyP95Ms == nil || *h11.LatencyP95Ms != 300 || h11.PromptTokens != 5 {
		t.Fatalf("hour-11 = %+v", h11)
	}

	glm := stats.Models[0]
	if glm.Key != "glm" || glm.Count != 3 || glm.PromptTokens != 60 || glm.CompletionTokens != 6 ||
		glm.CacheReadTokens != 10 || glm.CacheWriteTokens != 3 || glm.Credits != 0.75 {
		t.Fatalf("glm usage = %+v", glm)
	}
	if glm.LatencyP95Ms == nil || *glm.LatencyP95Ms != 1000 {
		t.Fatalf("glm p95 = %+v", glm.LatencyP95Ms)
	}
	if len(stats.Providers) != 2 || stats.Providers[1].Key != "workbuddy" || stats.Providers[1].Credits != 2 {
		t.Fatalf("providers = %+v", stats.Providers)
	}

	// dim selects one grouped list; limit caps its rows.
	only, err := store.SummarizeRequestLogsWith(ctx, accounts.RequestStatsOptions{From: from, To: to, Dims: []string{accounts.RequestStatsDimAccount}, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(only.Accounts) != 1 || only.Accounts[0].Key != "acc_a" || len(only.Models) != 0 || len(only.Providers) != 0 {
		t.Fatalf("dim/limit = accounts %+v models %+v providers %+v", only.Accounts, only.Models, only.Providers)
	}
	if only.Totals.Requests != 4 {
		t.Fatalf("dim query keeps totals, got %+v", only.Totals)
	}
}

func TestSummarizeRequestLogsDefaultLimitIsSix(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 9; i++ {
		if err := store.InsertRequestLog(ctx, accounts.RequestLog{
			ID: accounts.NewRequestID(), CreatedAt: base.Add(time.Duration(i) * time.Minute), Status: accounts.RequestStatusOK,
			RequestedModel: "m" + string(rune('a'+i)), AttemptCount: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	from, to := base.Add(-time.Minute), base.Add(time.Hour)
	def, err := store.SummarizeRequestLogs(ctx, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(def.Models) != accounts.RequestStatsDefaultLimit {
		t.Fatalf("default models = %d", len(def.Models))
	}
	all, err := store.SummarizeRequestLogsWith(ctx, accounts.RequestStatsOptions{From: from, To: to, Dims: []string{accounts.RequestStatsDimModel}, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Models) != 9 {
		t.Fatalf("limit=100 models = %d", len(all.Models))
	}
}

func TestListRequestLogsProviderFilter(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	for i, provider := range []string{"qoder", "workbuddy", "workbuddy"} {
		if err := store.InsertRequestLog(ctx, accounts.RequestLog{
			ID: accounts.NewRequestID(), CreatedAt: base.Add(time.Duration(i) * time.Minute), Status: accounts.RequestStatusOK,
			RequestedModel: "m", AccountID: "acc", Provider: provider, AttemptCount: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := store.ListRequestLogs(ctx, accounts.RequestLogFilter{Provider: "workbuddy", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != 2 {
		t.Fatalf("provider filter total = %d", list.Total)
	}
	for _, item := range list.Items {
		if item.Provider != "workbuddy" {
			t.Fatalf("unexpected provider %q", item.Provider)
		}
	}
}

func TestSummarizeRequestUsageCreditsAndRealms(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	globalAcc, err := store.Create(ctx, accounts.CreateAccount{Name: "Global", Provider: "qoder", Region: "global"})
	if err != nil {
		t.Fatal(err)
	}
	cnAcc, err := store.Create(ctx, accounts.CreateAccount{Name: "CN", Provider: "workbuddy", Region: "cn"})
	if err != nil {
		t.Fatal(err)
	}

	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	fptr := func(v float64) *float64 { return &v }
	iptr := func(v int) *int { return &v }

	insert := func(at time.Time, accountID, model, provider string, prompt, completion int, credits *float64) string {
		t.Helper()
		id := accounts.NewRequestID()
		if err := store.InsertRequestLog(ctx, accounts.RequestLog{
			ID: id, CreatedAt: at, Status: accounts.RequestStatusOK,
			RequestedModel: model, AccountID: accountID, Provider: provider, AttemptCount: 1,
			PromptTokens: iptr(prompt), CompletionTokens: iptr(completion), Credits: credits,
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}

	// Native credits on request_logs.
	insert(base, globalAcc.ID, "glm", "qoder", 100_000, 0, fptr(1.0))
	// Detail-only fallback (credits column NULL): older rows before native column.
	fallbackID := insert(base.Add(time.Minute), cnAcc.ID, "hy3", "workbuddy", 50_000, 0, nil)
	if err := store.InsertRequestUsageDetail(ctx, accounts.RequestUsageDetail{
		RequestID: fallbackID, CreatedAt: base.Add(time.Minute), Provider: "workbuddy",
		Credit: fptr(0.5), Unit: "credits",
	}); err != nil {
		t.Fatal(err)
	}
	// Explicit zero credits still counts as an observed sample (≠ unobserved).
	insert(base.Add(2*time.Minute), globalAcc.ID, "glm", "qoder", 10_000, 0, fptr(0))
	// Dual-NULL: no native credits and no usage_details → excluded from samples,
	// but its tokens must NOT enter the CreditsPer1M denominator.
	insert(base.Add(3*time.Minute), cnAcc.ID, "hy3", "workbuddy", 999_000, 0, nil)

	from, to := base.Add(-time.Minute), base.Add(time.Hour)
	usage, err := store.SummarizeRequestUsage(ctx, from, to, "")
	if err != nil {
		t.Fatal(err)
	}

	// 1.0 + 0.5 + 0 + (nil excluded) = 1.5; samples = 3 (native, fallback, zero).
	if usage.Totals.Requests != 4 || usage.Totals.Credits != 1.5 || usage.Totals.CreditSamples != 3 {
		t.Fatalf("totals = %+v", usage.Totals)
	}
	// CreditTokens = 100k + 50k + 10k (exclude dual-NULL 999k).
	if usage.Totals.CreditTokens != 160_000 {
		t.Fatalf("credit_tokens = %d, want 160000", usage.Totals.CreditTokens)
	}
	if usage.Totals.CreditsPer1MTokens == nil {
		t.Fatal("expected CreditsPer1MTokens")
	}
	// 1.5 / (160000/1e6) = 9.375
	wantPer1M := 1.5 / (160_000 / 1e6)
	if diff := *usage.Totals.CreditsPer1MTokens - wantPer1M; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("CreditsPer1M = %v, want %v", *usage.Totals.CreditsPer1MTokens, wantPer1M)
	}

	// SUM aligns with #361 creditExprOrZero (dual-NULL contributes 0 to the sum).
	stats, err := store.SummarizeRequestLogs(ctx, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Totals.Credits != usage.Totals.Credits {
		t.Fatalf("usage credits %v != #361 stats credits %v", usage.Totals.Credits, stats.Totals.Credits)
	}

	// by_realm follows account provider_region.
	realms := map[string]accounts.RequestUsageRow{}
	for _, row := range usage.ByRealm {
		realms[row.Key] = row
	}
	if realms["global"].Requests != 2 || realms["cn"].Requests != 2 {
		t.Fatalf("by_realm = %+v", usage.ByRealm)
	}

	// Credit ledger prefers native, falls back to detail, drops dual-NULL samples.
	byAcc := map[string]accounts.RequestUsageCreditRow{}
	for _, row := range usage.CreditByAccount {
		byAcc[row.Key] = row
	}
	g := byAcc[globalAcc.ID]
	if g.Credits != 1.0 || g.CreditSamples != 2 || g.CreditTokens != 110_000 {
		t.Fatalf("global credit row = %+v", g)
	}
	c := byAcc[cnAcc.ID]
	if c.Credits != 0.5 || c.CreditSamples != 1 || c.CreditTokens != 50_000 {
		t.Fatalf("cn credit row = %+v", c)
	}
}

func TestSummarizeModelCostsAggregatesPricedModels(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Peak hour (Beijing 10:00 = UTC 02:00) and off-peak hour, same day.
	peak := time.Date(2026, 10, 12, 2, 15, 0, 0, time.UTC)
	off := time.Date(2026, 10, 12, 20, 15, 0, 0, time.UTC)
	insert := func(id, model string, at time.Time, prompt, completion, cacheRead int) {
		p, c, cr := prompt, completion, cacheRead
		if err := store.InsertRequestLog(ctx, accounts.RequestLog{
			ID: id, CreatedAt: at, Status: accounts.RequestStatusOK, RequestedModel: model,
			PromptTokens: &p, CompletionTokens: &c, CacheReadTokens: &cr, UsageSource: "estimate",
		}); err != nil {
			t.Fatal(err)
		}
	}
	insert("req_peak1", "deepseek-v4.1-flash", peak, 1_000_000, 1_000_000, 400_000)
	insert("req_peak2", "deepseek-v4.1-flash", peak, 500_000, 0, 500_000)
	insert("req_off1", "workbuddy/deepseek-v4.1-flash", off, 1_000_000, 1_000_000, 0)
	insert("req_glm", "glm-5.3-flash", peak, 10, 10, 0) // unpriced, must be excluded
	// A failed row with NULL prompt tokens must contribute nothing.
	if err := store.InsertRequestLog(ctx, accounts.RequestLog{
		ID: "req_failed", CreatedAt: peak, Status: accounts.RequestStatusError, RequestedModel: "deepseek-v4.1-flash",
	}); err != nil {
		t.Fatal(err)
	}

	from := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	rows, err := store.SummarizeModelCosts(ctx, from, to, "")
	if err != nil {
		t.Fatal(err)
	}
	breakdown := accounts.BuildCostBreakdown(rows, from, to, true)
	// Model rows now include unpriced models so the console can still report
	// their tokens and quota; only the priced one carries the monetary columns.
	if len(breakdown.Models) != 2 {
		t.Fatalf("models = %d, want 2 (priced + unpriced glm)", len(breakdown.Models))
	}
	var m accounts.ModelCost
	var unpriced accounts.ModelCost
	for _, candidate := range breakdown.Models {
		if candidate.Priced {
			m = candidate
		} else {
			unpriced = candidate
		}
	}
	if unpriced.ID != "glm-5.3-flash" {
		t.Fatalf("unpriced id = %q, want glm-5.3-flash", unpriced.ID)
	}
	if unpriced.Output != 10 || unpriced.Cost != 0 {
		t.Fatalf("unpriced output/cost = %d/%v, want 10/0", unpriced.Output, unpriced.Cost)
	}
	// Peak rows: hit 400k+500k=900k, miss (600k+0)=600k, output 1M.
	// Off row: hit 0, miss 1M, output 1M.
	if m.CacheHit != 900_000 || m.CacheMiss != 1_600_000 || m.Output != 2_000_000 {
		t.Fatalf("tokens hit/miss/out = %d/%d/%d", m.CacheHit, m.CacheMiss, m.Output)
	}
	if m.Requests != 3 {
		t.Fatalf("requests = %d, want 3", m.Requests)
	}
	wantPeak := (900_000*0.04 + 600_000*2 + 1_000_000*8) / 1_000_000
	wantOff := (0*0.02 + 1_000_000*1 + 1_000_000*4) / 1_000_000
	if math.Abs(m.CostPeak-wantPeak) > 1e-9 || math.Abs(m.CostOffPeak-wantOff) > 1e-9 {
		t.Fatalf("peak/off = %v/%v, want %v/%v", m.CostPeak, m.CostOffPeak, wantPeak, wantOff)
	}
}

func TestSummarizeModelCostsUsesServedModel(t *testing.T) {
	cases := []struct {
		name, requested, mapped, want string
		priced                        bool
	}{
		{"alias to priced model", "my-model", "workbuddy/deepseek-v4.1-flash-sg", "deepseek-v4.1-flash", true},
		{"priced alias to unpriced model", "deepseek-v4.1-flash", "glm-5.3-flash", "glm-5.3-flash", false},
		{"old log without mapping", "deepseek-v4.1-flash", "", "deepseek-v4.1-flash", true},
		{"blank mapping", "deepseek-v4.1-flash", "  ", "deepseek-v4.1-flash", true},
		{"mapping without requested model", "", "deepseek-v4.1-flash", "deepseek-v4.1-flash", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			at := time.Date(2026, 10, 12, 20, 15, 0, 0, time.UTC)
			prompt, output := 1_000_000, 1_000_000
			credits := 2.5
			if err := store.InsertRequestLog(ctx, accounts.RequestLog{
				ID: "mapped-usage", CreatedAt: at, Status: accounts.RequestStatusOK,
				RequestedModel: tc.requested, MappedModel: tc.mapped,
				PromptTokens: &prompt, CompletionTokens: &output, Credits: &credits,
			}); err != nil {
				t.Fatal(err)
			}
			from, to := at.Add(-time.Hour), at.Add(time.Hour)
			rows, err := store.SummarizeModelCosts(ctx, from, to, "")
			if err != nil {
				t.Fatal(err)
			}
			got := accounts.BuildCostBreakdown(rows, from, to, false)
			if len(got.Models) != 1 {
				t.Fatalf("models = %+v, want one served model", got.Models)
			}
			model := got.Models[0]
			if model.ID != tc.want || model.Priced != tc.priced {
				t.Fatalf("model = %q priced=%v, want %q priced=%v", model.ID, model.Priced, tc.want, tc.priced)
			}
			wantCost := 0.0
			if tc.priced {
				wantCost = 5.0
			}
			if math.Abs(model.Cost-wantCost) > 1e-9 || model.TotalTokens != 2_000_000 || model.Requests != 1 || model.Credits != credits {
				t.Fatalf("wrong served-model usage: %+v", model)
			}
		})
	}
}
