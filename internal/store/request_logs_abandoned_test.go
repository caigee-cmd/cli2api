package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func TestCloseAbandonedRequestLogs(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().UTC()
	open := []accounts.RequestLog{
		{ID: "req-open-start", CreatedAt: now, Status: accounts.RequestStatusStarted},
		{ID: "req-open-stream", CreatedAt: now, Status: accounts.RequestStatusStreaming, Stream: true},
	}
	for _, log := range open {
		if err := store.InsertRequestLog(ctx, log); err != nil {
			t.Fatal(err)
		}
	}
	finished := now.Add(time.Second)
	if err := store.UpdateRequestLog(ctx, accounts.RequestLog{
		ID: "req-open-stream", CreatedAt: now, FinishedAt: &finished,
		Status: accounts.RequestStatusOK, Stream: true,
	}); err != nil {
		t.Fatal(err)
	}

	closed, err := store.CloseAbandonedRequestLogs(ctx, now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("close abandoned: %v", err)
	}
	if closed != 1 {
		t.Fatalf("closed = %d, want 1 (only the row still running)", closed)
	}

	row, err := store.GetRequestLog(ctx, "req-open-start")
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != accounts.RequestStatusError {
		t.Fatalf("status = %q, want error", row.Status)
	}
	if row.ErrorCode != "stream_abandoned" {
		t.Fatalf("error_code = %q, want stream_abandoned", row.ErrorCode)
	}
	if row.FinishedAt == nil {
		t.Fatal("abandoned row must carry finished_at")
	}

	done, err := store.GetRequestLog(ctx, "req-open-stream")
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != accounts.RequestStatusOK {
		t.Fatalf("completed row was rewritten: %q", done.Status)
	}
}
