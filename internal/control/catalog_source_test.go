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

func TestProviderModelEntryCarriesDeclaredImageSupport(t *testing.T) {
	vision := ProviderModelEntry(providers.ModelInfo{
		NativeModel: "deepseek-v4.1-flash", PublicModel: "deepseek-v4.1-flash",
		Capabilities: providers.ModelCapabilities{Images: true},
	}, "workbuddy")
	if vision["supports_images"] != true {
		t.Fatalf("declared vision model should carry supports_images=true: %v", vision)
	}
	textOnly := ProviderModelEntry(providers.ModelInfo{
		NativeModel: "deepseek-v3.2", PublicModel: "deepseek-v3.2",
		Capabilities: providers.ModelCapabilities{Images: false},
	}, "workbuddy")
	if _, present := textOnly["supports_images"]; present {
		t.Fatalf("undeclared image support must stay absent: %v", textOnly)
	}
	if mergedCaps := ModelCapabilitiesEntry(providers.ModelInfo{
		Capabilities: providers.ModelCapabilities{Images: true},
	}); mergedCaps["supports_images"] != true {
		t.Fatalf("merge capabilities should carry supports_images=true: %v", mergedCaps)
	}
	if mergedCaps := ModelCapabilitiesEntry(providers.ModelInfo{
		Capabilities: providers.ModelCapabilities{Images: false},
	}); mergedCaps["supports_images"] != false {
		t.Fatalf("merge view must state an explicit false, not omit it: %v", mergedCaps)
	}
}

func TestCatalogSourceMergeRequiresEveryRegionToDeclareImages(t *testing.T) {
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
		"wb-cn": {
			{NativeModel: "deepseek-v4.1-flash", PublicModel: "deepseek-v4.1-flash", Capabilities: providers.ModelCapabilities{Images: true}},
			{NativeModel: "deepseek-v4-pro", PublicModel: "deepseek-v4-pro", Capabilities: providers.ModelCapabilities{Images: true}},
			{NativeModel: "deepseek-v3.2", PublicModel: "deepseek-v3.2"},
		},
		"wb-global": {
			{NativeModel: "deepseek-v4.1-flash", PublicModel: "deepseek-v4.1-flash", Capabilities: providers.ModelCapabilities{Images: false}},
			{NativeModel: "deepseek-v4-pro", PublicModel: "deepseek-v4-pro", Capabilities: providers.ModelCapabilities{Images: true}},
			{NativeModel: "deepseek-v3.2", PublicModel: "deepseek-v3.2"},
		},
	}}})

	merged, err := source.Fetch(false, "", CatalogModeMerge)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]map[string]any{}
	for _, entry := range merged {
		byID[entry["id"].(string)] = entry
	}
	if byID["deepseek-v4.1-flash"]["supports_images"] != false {
		// One region does not declare image input, so the merged route cannot
		// claim it: a request routed there would be rejected upstream.
		t.Fatalf("mixed regions must merge to supports_images=false: %v", byID["deepseek-v4.1-flash"])
	}
	if byID["deepseek-v4-pro"]["supports_images"] != true {
		t.Fatalf("every region declares images: %v", byID["deepseek-v4-pro"])
	}
	if _, present := byID["deepseek-v3.2"]["supports_images"]; present {
		// No region declared image input, so the public entry must stay silent
		// instead of advertising an explicit false the provider never stated.
		t.Fatalf("undeclared model must not grow a false flag: %v", byID["deepseek-v3.2"])
	}
}

func TestCatalogSourceQoderMirrorsVisionFlag(t *testing.T) {
	source := &CatalogSource{
		Accounts: func() []CatalogAccount {
			return []CatalogAccount{{ID: "q-cn", Provider: "qoder", Region: "cn", Worker: true}}
		},
		Providers: providers.NewRegistry(),
		WorkerModels: func(context.Context, string, bool) ([]map[string]any, error) {
			return []map[string]any{
				{"id": "vision-model", "is_vl": true},
				{"id": "text-model", "is_vl": false},
				{"id": "undeclared-model"},
			}, nil
		},
	}
	models, err := source.Fetch(false, "q-cn", CatalogModeMerge)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]map[string]any{}
	for _, entry := range models {
		byID[entry["id"].(string)] = entry
	}
	if byID["vision-model"]["supports_images"] != true {
		t.Fatalf("is_vl=true must surface: %v", byID["vision-model"])
	}
	if byID["text-model"]["supports_images"] != false {
		t.Fatalf("is_vl=false must surface: %v", byID["text-model"])
	}
	if byID["undeclared-model"]["supports_images"] != true {
		// The Qoder adapter sends is_vl=true for every request, so a catalog
		// entry without an explicit flag inherits that declared behavior.
		t.Fatalf("absent is_vl must follow the adapter default: %v", byID["undeclared-model"])
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

func TestFilterModelsByAccountAllowDropsModelsNoGrantedAccountServes(t *testing.T) {
	models := []map[string]any{
		{"id": "glm-5.2", "provider": "qoder", "regions": []string{"global"}},
		{"id": "kimi-k2.6", "provider": "qoder", "regions": []string{"global"}},
		{"id": "qoder/deepseek-v4", "provider": "qoder", "regions": []string{"cn"}},
		{"id": "trae-only", "provider": "trae", "regions": []string{"cn"}},
	}
	pool := []AccountModelAllow{
		{Provider: "qoder", Region: "global", ModelsRestricted: true, EnabledModels: []string{"glm-5.2"}},
		{Provider: "qoder", Region: "cn", ModelsRestricted: true, EnabledModels: []string{"glm-5.2"}},
		{Provider: "trae", Region: "cn"},
	}
	key := auth.Identity{Kind: auth.KindKey, AllowedProviders: []string{"qoder"}}
	filtered := FilterModelsByAccountAllow(models, key, pool)
	if len(filtered) != 1 || filtered[0]["id"] != "glm-5.2" {
		t.Fatalf("qoder key filtered=%v", filtered)
	}

	// An alias stays visible only when its target is one this account may serve.
	aliased := []AccountModelAllow{{
		Provider: "qoder", Region: "global", ModelsRestricted: true,
		EnabledModels: []string{"glm-5.2"}, Aliases: map[string]string{"my-glm": "glm-5.2", "my-kimi": "kimi-k2.6"},
	}}
	withAlias := append(models, map[string]any{"id": "my-glm", "provider": "qoder", "regions": []string{"global"}})
	withAlias = append(withAlias, map[string]any{"id": "my-kimi", "provider": "qoder", "regions": []string{"global"}})
	filtered = FilterModelsByAccountAllow(withAlias, key, aliased)
	got := map[string]bool{}
	for _, model := range filtered {
		id, _ := model["id"].(string)
		got[id] = true
	}
	if !got["glm-5.2"] || !got["my-glm"] || got["my-kimi"] || got["kimi-k2.6"] {
		t.Fatalf("alias filtered=%v", got)
	}

	// One unrestricted account of the same provider keeps the whole catalog,
	// and an empty pool (tests, pool not yet reported) changes nothing.
	open := []AccountModelAllow{{Provider: "qoder", Region: "global"}}
	// The CN-only entry stays hidden: this account does not cover that region.
	if n := len(FilterModelsByAccountAllow(models, key, open)); n != 2 {
		t.Fatalf("unrestricted qoder account hid models: %d", n)
	}
	if n := len(FilterModelsByAccountAllow(models, key, nil)); n != len(models) {
		t.Fatalf("empty pool filtered %d of %d", n, len(models))
	}
}

func TestFilterModelsByAccountAllowHonorsAuto(t *testing.T) {
	models := []map[string]any{
		{"id": "auto", "provider": "qoder", "regions": []string{"cn"}},
		{"id": "deepseek-flash", "provider": "qoder", "regions": []string{"cn"}},
		{"id": "glm-5.3-flash", "provider": "qoder", "regions": []string{"cn"}},
	}
	key := auth.Identity{Kind: auth.KindKey, AllowedProviders: []string{"qoder"}}
	ids := func(filtered []map[string]any) map[string]bool {
		seen := map[string]bool{}
		for _, model := range filtered {
			id, _ := model["id"].(string)
			seen[id] = true
		}
		return seen
	}

	restricted := []AccountModelAllow{{
		Provider: "qoder", Region: "cn", ModelsRestricted: true,
		EnabledModels: []string{"deepseek-flash", "glm-5.3-flash"},
	}}
	got := FilterModelsByAccountAllow(models, key, restricted)
	if len(got) != 2 || ids(got)["auto"] || !ids(got)["deepseek-flash"] || !ids(got)["glm-5.3-flash"] {
		t.Fatalf("restricted auto leaked: %v", ids(got))
	}

	autoEnabled := []AccountModelAllow{{
		Provider: "qoder", Region: "cn", ModelsRestricted: true,
		EnabledModels: []string{"deepseek-flash", "auto"},
	}}
	if got := ids(FilterModelsByAccountAllow(models, key, autoEnabled)); !got["auto"] || !got["deepseek-flash"] || got["glm-5.3-flash"] {
		t.Fatalf("enabled auto filtered: %v", got)
	}

	unrestricted := []AccountModelAllow{{Provider: "qoder", Region: "cn"}}
	if got := ids(FilterModelsByAccountAllow(models, key, unrestricted)); len(got) != 3 || !got["auto"] {
		t.Fatalf("unrestricted auto hidden: %v", got)
	}
}

func TestExpandModelAliasesAddsPublicAlias(t *testing.T) {
	models := []map[string]any{
		{"id": "kimi-k2.6", "provider": "qoder", "regions": []string{"global"}, "native_model": "kimi-native-q"},
		{"id": "kimi-k2.5", "provider": "workbuddy", "regions": []string{"cn"}, "native_model": "kimi-native-w"},
	}
	pool := []AccountModelAllow{
		{Provider: "qoder", Region: "global", ModelsRestricted: true, EnabledModels: []string{"kimi-k2.6"}, Aliases: map[string]string{"kimi-k3": "kimi-k2.6"}},
		{Provider: "workbuddy", Region: "cn", ModelsRestricted: true, EnabledModels: []string{"kimi-k2.5"}, Aliases: map[string]string{"kimi-k3": "kimi-k2.5"}},
	}
	got := ExpandModelAliases(models, pool)
	if len(got) != 4 {
		t.Fatalf("expanded len=%d %v", len(got), got)
	}
	byProvider := map[string]map[string]any{}
	for _, entry := range got {
		id, _ := entry["id"].(string)
		if id == "kimi-k3" {
			byProvider[modelEntryProvider(entry)] = entry
		}
	}
	if byProvider["qoder"]["alias_of"] != "kimi-k2.6" || byProvider["workbuddy"]["alias_of"] != "kimi-k2.5" {
		t.Fatalf("alias providers=%v", byProvider)
	}
	if byProvider["qoder"]["display_name"] != "kimi-k3" {
		t.Fatalf("display name=%v", byProvider["qoder"])
	}
	visible := FilterModelsByAccountAllow(got, auth.Identity{Kind: auth.KindKey, AllowedProviders: []string{"qoder"}}, pool)
	ids := map[string]bool{}
	for _, entry := range visible {
		id, _ := entry["id"].(string)
		ids[id] = true
	}
	if !ids["kimi-k2.6"] || !ids["kimi-k3"] || ids["kimi-k2.5"] {
		t.Fatalf("alias visibility=%v", ids)
	}

	// A real catalog entry wins over an alias with the same id.
	withReal := append(models, map[string]any{"id": "kimi-k3", "provider": "qoder"})
	if n := len(ExpandModelAliases(withReal, pool)); n != 3 {
		t.Fatalf("real id did not win: %d", n)
	}
	// A disabled target contributes no public alias.
	disabled := []AccountModelAllow{{
		Provider: "qoder", Region: "global", ModelsRestricted: true,
		EnabledModels: []string{"other"}, Aliases: map[string]string{"kimi-k3": "kimi-k2.6"},
	}}
	if n := len(ExpandModelAliases(models, disabled)); n != 2 {
		t.Fatalf("disabled target expanded: %d", n)
	}
}

func TestAggregateModelEntriesFoldsProviderAliases(t *testing.T) {
	models := []map[string]any{
		{"id": "kimi-k2.6", "provider": "qoder", "regions": []string{"global"}, "native_model": "kimi-native-q"},
		{"id": "kimi-k2.5", "provider": "workbuddy", "regions": []string{"cn"}, "native_model": "kimi-native-w"},
	}
	pool := []AccountModelAllow{
		{Provider: "qoder", Region: "global", ModelsRestricted: true, EnabledModels: []string{"kimi-k2.6"}, Aliases: map[string]string{"kimi-k3": "kimi-k2.6"}},
		{Provider: "workbuddy", Region: "cn", ModelsRestricted: true, EnabledModels: []string{"kimi-k2.5"}, Aliases: map[string]string{"kimi-k3": "kimi-k2.5"}},
	}
	identity := auth.Identity{Kind: auth.KindKey, ModelAggregation: true}
	visible := FilterModelsForIdentity(identity, FilterModelsByAccountAllow(ExpandModelAliases(models, pool), identity, pool))
	aggregated := AggregateModelEntries(visible, identity, pool)
	if len(aggregated) != 1 {
		t.Fatalf("aggregated=%v", aggregated)
	}
	entry := aggregated[0]
	if entry["id"] != "kimi-k3" || entry["owned_by"] != "aggregate" || entry["display_name"] != "kimi-k3" {
		t.Fatalf("aggregated entry=%v", entry)
	}
	for _, field := range []string{"provider", "native_model", "alias_of", "regions"} {
		if _, present := entry[field]; present {
			t.Fatalf("%s leaked: %v", field, entry)
		}
	}
	// A key without the switch keeps the normal catalog view.
	if plain := AggregateModelEntries(visible, auth.Identity{Kind: auth.KindKey}, pool); len(plain) != len(visible) {
		t.Fatalf("plain key list changed: %v", plain)
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
	for _, provider := range []string{"trae", "workbuddy", "kimi", "codearts"} {
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
			entry := map[string]any{
				"id": "glm-5.2", "provider": scenario.provider, "reasoning_default": "medium",
				"supports_max_mode": scenario.supportsMax,
			}
			// A real Max tier backs the "supported" case; without it the toggle
			// would be a no-op and must not be surfaced.
			if scenario.supportsMax {
				entry["catalog_context_length"] = 200000
				entry["catalog_context_length_max"] = 1000000
			}
			models := DecorateModelsWithContext(context.Background(), NewSettings(store), []map[string]any{entry})
			if models[0]["context_custom"] != scenario.wantCustom || models[0]["max_mode"] != scenario.wantCustom {
				test.Fatalf("model=%v", models[0])
			}
		})
	}
}

// A model tagged with upstream's v2_max_mode_enabled but carrying no larger
// window and no larger ceilings has no real Max tier: the toggle must be hidden
// (supports_max_mode=false) rather than offered as a no-op.
func TestDecorateHidesMaxModeWithoutRealTier(test *testing.T) {
	store := newFakeStore(&callLog{})
	store.providerSetting = accounts.ProviderModelSetting{MaxMode: true}
	models := DecorateModelsWithContext(context.Background(), NewSettings(store), []map[string]any{{
		"id": "kimi-k2.7-code", "provider": "trae",
		"catalog_context_length": 200000, "supports_max_mode": true,
	}})
	if models[0]["supports_max_mode"] != false || models[0]["max_mode"] != false {
		test.Fatalf("no-op toggle leaked: %v", models[0])
	}
}

// A ceiling-only Max tier (a larger prompt/output limit but the same window) is
// still a real tier and must keep the toggle.
func TestDecorateKeepsMaxModeForCeilingOnlyTier(test *testing.T) {
	store := newFakeStore(&callLog{})
	models := DecorateModelsWithContext(context.Background(), NewSettings(store), []map[string]any{{
		"id": "glm-5.3", "provider": "trae",
		"catalog_context_length": 200000, "max_output_tokens": 16000,
		"max_output_tokens_max": 64000, "supports_max_mode": true,
	}})
	if models[0]["supports_max_mode"] != true {
		test.Fatalf("ceiling-only tier dropped: %v", models[0])
	}
}

// A Trae model with a real Max tier is unlocked by the catalogue itself: the
// window the console advertises is the larger tier, with no stored setting
// needed, while the prompt and output ceilings stay at their catalog defaults.
func TestDecorateTraeMaxWindowIsUnlockedWithoutSetting(t *testing.T) {
	entry := map[string]any{
		"id": "glm-5.3", "provider": "trae",
		"catalog_context_length": 200000, "catalog_context_length_max": 1000000,
		"prompt_max_tokens": 168000, "max_output_tokens": 32000,
		"prompt_max_tokens_max": 936000, "max_output_tokens_max": 64000,
		"supports_max_mode": true, "reasoning_default": "high",
	}
	store := newFakeStore(&callLog{})
	decorated := DecorateModelsWithContext(context.Background(), NewSettings(store), []map[string]any{cloneMap(entry)})
	if decorated[0]["max_mode"] != true || decorated[0]["context_length"] != 1000000 {
		t.Fatalf("max tier not unlocked: %v", decorated[0])
	}
	if decorated[0]["default_context_length"] != 200000 {
		t.Fatalf("default tier lost: %v", decorated[0])
	}
	if decorated[0]["prompt_max_tokens"] != 168000 || decorated[0]["max_output_tokens"] != 32000 {
		t.Fatalf("ceilings must stay at baseline: %v", decorated[0])
	}
	if decorated[0]["prompt_max_tokens_max"] != 936000 || decorated[0]["max_output_tokens_max"] != 64000 {
		t.Fatalf("max tier must remain available: %v", decorated[0])
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func TestDecorateModelsWithContextQoderMaxMode(t *testing.T) {
	store := newFakeStore(&callLog{})
	settings := NewSettings(store)
	entry := map[string]any{
		"id": "qwen3.8-max", "provider": "qoder",
		"catalog_context_length": 200000, "catalog_context_length_max": 1000000,
	}

	off := DecorateModelsWithContext(context.Background(), settings, []map[string]any{cloneMap(entry)})[0]
	if off["supports_max_mode"] != true || off["max_mode"] != false || off["context_length"] != 200000 {
		t.Fatalf("qoder max off = %v", off)
	}

	// Toggle on sends the target window (the frontend has it from the catalog).
	on := true
	maxWindow := 1000000
	if _, err := settings.UpdateModelSetting(context.Background(), "qoder", "qwen3.8-max", &maxWindow, ProviderModelSettingPatch{MaxMode: &on}); err != nil {
		t.Fatal(err)
	}
	decorated := DecorateModelsWithContext(context.Background(), settings, []map[string]any{cloneMap(entry)})[0]
	if decorated["max_mode"] != true || decorated["context_length"] != 1000000 || decorated["context_custom"] != true {
		t.Fatalf("qoder max on = %v", decorated)
	}
}
