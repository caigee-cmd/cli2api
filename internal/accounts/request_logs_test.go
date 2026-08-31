package accounts

import (
	"context"
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
	if _, err := store.Create(ctx, CreateAccount{Name: "A", Provider: "qoder", Region: "global"}); err != nil {
		t.Fatal(err)
	}
	wb, err := store.Create(ctx, CreateAccount{Name: "WB", Provider: "workbuddy", Region: "cn"})
	if err != nil {
		t.Fatal(err)
	}
	parentID := NewRequestID()
	prompt, completion := 12, 34
	if err := store.InsertRequestLog(ctx, RequestLog{
		ID: parentID, CreatedAt: now, Stream: false, Status: RequestStatusStarted,
		RequestedModel: "glm-5.3", AccountID: wb.ID, AttemptCount: 0,
	}); err != nil {
		t.Fatal(err)
	}
	finished := now.Add(120 * time.Millisecond)
	latency := 120
	if err := store.UpdateRequestLog(ctx, RequestLog{
		ID: parentID, CreatedAt: now, FinishedAt: &finished, Stream: false, Status: RequestStatusOK,
		RequestedModel: "glm-5.3", AccountID: wb.ID, PromptTokens: &prompt, CompletionTokens: &completion,
		UsageSource: "upstream", LatencyMs: &latency, AttemptCount: 2,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertRequestAttempt(ctx, RequestAttempt{
		ID: NewAttemptID(), RequestID: parentID, AttemptIndex: 0, AccountID: "acc_a",
		StartedAt: now, FinishedAt: &finished, Status: AttemptStatusFailover,
		ErrorKind: KindRateLimit, ErrorMessage: "too many requests",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertRequestAttempt(ctx, RequestAttempt{
		ID: NewAttemptID(), RequestID: parentID, AttemptIndex: 1, AccountID: "acc_b",
		StartedAt: now, FinishedAt: &finished, Status: AttemptStatusOK,
		PromptTokens: &prompt, CompletionTokens: &completion, UsageSource: "upstream",
	}); err != nil {
		t.Fatal(err)
	}

	list, err := store.ListRequestLogs(ctx, RequestLogFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || len(list.Items) != 1 || list.Items[0].Status != RequestStatusOK || list.Items[0].Provider != "workbuddy" {
		t.Fatalf("list = %+v", list)
	}

	got, err := store.GetRequestLog(ctx, parentID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccountID != wb.ID || got.Provider != "workbuddy" || len(got.Attempts) != 2 || got.Attempts[0].Status != AttemptStatusFailover {
		t.Fatalf("detail = %+v", got)
	}

	filtered, err := store.ListRequestLogs(ctx, RequestLogFilter{AccountID: wb.ID, Status: RequestStatusOK})
	if err != nil || filtered.Total != 1 {
		t.Fatalf("filtered = %+v err=%v", filtered, err)
	}
	none, err := store.ListRequestLogs(ctx, RequestLogFilter{ErrorKind: KindQuota})
	if err != nil || none.Total != 0 {
		t.Fatalf("error filter = %+v err=%v", none, err)
	}

	from := now.Add(-time.Second)
	to := now.Add(time.Second)
	timed, err := store.ListRequestLogs(ctx, RequestLogFilter{From: &from, To: &to, Model: "glm-5.3"})
	if err != nil || timed.Total != 1 {
		t.Fatalf("time/model filter = %+v err=%v", timed, err)
	}
	tooNew := now.Add(time.Hour)
	emptyTime, err := store.ListRequestLogs(ctx, RequestLogFilter{From: &tooNew})
	if err != nil || emptyTime.Total != 0 {
		t.Fatalf("future from filter = %+v err=%v", emptyTime, err)
	}

	page, err := store.ListRequestLogs(ctx, RequestLogFilter{Limit: 1, Offset: 0})
	if err != nil || page.Total != 1 || page.Limit != 1 || page.Offset != 0 || len(page.Items) != 1 {
		t.Fatalf("page = %+v err=%v", page, err)
	}

	oldID := NewRequestID()
	if err := store.InsertRequestLog(ctx, RequestLog{
		ID: oldID, CreatedAt: now.Add(-10 * 24 * time.Hour), Status: RequestStatusError, RequestedModel: "old",
	}); err != nil {
		t.Fatal(err)
	}
	deleted, err := store.PurgeRequestLogs(ctx, 7*24*time.Hour, 20_000)
	if err != nil || deleted < 1 {
		t.Fatalf("purge deleted=%d err=%v", deleted, err)
	}
	if _, err := store.GetRequestLog(ctx, oldID); err != ErrRequestLogNotFound {
		t.Fatalf("expected old log gone, got %v", err)
	}

	cleared, err := store.ClearRequestLogs(ctx)
	if err != nil || cleared < 1 {
		t.Fatalf("clear = %d err=%v", cleared, err)
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
		if err := store.InsertRequestLog(ctx, RequestLog{
			ID: NewRequestID(), CreatedAt: base.Add(time.Duration(i) * time.Minute),
			Status: RequestStatusOK, RequestedModel: model, AccountID: account,
		}); err != nil {
			t.Fatal(err)
		}
	}

	page1, err := store.ListRequestLogs(ctx, RequestLogFilter{Limit: 2, Offset: 0})
	if err != nil || page1.Total != 5 || page1.Limit != 2 || page1.Offset != 0 || len(page1.Items) != 2 {
		t.Fatalf("page1 = %+v err=%v", page1, err)
	}
	page3, err := store.ListRequestLogs(ctx, RequestLogFilter{Limit: 2, Offset: 4})
	if err != nil || page3.Total != 5 || len(page3.Items) != 1 {
		t.Fatalf("page3 = %+v err=%v", page3, err)
	}

	from := base.Add(2 * time.Minute)
	to := base.Add(3 * time.Minute)
	window, err := store.ListRequestLogs(ctx, RequestLogFilter{From: &from, To: &to, Limit: 10})
	if err != nil || window.Total != 2 {
		t.Fatalf("window = %+v err=%v", window, err)
	}
	accountB, err := store.ListRequestLogs(ctx, RequestLogFilter{AccountID: "acc_b", Limit: 10})
	if err != nil || accountB.Total != 2 {
		t.Fatalf("account = %+v err=%v", accountB, err)
	}
	model, err := store.ListRequestLogs(ctx, RequestLogFilter{Model: "qwen3.7-plus", Limit: 10})
	if err != nil || model.Total != 2 {
		t.Fatalf("model = %+v err=%v", model, err)
	}
	exactID := page1.Items[0].ID
	byID, err := store.ListRequestLogs(ctx, RequestLogFilter{ID: exactID, Limit: 10})
	if err != nil || byID.Total != 1 || byID.Items[0].ID != exactID {
		t.Fatalf("id = %+v err=%v", byID, err)
	}
	prefix, err := store.ListRequestLogs(ctx, RequestLogFilter{Query: exactID[:8], Limit: 10})
	if err != nil || prefix.Total != 1 || prefix.Items[0].ID != exactID {
		t.Fatalf("id prefix = %+v err=%v", prefix, err)
	}
	noneQuery, err := store.ListRequestLogs(ctx, RequestLogFilter{Query: "glm-5.3", Limit: 10})
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
		if err := store.InsertRequestLog(ctx, RequestLog{
			ID: NewRequestID(), CreatedAt: base.Add(time.Duration(i) * time.Second),
			Status: RequestStatusOK, RequestedModel: "m",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.PurgeRequestLogs(ctx, 30*24*time.Hour, 3); err != nil {
		t.Fatal(err)
	}
	list, err := store.ListRequestLogs(ctx, RequestLogFilter{Limit: 10})
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
		log := RequestLog{
			ID: NewRequestID(), CreatedAt: at, Status: status, RequestedModel: model, AccountID: account,
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
	insert(base, RequestStatusOK, "glm-5.3", "acc_a", "qoder", "", 100, 12, 34, false)
	insert(base.Add(20*time.Minute), RequestStatusOK, "glm-5.3", "acc_a", "qoder", "", 200, 10, 20, true)
	insert(base.Add(90*time.Minute), RequestStatusError, "qwen3.7-plus", "acc_b", "workbuddy", KindRateLimit, 400, 8, 0, false)
	insert(base.Add(2*time.Hour), RequestStatusCanceled, "glm-5.3", "acc_a", "qoder", "", 0, 0, 0, false)
	insert(base.Add(-30*time.Hour), RequestStatusOK, "old", "acc_a", "qoder", "", 50, 1, 1, false)

	from := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 28, 13, 0, 0, 0, time.UTC)
	stats, err := store.SummarizeRequestLogs(ctx, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Totals.Requests != 4 || stats.Totals.OK != 2 || stats.Totals.Error != 1 || stats.Totals.Canceled != 1 || stats.Totals.Streaming != 1 {
		t.Fatalf("totals = %+v", stats.Totals)
	}
	if stats.Totals.SuccessRate != 0.5 {
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
	if len(stats.Errors) != 1 || stats.Errors[0].Key != KindRateLimit || stats.Errors[0].Count != 1 {
		t.Fatalf("errors = %+v", stats.Errors)
	}
	if len(stats.Models) == 0 || stats.Models[0].Key != "glm-5.3" || stats.Models[0].Count != 3 {
		t.Fatalf("models = %+v", stats.Models)
	}
	if len(stats.Accounts) == 0 || stats.Accounts[0].Key != "acc_a" || stats.Accounts[0].Count != 3 {
		t.Fatalf("accounts = %+v", stats.Accounts)
	}
	if len(stats.Providers) == 0 || stats.Providers[0].Key != "qoder" || stats.Providers[0].Count != 3 {
		t.Fatalf("providers = %+v", stats.Providers)
	}
	if len(stats.Series) != 3 {
		t.Fatalf("series len = %d %+v", len(stats.Series), stats.Series)
	}
	if stats.Series[0].Requests != 2 || stats.Series[1].Requests != 1 || stats.Series[2].Requests != 1 {
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
