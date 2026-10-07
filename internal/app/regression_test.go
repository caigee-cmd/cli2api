package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/app"
	"github.com/caigee-cmd/cli2api/internal/config"
	"github.com/caigee-cmd/cli2api/internal/executor"
	"github.com/caigee-cmd/cli2api/internal/providers/qoder"
	"github.com/caigee-cmd/cli2api/internal/update"
)

func regressionApp(t *testing.T) *app.App {
	t.Helper()
	a := app.New(config.Config{ProxyAPIKey: "old-key", QoderHome: t.TempDir(), DataDir: t.TempDir(), RuntimeDir: t.TempDir()})
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func serveRegression(h http.Handler, method, path, key, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+key)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestConsoleKeyRotationUpdatesExistingHandlerAndWorkerRequests(t *testing.T) {
	a := regressionApp(t)
	// Capture the production handler once, exactly as http.Server does.
	h := a.Handler()
	named, err := a.Control.Keys.Create(context.Background(), accounts.CreateAPIKey{Name: "rotation-test", Providers: []string{"qoder"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	currentKey := "old-key"
	for rotation := 0; rotation < 2; rotation++ {
		w := serveRegression(h, "POST", "/api/system/console-key", currentKey, `{"rotate":true}`)
		if w.Code != 200 {
			t.Fatalf("rotation: %d %s", w.Code, w.Body.String())
		}
		var result struct {
			Secret string `json:"secret"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Secret == "" || result.Secret == currentKey {
			t.Fatal("rotation did not mint a new key")
		}
		if got := serveRegression(h, "GET", "/api/system/console-key", currentKey, ""); got.Code != 401 {
			t.Errorf("old key: got %d want 401", got.Code)
		}
		if got := serveRegression(h, "GET", "/api/system/console-key", result.Secret, ""); got.Code != 200 {
			t.Errorf("new key: got %d want 200", got.Code)
		}
		if got := serveRegression(h, "GET", "/api/system/console-key", named.Secret, ""); got.Code != 403 {
			t.Errorf("named key accessed console: %d", got.Code)
		}
		if got := serveRegression(h, "GET", "/v1/models", named.Secret, ""); got.Code != 200 {
			t.Errorf("named key invalidated: %d", got.Code)
		}
		saved, _, err := a.Manager.Store().GetSecret(context.Background(), "proxy_api_key")
		if err != nil || saved != result.Secret {
			t.Fatal("rotated key not persisted")
		}
		if a.Manager.ProxyAPIKey() != result.Secret {
			t.Fatal("runtime key not updated")
		}
		currentKey = result.Secret
	}
	// Chat no longer enters the login worker. The gateway sees a COSY bearer,
	// never the console key that rotation just changed.
	var calls atomic.Int32
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if strings.Contains(r.Header.Get("Authorization"), currentKey) {
			t.Error("upstream received the console key")
			w.WriteHeader(401)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer COSY.") {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"body\":{\"choices\":[{\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}}\n\n")
	}))
	defer worker.Close()
	account, err := a.Manager.Store().Create(context.Background(), accounts.CreateAccount{Name: "fake-worker", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Manager.Store().SaveCredential(context.Background(), account.ID, "oauth", accounts.NativeCredential{
		UserBlob:  []byte(`{"uid":"u-rotation","access_token":"dt-rotation"}`),
		MachineID: "0123456789abcdef",
	}); err != nil {
		t.Fatal(err)
	}
	original := qoder.ChatEndpointHook
	qoder.ChatEndpointHook = func(string) string { return worker.URL + "/algo/api/v2/service/pro/sse/agent_chat_generation?Encode=1" }
	t.Cleanup(func() { qoder.ChatEndpointHook = original })
	a.Pool.Upsert(executor.Item{ID: account.ID, Provider: "qoder", Runtime: "child_process", URL: "http://127.0.0.1:1", Models: []string{"glm-5.2"}})
	// Catalog probing is tested separately; keep the fake worker deterministic.
	a.Gateway.Catalogs = nil
	for _, path := range []string{"/v1/chat/completions", "/api/chat", "/v1/messages", "/v1/responses"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", path, stream), func(t *testing.T) {
				body := fmt.Sprintf(`{"model":"qoder/glm-5.2","messages":[{"role":"user","content":"hello"}],"max_tokens":32,"stream":%v}`, stream)
				if path == "/v1/responses" {
					body = fmt.Sprintf(`{"model":"qoder/glm-5.2","input":"hello","stream":%v}`, stream)
				}
				key := currentKey
				if path != "/api/chat" {
					key = named.Secret
				}
				got := serveRegression(h, "POST", path, key, body)
				if got.Code != 200 || !strings.Contains(got.Body.String(), "OK") {
					t.Errorf("chat failed: %d %s", got.Code, got.Body.String())
				}
			})
		}
	}
	if calls.Load() != 8 {
		t.Errorf("worker requests=%d want 8", calls.Load())
	}
}

func TestSaturatedPoolKeepsFiveSecondRetryAfter(t *testing.T) {
	a := regressionApp(t)
	h := a.Handler()
	var calls atomic.Int32
	worker := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
		t.Error("saturated pool must not call upstream")
	}))
	defer worker.Close()
	account, err := a.Manager.Store().Create(context.Background(), accounts.CreateAccount{Name: "saturated", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	a.Pool.Upsert(executor.Item{
		ID: account.ID, Provider: "qoder", Runtime: "child_process", URL: worker.URL,
		Models: []string{"glm-5.2"}, MaxInFlight: 1,
	})
	a.Pool.MergeHealth(account.ID, true, true, 1, 0, "")
	a.Gateway.Catalogs = nil
	for _, path := range []string{"/v1/chat/completions", "/api/chat", "/v1/messages", "/v1/responses"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", path, stream), func(t *testing.T) {
				body := fmt.Sprintf(`{"model":"qoder/glm-5.2","messages":[{"role":"user","content":"hello"}],"max_tokens":32,"stream":%v}`, stream)
				if path == "/v1/messages" {
					body = fmt.Sprintf(`{"model":"qoder/glm-5.2","messages":[{"role":"user","content":"hello"}],"max_tokens":32,"stream":%v}`, stream)
				}
				if path == "/v1/responses" {
					body = fmt.Sprintf(`{"model":"qoder/glm-5.2","input":"hello","stream":%v}`, stream)
				}
				got := serveRegression(h, "POST", path, "old-key", body)
				if got.Code != http.StatusTooManyRequests {
					t.Fatalf("status=%d body=%s", got.Code, got.Body.String())
				}
				if got.Header().Get("Retry-After") != "5" {
					t.Fatalf("Retry-After=%q want 5 body=%s", got.Header().Get("Retry-After"), got.Body.String())
				}
				item, _ := a.Pool.ByID(account.ID)
				if !item.DownUntil.IsZero() {
					t.Fatalf("capacity error cooled account until %v", item.DownUntil)
				}
			})
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("upstream calls=%d", calls.Load())
	}
}

type concurrentChecker struct{ calls atomic.Int32 }

func (c *concurrentChecker) Check(context.Context, bool) (update.Info, error) {
	c.calls.Add(1)
	return update.Info{}, nil
}

type concurrentAgent struct{ calls atomic.Int32 }

func (a *concurrentAgent) Status(context.Context) (update.AgentStatus, error) {
	a.calls.Add(1)
	return update.AgentStatus{}, nil
}
func (a *concurrentAgent) Apply(context.Context, update.ApplyRequest) (update.ApplyResponse, error) {
	return update.ApplyResponse{}, nil
}

func TestConcurrentUpdateInfoUsesStableDependencies(t *testing.T) {
	a := regressionApp(t)
	checker := &concurrentChecker{}
	agent := &concurrentAgent{}
	a.Update.Checker = checker
	a.Update.Agent = agent
	coord := a.Update
	h := a.Handler()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if got := serveRegression(h, "GET", "/api/system/update", "old-key", ""); got.Code != 200 {
					t.Errorf("update GET: %d", got.Code)
				}
			}
		}()
	}
	wg.Wait()
	if a.Update != coord || a.Console.Update != coord || a.HTTP.Update != coord {
		t.Fatal("coordinator identity changed")
	}
	if checker.calls.Load() != 160 || agent.calls.Load() != 160 {
		t.Fatal("injected dependencies were not used")
	}
}

func TestConsoleKeyRotationConcurrentWithHTTPReaders(t *testing.T) {
	a := regressionApp(t)
	h := a.Handler()
	key := atomic.Pointer[string]{}
	initial := "old-key"
	key.Store(&initial)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 40; j++ {
				// Either answer is valid if the key rotates between snapshot and auth.
				secret := *key.Load()
				got := serveRegression(h, "GET", "/api/system/console-key", secret, "")
				if got.Code != 200 && got.Code != 401 {
					t.Errorf("concurrent key read: %d", got.Code)
				}
			}
		}()
	}
	for i := 0; i < 8; i++ {
		got := serveRegression(h, "POST", "/api/system/console-key", *key.Load(), `{"rotate":true}`)
		if got.Code != 200 {
			t.Errorf("rotation: %d", got.Code)
			break
		}
		var result struct {
			Secret string `json:"secret"`
		}
		if err := json.Unmarshal(got.Body.Bytes(), &result); err != nil {
			t.Error(err)
			break
		}
		key.Store(&result.Secret)
	}
	wg.Wait()
	if got := serveRegression(h, "GET", "/api/system/console-key", *key.Load(), ""); got.Code != 200 {
		t.Errorf("final key rejected: %d", got.Code)
	}
}
