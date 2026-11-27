package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestReadyFollowsDrainingState(t *testing.T) {
	var draining atomic.Bool
	server := New(Server{Draining: &draining})

	ready := func() (*httptest.ResponseRecorder, map[string]any) {
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode ready body: %v", err)
		}
		return rec, body
	}

	rec, body := ready()
	if rec.Code != http.StatusOK || body["ok"] != true {
		t.Fatalf("ready before drain = %d %v", rec.Code, body)
	}

	server.SetDraining(true)
	rec, body = ready()
	if rec.Code != http.StatusServiceUnavailable || body["ok"] != false || body["draining"] != true {
		t.Fatalf("ready after drain = %d %v", rec.Code, body)
	}

	health := httptest.NewRecorder()
	server.Handler().ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health during drain = %d, want 200", health.Code)
	}
	var healthBody map[string]any
	if err := json.Unmarshal(health.Body.Bytes(), &healthBody); err != nil {
		t.Fatalf("decode health body: %v", err)
	}
	if healthBody["draining"] != true || healthBody["ok"] != true {
		t.Fatalf("health during drain = %v", healthBody)
	}
}

func TestHandlerRejectsNewWorkWhileDraining(t *testing.T) {
	var draining atomic.Bool
	server := New(Server{Draining: &draining})
	server.SetDraining(true)

	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status while draining = %d, want 503", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode drain body: %v", err)
	}
	errObj, _ := body["error"].(map[string]any)
	if errObj["code"] != "service_draining" {
		t.Fatalf("drain error = %v", body["error"])
	}
	if rec.Header().Get("Connection") != "close" {
		t.Fatalf("Connection header = %q, want close", rec.Header().Get("Connection"))
	}
}
