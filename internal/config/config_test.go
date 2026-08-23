package config

import (
	"testing"
)

func TestLoadAcceptsMissingAPIKeyForSQLiteInitialization(t *testing.T) {
	t.Setenv("PROXY_API_KEY", "")
	t.Setenv("ALLOW_INSECURE_API_KEY", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProxyAPIKey != "" {
		t.Fatalf("got %q", cfg.ProxyAPIKey)
	}
}

func TestLoadKeepsOptionalBootstrapKey(t *testing.T) {
	t.Setenv("PROXY_API_KEY", "change-me")
	t.Setenv("ALLOW_INSECURE_API_KEY", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProxyAPIKey != "change-me" {
		t.Fatalf("got %q", cfg.ProxyAPIKey)
	}
}

func TestLoadAcceptsRealKey(t *testing.T) {
	t.Setenv("PROXY_API_KEY", "real-secret")
	t.Setenv("ALLOW_INSECURE_API_KEY", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProxyAPIKey != "real-secret" {
		t.Fatalf("got %q", cfg.ProxyAPIKey)
	}
}

func TestLoadAccountRuntimeDefaults(t *testing.T) {
	t.Setenv("PROXY_API_KEY", "real-secret")
	t.Setenv("QODER_DATA_DIR", "/tmp/qoder-data")
	t.Setenv("QODER_WORKER_BASE_PORT", "33000")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != "/tmp/qoder-data" || cfg.WorkerBasePort != 33000 || cfg.RuntimeDir == "" {
		t.Fatalf("runtime config = %+v", cfg)
	}
	if cfg.NodeBinary == "" || cfg.WorkerDaemonPath == "" || cfg.QoderCLIPath == "" {
		t.Fatalf("missing runtime paths: %+v", cfg)
	}
}
