package api

import (
	"context"
	sqlstore "github.com/caigee-cmd/cli2api/internal/store"
	"path/filepath"
	"testing"
)

func TestEnsureProxyAPIKeyGeneratesAndPersists(t *testing.T) {
	ctx := context.Background()
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	first, initialized, err := ensureProxyAPIKey(ctx, store, "")
	if err != nil || !initialized || len(first) < 40 {
		t.Fatalf("first key = %q, initialized=%v, err=%v", first, initialized, err)
	}
	second, initialized, err := ensureProxyAPIKey(ctx, store, "another-bootstrap-key")
	if err != nil || initialized || second != first {
		t.Fatalf("second key = %q, initialized=%v, err=%v", second, initialized, err)
	}
}

func TestEnsureProxyAPIKeyUsesBootstrapOnlyOnce(t *testing.T) {
	ctx := context.Background()
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	got, initialized, err := ensureProxyAPIKey(ctx, store, "bootstrap-secret")
	if err != nil || !initialized || got != "bootstrap-secret" {
		t.Fatalf("key = %q, initialized=%v, err=%v", got, initialized, err)
	}
}
