package runtime_test

import (
	"context"
	"fmt"
	"github.com/caigee-cmd/cli2api/internal/accounts"
	accountruntime "github.com/caigee-cmd/cli2api/internal/runtime"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	sqlstore "github.com/caigee-cmd/cli2api/internal/store"
)

func TestExecStarterSetProxyURLAppliesToNewWorkers(t *testing.T) {
	starter := &accountruntime.ExecStarter{Config: accountruntime.ManagerConfig{
		DaemonPath:   "/app/worker/daemon.mjs",
		QoderCLIPath: "/usr/lib/qodercli.js",
	}}

	starter.SetProxyURL("http://proxy.example:8080")
	env := starterEnvForTest(t, starter, accounts.Account{ID: "acc1", Provider: "qoder", ProviderRegion: "global", MaxInFlight: 4}, "/tmp/home", 32100)

	if got := envValue(env, "QODER_PROXY_URL"); got != "http://proxy.example:8080" {
		t.Fatalf("QODER_PROXY_URL = %q", got)
	}
	if got := envValue(env, "HTTPS_PROXY"); got != "http://proxy.example:8080" {
		t.Fatalf("HTTPS_PROXY = %q", got)
	}

	// A later update is visible to the next spawn.
	starter.SetProxyURL("  direct  ")
	env = starterEnvForTest(t, starter, accounts.Account{ID: "acc1", Provider: "qoder", ProviderRegion: "global", MaxInFlight: 4}, "/tmp/home", 32100)
	if got := envValue(env, "QODER_PROXY_URL"); got != "direct" {
		t.Fatalf("QODER_PROXY_URL after update = %q", got)
	}
	if got := envValue(env, "HTTPS_PROXY"); got != "" {
		t.Fatalf("direct must not inject HTTPS_PROXY, got %q", got)
	}
}

func TestStarterEnvAccountProxyOverridesGlobal(t *testing.T) {
	config := accountruntime.ManagerConfig{
		DaemonPath:   "/app/worker/daemon.mjs",
		QoderCLIPath: "/usr/lib/qodercli.js",
		ProxyURL:     "http://global.example:8080",
	}

	// accounts.Account override wins.
	env := starterEnvForTestConfig(t, config, accounts.Account{
		ID: "acc1", Provider: "qoder", ProviderRegion: "global", MaxInFlight: 4,
		ProxyURL: "http://account.example:9090",
	}, "/tmp/home", 32100)
	if got := envValue(env, "QODER_PROXY_URL"); got != "http://account.example:9090" {
		t.Fatalf("account override QODER_PROXY_URL = %q", got)
	}

	// accounts.Account "direct" beats the global HTTP proxy.
	env = starterEnvForTestConfig(t, config, accounts.Account{
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
	env = starterEnvForTestConfig(t, config, accounts.Account{
		ID: "acc1", Provider: "qoder", ProviderRegion: "global", MaxInFlight: 4,
	}, "/tmp/home", 32100)
	if got := envValue(env, "QODER_PROXY_URL"); got != "http://global.example:8080" {
		t.Fatalf("inherited QODER_PROXY_URL = %q", got)
	}
}

func TestExecStarterConfigSnapshotConcurrentWithSetProxyURL(t *testing.T) {
	starter := &accountruntime.ExecStarter{Config: accountruntime.ManagerConfig{
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
			_ = starter.ConfigSnapshot()
		}
	}()
	wg.Wait()
}

func TestReloadProxyURLRestartsOnlyInheritingQoder(t *testing.T) {
	ctx := context.Background()
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	inherits, err := store.Create(ctx, accounts.CreateAccount{Name: "InheritsGlobal", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	overrides, err := store.Create(ctx, accounts.CreateAccount{Name: "HasAccountProxy", Enabled: true, ProxyURL: "http://account.example:9090"})
	if err != nil {
		t.Fatal(err)
	}
	workbuddy, err := store.Create(ctx, accounts.CreateAccount{Name: "WorkBuddy", Provider: "workbuddy", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	trae, err := store.Create(ctx, accounts.CreateAccount{Name: "Trae", Provider: "trae", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	starter := &fakeStarter{}
	manager := accountruntime.NewManager(accountruntime.ManagerConfig{DataDir: t.TempDir()}, store, starter)
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
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	first, err := store.Create(ctx, accounts.CreateAccount{Name: "First", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Create(ctx, accounts.CreateAccount{Name: "Second", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	starter := &fakeStarter{}
	manager := accountruntime.NewManager(accountruntime.ManagerConfig{DataDir: t.TempDir()}, store, starter)
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

func starterEnvForTest(t *testing.T, starter *accountruntime.ExecStarter, account accounts.Account, home string, port int) []string {
	t.Helper()
	return starterEnvForTestConfig(t, starter.ConfigSnapshot(), account, home, port)
}

func starterEnvForTestConfig(t *testing.T, config accountruntime.ManagerConfig, account accounts.Account, home string, port int) []string {
	t.Helper()
	env, err := accountruntime.StarterEnv(config, account, home, port)
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
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.Create(ctx, accounts.CreateAccount{Name: "Inherits", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	starter := &fakeStarter{}
	manager := accountruntime.NewManager(accountruntime.ManagerConfig{DataDir: t.TempDir(), ProxyURL: "http://global.example:8080"}, store, starter)
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

func TestReloadProxyURLRetriesAfterFailureWithSameValue(t *testing.T) {
	ctx := context.Background()
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.Create(ctx, accounts.CreateAccount{Name: "Inherits", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	starter := &fakeStarter{}
	manager := accountruntime.NewManager(accountruntime.ManagerConfig{DataDir: t.TempDir(), ProxyURL: "http://old.example:8080"}, store, starter)
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

func TestExecStarterConcurrentKeyProxyAndSnapshot(t *testing.T) {
	for _, constructed := range []bool{false, true} {
		t.Run(fmt.Sprintf("constructor=%v", constructed), func(t *testing.T) {
			initial := accountruntime.ManagerConfig{DataDir: "runtime-dir", BasePort: 32100, NodeBinary: "node", DaemonPath: "daemon.mjs", QoderCLIPath: "qodercli.js", RestartDelay: time.Second}
			starter := &accountruntime.ExecStarter{Config: initial}
			if constructed {
				starter = accountruntime.NewExecStarter(initial)
			}
			var wg sync.WaitGroup
			wg.Add(3)
			go func() {
				defer wg.Done()
				for i := 0; i < 500; i++ {
					starter.SetProxyURL(" http://proxy.example:8080 ")
				}
			}()
			go func() {
				defer wg.Done()
				for i := 0; i < 500; i++ {
					starter.SetProxyAPIKey("rotated-key")
				}
			}()
			go func() {
				defer wg.Done()
				for i := 0; i < 500; i++ {
					cfg := starter.ConfigSnapshot()
					if cfg.DataDir != initial.DataDir || cfg.BasePort != initial.BasePort || cfg.RestartDelay != initial.RestartDelay {
						t.Error("snapshot lost runtime fields")
						return
					}
				}
			}()
			wg.Wait()
			cfg := starter.ConfigSnapshot()
			if cfg.ProxyURL != "http://proxy.example:8080" || cfg.ProxyAPIKey != "rotated-key" {
				t.Fatal("snapshot did not retain both updates")
			}
			env := starterEnvForTest(t, starter, accounts.Account{ID: "a", Provider: "qoder", ProviderRegion: "global"}, t.TempDir(), 32100)
			if envValue(env, "QODER_PROXY_URL") != cfg.ProxyURL || envValue(env, "PROXY_API_KEY") != cfg.ProxyAPIKey {
				t.Fatal("worker environment did not use final credentials/proxy")
			}
		})
	}
}
