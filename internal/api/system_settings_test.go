package api

import (
	"bytes"
	"context"
	sqlstore "github.com/caigee-cmd/cli2api/internal/store"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/config"
)

func TestEnsureCrossProviderModelPoolDefaultsToEnabled(t *testing.T) {
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
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
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
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

func TestEnsureProxyURLRejectsSOCKSBootstrap(t *testing.T) {
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := ensureProxyURL(context.Background(), store, "socks5://proxy.example:1080"); err == nil {
		t.Fatal("SOCKS bootstrap was accepted for the global proxy")
	}
	if _, ok, err := store.GetSecret(context.Background(), proxyURLSecret); err != nil || ok {
		t.Fatalf("rejected bootstrap was persisted: ok=%v err=%v", ok, err)
	}
}

func TestEnsureProxyURLAcceptsHTTPBootstrap(t *testing.T) {
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	value, err := ensureProxyURL(context.Background(), store, "http://proxy.example:8080")
	if err != nil || value != "http://proxy.example:8080" {
		t.Fatalf("value=%q err=%v", value, err)
	}
	stored, ok, err := store.GetSecret(context.Background(), proxyURLSecret)
	if err != nil || !ok || stored != "http://proxy.example:8080" {
		t.Fatalf("stored=%q ok=%v err=%v", stored, ok, err)
	}
}

func TestSystemSettingsProxyURLRejectsSOCKS(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(),
	})
	defer srv.Close()

	request := httptest.NewRequest(http.MethodPatch, "/api/system/settings", bytes.NewBufferString(`{"proxy_url":"socks5://proxy.example:1080"}`))
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	srv.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("SOCKS global proxy response: %d %s", response.Code, response.Body.String())
	}
	if _, ok, err := srv.manager.Store().GetSecret(context.Background(), proxyURLSecret); err != nil || ok {
		t.Fatalf("rejected global proxy was persisted: ok=%v err=%v", ok, err)
	}
}

func TestSystemSettingsProxyURLAcceptsHTTP(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(),
	})
	defer srv.Close()

	request := httptest.NewRequest(http.MethodPatch, "/api/system/settings", bytes.NewBufferString(`{"proxy_url":"http://proxy.example:8080"}`))
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	srv.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("HTTP global proxy response: %d %s", response.Code, response.Body.String())
	}
	stored, ok, err := srv.manager.Store().GetSecret(context.Background(), proxyURLSecret)
	if err != nil || !ok || stored != "http://proxy.example:8080" {
		t.Fatalf("stored=%q ok=%v err=%v", stored, ok, err)
	}
}

// A failed global-proxy reload must stay retryable. The database already holds
// the new value after the first attempt, so repeating the PATCH with the *same*
// value must still attempt the reload (and keep reporting the failure) instead
// of being short-circuited as a no-op.
func TestSystemSettingsProxyURLRetriesFailedReloadWithSameValue(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(),
	})
	defer srv.Close()

	// An enabled inheriting Qoder account; with no daemon path configured its
	// worker cannot start, so every reload attempt fails deterministically.
	if _, err := srv.manager.Store().Create(context.Background(), accounts.CreateAccount{Name: "Inherits", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	patch := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, "/api/system/settings", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer secret")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	const newProxy = "http://new-global.example:8080"

	first := patch(`{"proxy_url":"` + newProxy + `"}`)
	if first.Code != http.StatusInternalServerError {
		t.Fatalf("first reload response: %d %s", first.Code, first.Body.String())
	}
	stored, ok, err := srv.manager.Store().GetSecret(context.Background(), proxyURLSecret)
	if err != nil || !ok || stored != newProxy {
		t.Fatalf("stored after failed reload: %q ok=%v err=%v", stored, ok, err)
	}

	// Identical value: the write is skipped, but the reload is retried.
	second := patch(`{"proxy_url":"` + newProxy + `"}`)
	if second.Code != http.StatusInternalServerError {
		t.Fatalf("retry was treated as a no-op: %d %s", second.Code, second.Body.String())
	}
	if !bytes.Contains(second.Body.Bytes(), []byte("proxy_reload_failed")) {
		t.Fatalf("retry body = %s", second.Body.String())
	}
}

func TestSystemSettingsProxyURLClearPersistsEmptyValue(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(),
	})
	defer srv.Close()

	if err := srv.manager.Store().SetSecret(context.Background(), proxyURLSecret, "http://proxy.example:8080"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/api/system/settings", bytes.NewBufferString(`{"proxy_url":""}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear response: %d %s", rec.Code, rec.Body.String())
	}

	// The row must remain with an empty value so a restart does not treat the
	// clear as "never configured" and re-apply the environment bootstrap.
	stored, ok, err := srv.manager.Store().GetSecret(context.Background(), proxyURLSecret)
	if err != nil || !ok || stored != "" {
		t.Fatalf("cleared proxy: stored=%q ok=%v err=%v (want present empty row)", stored, ok, err)
	}
}

func TestEnsureProxyURLDoesNotReapplyBootstrapAfterClear(t *testing.T) {
	ctx := context.Background()
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Simulate a user clearing the proxy: the row exists with an empty value.
	if err := store.SetSecretOrEmpty(ctx, proxyURLSecret, ""); err != nil {
		t.Fatal(err)
	}

	value, err := ensureProxyURL(ctx, store, "http://env-bootstrap.example:8080")
	if err != nil {
		t.Fatalf("ensureProxyURL: %v", err)
	}
	if value != "" {
		t.Fatalf("cleared proxy was re-bootstrapped from the environment: %q", value)
	}
}

// Two concurrent proxy saves must not leave SQLite and the running workers
// disagreeing. The read of the persisted value, Preserve, validation, and the
// change comparison must all run inside the same settingsMu critical section
// that saves and reloads. This test holds settingsMu, fires a PATCH that
// submits the *old* value, changes the stored value underneath it, then
// releases the lock. With the read outside the lock the request would see the
// stale old value, skip its write, and reload the runtime to old.example while
// the database holds new.example. Correct behavior re-reads inside the lock,
// notices the difference, and persists its own value so the two agree.
func TestSystemSettingsProxyURLReadIsInsideSettingsLock(t *testing.T) {
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(),
	})
	defer srv.Close()
	ctx := context.Background()

	const oldProxy = "http://old.example:8080"
	const newProxy = "http://new.example:9090"
	if err := srv.manager.Store().SetSecret(ctx, proxyURLSecret, oldProxy); err != nil {
		t.Fatal(err)
	}

	// Take the settings lock so the PATCH cannot enter the critical section.
	srv.settingsMu.Lock()

	type result struct {
		code int
		body string
	}
	done := make(chan result, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPatch, "/api/system/settings", bytes.NewBufferString(`{"proxy_url":"`+oldProxy+`"}`))
		req.Header.Set("Authorization", "Bearer secret")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		done <- result{code: rec.Code, body: rec.Body.String()}
	}()

	// Give the goroutine time to reach (and block on) the settings lock, then
	// change the stored value as a concurrent request would.
	time.Sleep(100 * time.Millisecond)
	if err := srv.manager.Store().SetSecret(ctx, proxyURLSecret, newProxy); err != nil {
		t.Fatal(err)
	}
	srv.settingsMu.Unlock()

	res := <-done
	if res.code != http.StatusOK {
		t.Fatalf("patch response: %d %s", res.code, res.body)
	}

	// The request submitted old.example while the row held new.example, so it
	// must have persisted old.example. If it had compared against a stale read
	// taken before the lock, it would have skipped the write and left
	// new.example on disk while pointing the runtime at old.example.
	stored, ok, err := srv.manager.Store().GetSecret(ctx, proxyURLSecret)
	if err != nil || !ok {
		t.Fatalf("stored proxy missing: ok=%v err=%v", ok, err)
	}
	if stored != oldProxy {
		t.Fatalf("database/runtime split: database=%q, the request's runtime value=%q", stored, oldProxy)
	}
}
