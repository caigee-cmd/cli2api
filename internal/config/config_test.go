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
	if cfg.NodeBinary == "" || cfg.WorkerDaemonPath == "" || cfg.QoderCLIPath == "" {
		t.Fatalf("missing runtime paths: %+v", cfg)
	}
}
