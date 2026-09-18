package qoder

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/endpoint"
	"github.com/caigee-cmd/cli2api/internal/logs"
)

func TestWorkerClientHealthModelsQuotaAndAdmin(t *testing.T) {
	var modelsRefresh string
	var quotaRefresh string
	var adminAuth string
	var adminAccount string
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "ready": true, "hot": true, "uid": "u1", "inFlight": 2, "hasAuthManager": true,
			})
		case "/admin/models":
			modelsRefresh = r.URL.Query().Get("refresh")
			if got := r.Header.Get("Authorization"); got != "Bearer proxy-key" {
				t.Errorf("models auth = %q", got)
			}
			if got := r.Header.Get("X-Qoder-Account"); got != "acc-1" {
				t.Errorf("models account = %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{"id": "glm-5.2", "native_model": "glm-5"}},
			})
		case "/admin/quota":
			quotaRefresh = r.URL.Query().Get("refresh")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"quota": map[string]any{
					"userQuota":       map[string]any{"total": 100.0, "used": 10.0, "remaining": 90.0, "percentage": 10.0, "unit": "credits"},
					"isQuotaExceeded": false,
					"fetchedAt":       "now",
				},
			})
		case "/admin/login/device":
			adminAuth = r.Header.Get("Authorization")
			adminAccount = r.Header.Get("X-Qoder-Account")
			w.Header().Set("X-Worker", "ok")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer worker.Close()

	client := WorkerClient{HTTP: worker.Client(), ProxyAPIKey: "proxy-key", AccountID: "acc-1"}
	health, status, err := client.Health(context.Background(), worker.URL)
	if err != nil || status != 200 || !health.OK || !health.Ready || health.UID != "u1" {
		t.Fatalf("health status=%d err=%v health=%+v", status, err, health)
	}

	models, status, _, err := client.Models(context.Background(), worker.URL, true)
	if err != nil || status != 200 || len(models) != 1 || models[0]["id"] != "glm-5.2" {
		t.Fatalf("models status=%d err=%v models=%v", status, err, models)
	}
	if modelsRefresh != "1" {
		t.Fatalf("models refresh = %q", modelsRefresh)
	}
	ids := CatalogIDs(models, nil)
	if strings.Join(ids, ",") != "glm-5.2,glm-5" {
		t.Fatalf("catalog ids = %v", ids)
	}

	quota, err := client.Quota(context.Background(), worker.URL, true)
	if err != nil || quota == nil || quota.Remaining != 90 || quota.Exceeded {
		t.Fatalf("quota err=%v quota=%+v", err, quota)
	}
	if quotaRefresh != "1" {
		t.Fatalf("quota refresh = %q", quotaRefresh)
	}

	status, header, body, err := client.Admin(context.Background(), worker.URL, http.MethodPost, "/admin/login/device", "application/json", []byte(`{"x":1}`))
	if err != nil || status != 200 || header.Get("X-Worker") != "ok" || !strings.Contains(string(body), `"ok":true`) {
		t.Fatalf("admin status=%d err=%v header=%v body=%s", status, err, header, body)
	}
	if adminAuth != "Bearer proxy-key" || adminAccount != "acc-1" {
		t.Fatalf("admin auth=%q account=%q", adminAuth, adminAccount)
	}
}

func TestWorkerClientHealthTransportAndDecode(t *testing.T) {
	client := WorkerClient{HTTP: &http.Client{Timeout: 50 * time.Millisecond}}
	_, status, err := client.Health(context.Background(), "http://127.0.0.1:1")
	if status != 0 {
		t.Fatalf("transport status = %d", status)
	}
	var transport TransportError
	if !errors.As(err, &transport) {
		t.Fatalf("transport err = %v", err)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "not-json")
	}))
	defer bad.Close()
	_, status, err = WorkerClient{HTTP: bad.Client()}.Health(context.Background(), bad.URL)
	if err == nil || status != 200 {
		t.Fatalf("decode status=%d err=%v", status, err)
	}
}

func TestNewChatRequestSetsWorkerHeaders(t *testing.T) {
	req, err := NewChatRequest(context.Background(), "http://127.0.0.1:32100", "acc-1", "req-9", "worker-key", []byte(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.Path != endpoint.ChatCompletionsPath {
		t.Fatalf("path = %s", req.URL.Path)
	}
	if req.Header.Get("Authorization") != "Bearer worker-key" {
		t.Fatalf("auth = %q", req.Header.Get("Authorization"))
	}
	if req.Header.Get("X-Qoder-Account") != "acc-1" {
		t.Fatalf("account = %q", req.Header.Get("X-Qoder-Account"))
	}
	if req.Header.Get("X-Request-Id") != "req-9" {
		t.Fatalf("request id = %q", req.Header.Get("X-Request-Id"))
	}
}

func TestLoginCompleteAuthType(t *testing.T) {
	if got := LoginCompleteAuthType("", []byte(`{"login":{"status":"ok"}}`)); got != "" {
		t.Fatalf("empty sync = %q", got)
	}
	if got := LoginCompleteAuthType("pat", []byte(`{}`)); got != "pat" {
		t.Fatalf("pat = %q", got)
	}
	if got := LoginCompleteAuthType("oauth_if_complete", []byte(`{"login":{"status":"pending"}}`)); got != "" {
		t.Fatalf("pending = %q", got)
	}
	if got := LoginCompleteAuthType("oauth_if_complete", []byte(`{"login":{"status":"ok"}}`)); got != "oauth" {
		t.Fatalf("complete = %q", got)
	}
}

func TestWaitForAuthManagerRetriesUntilReady(t *testing.T) {
	var hits atomic.Int32
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		n := hits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "hasAuthManager": n >= 3})
	}))
	defer worker.Close()

	got, err := WaitForAuthManager(context.Background(), func() (string, bool) {
		return worker.URL, true
	}, time.Second, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if got != worker.URL {
		t.Fatalf("url = %s", got)
	}
	if hits.Load() < 3 {
		t.Fatalf("hits = %d", hits.Load())
	}
}

func TestWaitForAuthManagerTimesOutWhileConnecting(t *testing.T) {
	_, err := WaitForAuthManager(context.Background(), func() (string, bool) {
		return "http://127.0.0.1:1", true
	}, 40*time.Millisecond, 10*time.Millisecond)
	if !errors.Is(err, ErrWorkerNotWarm) {
		t.Fatalf("err = %v", err)
	}
}

func TestRuntimeSpecSelectsCNCLIAndConfigDir(t *testing.T) {
	global, err := RuntimeSpec("/opt/qodercli.js", "/opt/qoderclicn.js", "global", "/run/acc-g")
	if err != nil {
		t.Fatal(err)
	}
	if global.CLIPath != "/opt/qodercli.js" || global.Site != "global" || global.ConfigDir != "/run/acc-g/.qoder" || global.ConfigEnv != "QODER_CONFIG_DIR" {
		t.Fatalf("global spec = %+v", global)
	}
	cn, err := RuntimeSpec("/opt/qodercli.js", "/opt/qoderclicn.js", "cn", "/run/acc-c")
	if err != nil {
		t.Fatal(err)
	}
	if cn.CLIPath != "/opt/qoderclicn.js" || cn.Site != "cn" || cn.ConfigDir != "/run/acc-c/.qoder-cn" || cn.ConfigEnv != "QODERCN_CONFIG_DIR" {
		t.Fatalf("cn spec = %+v", cn)
	}
	if _, err := RuntimeSpec("/opt/qodercli.js", "", "cn", "/run/acc-c"); err == nil {
		t.Fatal("expected missing CN CLI path to fail")
	}
}

func TestStarterEnvSetsRegionAndProxy(t *testing.T) {
	env, err := StarterEnv(StarterConfig{
		DaemonPath:   "/app/worker/daemon.mjs",
		QoderCLIPath: "/usr/lib/qodercli.js",
		ProxyAPIKey:  "k",
		ProxyURL:     "http://global.example:8080",
	}, accounts.Account{ID: "acc1", Provider: "qoder", ProviderRegion: "global", MaxInFlight: 4}, "/tmp/home", 32100)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	if values["QODER_SITE"] != "global" || values["QODER_HOME"] != "/tmp/home/.qoder" || values["QODERCLI_JS"] != "/usr/lib/qodercli.js" {
		t.Fatalf("env = %+v", values)
	}
	if values["PROXY_API_KEY"] != "k" || values["WORKER_PORT"] != "32100" {
		t.Fatalf("worker env = %+v", values)
	}
	if values["QODER_PROXY_URL"] != "http://global.example:8080" {
		t.Fatalf("proxy = %q", values["QODER_PROXY_URL"])
	}

	cnEnv, err := StarterEnv(StarterConfig{
		DaemonPath:     "/app/worker/daemon.mjs",
		QoderCLIPath:   "/usr/lib/qodercli.js",
		QoderCNCLIPath: "/usr/lib/qoderclicn.js",
	}, accounts.Account{ID: "acc-cn", Provider: "qoder", ProviderRegion: "cn", MaxInFlight: 2}, "/tmp/cn", 32101)
	if err != nil {
		t.Fatal(err)
	}
	cnValues := map[string]string{}
	for _, item := range cnEnv {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			cnValues[key] = value
		}
	}
	if cnValues["QODER_SITE"] != "cn" || cnValues["QODERCN_CONFIG_DIR"] != "/tmp/cn/.qoder-cn" || cnValues["QODERCLI_JS"] != "/usr/lib/qoderclicn.js" {
		t.Fatalf("cn env = %+v", cnValues)
	}
}

func TestPrefixWriterAddsAccountPrefix(t *testing.T) {
	ring := logs.NewRing(10)
	writer := &prefixLogWriter{prefix: "[account=acc_x] ", next: ring}
	if _, err := writer.Write([]byte("hello\nworld")); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	entries := ring.Latest(10)
	if len(entries) != 2 {
		t.Fatalf("entries=%+v", entries)
	}
	if entries[1].AccountID != "acc_x" || !strings.Contains(entries[1].Message, "[account=acc_x] hello") {
		t.Fatalf("first=%+v", entries[1])
	}
	if entries[0].AccountID != "acc_x" {
		t.Fatalf("second=%+v", entries[0])
	}
}
