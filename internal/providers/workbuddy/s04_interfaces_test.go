package workbuddy

import (
	"context"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

var _ Store = (*accounts.Store)(nil)
var _ SecretReader = (*accounts.Store)(nil)
var _ ModelSettingReader = (*accounts.Store)(nil)

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
