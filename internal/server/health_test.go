package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthReportsLogQueueCounters(t *testing.T) {
	server := New(Server{
		LogDrops:      func() uint64 { return 7 },
		LogQueueDepth: func() int { return 3 },
	})

	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode health body: %v", err)
	}
	if body["log_dropped"] != float64(7) || body["log_queue"] != float64(3) {
		t.Fatalf("health log counters = %v", body)
	}
}
