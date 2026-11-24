package config

import (
	"testing"
)

func TestLoadDoesNotReadAPIKeyFromEnvironment(t *testing.T) {
	t.Setenv("PROXY_API_KEY", "environment-key")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProxyAPIKey != "" {
		t.Fatalf("got %q", cfg.ProxyAPIKey)
	}
}

func TestLoadAccountRuntimeDefaults(t *testing.T) {
	t.Setenv("QODER_DATA_DIR", "/tmp/qoder-data")
	t.Setenv("QODER_WORKER_BASE_PORT", "33000")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != "/tmp/qoder-data" || cfg.WorkerBasePort != 33000 || cfg.RuntimeDir == "" {
		t.Fatalf("runtime config = %+v", cfg)
	}
	if cfg.NodeBinary == "" || cfg.WorkerDaemonPath == "" || cfg.QoderCLIPath == "" || cfg.UpdateSocketPath == "" {
		t.Fatalf("missing runtime paths: %+v", cfg)
	}
}

func TestLoadLocalUpdaterEndpoint(t *testing.T) {
	t.Setenv("UPDATE_AGENT_URL", "http://host.docker.internal:3011")
	t.Setenv("UPDATE_AGENT_TOKEN", "local-token")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UpdateAgentURL != "http://host.docker.internal:3011" || cfg.UpdateAgentToken != "local-token" {
		t.Fatalf("updater config = %+v", cfg)
	}
}

func TestLoadPercentFloor(t *testing.T) {
	t.Setenv("CLI2API_PERCENT_FLOOR", "15")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PercentFloor != 15 {
		t.Fatalf("percent floor = %v, want 15", cfg.PercentFloor)
	}
}

func TestLoadRejectsNegativePercentFloor(t *testing.T) {
	t.Setenv("CLI2API_PERCENT_FLOOR", "-1")
	if _, err := Load(); err == nil {
		t.Fatal("negative percent floor must fail closed")
	}
}

func TestLoadRetryAccountBudget(t *testing.T) {
	t.Setenv("QODER_MAX_RETRY_ACCOUNTS", "99")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxRetryAccounts != 64 {
		t.Fatalf("max retry accounts = %d", cfg.MaxRetryAccounts)
	}
}

func TestLoadUpdateGitHubRepo(t *testing.T) {
	t.Setenv("UPDATE_GITHUB_REPO", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UpdateGitHubRepo != "caigee-cmd/cli2api" {
		t.Fatalf("default repo = %q", cfg.UpdateGitHubRepo)
	}

	t.Setenv("UPDATE_GITHUB_REPO", "guyu233/cli2api")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UpdateGitHubRepo != "guyu233/cli2api" {
		t.Fatalf("override repo = %q", cfg.UpdateGitHubRepo)
	}
}
