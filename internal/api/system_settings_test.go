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
