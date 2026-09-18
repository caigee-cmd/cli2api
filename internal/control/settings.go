package control

import (
	"context"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

type Settings struct {
	store accounts.AccountStore
}

func NewSettings(store accounts.AccountStore) *Settings {
	if store == nil {
		return nil
	}
	return &Settings{store: store}
}

func (s *Settings) GetSecret(ctx context.Context, name string) (string, bool, error) {
	return s.store.GetSecret(ctx, name)
}

func (s *Settings) SetSecret(ctx context.Context, name, value string) error {
	return s.store.SetSecret(ctx, name, value)
}

func (s *Settings) SetSecretOrEmpty(ctx context.Context, name, value string) error {
	return s.store.SetSecretOrEmpty(ctx, name, value)
}

func (s *Settings) WorkBuddyCheckinTimeDefault(ctx context.Context) string {
	return s.store.WorkBuddyCheckinTimeDefault(ctx)
}

func (s *Settings) GetModelContext(ctx context.Context, modelID string) (int, bool, error) {
	return s.store.GetModelContext(ctx, modelID)
}

func (s *Settings) SetModelContext(ctx context.Context, modelID string, contextLength int) error {
	return s.store.SetModelContext(ctx, modelID, contextLength)
}

func (s *Settings) ListModelContexts(ctx context.Context) (map[string]int, error) {
	return s.store.ListModelContexts(ctx)
}

func (s *Settings) GetProviderModelSetting(ctx context.Context, provider, modelID string) (accounts.ProviderModelSetting, error) {
	return s.store.GetProviderModelSetting(ctx, provider, modelID)
}

func (s *Settings) SetProviderModelSetting(ctx context.Context, provider, modelID string, setting accounts.ProviderModelSetting) error {
	return s.store.SetProviderModelSetting(ctx, provider, modelID, setting)
}
