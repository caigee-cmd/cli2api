package qoder

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

type legacyPATStore struct{ err error }

func (s legacyPATStore) LoadCredentialPayload(context.Context, string) (string, []byte, error) {
	return "", nil, s.err
}

func (legacyPATStore) SaveCredentialPayload(context.Context, string, string, []byte) error {
	return nil
}

// A legacy account (CLI blob only, no credential-payload row) must surface
// ErrPATRequired so Models/Quota/Checkin fall back to the Node worker.
func TestBridgeLegacyAccountWithoutPayloadReturnsErrPATRequired(t *testing.T) {
	for _, storeErr := range []error{
		accounts.ErrAccountNotFound,
		fmt.Errorf("load payload: %w", accounts.ErrAccountNotFound),
	} {
		client := NewClient()
		client.SetPATStore(legacyPATStore{err: storeErr}, nil)
		if _, err := client.Bridge(context.Background(), "legacy"); !errors.Is(err, ErrPATRequired) {
			t.Fatalf("Bridge(%v) err = %v, want ErrPATRequired", storeErr, err)
		}
	}
}

func TestBridgeOtherStoreErrorsPassThrough(t *testing.T) {
	boom := errors.New("db down")
	client := NewClient()
	client.SetPATStore(legacyPATStore{err: boom}, nil)
	if _, err := client.Bridge(context.Background(), "acct"); !errors.Is(err, boom) || errors.Is(err, ErrPATRequired) {
		t.Fatalf("Bridge err = %v, want %v", err, boom)
	}
}
