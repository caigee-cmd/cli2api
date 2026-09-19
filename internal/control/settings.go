package control

import (
	"context"
	"strings"

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
	if err := accounts.ValidateModelContextLength(contextLength); err != nil {
		return operationError("model_setting_failed", err.Error())
	}
	if err := s.store.SetModelContext(ctx, modelID, contextLength); err != nil {
		return operationError("model_setting_failed", err.Error())
	}
	return nil
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

type ProviderModelSettingPatch struct {
	MaxMode         *bool
	ReasoningEffort *string
}

type ModelSetting struct {
	Provider             string
	Model                string
	ContextLength        int
	DefaultContextLength int
	ContextCustom        bool
	MaxMode              bool
	ReasoningEffort      string
}

func (s *Settings) ReadModelSetting(ctx context.Context, provider, modelID string) (ModelSetting, error) {
	modelID = ModelContextKey(modelID)
	provider = strings.ToLower(strings.TrimSpace(provider))
	out := ModelSetting{Provider: provider, Model: modelID}
	switch provider {
	case "trae", "workbuddy":
		setting, err := s.GetProviderModelSetting(ctx, provider, modelID)
		if err != nil {
			return ModelSetting{}, operationError("model_setting_failed", err.Error())
		}
		out.MaxMode = setting.MaxMode
		out.ReasoningEffort = setting.ReasoningEffort
		out.ContextCustom = setting.MaxMode || setting.ReasoningEffort != ""
		return out, nil
	default:
		value, custom, err := s.GetModelContext(ctx, modelID)
		if err != nil {
			return ModelSetting{}, operationError("model_setting_failed", err.Error())
		}
		defaultValue := DefaultContextForModel(modelID)
		if !custom {
			value = defaultValue
		}
		out.ContextLength = value
		out.DefaultContextLength = defaultValue
		out.ContextCustom = custom
		return out, nil
	}
}

func (s *Settings) UpdateModelSetting(ctx context.Context, provider, modelID string, contextLength *int, patch ProviderModelSettingPatch) (ModelSetting, error) {
	modelID = ModelContextKey(modelID)
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case "trae", "workbuddy":
		setting, err := s.UpdateProviderModelSetting(ctx, provider, modelID, patch)
		if err != nil {
			return ModelSetting{}, err
		}
		return ModelSetting{
			Provider:        provider,
			Model:           modelID,
			MaxMode:         setting.MaxMode,
			ReasoningEffort: setting.ReasoningEffort,
			ContextCustom:   setting.MaxMode || setting.ReasoningEffort != "",
		}, nil
	default:
		length := 0
		if contextLength != nil {
			length = *contextLength
		}
		if err := s.SetModelContext(ctx, modelID, length); err != nil {
			return ModelSetting{}, err
		}
		return s.ReadModelSetting(ctx, provider, modelID)
	}
}

func (s *Settings) UpdateProviderModelSetting(ctx context.Context, provider, modelID string, patch ProviderModelSettingPatch) (accounts.ProviderModelSetting, error) {
	if patch.MaxMode == nil && patch.ReasoningEffort == nil {
		return accounts.ProviderModelSetting{}, operationError("invalid_request", "max_mode or reasoning_effort required")
	}
	if provider == "workbuddy" && patch.MaxMode != nil {
		return accounts.ProviderModelSetting{}, operationError("invalid_request", "workbuddy has no max-mode switch")
	}
	setting, err := s.GetProviderModelSetting(ctx, provider, modelID)
	if err != nil {
		return accounts.ProviderModelSetting{}, operationError("model_setting_failed", err.Error())
	}
	if patch.MaxMode != nil {
		setting.MaxMode = *patch.MaxMode
	}
	if patch.ReasoningEffort != nil {
		setting.ReasoningEffort = strings.TrimSpace(*patch.ReasoningEffort)
	}
	if err := s.SetProviderModelSetting(ctx, provider, modelID, setting); err != nil {
		return accounts.ProviderModelSetting{}, operationError("model_setting_failed", err.Error())
	}
	return setting, nil
}

// ModelContextKey normalizes a console setting key without treating an empty
// input as the routing model "auto".
func ModelContextKey(model string) string {
	key := strings.ToLower(strings.TrimSpace(model))
	return strings.NewReplacer("_", "-", " ", "-").Replace(key)
}

// DefaultContextForModel is shared by catalog decoration and settings HTTP.
func DefaultContextForModel(model string) int {
	if ModelContextKey(model) == "minimax-m3" {
		return 1000000
	}
	return 180000
}
