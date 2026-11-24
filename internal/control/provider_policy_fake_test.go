package control

import (
	"context"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func (s *fakeStore) ListRaw(ctx context.Context) ([]accounts.Account, error) {
	return s.List(ctx)
}

func (s *fakeStore) ListProviderPolicies(context.Context) ([]accounts.ProviderPolicy, error) {
	return nil, nil
}

func (s *fakeStore) GetProviderPolicy(context.Context, string, string) (accounts.ProviderPolicy, bool, error) {
	return accounts.ProviderPolicy{}, false, nil
}

func (s *fakeStore) SaveProviderPolicy(context.Context, accounts.ProviderPolicy) error {
	return nil
}
