package codearts

import (
	"context"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

// requiredStore is the minimum store surface every in-process adapter needs.
type requiredStore struct{}

func (requiredStore) Get(context.Context, string) (accounts.Account, error) {
	return accounts.Account{}, nil
}
func (requiredStore) LoadCredentialPayload(context.Context, string) (string, []byte, error) {
	return "", nil, accounts.ErrAccountNotFound
}
func (requiredStore) SaveCredentialPayload(context.Context, string, string, []byte) error {
	return nil
}
func (requiredStore) Observe(context.Context, string, string, string, string, string) error {
	return nil
}

func TestS04InterfaceContract(t *testing.T) {
	client := NewClient(requiredStore{})
	var _ providers.CredentialCodec = credentialCodec{}
	var _ providers.LoginSessionProvider = client
	var _ providers.LoginCompleter = client
	var _ providers.ProviderChat = client
	var _ providers.ModelCatalogProvider = client
	var _ providers.AccountProber = client
	var _ providers.ErrorClassifier = classifier{}
	var _ providers.CredentialImporter = importer{}
	var _ providers.ImportExporter = importer{}
	var _ providers.AccountCheckiner = client

	adapter := client.Adapter()
	if adapter.ID != ProviderID {
		t.Fatalf("adapter id = %q", adapter.ID)
	}
	for _, capability := range []string{"credential", "login", "chat", "models", "classifier", "import_export", "prober", "checkin"} {
		if !adapter.Supports(capability) {
			t.Fatalf("adapter missing capability %q", capability)
		}
	}
}

func TestMissingSecretReaderSkipsGlobalProxy(t *testing.T) {
	client := NewClient(requiredStore{})
	got, err := client.globalProxy(context.Background())
	if err != nil {
		t.Fatalf("missing SecretReader must skip, not fail: %v", err)
	}
	if got != "" {
		t.Fatalf("missing SecretReader returned %q", got)
	}
}
