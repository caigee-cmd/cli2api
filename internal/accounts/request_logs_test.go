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
	parentID := NewRequestID()
	prompt, completion := 12, 34
	if err := store.InsertRequestLog(ctx, RequestLog{
		ID: parentID, CreatedAt: now, Stream: false, Status: RequestStatusStarted,
		RequestedModel: "glm-5.3", AccountID: "acc_a", AttemptCount: 0,
	}); err != nil {
		t.Fatal(err)
	}
	finished := now.Add(120 * time.Millisecond)
	latency := 120
	if err := store.UpdateRequestLog(ctx, RequestLog{
		ID: parentID, CreatedAt: now, FinishedAt: &finished, Stream: false, Status: RequestStatusOK,
		RequestedModel: "glm-5.3", AccountID: "acc_b", PromptTokens: &prompt, CompletionTokens: &completion,
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
	if list.Total != 1 || len(list.Items) != 1 || list.Items[0].Status != RequestStatusOK {
		t.Fatalf("list = %+v", list)
	}

	got, err := store.GetRequestLog(ctx, parentID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccountID != "acc_b" || len(got.Attempts) != 2 || got.Attempts[0].Status != AttemptStatusFailover {
		t.Fatalf("detail = %+v", got)
	}

	filtered, err := store.ListRequestLogs(ctx, RequestLogFilter{AccountID: "acc_b", Status: RequestStatusOK})
	if err != nil || filtered.Total != 1 {
		t.Fatalf("filtered = %+v err=%v", filtered, err)
	}
	none, err := store.ListRequestLogs(ctx, RequestLogFilter{ErrorKind: KindQuota})
	if err != nil || none.Total != 0 {
		t.Fatalf("error filter = %+v err=%v", none, err)
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
