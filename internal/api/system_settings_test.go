package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/config"
)

func TestEnsureCrossProviderModelPoolDefaultsToEnabled(t *testing.T) {
	store, err := accounts.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	enabled, err := ensureCrossProviderModelPool(context.Background(), store)
	if err != nil || !enabled {
		t.Fatalf("enabled=%v err=%v", enabled, err)
	}
	value, ok, err := store.GetSecret(context.Background(), crossProviderModelPoolSecret)
	if err != nil || !ok || value != "1" {
		t.Fatalf("stored setting=%q ok=%v err=%v", value, ok, err)
	}
}

func TestSystemSettingsRoutePersistsAndAppliesModelPool(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(),
	})
	defer srv.Close()

	request := httptest.NewRequest(http.MethodGet, "/api/system/settings", nil)
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	srv.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"cross_provider_model_pool":true`)) {
		t.Fatalf("default settings: %d %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPatch, "/api/system/settings", bytes.NewBufferString(`{"cross_provider_model_pool":false}`))
	request.Header.Set("Authorization", "Bearer secret")
	response = httptest.NewRecorder()
	srv.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"cross_provider_model_pool":false`)) {
		t.Fatalf("updated settings: %d %s", response.Code, response.Body.String())
	}
	if srv.crossProviderModelPool.Load() {
		t.Fatal("runtime model pool setting remains enabled")
	}

	value, ok, err := srv.manager.Store().GetSecret(context.Background(), crossProviderModelPoolSecret)
	if err != nil || !ok || value != "0" {
		t.Fatalf("persisted setting=%q ok=%v err=%v", value, ok, err)
	}
}

func TestSystemSettingsRoutePersistsAndAppliesRoutingStrategy(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(),
	})
	defer srv.Close()

	request := httptest.NewRequest(http.MethodPatch, "/api/system/settings", bytes.NewBufferString(`{"routing_strategy":"fill-first"}`))
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	srv.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"routing_strategy":"fill-first"`)) {
		t.Fatalf("updated settings: %d %s", response.Code, response.Body.String())
	}
	if got := srv.pool.RoutingStrategy(); got != accounts.RoutingStrategyFillFirst {
		t.Fatalf("runtime strategy = %q", got)
	}
	value, ok, err := srv.manager.Store().GetSecret(context.Background(), routingStrategySecret)
	if err != nil || !ok || value != accounts.RoutingStrategyFillFirst {
		t.Fatalf("persisted strategy=%q ok=%v err=%v", value, ok, err)
	}
}

func TestEnsureWorkBuddyCheckinTimeDefaultsToNine(t *testing.T) {
	store, err := accounts.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	value, err := ensureWorkBuddyCheckinTime(context.Background(), store)
	if err != nil || value != accounts.DefaultWorkBuddyCheckinTime {
		t.Fatalf("value=%q err=%v", value, err)
	}
	stored, ok, err := store.GetSecret(context.Background(), accounts.WorkBuddyCheckinTimeSecret)
	if err != nil || !ok || stored != accounts.DefaultWorkBuddyCheckinTime {
		t.Fatalf("stored=%q ok=%v err=%v", stored, ok, err)
	}
}

func TestSystemSettingsRoutePersistsWorkBuddyCheckinTime(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(),
	})
	defer srv.Close()

	request := httptest.NewRequest(http.MethodGet, "/api/system/settings", nil)
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	srv.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"workbuddy_checkin_time":"09:00"`)) {
		t.Fatalf("default settings: %d %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPatch, "/api/system/settings", bytes.NewBufferString(`{"workbuddy_checkin_time":"18:30"}`))
	request.Header.Set("Authorization", "Bearer secret")
	response = httptest.NewRecorder()
	srv.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"workbuddy_checkin_time":"18:30"`)) {
		t.Fatalf("updated settings: %d %s", response.Code, response.Body.String())
	}
	stored, ok, err := srv.manager.Store().GetSecret(context.Background(), accounts.WorkBuddyCheckinTimeSecret)
	if err != nil || !ok || stored != "18:30" {
		t.Fatalf("persisted=%q ok=%v err=%v", stored, ok, err)
	}
}

func TestSystemSettingsRejectsInvalidWorkBuddyCheckinTime(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(),
	})
	defer srv.Close()

	request := httptest.NewRequest(http.MethodPatch, "/api/system/settings", bytes.NewBufferString(`{"workbuddy_checkin_time":"9:00"}`))
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	srv.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid time response: %d %s", response.Code, response.Body.String())
	}
	stored, ok, err := srv.manager.Store().GetSecret(context.Background(), accounts.WorkBuddyCheckinTimeSecret)
	if err != nil || !ok || stored != accounts.DefaultWorkBuddyCheckinTime {
		t.Fatalf("default overwritten after invalid patch: stored=%q ok=%v err=%v", stored, ok, err)
	}
}

func TestSystemSettingsRejectsInvalidStrategyWithoutPartialUpdate(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(),
	})
	defer srv.Close()

	request := httptest.NewRequest(http.MethodPatch, "/api/system/settings", bytes.NewBufferString(`{"cross_provider_model_pool":false,"routing_strategy":"invalid"}`))
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	srv.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid strategy response: %d %s", response.Code, response.Body.String())
	}
	if !srv.crossProviderModelPool.Load() {
		t.Fatal("invalid strategy request partially changed model pool setting")
	}
	value, ok, err := srv.manager.Store().GetSecret(context.Background(), crossProviderModelPoolSecret)
	if err != nil || !ok || value != "1" {
		t.Fatalf("cross-provider setting after invalid request=%q ok=%v err=%v", value, ok, err)
	}
}

func TestChatRejectsBareModelWhenCrossProviderPoolDisabled(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(),
	})
	defer srv.Close()
	srv.crossProviderModelPool.Store(false)

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"glm-5.2","messages":[{"role":"user","content":"hi"}]}`))
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	srv.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte(`"provider_prefix_required"`)) {
		t.Fatalf("bare model response: %d %s", response.Code, response.Body.String())
	}
}
