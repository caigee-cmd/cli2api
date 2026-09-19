package store

import (
	"context"
	"github.com/caigee-cmd/cli2api/internal/accounts"
	"path/filepath"
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
		if err := accounts.ValidateAccountProxy(test.provider, test.region, test.raw); err != nil {
			t.Fatalf("ValidateAccountProxy(%q,%q,%q) = %v, want nil", test.provider, test.region, test.raw, err)
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
		if err := accounts.ValidateAccountProxy(test.provider, test.region, test.raw); err == nil {
			t.Fatalf("ValidateAccountProxy(%q,%q,%q) unexpectedly succeeded", test.provider, test.region, test.raw)
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

	if _, err := store.Create(ctx, accounts.CreateAccount{Name: "QoderSocks", Enabled: true, ProxyURL: "socks5://proxy.example:1080"}); err == nil {
		t.Fatal("Qoder account with SOCKS proxy was accepted")
	}
	if _, err := store.Create(ctx, accounts.CreateAccount{Name: "QoderHTTP", Enabled: true, ProxyURL: "http://proxy.example:8080"}); err != nil {
		t.Fatalf("Qoder account with HTTP proxy rejected: %v", err)
	}
	if _, err := store.Create(ctx, accounts.CreateAccount{Name: "WbSocks", Provider: "workbuddy", ProxyURL: "socks5://proxy.example:1080"}); err != nil {
		t.Fatalf("WorkBuddy account with SOCKS proxy rejected: %v", err)
	}
	if _, err := store.Create(ctx, accounts.CreateAccount{Name: "TraeSocks", Provider: "trae", ProxyURL: "socks5://proxy.example:1080"}); err != nil {
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

	account, err := store.Create(ctx, accounts.CreateAccount{Name: "QoderUpdate", Enabled: true, ProxyURL: "http://proxy.example:8080"})
	if err != nil {
		t.Fatal(err)
	}

	socks := "socks5://proxy.example:1080"
	if err := store.Update(ctx, account.ID, accounts.UpdateAccount{ProxyURL: &socks}); err == nil {
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
