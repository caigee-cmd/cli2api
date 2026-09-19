package control

import (
	"context"
	"errors"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

type stubModels struct {
	byAccount map[string][]providers.ModelInfo
	err       error
}

func (s stubModels) Models(_ context.Context, accountID string) ([]providers.ModelInfo, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.byAccount != nil {
		return s.byAccount[accountID], nil
	}
	return nil, nil
}

func TestCatalogSourceMergeDedupsPublicIDAndUnionsRegions(t *testing.T) {
	source := &CatalogSource{
		Accounts: func() []CatalogAccount {
			return []CatalogAccount{
				{ID: "wb-cn", Provider: "workbuddy", Region: "cn"},
				{ID: "wb-global", Provider: "workbuddy", Region: "global"},
			}
		},
		Providers: providers.NewRegistry(),
		WorkerModels: func(context.Context, string, bool) ([]map[string]any, error) {
			return nil, nil
		},
	}
	source.Providers.Register(providers.Adapter{ID: "workbuddy", Models: stubModels{byAccount: map[string][]providers.ModelInfo{
		"wb-cn":     {{NativeModel: "deep-model", PublicModel: "deepseek-v4.1-flash", Capabilities: providers.ModelCapabilities{ContextWindow: 128000}}},
		"wb-global": {{NativeModel: "deep-model", PublicModel: "deepseek-v4.1-flash", Capabilities: providers.ModelCapabilities{ContextWindow: 64000}}},
	}}})

	merged, err := source.Fetch(false, "", CatalogModeMerge)
	if err != nil || len(merged) != 1 {
		t.Fatalf("merged=%v err=%v", merged, err)
	}
	if merged[0]["id"] != "deepseek-v4.1-flash" {
		t.Fatalf("id=%v", merged[0]["id"])
	}
	regions := EntryModelRegions(merged[0])
	if len(regions) != 2 {
		t.Fatalf("regions=%v", regions)
	}
	if window, ok := catalogInt(merged[0]["catalog_context_length"]); !ok || window != 64000 {
		t.Fatalf("merge should keep the smaller window: %v", merged[0]["catalog_context_length"])
	}

	expanded, err := source.Fetch(false, "", CatalogModeExpand)
	if err != nil || len(expanded) != 2 {
		t.Fatalf("expanded=%v err=%v", expanded, err)
	}
}

func TestCatalogSourceUnknownAccountDoesNotFallBack(t *testing.T) {
	source := &CatalogSource{
		Accounts: func() []CatalogAccount {
			return []CatalogAccount{{ID: "wb-1", Provider: "workbuddy", Region: "cn"}}
		},
		Providers: providers.NewRegistry(),
		WorkerModels: func(context.Context, string, bool) ([]map[string]any, error) {
			t.Fatal("unknown account must not hit the worker fallback")
			return nil, nil
		},
	}
	source.Providers.Register(providers.Adapter{ID: "workbuddy", Models: stubModels{}})
	_, err := source.Fetch(false, "missing", CatalogModeMerge)
	if err == nil || err.Error() != "account missing not found" {
		t.Fatalf("err=%v", err)
	}
}

func TestCatalogSourceQoderFallbackStampsRegion(t *testing.T) {
	source := &CatalogSource{
		Accounts: func() []CatalogAccount {
			return []CatalogAccount{{ID: "q-cn", Provider: "qoder", Region: "cn", Worker: true}}
		},
		Providers: providers.NewRegistry(),
		WorkerModels: func(_ context.Context, id string, _ bool) ([]map[string]any, error) {
			if id != "q-cn" {
				return nil, errors.New("wrong account")
			}
			return []map[string]any{{"id": "glm-5.2"}}, nil
		},
	}
	models, err := source.Fetch(false, "q-cn", CatalogModeMerge)
	if err != nil || len(models) != 1 {
		t.Fatalf("models=%v err=%v", models, err)
	}
	if models[0]["provider"] != "qoder" {
		t.Fatalf("provider=%v", models[0]["provider"])
	}
	if got := EntryModelRegions(models[0]); len(got) != 1 || got[0] != "cn" {
		t.Fatalf("regions=%v", got)
	}
}

func TestFilterModelsForIdentityDropsDisallowedProviderAndRegion(t *testing.T) {
	models := []map[string]any{
		{"id": "glm-5.2", "provider": "qoder", "regions": []string{"cn"}},
		{"id": "deepseek", "provider": "workbuddy", "regions": []string{"cn"}},
		{"id": "global-only", "provider": "workbuddy", "regions": []string{"global"}},
	}
	filtered := FilterModelsForIdentity(auth.Identity{Kind: auth.KindKey, AllowedProviders: []string{"workbuddy:cn"}}, models)
	if len(filtered) != 1 || filtered[0]["id"] != "deepseek" {
		t.Fatalf("filtered=%v", filtered)
	}
}

func TestDecorateModelsWithContextAppliesTraeMaxMode(t *testing.T) {
	store := newFakeStore(&callLog{})
	store.providerSetting.MaxMode = true
	models := DecorateModelsWithContext(context.Background(), NewSettings(store), []map[string]any{
		{
			"id": "glm-5.2", "provider": "trae",
			"catalog_context_length": 128000, "catalog_context_length_max": 256000,
			"supports_max_mode": true, "reasoning_default": "medium",
		},
		{"id": "glm-5.2", "provider": "qoder", "catalog_context_length": 180000},
	})
	if len(models) != 2 {
		t.Fatalf("models=%v", models)
	}
	if models[0]["max_mode"] != true || models[0]["context_length"] != 256000 || models[0]["context_custom"] != true {
		t.Fatalf("trae decorate=%v", models[0])
	}
	if models[1]["context_length"] != 180000 || models[1]["context_editable"] != true {
		t.Fatalf("qoder decorate=%v", models[1])
	}
}

func TestDecorateProviderSettingsRestoresDefaultReasoningState(test *testing.T) {
	for _, provider := range []string{"trae", "workbuddy"} {
		test.Run(provider, func(test *testing.T) {
			services, _, _, _ := newTestServices()
			source := []map[string]any{{
				"id": "glm-5.2", "provider": provider, "reasoning_default": "medium",
				"catalog_context_length": 200000,
			}}
			for _, effort := range []string{"high", "medium", ""} {
				if _, err := services.Settings.UpdateModelSetting(context.Background(), provider, "glm-5.2", nil, ProviderModelSettingPatch{ReasoningEffort: &effort}); err != nil {
					test.Fatal(err)
				}
				for refresh := 0; refresh < 2; refresh++ {
					models := DecorateModelsWithContext(context.Background(), services.Settings, source)
					wantEffort := effort
					if wantEffort == "" {
						wantEffort = "medium"
					}
					if models[0]["reasoning_effort"] != wantEffort || models[0]["context_custom"] != (effort == "high") {
						test.Fatalf("effort=%q refresh=%d model=%v", effort, refresh, models[0])
					}
				}
			}
			if _, mutated := source[0]["context_custom"]; mutated {
				test.Fatal("catalog source was mutated")
			}
		})
	}
}

func TestDecorateProviderSettingsUsesEffectiveMaxMode(test *testing.T) {
	for _, scenario := range []struct {
		name, provider string
		supportsMax    bool
		wantCustom     bool
	}{
		{"supported", "trae", true, true},
		{"unsupported", "trae", false, false},
		{"workbuddy", "workbuddy", true, false},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			store := newFakeStore(&callLog{})
			store.providerSetting = accounts.ProviderModelSetting{MaxMode: true, ReasoningEffort: "medium"}
			models := DecorateModelsWithContext(context.Background(), NewSettings(store), []map[string]any{{
				"id": "glm-5.2", "provider": scenario.provider, "reasoning_default": "medium",
				"supports_max_mode": scenario.supportsMax,
			}})
			if models[0]["context_custom"] != scenario.wantCustom || models[0]["max_mode"] != scenario.wantCustom {
				test.Fatalf("model=%v", models[0])
			}
		})
	}
}
