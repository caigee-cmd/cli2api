package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/config"
	"github.com/caigee-cmd/cli2api/internal/endpoint"
	control "github.com/caigee-cmd/cli2api/internal/update"
)

func newS01HTTPServer(t *testing.T) *Server {
	t.Helper()
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(), RuntimeDir: t.TempDir(),
	})
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

func serveS01(t *testing.T, srv *Server, method, path, body, key string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestHealthStaysOpenAndReportsMaintenance(t *testing.T) {
	srv := newS01HTTPServer(t)
	rec := serveS01(t, srv, http.MethodGet, endpoint.HealthPath, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("health without key: %d %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ok"] != true || payload["service"] != "cli2api" || payload["maintenance"] != false {
		t.Fatalf("health=%v", payload)
	}
	if payload["cross_provider_model_pool"] != true {
		t.Fatalf("cross_provider_model_pool=%v", payload["cross_provider_model_pool"])
	}
	providers, _ := payload["providers"].([]any)
	if len(providers) == 0 {
		t.Fatalf("providers=%v", payload["providers"])
	}

	srv.updater().Maintenance.Store(true)
	maintained := serveS01(t, srv, http.MethodGet, endpoint.HealthPath, "", "")
	if maintained.Code != http.StatusOK {
		t.Fatalf("health during maintenance: %d %s", maintained.Code, maintained.Body.String())
	}
	if !strings.Contains(maintained.Body.String(), `"maintenance":true`) {
		t.Fatalf("health maintenance flag missing: %s", maintained.Body.String())
	}
}

func TestUnauthorizedChatReturnsInvalidAPIKeyBody(t *testing.T) {
	srv := newS01HTTPServer(t)
	rec := serveS01(t, srv, http.MethodPost, endpoint.ChatCompletionsPath, `{"model":"glm-5.2","messages":[{"role":"user","content":"hi"}]}`, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"code":"invalid_api_key"`)) || !bytes.Contains(rec.Body.Bytes(), []byte(`"message":"Missing/invalid API key"`)) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestNamedAPIKeyCannotUseConsoleChat(t *testing.T) {
	srv := newS01HTTPServer(t)
	created, err := srv.Manager.Store().CreateAPIKey(context.Background(), accounts.CreateAPIKey{
		Name: "ci", Providers: []string{"qoder"}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := serveS01(t, srv, http.MethodPost, "/api/chat", `{"model":"glm-5.2","messages":[{"role":"user","content":"hi"}]}`, created.Secret)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"code":"console_key_required"`)) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestMaintenanceBlocksAPIAndV1ButKeepsUpdateAndHealth(t *testing.T) {
	srv := newS01HTTPServer(t)
	srv.updater().Maintenance.Store(true)
	srv.UpdateChecker = &updateCheckerStub{info: control.Info{CurrentVersion: "v0.5.7", Managed: true}}
	srv.UpdateAgent = &updateAgentStub{status: control.AgentStatus{Available: true, State: "idle"}}

	blocked := []struct{ method, path, body string }{
		{http.MethodGet, "/api/overview", ""},
		{http.MethodPost, endpoint.ChatCompletionsPath, `{"model":"glm-5.2","messages":[{"role":"user","content":"hi"}]}`},
		{http.MethodGet, endpoint.ModelsPath, ""},
	}
	for _, target := range blocked {
		rec := serveS01(t, srv, target.method, target.path, target.body, "secret")
		if rec.Code != http.StatusServiceUnavailable || !bytes.Contains(rec.Body.Bytes(), []byte(`"code":"service_updating"`)) {
			t.Fatalf("%s %s: %d %s", target.method, target.path, rec.Code, rec.Body.String())
		}
	}

	chat := httptest.NewRequest(http.MethodPost, endpoint.ChatCompletionsPath, strings.NewReader(`{"model":"glm-5.2","messages":[{"role":"user","content":"hi"}]}`))
	chat.Header.Set("Authorization", "Bearer secret")
	chat.Header.Set("Origin", "chrome-extension://example")
	chat.Header.Set("Content-Type", "application/json")
	chatRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(chatRec, chat)
	if chatRec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("maintenance chat missing CORS: %v", chatRec.Header())
	}

	options := httptest.NewRequest(http.MethodOptions, endpoint.ChatCompletionsPath, nil)
	options.Header.Set("Origin", "chrome-extension://example")
	options.Header.Set("Access-Control-Request-Headers", "authorization, x-api-key")
	optionsRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(optionsRec, options)
	if optionsRec.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS during maintenance: %d", optionsRec.Code)
	}
	if optionsRec.Header().Get("Access-Control-Allow-Headers") != "authorization, x-api-key" {
		t.Fatalf("allow-headers=%q", optionsRec.Header().Get("Access-Control-Allow-Headers"))
	}

	for _, path := range []string{endpoint.HealthPath, "/api/system/update"} {
		rec := serveS01(t, srv, http.MethodGet, path, "", "secret")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s during maintenance: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	login := serveS01(t, srv, http.MethodGet, "/login", "", "")
	if login.Code != http.StatusOK {
		t.Fatalf("SPA during maintenance: %d", login.Code)
	}
}

func TestUnsupportedChatMethodAndTrailingSlashStayOnServeMux(t *testing.T) {
	srv := newS01HTTPServer(t)
	get := serveS01(t, srv, http.MethodGet, endpoint.ChatCompletionsPath, "", "secret")
	if get.Code != http.StatusMethodNotAllowed || !bytes.Contains(get.Body.Bytes(), []byte(`"code":"method_not_allowed"`)) {
		t.Fatalf("GET chat: %d %s", get.Code, get.Body.String())
	}

	slash := serveS01(t, srv, http.MethodPost, endpoint.ChatCompletionsPath+"/", `{"model":"glm-5.2","messages":[{"role":"user","content":"hi"}]}`, "secret")
	if slash.Code != http.StatusNotFound {
		t.Fatalf("trailing slash: %d %s", slash.Code, slash.Body.String())
	}
	healthSlash := serveS01(t, srv, http.MethodGet, endpoint.HealthPath+"/", "", "")
	if healthSlash.Code != http.StatusNotFound {
		t.Fatalf("health slash: %d %s", healthSlash.Code, healthSlash.Body.String())
	}
	unknownAPI := serveS01(t, srv, http.MethodGet, "/api/does-not-exist", "", "secret")
	if unknownAPI.Code != http.StatusNotFound {
		t.Fatalf("unknown api: %d %s", unknownAPI.Code, unknownAPI.Body.String())
	}
}

func TestSPAAllowlistAndStaticAssetsDoNotExpandFallback(t *testing.T) {
	srv := newS01HTTPServer(t)
	for _, path := range []string{"/", "/providers", "/access", "/system", "/keys"} {
		rec := serveS01(t, srv, http.MethodGet, path, "", "")
		if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("%s: %d type=%q", path, rec.Code, rec.Header().Get("Content-Type"))
		}
		if !bytes.Contains(rec.Body.Bytes(), []byte("CLI2API")) {
			t.Fatalf("%s did not serve index", path)
		}
	}
	unknown := serveS01(t, srv, http.MethodGet, "/random-deep-link", "", "")
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown SPA path: %d", unknown.Code)
	}
	favicon := serveS01(t, srv, http.MethodGet, "/favicon.svg", "", "")
	if favicon.Code != http.StatusOK || !strings.Contains(favicon.Header().Get("Content-Type"), "image/svg") {
		t.Fatalf("favicon: %d type=%q", favicon.Code, favicon.Header().Get("Content-Type"))
	}
	manifest := serveS01(t, srv, http.MethodGet, "/site.webmanifest", "", "")
	if manifest.Code != http.StatusOK {
		t.Fatalf("manifest: %d %s", manifest.Code, manifest.Body.String())
	}
}

func TestMalformedJSONAndEmptyMessagesShareErrorPriority(t *testing.T) {
	srv := newS01HTTPServer(t)
	cases := []struct {
		path, body, wantCode, wantMessage string
		status                            int
	}{
		{endpoint.ChatCompletionsPath, `{`, "invalid_request", "unexpected EOF", http.StatusBadRequest},
		{endpoint.ChatCompletionsPath, `{"model":"glm-5.2","messages":[]}`, "invalid_request", "messages required", http.StatusBadRequest},
		{"/api/chat", `{"model":"glm-5.2","messages":[]}`, "invalid_request", "messages required", http.StatusBadRequest},
		{endpoint.ResponsesPath, `{`, "invalid_request", "unexpected EOF", http.StatusBadRequest},
		{endpoint.ResponsesPath, `{"model":"glm-5.2","input":[]}`, "invalid_request", "input required", http.StatusBadRequest},
	}
	for _, tc := range cases {
		rec := serveS01(t, srv, http.MethodPost, tc.path, tc.body, "secret")
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), `"code":"`+tc.wantCode+`"`) || !strings.Contains(rec.Body.String(), tc.wantMessage) {
			t.Fatalf("%s body=%q: %d %s", tc.path, tc.body, rec.Code, rec.Body.String())
		}
	}
	anthJSON := serveS01(t, srv, http.MethodPost, endpoint.MessagesPath, `{`, "secret")
	if anthJSON.Code != http.StatusBadRequest || !strings.Contains(anthJSON.Body.String(), `"type":"error"`) || !strings.Contains(anthJSON.Body.String(), "unexpected EOF") {
		t.Fatalf("anthropic malformed: %d %s", anthJSON.Code, anthJSON.Body.String())
	}
	anthEmpty := serveS01(t, srv, http.MethodPost, endpoint.MessagesPath, `{"model":"glm-5.2","max_tokens":16,"messages":[]}`, "secret")
	if anthEmpty.Code != http.StatusBadRequest || !strings.Contains(anthEmpty.Body.String(), "messages required") {
		t.Fatalf("anthropic empty: %d %s", anthEmpty.Code, anthEmpty.Body.String())
	}
}

func TestFailedNativeImportLeavesNoAccount(t *testing.T) {
	srv := newS01HTTPServer(t)
	rec := serveS01(t, srv, http.MethodPost, "/api/accounts/import", `{"format":"qoder-native-v1","name":"Broken","enabled":false,"user_blob":"Y2lwaGVy","machine_id":""}`, "secret")
	if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte(`"code":"account_import_failed"`)) {
		t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
	}
	accounts, err := srv.Manager.Store().List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 0 {
		t.Fatalf("leftover accounts=%+v", accounts)
	}
	if items := srv.Pool.Items(); len(items) != 0 {
		t.Fatalf("leftover pool=%+v", items)
	}
}

func TestNamedAPIKeyCannotImportAccounts(t *testing.T) {
	srv := newS01HTTPServer(t)
	created, err := srv.Manager.Store().CreateAPIKey(context.Background(), accounts.CreateAPIKey{
		Name: "ci", Providers: []string{"qoder"}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := serveS01(t, srv, http.MethodPost, "/api/accounts/import", `{"format":"qoder-native-v1","name":"Imported","enabled":false,"user_blob":"Y2lwaGVy","machine_id":"machine-1"}`, created.Secret)
	if rec.Code != http.StatusForbidden || !bytes.Contains(rec.Body.Bytes(), []byte(`"code":"console_key_required"`)) {
		t.Fatalf("named import: %d %s", rec.Code, rec.Body.String())
	}
}

func TestClearRequestLogsRequiresConsoleKey(t *testing.T) {
	srv := newS01HTTPServer(t)
	if err := srv.Recorder.Store().InsertRequestLog(context.Background(), accounts.RequestLog{
		ID: accounts.NewRequestID(), CreatedAt: time.Now().UTC(), Status: accounts.RequestStatusOK, RequestedModel: "glm-5.2",
	}); err != nil {
		t.Fatal(err)
	}
	created, err := srv.Manager.Store().CreateAPIKey(context.Background(), accounts.CreateAPIKey{
		Name: "ci", Providers: []string{"qoder"}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	denied := serveS01(t, srv, http.MethodDelete, "/api/logs/requests", "", created.Secret)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("named clear: %d %s", denied.Code, denied.Body.String())
	}
	cleared := serveS01(t, srv, http.MethodDelete, "/api/logs/requests", "", "secret")
	if cleared.Code != http.StatusOK || !bytes.Contains(cleared.Body.Bytes(), []byte(`"deleted"`)) {
		t.Fatalf("console clear: %d %s", cleared.Code, cleared.Body.String())
	}
}

func TestMaintenanceApplyConflictAndFailedAgentUnblock(t *testing.T) {
	srv := newS01HTTPServer(t)
	srv.UpdateChecker = &updateCheckerStub{info: control.Info{CurrentVersion: "v0.2.1", NextVersion: "v0.2.2", HasUpdate: true, Managed: true}}
	srv.UpdateAgent = &updateAgentStub{status: control.AgentStatus{
		Available: true, StagedUpdate: true, State: "ready_to_apply", JobID: "agent-job",
		CurrentVersion: "v0.2.1", TargetVersion: "v0.2.2",
	}}
	srv.updater().Running.Store(true)
	srv.updater().ReplaceJob(&systemUpdateJob{JobID: "update-1", AgentJobID: "agent-job", State: "ready_to_apply", CurrentVersion: "v0.2.1", TargetVersion: "v0.2.2"})

	apply := serveS01(t, srv, http.MethodPost, "/api/system/update/apply", `{}`, "secret")
	if apply.Code != http.StatusAccepted {
		t.Fatalf("apply: %d %s", apply.Code, apply.Body.String())
	}
	if !srv.updater().Maintenance.Load() {
		t.Fatal("apply should set maintenance")
	}
	conflict := serveS01(t, srv, http.MethodPost, "/api/system/update/prepare", "", "secret")
	if conflict.Code != http.StatusConflict || !bytes.Contains(conflict.Body.Bytes(), []byte(`"code":"update_in_progress"`)) {
		t.Fatalf("prepare conflict: %d %s", conflict.Code, conflict.Body.String())
	}

	srv.updater().Maintenance.Store(true)
	srv.updater().Running.Store(true)
	srv.updater().ReplaceJob(&systemUpdateJob{JobID: "update-2", AgentJobID: "agent-fail", State: "running"})
	srv.finishUpdateJob("update-2", "failed", "host apply failed", true)
	srv.updater().Maintenance.Store(false)
	srv.updater().Running.Store(false)
	job := srv.snapshotUpdateJob()
	if job == nil || job.State != "failed" || job.Error != "host apply failed" {
		t.Fatalf("failed agent job=%+v", job)
	}
	if srv.updater().Maintenance.Load() || srv.updater().Running.Load() {
		t.Fatal("failed agent should unblock traffic")
	}
	open := serveS01(t, srv, http.MethodGet, "/api/overview", "", "secret")
	if open.Code != http.StatusOK {
		t.Fatalf("overview after failed agent: %d %s", open.Code, open.Body.String())
	}
}
