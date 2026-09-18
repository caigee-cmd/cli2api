package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/config"
	"github.com/caigee-cmd/cli2api/internal/executor"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

func TestWaitForWorkerAuthManagerRetriesUntilReady(t *testing.T) {
	var hits atomic.Int32
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		n := hits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "hasAuthManager": n >= 3,
		})
	}))
	defer worker.Close()

	got, err := waitForWorkerAuthManager(context.Background(), func() (string, bool) {
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

func TestWaitForWorkerAuthManagerTimesOutWhileConnecting(t *testing.T) {
	_, err := waitForWorkerAuthManager(context.Background(), func() (string, bool) {
		return "http://127.0.0.1:1", true
	}, 40*time.Millisecond, 10*time.Millisecond)
	if !errors.Is(err, errWorkerNotWarm) {
		t.Fatalf("err = %v", err)
	}
}

func TestDeviceLoginMissingAccountIsConflict(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(), RuntimeDir: t.TempDir(),
		WorkerDaemonPath: "/dev/null",
	})
	t.Cleanup(func() { _ = srv.Close() })

	req := httptest.NewRequest(http.MethodPost, "/api/accounts/missing/login/device", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDeviceLoginWaitsForAuthManagerThenOpens(t *testing.T) {
	var healthHits atomic.Int32
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			n := healthHits.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "hasAuthManager": n >= 2,
			})
		case "/admin/login/device":
			if healthHits.Load() < 2 {
				t.Fatal("login reached worker before AuthManager was ready")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "status": "pending", "authUrl": "https://qoder.com.cn/device",
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer worker.Close()

	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(), RuntimeDir: t.TempDir(),
		WorkerDaemonPath: "/dev/null",
	})
	t.Cleanup(func() { _ = srv.Close() })
	srv.pool.Upsert(executor.Item{ID: "acc-cn", URL: worker.URL, Provider: "qoder", Region: "cn"})

	req := httptest.NewRequest(http.MethodPost, "/api/accounts/acc-cn/login/device", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		AuthURL string `json:"authUrl"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.AuthURL != "https://qoder.com.cn/device" {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

// An explicit account that is not in the pool must not fall back to the
// first running account. Without this guard, GET /v1/models?account=missing
// returns another account's catalog, misleading the client and routing
// subsequent requests to the wrong account.
func TestFetchWorkerModelsForNotFoundAccount(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(), RuntimeDir: t.TempDir(),
		WorkerDaemonPath: "/dev/null",
	})
	t.Cleanup(func() { _ = srv.Close() })
	// A running account that must never receive the query.
	srv.pool.Upsert(executor.Item{ID: "real-acc", URL: "http://127.0.0.1:1", Provider: "qoder", Region: "global", Runtime: "child_process"})

	_, err := srv.fetchWorkerModelsFor(false, "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent account")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
}

// A WorkBuddy alias shares its native model with the base entry but exposes a
// distinct public model ID. Deduping the merged catalog on the native ID would
// drop the alias; keying on the public ID keeps both the native model and the
// alias visible to clients.
func TestFetchProviderModelsKeepsAliasWithSharedNativeModel(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(),
	})
	defer srv.Close()
	srv.pool.Upsert(executor.Item{ID: "wb-cn", Provider: "workbuddy", Runtime: string(providers.RuntimeInProcess)})
	srv.providers.Register(providers.Adapter{ID: "workbuddy", Models: &countingCatalog{models: []providers.ModelInfo{
		{NativeModel: "deep-model", PublicModel: "deep-model", DisplayName: "Deep"},
		{NativeModel: "deep-model", PublicModel: "deepseek-v4.1-flash", DisplayName: "Deepseek-V4.1-Flash"},
	}}})

	models, err := srv.fetchWorkerModelsFor(false, "wb-cn")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, model := range models {
		if id, _ := model["id"].(string); id != "" {
			ids[id] = true
		}
	}
	if !ids["deep-model"] {
		t.Fatalf("native model missing from catalog: %v", ids)
	}
	if !ids["deepseek-v4.1-flash"] {
		t.Fatalf("alias dropped from catalog: %v", ids)
	}
}

func TestFetchProviderModelsExposesCreditsAndFree(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(),
	})
	defer srv.Close()
	srv.pool.Upsert(executor.Item{ID: "wb-global", Provider: "workbuddy", Runtime: string(providers.RuntimeInProcess)})
	srv.providers.Register(providers.Adapter{ID: "workbuddy", Models: &countingCatalog{models: []providers.ModelInfo{
		{NativeModel: "deepseek-v4.1-flash", PublicModel: "deepseek-v4.1-flash", DisplayName: "Deepseek", Credits: "x0.00", Free: true},
		{NativeModel: "glm-5.3", PublicModel: "glm-5.3", DisplayName: "GLM", Credits: "x0.79"},
		{NativeModel: "default-model", PublicModel: "default-model", DisplayName: "Auto"},
	}}})

	models, err := srv.fetchWorkerModelsFor(false, "wb-global")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]map[string]any{}
	for _, model := range models {
		id, _ := model["id"].(string)
		byID[id] = model
	}
	if byID["deepseek-v4.1-flash"]["credits"] != "x0.00" || byID["deepseek-v4.1-flash"]["free"] != true {
		t.Fatalf("free entry=%v", byID["deepseek-v4.1-flash"])
	}
	if byID["glm-5.3"]["credits"] != "x0.79" {
		t.Fatalf("paid credits=%v", byID["glm-5.3"]["credits"])
	}
	if _, ok := byID["glm-5.3"]["free"]; ok {
		t.Fatalf("paid model must omit free: %v", byID["glm-5.3"])
	}
	if _, ok := byID["default-model"]["credits"]; ok {
		t.Fatalf("blank credits must stay omitted: %v", byID["default-model"])
	}
	if _, ok := byID["default-model"]["free"]; ok {
		t.Fatalf("blank credits must not invent free: %v", byID["default-model"])
	}
}

// Console /api/models expands one row per provider+region+model so each
// region's official credits/free stay intact. /v1/models keeps merging.
func TestFetchProviderModelsExpandKeepsPerRegionCredits(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(),
	})
	defer srv.Close()
	srv.pool.Upsert(executor.Item{ID: "wb-cn", Provider: "workbuddy", Region: "cn", Runtime: string(providers.RuntimeInProcess)})
	srv.pool.Upsert(executor.Item{ID: "wb-global", Provider: "workbuddy", Region: "global", Runtime: string(providers.RuntimeInProcess)})
	srv.providers.Register(providers.Adapter{ID: "workbuddy", Models: &regionCatalog{
		byAccount: map[string][]providers.ModelInfo{
			"wb-cn": {{
				NativeModel: "deepseek-v4-pro", PublicModel: "deepseek-v4-pro", DisplayName: "DeepSeek V4 Pro",
				Credits: "x0.79",
			}},
			"wb-global": {{
				NativeModel: "deepseek-v4-pro", PublicModel: "deepseek-v4-pro", DisplayName: "DeepSeek V4 Pro",
				Credits: "x0.00", Free: true,
			}},
		},
	}})

	expanded, err := srv.fetchWorkerModelsForMode(false, "", catalogModeExpand)
	if err != nil {
		t.Fatal(err)
	}
	byRegion := map[string]map[string]any{}
	for _, model := range expanded {
		if id, _ := model["id"].(string); id != "deepseek-v4-pro" {
			continue
		}
		region, _ := model["region"].(string)
		byRegion[region] = model
	}
	if len(byRegion) != 2 {
		t.Fatalf("expand rows=%v", byRegion)
	}
	if byRegion["cn"]["credits"] != "x0.79" {
		t.Fatalf("cn credits=%v", byRegion["cn"])
	}
	if _, ok := byRegion["cn"]["free"]; ok {
		t.Fatalf("cn must omit free: %v", byRegion["cn"])
	}
	if byRegion["global"]["credits"] != "x0.00" || byRegion["global"]["free"] != true {
		t.Fatalf("global free entry=%v", byRegion["global"])
	}
	if _, ok := byRegion["cn"]["regions"]; ok {
		t.Fatalf("expand rows must use singular region, got regions: %v", byRegion["cn"])
	}

	merged, err := srv.fetchWorkerModelsForMode(false, "", catalogModeMerge)
	if err != nil {
		t.Fatal(err)
	}
	var mergedEntry map[string]any
	for _, model := range merged {
		if id, _ := model["id"].(string); id == "deepseek-v4-pro" {
			mergedEntry = model
			break
		}
	}
	if mergedEntry == nil {
		t.Fatal("merged catalog missing deepseek-v4-pro")
	}
	regions := entryModelRegions(mergedEntry)
	if len(regions) != 2 {
		t.Fatalf("merged regions=%v", regions)
	}
	seen := map[string]bool{}
	for _, region := range regions {
		seen[region] = true
	}
	if !seen["cn"] || !seen["global"] {
		t.Fatalf("merged regions=%v", regions)
	}
	if _, ok := mergedEntry["region"]; ok {
		t.Fatalf("merged entry must not stamp singular region: %v", mergedEntry)
	}
	if _, ok := mergedEntry["credits"]; ok {
		t.Fatalf("conflicting regional credits must be omitted from merge: %v", mergedEntry)
	}
	if _, ok := mergedEntry["free"]; ok {
		t.Fatalf("conflicting free flags must be omitted from merge: %v", mergedEntry)
	}
}

func TestFetchProviderModelsExpandStampsQoderRegion(t *testing.T) {
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/models" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "glm-5.2", "object": "model"}},
		})
	}))
	defer worker.Close()

	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(), WorkerDaemonPath: "/dev/null",
	})
	defer srv.Close()
	srv.pool.Upsert(executor.Item{ID: "q-cn", URL: worker.URL, Provider: "qoder", Region: "cn", Runtime: "child_process"})

	expanded, err := srv.fetchWorkerModelsForMode(false, "", catalogModeExpand)
	if err != nil {
		t.Fatal(err)
	}
	if len(expanded) != 1 {
		t.Fatalf("expanded=%v", expanded)
	}
	if expanded[0]["region"] != "cn" {
		t.Fatalf("qoder expand region=%v", expanded[0])
	}

	merged, err := srv.fetchWorkerModelsForMode(false, "", catalogModeMerge)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 1 {
		t.Fatalf("merged=%v", merged)
	}
	regions := entryModelRegions(merged[0])
	if len(regions) != 1 || regions[0] != "cn" {
		t.Fatalf("qoder merge regions=%v", regions)
	}
}
