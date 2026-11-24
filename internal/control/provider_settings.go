package control

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/proxy"
)

// ProviderSettings keeps the console's provider-wide routing and limit
// settings. The JSON secret is the single source of truth; account rows keep
// only identity/scheduling fields, and Store.Get/List overlay this policy for
// runtime, catalog, check-in, and account views.
type ProviderSettings struct {
	runtime Runtime
}

func NewProviderSettings(runtime Runtime) *ProviderSettings {
	if runtime == nil {
		return nil
	}
	return &ProviderSettings{runtime: runtime}
}

func (p *ProviderSettings) store() accounts.AccountStore {
	return p.runtime.Store()
}

// List returns one redacted policy per provider region. When no policy has
// been saved yet, the oldest existing account seeds scalar values and the
// routing fields are unioned across the provider region's accounts so the
// operator can migrate current custom settings without losing model access.
func (p *ProviderSettings) List(ctx context.Context) ([]accounts.ProviderPolicy, error) {
	store := p.store()
	stored, err := store.ListProviderPolicies(ctx)
	if err != nil {
		return nil, err
	}
	storedByKey := make(map[string]accounts.ProviderPolicy, len(stored))
	for _, policy := range stored {
		storedByKey[accounts.ProviderPolicyKey(policy.Provider, policy.Region)] = policy
	}
	all, err := store.ListRaw(ctx)
	if err != nil {
		return nil, err
	}
	accountsByKey := map[string][]accounts.Account{}
	for _, account := range all {
		key := accounts.ProviderPolicyKey(account.Provider, account.ProviderRegion)
		accountsByKey[key] = append(accountsByKey[key], account)
	}
	list := make([]accounts.ProviderPolicy, 0, len(providers.List()))
	for _, descriptor := range providers.List() {
		for _, region := range descriptor.Regions {
			key := accounts.ProviderPolicyKey(descriptor.ID, region.ID)
			policy, ok := storedByKey[key]
			if !ok {
				// No policy has been saved yet (for example a provider with no
				// accounts). Seed the form from the oldest account so the first
				// save is a migration, then union routing across the same provider
				// region to avoid silently dropping aliases or model access from
				// younger accounts.
				policy = providerPolicySeed(descriptor.ID, region.ID, accountsByKey[key])
			}
			p.ensureDefaults(ctx, &policy)
			policy.ProxyURL = proxy.Redact(policy.ProxyURL)
			list = append(list, policy)
		}
	}
	return list, nil
}

// SeedMissing creates a provider policy for every provider region that already
// has accounts but no saved policy. It runs once at startup so existing
// account-level values become provider-level immediately; it never writes
// those values back to account rows.
func (p *ProviderSettings) SeedMissing(ctx context.Context) error {
	store := p.store()
	stored, err := store.ListProviderPolicies(ctx)
	if err != nil {
		return err
	}
	storedByKey := make(map[string]accounts.ProviderPolicy, len(stored))
	for _, policy := range stored {
		storedByKey[accounts.ProviderPolicyKey(policy.Provider, policy.Region)] = policy
	}
	all, err := store.ListRaw(ctx)
	if err != nil {
		return err
	}
	accountsByKey := map[string][]accounts.Account{}
	for _, account := range all {
		key := accounts.ProviderPolicyKey(account.Provider, account.ProviderRegion)
		accountsByKey[key] = append(accountsByKey[key], account)
	}
	var errs []error
	for _, descriptor := range providers.List() {
		for _, region := range descriptor.Regions {
			key := accounts.ProviderPolicyKey(descriptor.ID, region.ID)
			if _, ok := storedByKey[key]; ok {
				continue
			}
			group := accountsByKey[key]
			if len(group) == 0 {
				continue
			}
			policy := providerPolicySeed(descriptor.ID, region.ID, group)
			p.ensureDefaults(ctx, &policy)
			if err := store.SaveProviderPolicy(ctx, policy); err != nil {
				errs = append(errs, fmt.Errorf("seed provider policy %s: %w", key, err))
			}
		}
	}
	return errors.Join(errs...)
}

func providerPolicySeed(provider, region string, group []accounts.Account) accounts.ProviderPolicy {
	policy := accounts.ProviderPolicy{Provider: provider, Region: region}
	if len(group) == 0 {
		return policy
	}
	first := group[0]
	policy.CreditFloor = copyFloat(first.CreditFloor)
	policy.PercentFloor = copyFloat(first.PercentFloor)
	drop := first.DropSystemPrompt
	policy.DropSystemPrompt = &drop
	policy.ProxyURL = providerBaseProxy(accounts.ProviderPolicy{}, false, group, provider, region)
	policy.ModelAliases = mergeProviderAliases(group)
	policy.EnabledModels = mergeProviderModels(group)
	policy.ModelsRestricted = len(policy.EnabledModels) > 0
	policy.CheckinTime = first.CheckinTime
	if policy.CheckinTime == "" && first.Provider == "workbuddy" {
		policy.CheckinTime = first.WorkBuddyCheckinTime
	}
	return policy
}

func mergeProviderAliases(group []accounts.Account) map[string]string {
	merged := map[string]string{}
	for _, account := range group {
		for alias, model := range account.ModelAliases {
			if _, exists := merged[alias]; !exists {
				merged[alias] = model
			}
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func mergeProviderModels(group []accounts.Account) []string {
	// If any account still exposes the whole catalog, make the provider default
	// unrestricted rather than silently hiding models from that account.
	for _, account := range group {
		if !account.ModelsRestricted {
			return nil
		}
	}
	seen := map[string]bool{}
	var merged []string
	for _, account := range group {
		for _, model := range account.EnabledModels {
			if seen[model] {
				continue
			}
			seen[model] = true
			merged = append(merged, model)
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func providerBaseProxy(existing accounts.ProviderPolicy, found bool, all []accounts.Account, provider, region string) string {
	if found {
		// The console renders a redacted proxy. Keep the stored full value when
		// the operator did not edit it.
		return existing.ProxyURL
	}
	// First save migrates the existing account values; the form was seeded from
	// an account whose proxy may have been redacted by List. Prefer the first
	// non-empty account proxy so a younger account's configuration is not lost.
	for _, account := range all {
		if account.Provider == provider && account.ProviderRegion == region && account.ProxyURL != "" {
			return account.ProxyURL
		}
	}
	return ""
}

func (p *ProviderSettings) ensureDefaults(ctx context.Context, policy *accounts.ProviderPolicy) {
	if policy.DropSystemPrompt == nil {
		drop := accounts.DefaultDropSystemPrompt(policy.Provider, nil)
		policy.DropSystemPrompt = &drop
	}
	if policy.CheckinTime == "" {
		if _, supported := providers.CheckinFor(policy.Provider, policy.Region); supported {
			if value, err := accounts.CheckinTimeDefault(ctx, p.store(), policy.Provider); err == nil {
				policy.CheckinTime = value
			}
		}
	}
}

// Update persists the provider policy and refreshes the live pool from the
// effective accounts. It never writes provider-owned values back to account
// rows: Store.Get/List already overlay this policy, so a later account read or
// restart sees the same values without a materialization step.
func (p *ProviderSettings) Update(ctx context.Context, input accounts.ProviderPolicy) (accounts.ProviderPolicy, error) {
	policy, err := accounts.NormalizeProviderPolicy(input)
	if err != nil {
		return accounts.ProviderPolicy{}, err
	}
	store := p.store()
	existing, found, err := store.GetProviderPolicy(ctx, policy.Provider, policy.Region)
	if err != nil {
		return accounts.ProviderPolicy{}, err
	}
	all, err := store.ListRaw(ctx)
	if err != nil {
		return accounts.ProviderPolicy{}, err
	}
	baseProxy := providerBaseProxy(existing, found, all, policy.Provider, policy.Region)
	if baseProxy != "" {
		policy.ProxyURL = proxy.Preserve(baseProxy, policy.ProxyURL)
	}
	if err := accounts.ValidateAccountProxy(policy.Provider, policy.Region, policy.ProxyURL); err != nil {
		return accounts.ProviderPolicy{}, err
	}
	if err := store.SaveProviderPolicy(ctx, policy); err != nil {
		return accounts.ProviderPolicy{}, err
	}
	var applyErr error
	for _, account := range all {
		if account.Provider != policy.Provider || account.ProviderRegion != policy.Region {
			continue
		}
		// Build the effective before/after in memory. The before state uses the
		// previously saved policy when there was one, so an unchanged save does
		// not restart child-process providers or churn the pool.
		before := account
		if found {
			accounts.ApplyProviderPolicy(&before, existing)
		}
		after := account
		accounts.ApplyProviderPolicy(&after, policy)
		if err := p.runtime.SyncAccount(ctx, before, after); err != nil {
			applyErr = errors.Join(applyErr, fmt.Errorf("sync %s: %w", account.ID, err))
		}
	}
	if applyErr != nil {
		return accounts.ProviderPolicy{}, applyErr
	}
	view := copyPolicy(policy)
	view.ProxyURL = proxy.Redact(view.ProxyURL)
	return view, nil
}

// providerPolicyAllowsModel applies a provider's enabled-model list to one
// member. An empty list means the provider is unrestricted; an alias is
// resolved before the comparison so members can use a provider alias exactly
// as the console model list spells it.
func providerPolicyAllowsModel(policy accounts.ProviderPolicy, model string) bool {
	if len(policy.EnabledModels) == 0 {
		return true
	}
	key := modelAllowKey(model)
	if target := policy.ModelAliases[key]; target != "" {
		key = modelAllowKey(target)
	}
	for _, enabled := range policy.EnabledModels {
		if modelAllowKey(enabled) == key {
			return true
		}
	}
	return false
}

type aggregationProviderGroup struct {
	provider string
	region   string
	accounts []accounts.Account
}

func aggregationGroupKey(provider, region string) string {
	return provider + "\x00" + region
}

func aggregationPolicyFor(
	policies map[string]accounts.ProviderPolicy,
	group *aggregationProviderGroup,
) accounts.ProviderPolicy {
	if policy, ok := policies[aggregationGroupKey(group.provider, group.region)]; ok {
		return policy
	}
	return providerPolicySeed(group.provider, group.region, group.accounts)
}

// ValidateAggregationMembers rejects a cross-provider member whose model is
// not enabled by the provider policy that owns its provider/region. For a
// region-less member, any connected region that allows the model is enough;
// a member pinned to one account is checked against that account's region.
func (p *ProviderSettings) ValidateAggregationMembers(ctx context.Context, members []accounts.AggregationMember) error {
	if len(members) == 0 {
		return nil
	}
	store := p.store()
	raw, err := store.ListRaw(ctx)
	if err != nil {
		return err
	}
	stored, err := store.ListProviderPolicies(ctx)
	if err != nil {
		return err
	}
	policies := make(map[string]accounts.ProviderPolicy, len(stored))
	for _, policy := range stored {
		provider := accounts.NormalizeProviderFamily(policy.Provider)
		region := accounts.NormalizeRegion(policy.Region)
		policies[aggregationGroupKey(provider, region)] = policy
	}
	groups := make(map[string]*aggregationProviderGroup)
	accountsByID := make(map[string]accounts.Account, len(raw))
	for _, account := range raw {
		provider := accounts.NormalizeProviderFamily(account.Provider)
		region := accounts.NormalizeRegion(account.ProviderRegion)
		key := aggregationGroupKey(provider, region)
		group := groups[key]
		if group == nil {
			group = &aggregationProviderGroup{provider: provider, region: region}
			groups[key] = group
		}
		group.accounts = append(group.accounts, account)
		accountsByID[account.ID] = account
	}
	for _, member := range members {
		if err := validateAggregationMember(member, groups, accountsByID, policies); err != nil {
			return err
		}
	}
	return nil
}

func validateAggregationMember(
	member accounts.AggregationMember,
	groups map[string]*aggregationProviderGroup,
	accountsByID map[string]accounts.Account,
	policies map[string]accounts.ProviderPolicy,
) error {
	provider := accounts.NormalizeProviderFamily(member.Provider)
	requestedRegion := strings.ToLower(strings.TrimSpace(member.Region))
	model := strings.TrimSpace(member.Model)
	accountID := strings.TrimSpace(member.AccountID)

	if accountID != "" {
		account, ok := accountsByID[accountID]
		if !ok {
			return fmt.Errorf("aggregation member account %q does not exist", accountID)
		}
		if accounts.NormalizeProviderFamily(account.Provider) != provider {
			return fmt.Errorf("aggregation member account %q is not a %s account", accountID, provider)
		}
		region := accounts.NormalizeRegion(account.ProviderRegion)
		if requestedRegion != "" && accounts.NormalizeRegion(requestedRegion) != region {
			return fmt.Errorf("aggregation member account %q is not in region %s", accountID, requestedRegion)
		}
		group := groups[aggregationGroupKey(provider, region)]
		if group == nil {
			return fmt.Errorf("provider %s/%s has no connected accounts", provider, region)
		}
		if !providerPolicyAllowsModel(aggregationPolicyFor(policies, group), model) {
			return fmt.Errorf("model %q is not enabled in provider settings for %s/%s", model, provider, region)
		}
		return nil
	}

	if requestedRegion != "" {
		region := accounts.NormalizeRegion(requestedRegion)
		group := groups[aggregationGroupKey(provider, region)]
		if group == nil {
			return fmt.Errorf("provider %s/%s has no connected accounts", provider, region)
		}
		if !providerPolicyAllowsModel(aggregationPolicyFor(policies, group), model) {
			return fmt.Errorf("model %q is not enabled in provider settings for %s/%s", model, provider, region)
		}
		return nil
	}

	connected := make([]*aggregationProviderGroup, 0)
	for _, group := range groups {
		if group.provider == provider {
			connected = append(connected, group)
		}
	}
	if len(connected) == 0 {
		return fmt.Errorf("provider %s has no connected accounts", provider)
	}
	sort.Slice(connected, func(i, j int) bool { return connected[i].region < connected[j].region })
	for _, group := range connected {
		if providerPolicyAllowsModel(aggregationPolicyFor(policies, group), model) {
			return nil
		}
	}
	return fmt.Errorf("model %q is not enabled in provider settings for any connected %s region", model, provider)
}

func copyPolicy(policy accounts.ProviderPolicy) accounts.ProviderPolicy {
	policy.CreditFloor = copyFloat(policy.CreditFloor)
	policy.PercentFloor = copyFloat(policy.PercentFloor)
	policy.ModelAliases = copyAliases(policy.ModelAliases)
	policy.EnabledModels = copyStrings(policy.EnabledModels)
	return policy
}

func copyFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func copyAliases(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]string, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}

func copyStrings(value []string) []string {
	if len(value) == 0 {
		return nil
	}
	return append([]string(nil), value...)
}
