package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

// seedExpiringLog writes one log with full detail: an attempt, a usage detail
// and a stream diagnostic, so expiry has something to clear on every child.
func seedExpiringLog(t *testing.T, store *Store, id string, at time.Time, credits float64) {
	t.Helper()
	ctx := context.Background()
	prompt, completion := 100, 20
	latency, ttfb := 900, 300
	if err := store.InsertRequestLog(ctx, accounts.RequestLog{
		ID: id, CreatedAt: at, FinishedAt: &at, Status: accounts.RequestStatusOK,
		RequestedModel: "glm-5.3", MappedModel: "glm-5.3", AccountID: "acc_a",
		APIKeyID: "key_a", Provider: "qoder", Routing: "pool",
		PromptTokens: &prompt, CompletionTokens: &completion,
		CacheReadTokens: &prompt, LatencyMs: &latency, TTFBMs: &ttfb,
		Credits: &credits, ErrorKind: accounts.KindRateLimit,
		ErrorCode: "429", ErrorMessage: "rate limited upstream",
		AttemptCount: 1, MessageCount: 3,
		MessageRoles:        []string{"system", "user"},
		EmptyMessageIndexes: []int{1},
		RequestedReasoning:  "high",
		ResolvedReasoning:   "high",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertRequestAttempt(ctx, accounts.RequestAttempt{
		ID: accounts.NewAttemptID(), RequestID: id, AttemptIndex: 0, AccountID: "acc_a",
		StartedAt: at, Status: accounts.AttemptStatusOK,
	}); err != nil {
		t.Fatal(err)
	}
	credit := credits
	if err := store.InsertRequestUsageDetail(ctx, accounts.RequestUsageDetail{
		RequestID: id, CreatedAt: at, Provider: "qoder", Credit: &credit, Unit: "credits",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertRequestStreamDiagnostic(ctx, accounts.RequestStreamDiagnostic{
		RequestID: id, CreatedAt: at, SSEEventCount: 4, BytesRead: 2048,
	}); err != nil {
		t.Fatal(err)
	}
}

func countRows(t *testing.T, store *Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := store.DB().QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

// The regression this guards: a single age-based DELETE used to remove the only
// copy of the usage history. Expiring the detail must keep every column the
// aggregates read, so the numbers do not move.
func TestExpireRequestLogDetailKeepsAggregatesExact(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().UTC()
	old := now.Add(-30 * 24 * time.Hour)
	seedExpiringLog(t, store, "req_old", old, 12.5)
	seedExpiringLog(t, store, "req_new", now.Add(-time.Hour), 3.25)

	from := old.Add(-time.Hour)
	to := now.Add(time.Hour)
	before, err := store.SummarizeRequestLogs(ctx, from, to)
	if err != nil {
		t.Fatal(err)
	}
	beforeCosts, err := store.SummarizeModelCosts(ctx, from, to, "")
	if err != nil {
		t.Fatal(err)
	}
	beforeUsage, err := store.SummarizeRequestUsage(ctx, from, to, "")
	if err != nil {
		t.Fatal(err)
	}

	expired, err := store.ExpireRequestLogDetail(ctx, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if expired != 1 {
		t.Fatalf("expired = %d, want 1 (only the 30-day-old log)", expired)
	}

	// Detail is gone for the old row only.
	for _, table := range requestLogChildTables {
		if left := countRows(t, store, `SELECT COUNT(*) FROM `+table+` WHERE request_id = ?`, "req_old"); left != 0 {
			t.Fatalf("%s kept %d detail rows for the expired log", table, left)
		}
		if left := countRows(t, store, `SELECT COUNT(*) FROM `+table+` WHERE request_id = ?`, "req_new"); left != 1 {
			t.Fatalf("%s lost detail of the in-window log: %d rows", table, left)
		}
	}

	// The row itself survives, flagged, with its detail-only text blanked.
	detail, err := store.GetRequestLog(ctx, "req_old")
	if err != nil {
		t.Fatal(err)
	}
	if !detail.DetailExpired {
		t.Fatal("expired log should be flagged detail_expired")
	}
	if detail.ErrorMessage != "" || detail.ErrorCode != "" || detail.Routing != "" {
		t.Fatalf("detail-only columns survived expiry: %+v", detail)
	}
	if detail.FinishedAt != nil {
		t.Fatalf("finished_at should be cleared, got %v", detail.FinishedAt)
	}
	if len(detail.Attempts) != 0 || detail.UsageDetail != nil || detail.StreamDiagnostic != nil {
		t.Fatal("child detail should be gone from the drill-down")
	}
	if detail.ErrorKind != accounts.KindRateLimit {
		t.Fatalf("error_kind must survive (error breakdown reads it), got %q", detail.ErrorKind)
	}

	// Aggregates are unchanged: this is the whole point of the change.
	after, err := store.SummarizeRequestLogs(ctx, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if after.Totals.Requests != before.Totals.Requests || after.Totals.OK != before.Totals.OK {
		t.Fatalf("totals moved: before %+v after %+v", before.Totals, after.Totals)
	}
	if after.Tokens != before.Tokens {
		t.Fatalf("tokens moved: before %+v after %+v", before.Tokens, after.Tokens)
	}
	if after.Totals.Credits != before.Totals.Credits {
		t.Fatalf("credits moved: before %v after %v", before.Totals.Credits, after.Totals.Credits)
	}
	if after.Latency.AvgMs == nil || before.Latency.AvgMs == nil || *after.Latency.AvgMs != *before.Latency.AvgMs {
		t.Fatalf("latency moved: before %+v after %+v", before.Latency, after.Latency)
	}
	if len(after.Errors) != len(before.Errors) || after.Errors[0].Count != before.Errors[0].Count {
		t.Fatalf("error breakdown moved: before %+v after %+v", before.Errors, after.Errors)
	}
	if len(after.Models) != len(before.Models) || after.Models[0].Credits != before.Models[0].Credits {
		t.Fatalf("model ranking moved: before %+v after %+v", before.Models, after.Models)
	}

	afterCosts, err := store.SummarizeModelCosts(ctx, from, to, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(afterCosts) != len(beforeCosts) {
		t.Fatalf("cost rows = %d, want %d", len(afterCosts), len(beforeCosts))
	}
	for i := range beforeCosts {
		if afterCosts[i].Credits != beforeCosts[i].Credits || afterCosts[i].Usage != beforeCosts[i].Usage {
			t.Fatalf("cost row %d moved: before %+v after %+v", i, beforeCosts[i], afterCosts[i])
		}
	}

	afterUsage, err := store.SummarizeRequestUsage(ctx, from, to, "")
	if err != nil {
		t.Fatal(err)
	}
	if afterUsage.Totals.Credits != beforeUsage.Totals.Credits || afterUsage.Totals.TotalTokens != beforeUsage.Totals.TotalTokens {
		t.Fatalf("usage totals moved: before %+v after %+v", beforeUsage.Totals, afterUsage.Totals)
	}
}

// Expiring twice must not double-count or re-touch rows already reduced.
func TestExpireRequestLogDetailIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	old := time.Now().UTC().Add(-30 * 24 * time.Hour)
	seedExpiringLog(t, store, "req_old", old, 7.5)

	if n, err := store.ExpireRequestLogDetail(ctx, 7*24*time.Hour); err != nil || n != 1 {
		t.Fatalf("first expiry = %d err=%v", n, err)
	}
	if n, err := store.ExpireRequestLogDetail(ctx, 7*24*time.Hour); err != nil || n != 0 {
		t.Fatalf("second expiry = %d err=%v, want 0", n, err)
	}
}

// The detail cap must not delete totals-only rows, otherwise raising the
// retention window still loses history as soon as traffic grows.
func TestPurgeRequestLogsCapIgnoresTotalsOnlyRows(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().UTC()
	// Three old rows that expiry will reduce to totals.
	for i := 0; i < 3; i++ {
		seedExpiringLog(t, store, accounts.NewRequestID(), now.Add(-40*24*time.Hour+time.Duration(i)*time.Minute), 1)
	}
	// Two recent rows that still carry detail.
	for i := 0; i < 2; i++ {
		seedExpiringLog(t, store, accounts.NewRequestID(), now.Add(-time.Duration(i+1)*time.Hour), 1)
	}
	if _, err := store.ExpireRequestLogDetail(ctx, 7*24*time.Hour); err != nil {
		t.Fatal(err)
	}

	// Cap of 2 applies to the two rows that still carry detail, so nothing is
	// deleted even though five rows exist.
	deleted, err := store.PurgeRequestLogs(ctx, 730*24*time.Hour, 2)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("cap deleted %d rows, want 0 (totals-only rows must not count)", deleted)
	}
	if total := countRows(t, store, `SELECT COUNT(*) FROM request_logs`); total != 5 {
		t.Fatalf("rows = %d, want 5", total)
	}

	// The age window is what deletes totals-only rows.
	deleted, err = store.PurgeRequestLogs(ctx, 30*24*time.Hour, 100)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 3 {
		t.Fatalf("age purge deleted %d rows, want the 3 totals-only rows", deleted)
	}
	if total := countRows(t, store, `SELECT COUNT(*) FROM request_logs`); total != 2 {
		t.Fatalf("rows after age purge = %d, want 2", total)
	}
}
