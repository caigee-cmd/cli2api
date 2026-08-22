package config

import (
	"testing"
)

func TestLoadRejectsPlaceholderAPIKey(t *testing.T) {
	t.Setenv("PROXY_API_KEY", "change-me")
	t.Setenv("ALLOW_INSECURE_API_KEY", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected placeholder PROXY_API_KEY to fail")
	}
}

func TestLoadAllowsInsecureOverride(t *testing.T) {
	t.Setenv("PROXY_API_KEY", "change-me")
	t.Setenv("ALLOW_INSECURE_API_KEY", "1")
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
