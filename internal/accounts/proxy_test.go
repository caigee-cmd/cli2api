package accounts

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestValidateAccountProxy(t *testing.T) {
	ok := []struct {
		provider string
		region   string
		raw      string
	}{
		{provider: "qoder", region: "global", raw: ""},
		{provider: "qoder", region: "global", raw: "direct"},
		{provider: "qoder", region: "cn", raw: "http://proxy.example:8080"},
		{provider: "qoder", region: "global", raw: "https://proxy.example:8443"},
		{provider: "workbuddy", raw: "socks5://proxy.example:1080"},
		{provider: "trae", raw: "socks5h://proxy.example:1080"},
	}
	for _, test := range ok {
		if err := validateAccountProxy(test.provider, test.region, test.raw); err != nil {
			t.Fatalf("validateAccountProxy(%q,%q,%q) = %v, want nil", test.provider, test.region, test.raw, err)
		}
	}

	rejected := []struct {
		provider string
		region   string
		raw      string
	}{
		{provider: "qoder", region: "global", raw: "socks5://proxy.example:1080"},
		{provider: "qoder", region: "cn", raw: "socks5h://proxy.example:1080"},
	}
	for _, test := range rejected {
		if err := validateAccountProxy(test.provider, test.region, test.raw); err == nil {
			t.Fatalf("validateAccountProxy(%q,%q,%q) unexpectedly succeeded", test.provider, test.region, test.raw)
		}
	}
}

func TestStoreCreateRejectsQoderSOCKS(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.Create(ctx, CreateAccount{Name: "QoderSocks", Enabled: true, ProxyURL: "socks5://proxy.example:1080"}); err == nil {
		t.Fatal("Qoder account with SOCKS proxy was accepted")
	}
	if _, err := store.Create(ctx, CreateAccount{Name: "QoderHTTP", Enabled: true, ProxyURL: "http://proxy.example:8080"}); err != nil {
		t.Fatalf("Qoder account with HTTP proxy rejected: %v", err)
	}
	if _, err := store.Create(ctx, CreateAccount{Name: "WbSocks", Provider: "workbuddy", ProxyURL: "socks5://proxy.example:1080"}); err != nil {
		t.Fatalf("WorkBuddy account with SOCKS proxy rejected: %v", err)
	}
	if _, err := store.Create(ctx, CreateAccount{Name: "TraeSocks", Provider: "trae", ProxyURL: "socks5://proxy.example:1080"}); err != nil {
		t.Fatalf("Trae account with SOCKS proxy rejected: %v", err)
	}
}

func TestStoreUpdateKeepsOriginalOnRejectedProxy(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	account, err := store.Create(ctx, CreateAccount{Name: "QoderUpdate", Enabled: true, ProxyURL: "http://proxy.example:8080"})
	if err != nil {
		t.Fatal(err)
	}

	socks := "socks5://proxy.example:1080"
	if err := store.Update(ctx, account.ID, UpdateAccount{ProxyURL: &socks}); err == nil {
		t.Fatal("Qoder proxy update to SOCKS was accepted")
	}

	reloaded, err := store.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ProxyURL != "http://proxy.example:8080" {
		t.Fatalf("stored proxy changed after rejected update: %q", reloaded.ProxyURL)
	}
}

func TestExecStarterSetProxyURLAppliesToNewWorkers(t *testing.T) {
	starter := &ExecStarter{Config: ManagerConfig{
		DaemonPath:   "/app/worker/daemon.mjs",
		QoderCLIPath: "/usr/lib/qodercli.js",
	}}

	starter.SetProxyURL("http://proxy.example:8080")
	env := starterEnvForTest(t, starter, Account{ID: "acc1", Provider: "qoder", ProviderRegion: "global", MaxInFlight: 4}, "/tmp/home", 32100)

	if got := envValue(env, "QODER_PROXY_URL"); got != "http://proxy.example:8080" {
		t.Fatalf("QODER_PROXY_URL = %q", got)
	}
	if got := envValue(env, "HTTPS_PROXY"); got != "http://proxy.example:8080" {
		t.Fatalf("HTTPS_PROXY = %q", got)
	}

	// A later update is visible to the next spawn.
	starter.SetProxyURL("  direct  ")
	env = starterEnvForTest(t, starter, Account{ID: "acc1", Provider: "qoder", ProviderRegion: "global", MaxInFlight: 4}, "/tmp/home", 32100)
	if got := envValue(env, "QODER_PROXY_URL"); got != "direct" {
		t.Fatalf("QODER_PROXY_URL after update = %q", got)
	}
	if got := envValue(env, "HTTPS_PROXY"); got != "" {
		t.Fatalf("direct must not inject HTTPS_PROXY, got %q", got)
	}
}

func TestStarterEnvAccountProxyOverridesGlobal(t *testing.T) {
	config := ManagerConfig{
		DaemonPath:   "/app/worker/daemon.mjs",
		QoderCLIPath: "/usr/lib/qodercli.js",
		ProxyURL:     "http://global.example:8080",
	}

	// Account override wins.
	env := starterEnvForTestConfig(t, config, Account{
		ID: "acc1", Provider: "qoder", ProviderRegion: "global", MaxInFlight: 4,
		ProxyURL: "http://account.example:9090",
	}, "/tmp/home", 32100)
	if got := envValue(env, "QODER_PROXY_URL"); got != "http://account.example:9090" {
		t.Fatalf("account override QODER_PROXY_URL = %q", got)
	}

	// Account "direct" beats the global HTTP proxy.
	env = starterEnvForTestConfig(t, config, Account{
		ID: "acc1", Provider: "qoder", ProviderRegion: "global", MaxInFlight: 4,
		ProxyURL: "direct",
	}, "/tmp/home", 32100)
	if got := envValue(env, "QODER_PROXY_URL"); got != "direct" {
		t.Fatalf("account direct QODER_PROXY_URL = %q", got)
	}
	if got := envValue(env, "HTTPS_PROXY"); got != "" {
		t.Fatalf("account direct must not inject HTTPS_PROXY, got %q", got)
	}

	// Empty account proxy inherits the global.
	env = starterEnvForTestConfig(t, config, Account{
		ID: "acc1", Provider: "qoder", ProviderRegion: "global", MaxInFlight: 4,
	}, "/tmp/home", 32100)
	if got := envValue(env, "QODER_PROXY_URL"); got != "http://global.example:8080" {
		t.Fatalf("inherited QODER_PROXY_URL = %q", got)
	}
}

func TestExecStarterConfigSnapshotConcurrentWithSetProxyURL(t *testing.T) {
	starter := &ExecStarter{Config: ManagerConfig{
		DaemonPath:   "/app/worker/daemon.mjs",
		QoderCLIPath: "/usr/lib/qodercli.js",
	}}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			starter.SetProxyURL("http://proxy.example:8080")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			_ = starter.configSnapshot()
		}
	}()
	wg.Wait()
}

func TestReloadProxyURLRestartsOnlyInheritingQoder(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	inherits, err := store.Create(ctx, CreateAccount{Name: "InheritsGlobal", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	overrides, err := store.Create(ctx, CreateAccount{Name: "HasAccountProxy", Enabled: true, ProxyURL: "http://account.example:9090"})
	if err != nil {
		t.Fatal(err)
	}
	workbuddy, err := store.Create(ctx, CreateAccount{Name: "WorkBuddy", Provider: "workbuddy", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	trae, err := store.Create(ctx, CreateAccount{Name: "Trae", Provider: "trae", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	starter := &fakeStarter{}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, starter)
	defer manager.Close()
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	before := len(starter.accounts)

	if err := manager.ReloadProxyURL(ctx, "http://new-global.example:8080"); err != nil {
		t.Fatalf("ReloadProxyURL: %v", err)
	}

	restarted := map[string]bool{}
	for _, account := range starter.accounts[before:] {
		restarted[account.ID] = true
	}
	if !restarted[inherits.ID] {
		t.Fatal("inheriting Qoder account was not restarted")
	}
	if restarted[overrides.ID] {
		t.Fatal("Qoder account with its own proxy was restarted")
	}
	if restarted[workbuddy.ID] {
		t.Fatal("WorkBuddy account was restarted (in-process)")
	}
	if restarted[trae.ID] {
		t.Fatal("Trae account was restarted (in-process)")
	}
}

func TestReloadProxyURLLogsAllFailuresAndContinues(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	first, err := store.Create(ctx, CreateAccount{Name: "First", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Create(ctx, CreateAccount{Name: "Second", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	starter := &fakeStarter{}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, starter)
	defer manager.Close()
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}

	// Force the next start attempt to fail; the reload must still attempt the
	// remaining accounts and report the failure.
	starter.failures = 1
	if err := manager.ReloadProxyURL(ctx, "http://new-global.example:8080"); err == nil {
		t.Fatal("expected a joined reload error")
	}

	attempted := map[string]bool{}
	for _, account := range starter.accounts {
		attempted[account.ID] = true
	}
	if !attempted[first.ID] || !attempted[second.ID] {
		t.Fatalf("reload did not attempt every account: %v", attempted)
	}
}

func starterEnvForTest(t *testing.T, starter *ExecStarter, account Account, home string, port int) []string {
	t.Helper()
	return starterEnvForTestConfig(t, starter.configSnapshot(), account, home, port)
}

func starterEnvForTestConfig(t *testing.T, config ManagerConfig, account Account, home string, port int) []string {
	t.Helper()
	env, err := starterEnv(config, account, home, port)
	if err != nil {
		t.Fatalf("starterEnv: %v", err)
	}
	return env
}

func envValue(env []string, key string) string {
	for _, entry := range env {
		if strings.HasPrefix(entry, key+"=") {
			return strings.TrimPrefix(entry, key+"=")
		}
	}
	return ""
}

func TestReloadProxyURLSkipsUnchangedValue(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.Create(ctx, CreateAccount{Name: "Inherits", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	starter := &fakeStarter{}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir(), ProxyURL: "http://global.example:8080"}, store, starter)
	defer manager.Close()
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if got := len(starter.accounts); got != 1 {
		t.Fatalf("initial starts = %d, want 1", got)
	}

	// Same value (module whitespace): no worker restart.
	if err := manager.ReloadProxyURL(ctx, "  http://global.example:8080  "); err != nil {
		t.Fatalf("ReloadProxyURL: %v", err)
	}
	if got := len(starter.accounts); got != 1 {
		t.Fatalf("worker restarted for an unchanged proxy: starts = %d, want 1", got)
	}

	// A real change still restarts.
	if err := manager.ReloadProxyURL(ctx, "http://other.example:9090"); err != nil {
		t.Fatalf("ReloadProxyURL: %v", err)
	}
	if got := len(starter.accounts); got != 2 {
		t.Fatalf("worker not restarted for a changed proxy: starts = %d, want 2", got)
	}
}

func TestSetSecretOrEmptyPersistsClearedValue(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.SetSecretOrEmpty(ctx, "proxy_url", "http://proxy.example:8080"); err != nil {
		t.Fatal(err)
	}
	value, found, err := store.GetSecret(ctx, "proxy_url")
	if err != nil || !found || value != "http://proxy.example:8080" {
		t.Fatalf("value=%q found=%v err=%v", value, found, err)
	}

	// Clearing keeps the row present with an empty value, unlike DeleteSecret.
	if err := store.SetSecretOrEmpty(ctx, "proxy_url", "   "); err != nil {
		t.Fatal(err)
	}
	value, found, err = store.GetSecret(ctx, "proxy_url")
	if err != nil || !found || value != "" {
		t.Fatalf("after clear: value=%q found=%v err=%v (want found empty row)", value, found, err)
	}

	// SetSecret still rejects empty so unrelated secrets keep their contract.
	if err := store.SetSecret(ctx, "other", ""); err == nil {
		t.Fatal("SetSecret accepted an empty value")
	}
}

// A failed global-proxy reload must stay retryable with the same value. The
// manager tracks a pending flag so "same value" alone does not short-circuit
// the reload while workers still run on the old proxy.
func TestReloadProxyURLRetriesAfterFailureWithSameValue(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.Create(ctx, CreateAccount{Name: "Inherits", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	starter := &fakeStarter{}
	manager := NewManager(ManagerConfig{DataDir: t.TempDir(), ProxyURL: "http://old.example:8080"}, store, starter)
	defer manager.Close()
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if got := len(starter.accounts); got != 1 {
		t.Fatalf("initial starts = %d, want 1", got)
	}

	const newProxy = "http://new.example:9090"

	// Attempt to switch to the new proxy with an already-cancelled context:
	// the restart fails, so the reload reports an error.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := manager.ReloadProxyURL(cancelled, newProxy); err == nil {
		t.Fatal("reload with a cancelled context unexpectedly succeeded")
	}
	if got := len(starter.accounts); got != 1 {
		t.Fatalf("failed reload restarted workers: starts = %d, want 1", got)
	}

	// Resubmitting the *same* value with a healthy context must retry and
	// restart the inheriting worker.
	if err := manager.ReloadProxyURL(ctx, newProxy); err != nil {
		t.Fatalf("retry with the same value failed: %v", err)
	}
	if got := len(starter.accounts); got != 2 {
		t.Fatalf("retry did not restart the worker: starts = %d, want 2", got)
	}

	// Now that the reload succeeded, an identical value is a genuine no-op.
	if err := manager.ReloadProxyURL(ctx, newProxy); err != nil {
		t.Fatalf("post-success no-op reload: %v", err)
	}
	if got := len(starter.accounts); got != 2 {
		t.Fatalf("post-success identical value restarted the worker: starts = %d, want 2", got)
	}
}
