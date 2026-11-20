package control

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

// TestBundleRoundTrip verifies export keeps settings + credentials and import
// restores them as fresh accounts without touching existing IDs.
func TestBundleRoundTrip(t *testing.T) {
	svc, _, store, _ := newTestServices()
	original := accounts.Account{
		ID: "acc-old", Name: "Primary", Provider: "qoder", ProviderRegion: "global",
		AuthType: "pat", Enabled: true, MaxInFlight: 4, Priority: 60,
		DropSystemPrompt: true, AutoCheckin: false, CheckinTime: "",
		ProxyURL: "http://127.0.0.1:7890",
	}
	store.accounts[original.ID] = original
	store.payloads[original.ID] = []byte(`{"pat":"secret"}`)
	store.payloadFormats[original.ID] = "qoder-pat-v1"
	ctx := context.Background()

	bundle, err := svc.Accounts.ExportBundle(ctx)
	if err != nil {
		t.Fatalf("ExportBundle: %v", err)
	}
	if bundle.Count != 1 || len(bundle.Accounts) != 1 {
		t.Fatalf("unexpected bundle: %+v", bundle)
	}
	entry := bundle.Accounts[0]
	if entry.Provider != "qoder" || entry.Region != "global" || entry.ProxyURL != original.ProxyURL {
		t.Fatalf("settings not exported: %+v", entry)
	}
	if entry.Credential == nil || entry.Credential.Format != "qoder-pat-v1" || string(entry.Credential.Credential) != `{"pat":"secret"}` {
		t.Fatalf("credential not exported: %+v", entry.Credential)
	}

	// Marshal/unmarshal so the test uses the real wire shape.
	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	var decoded AccountBundle
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	decoded.Accounts[0].Name = "Restored"
	decoded.Accounts[0].Enabled = false
	result, err := svc.Accounts.ImportBundle(ctx, decoded)
	if err != nil {
		t.Fatalf("ImportBundle: %v", err)
	}
	if result.Succeeded != 1 || result.Failed != 0 {
		t.Fatalf("unexpected import result: %+v", result)
	}
	imported := result.Data[0]
	if imported.ID == original.ID {
		t.Fatal("import must create a fresh account ID")
	}
	if imported.Name != "Restored" || imported.Enabled || imported.Provider != "qoder" {
		t.Fatalf("unexpected imported account: %+v", imported)
	}
	if got := string(store.payloads[imported.ID]); got != `{"pat":"secret"}` {
		t.Fatalf("credential not restored: %s", got)
	}
	if store.payloadFormats[imported.ID] != "qoder-pat-v1" {
		t.Fatalf("credential format not restored: %s", store.payloadFormats[imported.ID])
	}
	if _, stillThere := store.accounts[original.ID]; !stillThere {
		t.Fatal("import must not modify or remove existing accounts")
	}
}

func TestImportBundleRejectsWrongFormat(t *testing.T) {
	svc, _, _, _ := newTestServices()
	_, err := svc.Accounts.ImportBundle(context.Background(), AccountBundle{Format: "other", Accounts: []AccountBundleEntry{{Name: "x"}}})
	if err == nil {
		t.Fatal("expected unsupported_format error")
	}
}
