package control

import (
	"context"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

type Keys struct {
	store accounts.AccountStore
}

func NewKeys(store accounts.AccountStore) *Keys {
	if store == nil {
		return nil
	}
	return &Keys{store: store}
}

func (k *Keys) List(ctx context.Context) ([]accounts.APIKey, error) {
	keys, err := k.store.ListAPIKeys(ctx)
	if err != nil {
		return nil, err
	}
	if keys == nil {
		keys = []accounts.APIKey{}
	}
	return keys, nil
}

func (k *Keys) Create(ctx context.Context, input accounts.CreateAPIKey) (accounts.APIKey, error) {
	return k.store.CreateAPIKey(ctx, input)
}

func (k *Keys) Get(ctx context.Context, id string) (accounts.APIKey, error) {
	return k.store.GetAPIKey(ctx, id)
}

func (k *Keys) Update(ctx context.Context, id string, input accounts.UpdateAPIKey) (accounts.APIKey, error) {
	return k.store.UpdateAPIKey(ctx, id, input)
}

func (k *Keys) Delete(ctx context.Context, id string) error {
	return k.store.DeleteAPIKey(ctx, id)
}

func (k *Keys) SetConsoleSecret(ctx context.Context, secretName, secret string) error {
	return k.store.SetSecret(ctx, secretName, secret)
}

func (k *Keys) Touch(ctx context.Context, id string) error {
	return k.store.TouchAPIKey(ctx, id)
}
