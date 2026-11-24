package app_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/app"
	"github.com/caigee-cmd/cli2api/internal/config"
	sqlstore "github.com/caigee-cmd/cli2api/internal/store"
)

func TestAppSeedsProviderPolicyForExistingAccountWithoutRewritingIt(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	store, err := sqlstore.OpenStore(filepath.Join(dataDir, "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	account, err := store.Create(ctx, accounts.CreateAccount{
		Name: "existing", Provider: "trae", Region: "cn", Enabled: false,
		ProxyURL: "http://user:secret@example.test:8080",
	})
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	a := app.New(config.Config{ProxyAPIKey: "key", QoderHome: t.TempDir(), DataDir: dataDir, RuntimeDir: t.TempDir()})
	defer a.Close()

	policy, found, err := a.Manager.Store().GetProviderPolicy(ctx, "trae", "cn")
	if err != nil || !found {
		t.Fatalf("seeded policy: found=%v err=%v", found, err)
	}
	if policy.ProxyURL != "http://user:secret@example.test:8080" {
		t.Fatalf("seeded policy proxy = %q", policy.ProxyURL)
	}

	raw, err := a.Manager.Store().ListRaw(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 1 || raw[0].ID != account.ID || raw[0].ProxyURL != "http://user:secret@example.test:8080" {
		t.Fatalf("startup seed rewrote account row: %#v", raw)
	}

	effective, err := a.Manager.Store().Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if effective.ProxyURL != "http://user:secret@example.test:8080" {
		t.Fatalf("effective account proxy = %q", effective.ProxyURL)
	}
}
