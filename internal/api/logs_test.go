package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	base := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		account := "acc_a"
		model := "glm-5.3"
		stream := false
		if i%2 == 1 {
			account = "acc_b"
			model = "qwen3.7-plus"
			stream = true
		}
		if err := srv.recorder.Store().InsertRequestLog(ctx, accounts.RequestLog{
			ID: accounts.NewRequestID(), CreatedAt: base.Add(time.Duration(i) * time.Minute),
			Status: accounts.RequestStatusOK, RequestedModel: model, AccountID: account, Stream: stream,
		}); err != nil {
			t.Fatal(err)
		}
	}
	srv.ring.Append("[account=acc_b] warn line")
	srv.ring.Append("plain info")

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

	filtered := get("/api/logs/requests?account=acc_b&model=qwen3.7-plus&stream=1&from=2026-08-28T12:00:00Z&to=2026-08-28T12:04:00Z")
	if filtered.Code != http.StatusOK {
		t.Fatalf("filter status=%d body=%s", filtered.Code, filtered.Body.String())
	}
	if err := json.Unmarshal(filtered.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Total != 2 {
		t.Fatalf("filtered total=%d items=%+v", listed.Total, listed.Items)
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
		Count int `json:"count"`
	}
	if err := json.Unmarshal(runtime.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Count != 1 || snapshot.Items[0].AccountID != "acc_b" {
		t.Fatalf("runtime = %+v", snapshot)
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
