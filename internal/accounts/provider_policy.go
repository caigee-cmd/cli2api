package accounts

import (
	"fmt"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// ProviderPolicySecret stores the console's provider-wide settings. The value
// is a JSON envelope keyed by ProviderPolicyKey, so every account of the same
// provider and region receives the same routing and safety policy.
const ProviderPolicySecret = "provider_settings_v1"

// ProviderPolicy is the provider-wide counterpart of the per-account settings.
// CreditFloor and PercentFloor are optional: nil inherits the process-global
// floor, while a non-nil value (including zero) overrides it for the provider.
type ProviderPolicy struct {
	Provider         string            `json:"provider"`
	Region           string            `json:"region"`
	CreditFloor      *float64          `json:"credit_floor"`
	PercentFloor     *float64          `json:"percent_floor"`
	ProxyURL         string            `json:"proxy_url,omitempty"`
	DropSystemPrompt *bool             `json:"drop_system_prompt,omitempty"`
	ModelAliases     map[string]string `json:"model_aliases,omitempty"`
	EnabledModels    []string          `json:"enabled_models,omitempty"`
	ModelsRestricted bool              `json:"models_restricted,omitempty"`
	CheckinTime      string            `json:"checkin_time,omitempty"`
	// CheckinAuto defaults to true when absent so existing policies switch to
	// the upstream window + 1 hour without a migration. Manual mode keeps the
	// operator's CheckinTime.
	CheckinAuto *bool `json:"checkin_auto,omitempty"`
}

func (policy ProviderPolicy) CheckinScheduleAuto() bool {
	return policy.CheckinAuto == nil || *policy.CheckinAuto
}

func ProviderPolicyKey(provider, region string) string {
	return NormalizeProviderFamily(provider) + ":" + NormalizeRegion(region)
}

// NormalizeProviderPolicy canonicalizes the provider/region pair and validates
// the same routing, floor, proxy, and check-in rules used for accounts.
func NormalizeProviderPolicy(policy ProviderPolicy) (ProviderPolicy, error) {
	descriptor, parsedRegion, err := providers.Resolve(policy.Provider, policy.Region)
	if err != nil {
		return ProviderPolicy{}, err
	}
	policy.Provider = descriptor.ID
	policy.Region = parsedRegion.ID
	if policy.CreditFloor != nil && *policy.CreditFloor < 0 {
		return ProviderPolicy{}, fmt.Errorf("credit_floor must be >= 0")
	}
	if policy.PercentFloor != nil && (*policy.PercentFloor < 0 || *policy.PercentFloor > 100) {
		return ProviderPolicy{}, fmt.Errorf("percent_floor must be between 0 and 100")
	}
	policy.ProxyURL = strings.TrimSpace(policy.ProxyURL)
	if err := ValidateAccountProxy(policy.Provider, policy.Region, policy.ProxyURL); err != nil {
		return ProviderPolicy{}, err
	}
	aliases, enabled, err := NormalizeAccountRouting(policy.ModelAliases, policy.EnabledModels)
	if err != nil {
		return ProviderPolicy{}, err
	}
	policy.ModelAliases = aliases
	policy.EnabledModels = enabled
	policy.ModelsRestricted = len(enabled) > 0
	if policy.CheckinTime = strings.TrimSpace(policy.CheckinTime); policy.CheckinTime != "" {
		policy.CheckinTime, err = NormalizeCheckinTime(policy.CheckinTime)
		if err != nil {
			return ProviderPolicy{}, err
		}
		if _, supported := providers.CheckinFor(policy.Provider, policy.Region); !supported {
			return ProviderPolicy{}, fmt.Errorf("check-in is not available for this provider and region")
		}
	}
	return policy, nil
}

// ApplyProviderPolicy overlays an account with its provider-scoped settings in
// memory. The account keeps its own name, concurrency, priority, credential,
// and enabled state; only fields owned by the provider policy change. This is
// never written back to SQLite: the provider policy stays the source of truth.
func ApplyProviderPolicy(account *Account, policy ProviderPolicy) {
	if account == nil {
		return
	}
	account.CreditFloor = cloneFloat(policy.CreditFloor)
	account.PercentFloor = cloneFloat(policy.PercentFloor)
	account.ProxyURL = strings.TrimSpace(policy.ProxyURL)
	account.DropSystemPrompt = DefaultDropSystemPrompt(account.Provider, policy.DropSystemPrompt)
	account.ModelAliases = cloneAliases(policy.ModelAliases)
	account.EnabledModels = cloneStrings(policy.EnabledModels)
	account.ModelsRestricted = len(policy.EnabledModels) > 0
	// The effective check-in time is resolved for provider+region, not copied
	// into account storage. Auto mode ignores the stored clock and uses the
	// upstream window + 1 hour; manual mode keeps the operator's value. The old
	// provider-only secret could not distinguish Qoder global/CN or WorkBuddy
	// CN/global.
	if policy.CheckinScheduleAuto() {
		account.CheckinTime = ""
	} else {
		account.CheckinTime = strings.TrimSpace(policy.CheckinTime)
	}
	if account.Provider == "workbuddy" {
		account.WorkBuddyCheckinTime = account.CheckinTime
	}
}

// StripProviderOwnedSettings removes fields owned by a provider policy from an
// account PATCH. Identity, concurrency, priority, enabled state, and automatic
// check-in opt-in stay account-scoped.
func StripProviderOwnedSettings(input UpdateAccount) UpdateAccount {
	input.CreditFloor = nil
	input.SetCreditFloor = false
	input.PercentFloor = nil
	input.SetPercentFloor = false
	input.ProxyURL = nil
	input.DropSystemPrompt = nil
	input.ModelAliases = nil
	input.EnabledModels = nil
	input.SetModelRouting = false
	input.CheckinTime = nil
	input.WorkBuddyCheckinTime = nil
	return input
}

func cloneFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneAliases(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]string, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}

func cloneStrings(value []string) []string {
	if len(value) == 0 {
		return nil
	}
	return append([]string(nil), value...)
}
