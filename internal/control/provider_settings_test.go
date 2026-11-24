package control

import (
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func TestProviderPolicyAllowsAggregationModelAliasesAndAllowlist(t *testing.T) {
	policy := accounts.ProviderPolicy{
		EnabledModels: []string{"deepseek-flash", "glm-5.3-flash"},
		ModelAliases:  map[string]string{"fast": "deepseek-flash"},
	}
	for _, model := range []string{"deepseek-flash", "GLM-5.3-Flash", "fast", "qoder/deepseek-flash"} {
		if !providerPolicyAllowsModel(policy, model) {
			t.Fatalf("expected %q to be allowed", model)
		}
	}
	for _, model := range []string{"qwen4-max", "auto", "missing-model"} {
		if providerPolicyAllowsModel(policy, model) {
			t.Fatalf("expected %q to be rejected", model)
		}
	}
	if !providerPolicyAllowsModel(accounts.ProviderPolicy{}, "anything") {
		t.Fatal("an empty enabled list must keep the provider unrestricted")
	}
}

func TestMergeProviderAliasesKeepsFirstTarget(t *testing.T) {
	group := []accounts.Account{
		{ModelAliases: map[string]string{"shared": "one", "first": "a"}},
		{ModelAliases: map[string]string{"shared": "two", "second": "b"}},
	}
	got := mergeProviderAliases(group)
	if got["shared"] != "one" || got["first"] != "a" || got["second"] != "b" {
		t.Fatalf("unexpected merged aliases: %#v", got)
	}
}

func TestMergeProviderModelsUnionsRestrictedAccounts(t *testing.T) {
	group := []accounts.Account{
		{ModelsRestricted: true, EnabledModels: []string{"one", "shared"}},
		{ModelsRestricted: true, EnabledModels: []string{"shared", "two"}},
	}
	got := mergeProviderModels(group)
	if len(got) != 3 || got[0] != "one" || got[1] != "shared" || got[2] != "two" {
		t.Fatalf("unexpected merged models: %#v", got)
	}
}

func TestMergeProviderModelsRelaxesWhenAnyAccountIsUnrestricted(t *testing.T) {
	group := []accounts.Account{
		{ModelsRestricted: true, EnabledModels: []string{"one"}},
		{ModelsRestricted: false},
	}
	if got := mergeProviderModels(group); len(got) != 0 {
		t.Fatalf("expected unrestricted provider seed, got %#v", got)
	}
}

func TestProviderBaseProxyUsesAccountOnFirstSave(t *testing.T) {
	all := []accounts.Account{
		{Provider: "trae", ProviderRegion: "cn"},
		{Provider: "trae", ProviderRegion: "cn", ProxyURL: "http://user:secret@example.test:8080"},
	}
	got := providerBaseProxy(accounts.ProviderPolicy{}, false, all, "trae", "cn")
	if got != all[1].ProxyURL {
		t.Fatalf("expected account proxy %q, got %q", all[1].ProxyURL, got)
	}
	if got := providerBaseProxy(accounts.ProviderPolicy{ProxyURL: "http://saved.test:8080"}, true, all, "trae", "cn"); got != "http://saved.test:8080" {
		t.Fatalf("expected saved policy proxy, got %q", got)
	}
}

func TestStripProviderOwnedSettingsKeepsIdentityFields(t *testing.T) {
	maxInFlight := 7
	enabled := true
	credit := 3.0
	proxyURL := "http://user:pass@example.test:8080"
	drop := true
	checkin := "18:30"
	input := accounts.UpdateAccount{
		Name:             "kept",
		Enabled:          &enabled,
		MaxInFlight:      &maxInFlight,
		CreditFloor:      &credit,
		SetCreditFloor:   true,
		ProxyURL:         &proxyURL,
		DropSystemPrompt: &drop,
		ModelAliases:     map[string]string{"a": "b"},
		EnabledModels:    []string{"m"},
		SetModelRouting:  true,
		CheckinTime:      &checkin,
	}
	got := accounts.StripProviderOwnedSettings(input)
	if got.Name != "kept" || got.Enabled == nil || !*got.Enabled || got.MaxInFlight == nil || *got.MaxInFlight != 7 {
		t.Fatalf("identity fields changed: %#v", got)
	}
	if got.CreditFloor != nil || got.ProxyURL != nil || got.DropSystemPrompt != nil || got.SetModelRouting || got.ModelAliases != nil || got.EnabledModels != nil {
		t.Fatalf("provider-owned fields not stripped: %#v", got)
	}
	if got.CheckinTime != nil {
		t.Fatalf("check-in override not cleared: %#v", got.CheckinTime)
	}
}
