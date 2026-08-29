package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/config"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func TestManagementRoutesRequireAPIKey(t *testing.T) {
	srv := New(config.Config{
		Host:        "127.0.0.1",
		Port:        3010,
		ProxyAPIKey: "secret",
		QoderHome:   t.TempDir(),
	})
	h := srv.Handler()

	for _, path := range []string{
		"/api/overview",
		"/api/models",
		"/api/chat",
		"/api/accounts",
		"/api/logs/requests",
		"/api/logs/runtime",
		"/api/system/update",
		"/api/keys",
		"/api/system/console-key",
	} {
		method := http.MethodGet
		if path == "/api/chat" {
			method = http.MethodPost
		}
		req := httptest.NewRequest(method, path, bytes.NewReader([]byte("{}")))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without key: got %d want 401 body=%s", method, path, rec.Code, rec.Body.String())
		}
	}
}

func TestCanonicalModelIDNormalizesWithoutAliases(t *testing.T) {
	for input, want := range map[string]string{
		"MiniMax-M3":   "minimax-m3",
		"Qwen3.7-Plus": "qwen3.7-plus",
		"qmodel":       "qmodel",
		"GLM-5.2":      "glm-5.2",
	} {
		if got := canonicalModelID(input); got != want {
			t.Fatalf("canonicalModelID(%q) = %q, want %q", input, got, want)
		}
	}
	if modelContextKey("glm-5.2") == modelContextKey("qwen3.7-plus") {
		t.Fatal("GLM-5.2 and Qwen3.7-Plus must have independent context settings")
	}
}

func TestModelContextSettingsApplyToChatDefaults(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(),
	})
	defer srv.Close()
	if err := srv.manager.Store().SetModelContext(context.Background(), "minimax-m3", 500000); err != nil {
		t.Fatal(err)
	}
	req := translate.ChatRequest{Model: "MiniMax-M3"}
	if err := srv.applyModelContextDefaults(context.Background(), &req, "qoder"); err != nil {
		t.Fatal(err)
	}
	if string(req.ContextLength) != "500000" || string(req.MaxInputTokens) != "500000" {
		t.Fatalf("context=%s max_input=%s", req.ContextLength, req.MaxInputTokens)
	}

	explicit := translate.ChatRequest{
		Model:          "minimax-m3",
		ContextLength:  json.RawMessage("250000"),
		MaxInputTokens: json.RawMessage("900000"),
	}
	if err := srv.applyModelContextDefaults(context.Background(), &explicit, "qoder"); err != nil {
		t.Fatal(err)
	}
	if string(explicit.ContextLength) != "250000" || string(explicit.MaxInputTokens) != "900000" {
		t.Fatalf("explicit values overwritten: context=%s max_input=%s", explicit.ContextLength, explicit.MaxInputTokens)
	}
}

func TestModelContextSettingsAPI(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(),
	})
	defer srv.Close()

	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/models" {
			t.Fatalf("worker path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{
			"id": "minimax-m3", "display_name": "MiniMax-M3", "mapped_key": "mmodel",
		}}})
	}))
	defer worker.Close()
	srv.pool.Upsert(accounts.Item{ID: "test", URL: worker.URL})

	req := httptest.NewRequest(http.MethodPatch, "/api/models/minimax-m3", bytes.NewBufferString(`{"context_length":500000}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH model context: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/models", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"context_length":500000`)) {
		t.Fatalf("GET models: %d %s", rec.Code, rec.Body.String())
	}
}

func TestTraeMaxModeSettingDoesNotChangeQoderContext(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(),
	})
	defer srv.Close()
	if err := srv.manager.Store().SetModelContext(context.Background(), "glm-5.2", 250000); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/api/models/trae/glm-5.2", bytes.NewBufferString(`{"max_mode":true}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"max_mode":true`)) {
		t.Fatalf("PATCH trae max mode: %d %s", rec.Code, rec.Body.String())
	}

	got, ok, err := srv.manager.Store().GetModelContext(context.Background(), "glm-5.2")
	if err != nil || !ok || got != 250000 {
		t.Fatalf("qoder context mutated: %d %v %v", got, ok, err)
	}
	traeReq := translate.ChatRequest{Model: "glm-5.2"}
	if err := srv.applyModelContextDefaults(context.Background(), &traeReq, "trae"); err != nil {
		t.Fatal(err)
	}
	if len(traeReq.ContextLength) != 0 || len(traeReq.MaxInputTokens) != 0 {
		t.Fatalf("trae request received qoder context defaults: %+v", traeReq)
	}
}

type failingCatalog struct{}

func (failingCatalog) Models(context.Context, string) ([]providers.ModelInfo, error) {
	return nil, fmt.Errorf("models status=500: upstream html error page (internal server error)")
}

func TestModelsAPICatalogFailureUses503(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(),
	})
	defer srv.Close()
	srv.pool.Upsert(accounts.Item{ID: "wb-global", Provider: "workbuddy", Runtime: string(providers.RuntimeInProcess)})
	srv.providers.Register(providers.Adapter{ID: "workbuddy", Models: failingCatalog{}})

	req := httptest.NewRequest(http.MethodGet, "/api/models?account=wb-global", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("catalog failure status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Code == http.StatusBadGateway {
		t.Fatal("catalog failures must not use 502; reverse proxies replace that with HTML")
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"catalog_failed"`)) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestLegacyDiagnosticRoutesAreRemoved(t *testing.T) {
	srv := New(config.Config{
		Host:        "127.0.0.1",
		Port:        3010,
		ProxyAPIKey: "secret",
		QoderHome:   t.TempDir(),
	})

	for _, target := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/login/status"},
		{http.MethodPost, "/api/login/device"},
		{http.MethodPost, "/api/login/pat"},
		{http.MethodPost, "/api/rewarm"},
		{http.MethodGet, "/debug/auth-snapshot"},
		{http.MethodGet, "/debug/endpoints"},
	} {
		req := httptest.NewRequest(target.method, target.path, bytes.NewReader([]byte("{}")))
		req.Header.Set("Authorization", "Bearer secret")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s: got %d want 404 body=%s", target.method, target.path, rec.Code, rec.Body.String())
		}
	}
}

func TestSPAFallbackServesIndexForClientRoutes(t *testing.T) {
	srv := New(config.Config{
		Host:        "127.0.0.1",
		Port:        3010,
		ProxyAPIKey: "secret",
		QoderHome:   t.TempDir(),
	})
	req := httptest.NewRequest(http.MethodGet, "/auth", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /auth: got %d want 200", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if !bytes.Contains(body, []byte(`id="root"`)) && !bytes.Contains(body, []byte("CLI2API")) {
		t.Fatalf("GET /auth did not serve SPA index: %s", string(body[:min(200, len(body))]))
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("GET /auth content-type=%q", ct)
	}
}

func TestOverviewWithAPIKeyDoesNotLeakWorkerProxyFailureAs200Auth(t *testing.T) {
	srv := New(config.Config{
		Host:        "127.0.0.1",
		Port:        3010,
		ProxyAPIKey: "secret",
		QoderHome:   t.TempDir(),
	})
	req := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/overview with key: got %d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ok"] != true {
		t.Fatalf("overview ok=%v", payload["ok"])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestSPAFallbackServesAccounts(t *testing.T) {
	srv := New(config.Config{
		Host:        "127.0.0.1",
		Port:        3010,
		ProxyAPIKey: "secret",
		QoderHome:   t.TempDir(),
	})
	req := httptest.NewRequest(http.MethodGet, "/accounts", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /accounts: got %d want 200", rec.Code)
	}
}

func TestSPAFallbackServesLogin(t *testing.T) {
	srv := New(config.Config{
		Host:        "127.0.0.1",
		Port:        3010,
		ProxyAPIKey: "secret",
		QoderHome:   t.TempDir(),
	})
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /login: got %d want 200", rec.Code)
	}
}

func TestSPAFallbackServesLogs(t *testing.T) {
	srv := New(config.Config{
		Host:        "127.0.0.1",
		Port:        3010,
		ProxyAPIKey: "secret",
		QoderHome:   t.TempDir(),
	})
	defer srv.Close()
	req := httptest.NewRequest(http.MethodGet, "/logs", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /logs: got %d want 200", rec.Code)
	}
}

func TestAccountsAPICreatesAndListsDisabledAccount(t *testing.T) {
	dataDir := t.TempDir()
	srv := New(config.Config{
		Host:        "127.0.0.1",
		Port:        3010,
		ProxyAPIKey: "secret",
		QoderHome:   t.TempDir(),
		DataDir:     dataDir,
	})
	body := bytes.NewBufferString(`{"name":"Work","enabled":false,"max_inflight":5,"drop_system_prompt":false}`)
	req := httptest.NewRequest(http.MethodPost, "/api/accounts", body)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/accounts: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/accounts: %d %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data []struct {
			Name             string `json:"name"`
			Enabled          bool   `json:"enabled"`
			MaxInFlight      int    `json:"max_inflight"`
			DropSystemPrompt bool   `json:"drop_system_prompt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 1 || payload.Data[0].Name != "Work" || payload.Data[0].Enabled || payload.Data[0].MaxInFlight != 5 || payload.Data[0].DropSystemPrompt {
		t.Fatalf("accounts payload = %+v", payload.Data)
	}
}

func TestAccountsAPIImportsNativeCredential(t *testing.T) {
	srv := New(config.Config{
		Host:        "127.0.0.1",
		Port:        3010,
		ProxyAPIKey: "secret",
		QoderHome:   t.TempDir(),
		DataDir:     t.TempDir(),
	})
	body := bytes.NewBufferString(`{"format":"qoder-native-v1","name":"Imported","enabled":false,"user_blob":"Y2lwaGVy","machine_id":"machine-1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/accounts/import", body)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/accounts/import: %d %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"auth_type":"native"`)) {
		t.Fatalf("import response = %s", rec.Body.String())
	}
}

func TestAccountsAPIUpdatesExportsAndDeletesAccount(t *testing.T) {
	srv := New(config.Config{
		Host:        "127.0.0.1",
		Port:        3010,
		ProxyAPIKey: "secret",
		QoderHome:   t.TempDir(),
		DataDir:     t.TempDir(),
	})
	importBody := bytes.NewBufferString(`{"format":"qoder-native-v1","name":"Imported","enabled":false,"user_blob":"Y2lwaGVy","machine_id":"machine-1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/accounts/import", importBody)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	patchBody := bytes.NewBufferString(`{"name":"Renamed","max_inflight":8}`)
	req = httptest.NewRequest(http.MethodPatch, "/api/accounts/"+created.ID, patchBody)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"name":"Renamed"`)) {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/accounts/"+created.ID+"/export", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"format":"qoder-native-v1"`)) || !bytes.Contains(rec.Body.Bytes(), []byte(`"user_blob":"Y2lwaGVy"`)) || !bytes.Contains(rec.Body.Bytes(), []byte(`"provider":"qoder"`)) || !bytes.Contains(rec.Body.Bytes(), []byte(`"region":"global"`)) {
		t.Fatalf("export: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/accounts/"+created.ID, nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
}

func TestNamedAPIKeyCannotManageConsoleOrKeys(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(),
	})
	defer srv.Close()
	created, err := srv.manager.Store().CreateAPIKey(context.Background(), accounts.CreateAPIKey{
		Name: "ci", Providers: []string{"qoder"}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/keys", nil)
	req.Header.Set("Authorization", "Bearer "+created.Secret)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("named key GET /api/keys = %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/overview", nil)
	req.Header.Set("Authorization", "Bearer "+created.Secret)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("named key GET /api/overview = %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+created.Secret)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("named key GET /v1/models = %d %s", rec.Code, rec.Body.String())
	}
}

func TestAPIKeysCRUDAndConsoleKeyPrefix(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(),
	})
	defer srv.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/system/console-key", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"prefix"`)) {
		t.Fatalf("console key: %d %s", rec.Code, rec.Body.String())
	}

	body := bytes.NewBufferString(`{"name":"CI","providers":["qoder"]}`)
	req = httptest.NewRequest(http.MethodPost, "/api/keys", body)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || !bytes.Contains(rec.Body.Bytes(), []byte(`"secret"`)) {
		t.Fatalf("create key: %d %s", rec.Code, rec.Body.String())
	}
	var created accounts.APIKey
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPatch, "/api/keys/"+created.ID, bytes.NewBufferString(`{"name":"CI bot"}`))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"name":"CI bot"`)) {
		t.Fatalf("patch key: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, "/api/keys/"+created.ID, nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete key: %d %s", rec.Code, rec.Body.String())
	}
}
