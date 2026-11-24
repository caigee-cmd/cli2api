package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func TestStoreNewAccountReadsProviderPolicy(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	credit := 7.0
	percent := 11.0
	drop := false
	policy := accounts.ProviderPolicy{
		Provider:         "trae",
		Region:           "cn",
		CreditFloor:      &credit,
		PercentFloor:     &percent,
		ProxyURL:         "direct",
		DropSystemPrompt: &drop,
		ModelAliases:     map[string]string{"fast": "trae-fast"},
		EnabledModels:    []string{"trae-fast"},
		ModelsRestricted: true,
	}
	if err := store.SaveProviderPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}

	account, err := store.Create(ctx, accounts.CreateAccount{
		Name:        "Trae",
		Provider:    "trae",
		Region:      "cn",
		Enabled:     false,
		MaxInFlight: 3,
		Priority:    60,
		// The provider policy must win over stale create payload values.
		CreditFloor:      floatPtr(99),
		PercentFloor:     floatPtr(99),
		ProxyURL:         "http://127.0.0.1:9",
		DropSystemPrompt: boolPtr(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	if account.CreditFloor == nil || *account.CreditFloor != credit {
		t.Fatalf("credit floor = %v, want %v", account.CreditFloor, credit)
	}
	if account.PercentFloor == nil || *account.PercentFloor != percent {
		t.Fatalf("percent floor = %v, want %v", account.PercentFloor, percent)
	}
	if account.ProxyURL != "direct" {
		t.Fatalf("proxy = %q, want direct", account.ProxyURL)
	}
	if account.DropSystemPrompt {
		t.Fatal("drop system prompt should inherit the provider policy")
	}
	if account.ModelAliases["fast"] != "trae-fast" || len(account.EnabledModels) != 1 || account.EnabledModels[0] != "trae-fast" {
		t.Fatalf("routing = %v / %v, want provider policy", account.ModelAliases, account.EnabledModels)
	}
	if !account.ModelsRestricted {
		t.Fatal("new account should inherit the provider model restriction")
	}
	raw, err := store.ListRaw(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 1 {
		t.Fatalf("raw accounts = %d, want 1", len(raw))
	}
	stored := raw[0]
	if stored.CreditFloor != nil || stored.PercentFloor != nil || stored.ProxyURL != "" ||
		len(stored.ModelAliases) != 0 || len(stored.EnabledModels) != 0 || stored.ModelsRestricted || stored.CheckinTime != "" {
		t.Fatalf("provider-owned fields were persisted on the account: %#v", stored)
	}
}

func floatPtr(value float64) *float64 {
	return &value
}

func TestStoreUpdateCannotWriteProviderOwnedFields(t *testing.T) {
	ctx := context.Background()
	st, err := OpenStore(filepath.Join(t.TempDir(), "provider-update.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	account, err := st.Create(ctx, accounts.CreateAccount{
		Name:             "Trae",
		Provider:         "trae",
		Region:           "cn",
		Enabled:          false,
		CreditFloor:      floatPtr(1),
		PercentFloor:     floatPtr(2),
		ProxyURL:         "http://account.example:8080",
		DropSystemPrompt: boolPtr(true),
		ModelAliases:     map[string]string{"account": "account-model"},
		EnabledModels:    []string{"account-model"},
		SetModelRouting:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(ctx, account.ID, accounts.UpdateAccount{
		ModelAliases:    map[string]string{"account": "account-model"},
		EnabledModels:   []string{"account-model"},
		SetModelRouting: true,
	}); err != nil {
		t.Fatal(err)
	}
	policy := accounts.ProviderPolicy{
		Provider:         "trae",
		Region:           "cn",
		CreditFloor:      floatPtr(7),
		PercentFloor:     floatPtr(8),
		ProxyURL:         "direct",
		DropSystemPrompt: boolPtr(false),
		ModelAliases:     map[string]string{"provider": "provider-model"},
		EnabledModels:    []string{"provider-model"},
	}
	if err := st.SaveProviderPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}

	credit := 99.0
	percent := 98.0
	proxyURL := "http://patch.example:9090"
	drop := true
	enabled := true
	checkin := "23:00"
	if err := st.Update(ctx, account.ID, accounts.UpdateAccount{
		Enabled:          &enabled,
		CreditFloor:      &credit,
		SetCreditFloor:   true,
		PercentFloor:     &percent,
		SetPercentFloor:  true,
		ProxyURL:         &proxyURL,
		DropSystemPrompt: &drop,
		ModelAliases:     map[string]string{"patched": "patched-model"},
		EnabledModels:    []string{"patched-model"},
		SetModelRouting:  true,
		CheckinTime:      &checkin,
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := st.getRaw(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if raw.CreditFloor == nil || *raw.CreditFloor != 1 ||
		raw.PercentFloor == nil || *raw.PercentFloor != 2 ||
		raw.ProxyURL != "http://account.example:8080" ||
		!raw.DropSystemPrompt ||
		raw.ModelAliases["account"] != "account-model" ||
		len(raw.EnabledModels) != 1 || raw.EnabledModels[0] != "account-model" ||
		raw.CheckinTime != "" {
		t.Fatalf("account PATCH changed stored provider-owned fields: %#v", raw)
	}
	if !raw.Enabled {
		t.Fatal("account-owned enabled state should still change")
	}
	effective, err := st.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if effective.CreditFloor == nil || *effective.CreditFloor != 7 ||
		effective.ProxyURL != "direct" || effective.DropSystemPrompt ||
		effective.ModelAliases["provider"] != "provider-model" {
		t.Fatalf("effective account did not use provider policy: %#v", effective)
	}
}
