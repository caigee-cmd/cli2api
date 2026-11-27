package control

import (
	"context"
	"sync"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

// touchWriteInterval bounds how often one key's last_used_at is written. The
// value is display metadata, not auth state: writing it on every request puts a
// synchronous SQLite UPDATE in front of every completion.
const touchWriteInterval = time.Minute

type Keys struct {
	store accounts.AccountStore
	// invalidate drops the auth verifier's positive-key cache after a console
	// mutation, so revocation does not wait for the cache TTL.
	invalidate func()
	touchMu    sync.Mutex
	touched    map[string]time.Time
}

func NewKeys(store accounts.AccountStore) *Keys {
	if store == nil {
		return nil
	}
	return &Keys{store: store, touched: make(map[string]time.Time)}
}

func (k *Keys) SetInvalidator(fn func()) {
	if k == nil {
		return
	}
	k.invalidate = fn
}

func (k *Keys) changed() {
	if k != nil && k.invalidate != nil {
		k.invalidate()
	}
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
		Name:             prepared.Name,
		Prefix:           accounts.APIKeyPrefix(secret),
		KeyHash:          accounts.HashAPIKey(secret),
		Providers:        prepared.Providers,
		Enabled:          prepared.Enabled,
		MaxInFlight:      prepared.MaxInFlight,
		ModelAggregation: prepared.ModelAggregation,
		CreatedAt:        now,
		UpdatedAt:        now,
	})
	if err != nil {
		return accounts.APIKey{}, err
	}
	k.changed()
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
	saved, err := k.store.SaveAPIKey(ctx, accounts.StoredAPIKey{
		ID:               merged.ID,
		Name:             merged.Name,
		Prefix:           merged.Prefix,
		Providers:        merged.Providers,
		Enabled:          merged.Enabled,
		MaxInFlight:      merged.MaxInFlight,
		ModelAggregation: merged.ModelAggregation,
		UpdatedAt:        time.Now().UTC(),
	})
	if err != nil {
		return accounts.APIKey{}, err
	}
	k.changed()
	return saved, nil
}

func (k *Keys) Delete(ctx context.Context, id string) error {
	if err := k.store.DeleteAPIKey(ctx, id); err != nil {
		return err
	}
	k.touchMu.Lock()
	delete(k.touched, id)
	k.touchMu.Unlock()
	k.changed()
	return nil
}

func (k *Keys) SetConsoleSecret(ctx context.Context, secretName, secret string) error {
	return k.store.SetSecret(ctx, secretName, secret)
}

func (k *Keys) Touch(ctx context.Context, id string) error {
	now := time.Now()
	k.touchMu.Lock()
	if last, ok := k.touched[id]; ok && now.Sub(last) < touchWriteInterval {
		k.touchMu.Unlock()
		return nil
	}
	k.touched[id] = now
	k.touchMu.Unlock()

	if err := k.store.TouchAPIKey(ctx, id); err != nil {
		// A failed write must not suppress the next attempt for a full interval.
		k.touchMu.Lock()
		delete(k.touched, id)
		k.touchMu.Unlock()
		return err
	}
	return nil
}
