package control

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/events"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/store"
)

type providerTestRuntime struct {
	store accounts.AccountStore
	syncs []providerSync
}

type providerSync struct {
	before accounts.Account
	after  accounts.Account
}

func (r *providerTestRuntime) StartAccount(context.Context, accounts.Account) error { return nil }
func (r *providerTestRuntime) StopAccount(string) error                             { return nil }
func (r *providerTestRuntime) RemoveAccount(string) error                           { return nil }
func (r *providerTestRuntime) SyncAccount(_ context.Context, before, after accounts.Account) error {
	r.syncs = append(r.syncs, providerSync{before: before, after: after})
	return nil
}
func (r *providerTestRuntime) AccountView(context.Context, string) (accounts.AccountView, error) {
	return accounts.AccountView{}, nil
}
func (r *providerTestRuntime) Accounts(context.Context) ([]accounts.AccountView, error) {
	return nil, nil
}
func (r *providerTestRuntime) RefreshAccount(context.Context, string, bool) error { return nil }
func (r *providerTestRuntime) RefreshAll(context.Context, bool) error             { return nil }
func (r *providerTestRuntime) RefreshAllResults(context.Context, bool, bool) []accounts.AccountRefreshResult {
	return nil
}
func (r *providerTestRuntime) LiveAccounts(context.Context, bool) ([]accounts.AccountView, error) {
	return nil, nil
}
func (r *providerTestRuntime) RefreshAllAsync(context.Context, bool, bool) {}
func (r *providerTestRuntime) SubscribeEvents(uint64) (<-chan events.Event, func()) {
	ch := make(chan events.Event)
	close(ch)
	return ch, func() {}
}
func (r *providerTestRuntime) CheckinAccount(context.Context, string) (accounts.Account, error) {
	return accounts.Account{}, nil
}
func (r *providerTestRuntime) CheckinAll(context.Context, bool, bool) ([]accounts.CheckinAllResult, error) {
	return nil, nil
}
func (r *providerTestRuntime) ReloadProxyURL(context.Context, string) error { return nil }
func (r *providerTestRuntime) ReplaceProxyAPIKey(context.Context, string) error {
	return nil
}
func (r *providerTestRuntime) WorkerAdmin(context.Context, providers.AdminRequest) (providers.AdminResponse, error) {
	return providers.AdminResponse{}, nil
}
func (r *providerTestRuntime) Store() accounts.AccountStore { return r.store }

func TestProviderSettingsPolicyIsEffectiveWithoutAccountWriteback(t *testing.T) {
	st, err := store.OpenStore(filepath.Join(t.TempDir(), "provider-policy.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	runtime := &providerTestRuntime{store: st}
	ps := NewProviderSettings(runtime)

	first, err := st.Create(context.Background(), accounts.CreateAccount{
		Name: "first", Provider: "trae", Region: "cn",
		ProxyURL:      "http://user:secret@example.test:8080",
		ModelAliases:  map[string]string{"alias-one": "trae-model-one"},
		EnabledModels: []string{"trae-model-one"},
	})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	if err := st.Update(context.Background(), first.ID, accounts.UpdateAccount{
		ModelAliases:    map[string]string{"alias-one": "trae-model-one"},
		EnabledModels:   []string{"trae-model-one"},
		SetModelRouting: true,
	}); err != nil {
		t.Fatalf("save first routing: %v", err)
	}

	rawBefore, err := st.ListRaw(context.Background())
	if err != nil {
		t.Fatalf("list raw before: %v", err)
	}
	firstBefore := findAccount(rawBefore, first.ID)

	list, err := ps.List(context.Background())
	if err != nil {
		t.Fatalf("list seed: %v", err)
	}
	var seed accounts.ProviderPolicy
	for _, policy := range list {
		if policy.Provider == "trae" && policy.Region == "cn" {
			seed = policy
			break
		}
	}
	if seed.Provider == "" {
		t.Fatal("trae cn policy not found")
	}
	if seed.ProxyURL != "http://user:%2A%2A%2A%2A%2A%2A@example.test:8080" {
		t.Fatalf("expected redacted proxy, got %q", seed.ProxyURL)
	}

	seed.CreditFloor = floatPtr(12)
	seed.PercentFloor = floatPtr(34)
	seed.CheckinTime = "10:30"
	manual := false
	seed.CheckinAuto = &manual
	saved, err := ps.Update(context.Background(), seed)
	if err != nil {
		t.Fatalf("update policy: %v", err)
	}
	if saved.ProxyURL != seed.ProxyURL {
		t.Fatalf("saved policy should stay redacted, got %q", saved.ProxyURL)
	}
	if len(runtime.syncs) != 1 {
		t.Fatalf("runtime syncs = %d, want 1", len(runtime.syncs))
	}
	synced := runtime.syncs[0]
	if synced.before.CreditFloor != nil || synced.before.CheckinTime != "" {
		t.Fatalf("sync before state should be the raw account: %#v", synced.before)
	}
	if synced.after.CreditFloor == nil || *synced.after.CreditFloor != 12 ||
		synced.after.CheckinTime != "10:30" ||
		synced.after.ModelAliases["alias-one"] != "trae-model-one" {
		t.Fatalf("sync after state should be the effective policy: %#v", synced.after)
	}

	// Saving the policy must leave the existing account row untouched.
	rawAfter, err := st.ListRaw(context.Background())
	if err != nil {
		t.Fatalf("list raw after: %v", err)
	}
	firstRaw := findAccount(rawAfter, first.ID)
	if firstRaw.ProxyURL != firstBefore.ProxyURL ||
		!sameOptionalFloat(firstRaw.CreditFloor, firstBefore.CreditFloor) ||
		!sameOptionalFloat(firstRaw.PercentFloor, firstBefore.PercentFloor) ||
		firstRaw.CheckinTime != firstBefore.CheckinTime ||
		firstRaw.ModelAliases["alias-one"] != "trae-model-one" {
		t.Fatalf("provider save wrote account row: before=%#v after=%#v", firstBefore, firstRaw)
	}

	firstAfter, err := st.Get(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("get first: %v", err)
	}
	if firstAfter.ProxyURL != "http://user:secret@example.test:8080" {
		t.Fatalf("first account lost full proxy: %q", firstAfter.ProxyURL)
	}
	if firstAfter.CreditFloor == nil || *firstAfter.CreditFloor != 12 || firstAfter.PercentFloor == nil || *firstAfter.PercentFloor != 34 {
		t.Fatalf("first account floors not effective: %#v", firstAfter)
	}
	if firstAfter.CheckinTime != "10:30" {
		t.Fatalf("first account check-in time not effective: %q", firstAfter.CheckinTime)
	}

	second, err := st.Create(context.Background(), accounts.CreateAccount{
		Name: "second", Provider: "trae", Region: "cn",
		ProxyURL:     "direct",
		ModelAliases: map[string]string{"old": "old-model"},
	})
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	secondRawList, err := st.ListRaw(context.Background())
	if err != nil {
		t.Fatalf("list raw after second create: %v", err)
	}
	secondRaw := findAccount(secondRawList, second.ID)
	if secondRaw.ProxyURL != "" || secondRaw.CreditFloor != nil || secondRaw.CheckinTime != "" ||
		len(secondRaw.ModelAliases) != 0 || len(secondRaw.EnabledModels) != 0 {
		t.Fatalf("new account persisted provider-owned fields: %#v", secondRaw)
	}
	secondAfter, err := st.Get(context.Background(), second.ID)
	if err != nil {
		t.Fatalf("get second: %v", err)
	}
	if secondAfter.ProxyURL != firstAfter.ProxyURL || secondAfter.CreditFloor == nil || *secondAfter.CreditFloor != 12 {
		t.Fatalf("second account did not inherit effective provider policy: %#v", secondAfter)
	}
	if secondAfter.CheckinTime != "10:30" {
		t.Fatalf("second account check-in time not effective: %q", secondAfter.CheckinTime)
	}
	if secondAfter.ModelAliases["alias-one"] != "trae-model-one" || len(secondAfter.ModelAliases) != 1 {
		t.Fatalf("second account routing did not inherit effective provider policy: %#v", secondAfter.ModelAliases)
	}
}

func TestProviderSettingsSeedMissingMigratesExistingGroup(t *testing.T) {
	st, err := store.OpenStore(filepath.Join(t.TempDir(), "provider-seed.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	drop := false
	account, err := st.Create(context.Background(), accounts.CreateAccount{
		Name: "existing", Provider: "trae", Region: "cn",
		CreditFloor:      floatPtr(5),
		PercentFloor:     floatPtr(10),
		ProxyURL:         "http://user:secret@example.test:8080",
		DropSystemPrompt: &drop,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.Update(context.Background(), account.ID, accounts.UpdateAccount{
		ModelAliases:    map[string]string{"fast": "trae-fast"},
		EnabledModels:   []string{"trae-fast"},
		SetModelRouting: true,
	}); err != nil {
		t.Fatalf("save routing: %v", err)
	}
	rawBefore, err := st.ListRaw(context.Background())
	if err != nil {
		t.Fatalf("list raw before: %v", err)
	}
	before := findAccount(rawBefore, account.ID)

	if err := NewProviderSettings(&providerTestRuntime{store: st}).SeedMissing(context.Background()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	policy, found, err := st.GetProviderPolicy(context.Background(), "trae", "cn")
	if err != nil || !found {
		t.Fatalf("trae cn policy: found=%v err=%v", found, err)
	}
	if policy.CreditFloor == nil || *policy.CreditFloor != 5 ||
		policy.PercentFloor == nil || *policy.PercentFloor != 10 ||
		policy.ProxyURL != "http://user:secret@example.test:8080" ||
		policy.DropSystemPrompt == nil || *policy.DropSystemPrompt ||
		policy.ModelAliases["fast"] != "trae-fast" ||
		len(policy.EnabledModels) != 1 || policy.EnabledModels[0] != "trae-fast" {
		t.Fatalf("unexpected seeded policy: %#v", policy)
	}

	rawAfter, err := st.ListRaw(context.Background())
	if err != nil {
		t.Fatalf("list raw after: %v", err)
	}
	after := findAccount(rawAfter, account.ID)
	if after.ProxyURL != before.ProxyURL ||
		!sameOptionalFloat(after.CreditFloor, before.CreditFloor) ||
		!sameOptionalFloat(after.PercentFloor, before.PercentFloor) ||
		after.DropSystemPrompt != before.DropSystemPrompt ||
		after.CheckinTime != before.CheckinTime ||
		after.ModelAliases["fast"] != "trae-fast" {
		t.Fatalf("seeding rewrote account row: before=%#v after=%#v", before, after)
	}

	effective, err := st.Get(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("get effective: %v", err)
	}
	if effective.CreditFloor == nil || *effective.CreditFloor != 5 ||
		effective.ProxyURL != "http://user:secret@example.test:8080" ||
		effective.ModelAliases["fast"] != "trae-fast" {
		t.Fatalf("account did not read seeded provider policy: %#v", effective)
	}
}

func findAccount(list []accounts.Account, id string) accounts.Account {
	for _, account := range list {
		if account.ID == id {
			return account
		}
	}
	return accounts.Account{}
}

func sameOptionalFloat(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func floatPtr(value float64) *float64 { return &value }

func TestProviderSettingsKeepsRegionCheckinTimesSeparate(t *testing.T) {
	st, err := store.OpenStore(filepath.Join(t.TempDir(), "provider-region-checkin.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	ps := NewProviderSettings(&providerTestRuntime{store: st})

	global, err := st.Create(context.Background(), accounts.CreateAccount{Name: "global", Provider: "qoder", Region: "global"})
	if err != nil {
		t.Fatalf("create global: %v", err)
	}
	cn, err := st.Create(context.Background(), accounts.CreateAccount{Name: "cn", Provider: "qoder", Region: "cn"})
	if err != nil {
		t.Fatalf("create cn: %v", err)
	}
	manual := false
	if _, err := ps.Update(context.Background(), accounts.ProviderPolicy{Provider: "qoder", Region: "global", CheckinTime: "10:30", CheckinAuto: &manual}); err != nil {
		t.Fatalf("save global policy: %v", err)
	}
	if _, err := ps.Update(context.Background(), accounts.ProviderPolicy{Provider: "qoder", Region: "cn", CheckinTime: "11:30", CheckinAuto: &manual}); err != nil {
		t.Fatalf("save cn policy: %v", err)
	}
	globalAfter, err := st.Get(context.Background(), global.ID)
	if err != nil {
		t.Fatalf("get global: %v", err)
	}
	cnAfter, err := st.Get(context.Background(), cn.ID)
	if err != nil {
		t.Fatalf("get cn: %v", err)
	}
	if globalAfter.CheckinTime != "10:30" || cnAfter.CheckinTime != "11:30" {
		t.Fatalf("region check-in times collided: global=%q cn=%q", globalAfter.CheckinTime, cnAfter.CheckinTime)
	}
	raw, err := st.ListRaw(context.Background())
	if err != nil {
		t.Fatalf("list raw: %v", err)
	}
	for _, id := range []string{global.ID, cn.ID} {
		account := findAccount(raw, id)
		if account.CheckinTime != "" {
			t.Fatalf("check-in time was persisted on account %s: %#v", id, account)
		}
	}
}

func TestValidateAggregationMembersHonorsProviderPolicy(t *testing.T) {
	st, err := store.OpenStore(filepath.Join(t.TempDir(), "aggregation-policy.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	runtime := &providerTestRuntime{store: st}
	ps := NewProviderSettings(runtime)
	ctx := context.Background()

	account, err := st.Create(ctx, accounts.CreateAccount{Name: "qoder-cn", Provider: "qoder", Region: "cn"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	if _, err := ps.Update(ctx, accounts.ProviderPolicy{
		Provider: "qoder", Region: "cn",
		EnabledModels: []string{"deepseek-flash", "glm-5.3-flash"},
		ModelAliases:  map[string]string{"fast": "deepseek-flash"},
	}); err != nil {
		t.Fatalf("save policy: %v", err)
	}

	validate := func(member accounts.AggregationMember) error {
		return ps.ValidateAggregationMembers(ctx, []accounts.AggregationMember{member})
	}
	for _, member := range []accounts.AggregationMember{
		{Provider: "qoder", Region: "cn", Model: "deepseek-flash"},
		{Provider: "qoder", Region: "cn", Model: "GLM-5.3-Flash"},
		{Provider: "qoder", Region: "cn", Model: "fast"},
		{Provider: "qoder", Model: "deepseek-flash"},
		{Provider: "qoder", Region: "cn", Model: "deepseek-flash", AccountID: account.ID},
	} {
		if err := validate(member); err != nil {
			t.Fatalf("expected member %+v to be allowed: %v", member, err)
		}
	}
	for _, member := range []accounts.AggregationMember{
		{Provider: "qoder", Region: "cn", Model: "qwen4-max"},
		{Provider: "qoder", Model: "qwen4-max"},
		{Provider: "qoder", Region: "global", Model: "deepseek-flash"},
		{Provider: "trae", Region: "cn", Model: "deepseek-flash"},
		{Provider: "qoder", Region: "cn", Model: "deepseek-flash", AccountID: "missing-account"},
	} {
		if err := validate(member); err == nil {
			t.Fatalf("expected member %+v to be rejected", member)
		}
	}
}
