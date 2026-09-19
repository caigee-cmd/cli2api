package control

import (
	"context"
	"time"

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
	prepared, err := accounts.PrepareCreateAPIKey(input)
	if err != nil {
		return accounts.APIKey{}, err
	}
	secret, err := accounts.GenerateAPIKeySecret()
	if err != nil {
		return accounts.APIKey{}, err
	}
	now := time.Now().UTC()
	stored, err := k.store.InsertAPIKey(ctx, accounts.StoredAPIKey{
		Name:      prepared.Name,
		Prefix:    accounts.APIKeyPrefix(secret),
		KeyHash:   accounts.HashAPIKey(secret),
		Providers: prepared.Providers,
		Enabled:   prepared.Enabled,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		return accounts.APIKey{}, err
	}
	stored.Secret = secret
	stored.SecretOnce = true
	return stored, nil
}

func (k *Keys) Get(ctx context.Context, id string) (accounts.APIKey, error) {
	return k.store.GetAPIKey(ctx, id)
}

func (k *Keys) Update(ctx context.Context, id string, input accounts.UpdateAPIKey) (accounts.APIKey, error) {
	current, err := k.store.GetAPIKey(ctx, id)
	if err != nil {
		return accounts.APIKey{}, err
	}
	merged, err := accounts.ApplyAPIKeyUpdate(current, input)
	if err != nil {
		return accounts.APIKey{}, err
	}
	return k.store.SaveAPIKey(ctx, accounts.StoredAPIKey{
		ID:        merged.ID,
		Name:      merged.Name,
		Prefix:    merged.Prefix,
		Providers: merged.Providers,
		Enabled:   merged.Enabled,
		UpdatedAt: time.Now().UTC(),
	})
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
