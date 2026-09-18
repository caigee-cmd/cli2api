package accounts

import (
	"fmt"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// ProviderGrant is a single entry of an API key allowlist. Region is empty
// for a bare family grant ("workbuddy") that covers every region of the
// family, and set for a region-scoped grant ("workbuddy:cn").
//
// Grants stay in accounts: ParseProviderGrant uses the providers catalog, and
// ProviderAllowed / ProviderRegionAllowed plus NormalizeAPIKeyProviders own
// the runtime/write paths. Moving this file to auth would create
// accounts → auth while auth already imports accounts.
type ProviderGrant struct {
	Provider string
	Region   string
}

// String renders the canonical storage form: "workbuddy:cn" or "workbuddy".
func (g ProviderGrant) String() string {
	if g.Region == "" {
		return g.Provider
	}
	return g.Provider + ":" + g.Region
}

// ParseProviderGrant strictly parses one allowlist entry. Accepted forms:
//
//	workbuddy            bare family grant, any region
//	workbuddy:cn         family + region grant
//
// Rejected: empty strings, unknown families, unknown regions, missing sides
// ("workbuddy:", ":cn"), more than one colon ("workbuddy:cn:x"), and any
// whitespace inside the entry.
func ParseProviderGrant(entry string) (ProviderGrant, error) {
	raw := entry
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ProviderGrant{}, fmt.Errorf("empty provider grant")
	}
	if raw != trimmed {
		return ProviderGrant{}, fmt.Errorf("provider grant %q has surrounding whitespace", raw)
	}
	parts := strings.Split(trimmed, ":")
	if len(parts) > 2 {
		return ProviderGrant{}, fmt.Errorf("provider grant %q has more than one colon", raw)
	}
	family := strings.ToLower(parts[0])
	region := ""
	if len(parts) == 2 {
		region = strings.ToLower(parts[1])
		if region == "" {
			return ProviderGrant{}, fmt.Errorf("provider grant %q is missing a region", raw)
		}
	}
	if family == "" {
		return ProviderGrant{}, fmt.Errorf("provider grant %q is missing a provider", raw)
	}
	if strings.ContainsAny(family, " \t") || strings.ContainsAny(region, " \t") {
		return ProviderGrant{}, fmt.Errorf("provider grant %q must not contain inner whitespace", raw)
	}
	descriptor, ok := providers.Get(family)
	if !ok {
		return ProviderGrant{}, fmt.Errorf("unknown provider %q", family)
	}
	if region != "" {
		if _, ok := descriptor.Region(region); !ok {
			return ProviderGrant{}, fmt.Errorf("unknown region %q for provider %q", region, descriptor.ID)
		}
	}
	return ProviderGrant{Provider: family, Region: region}, nil
}

// parseProviderGrantsLenient parses stored allowlist entries for runtime
// authorization checks. Stored data is written through NormalizeAPIKeyProviders,
// but rows can be hand-edited; a malformed entry fails closed (deny) instead of
// being skipped silently. Parsing errors are not surfaced because callers only
// need a boolean decision on each candidate account.
func parseProviderGrantsLenient(entries []string) []ProviderGrant {
	if len(entries) == 0 {
		return nil
	}
	grants := make([]ProviderGrant, 0, len(entries))
	for _, entry := range entries {
		grant, err := ParseProviderGrant(entry)
		if err != nil {
			// Fail closed: keep the malformed entry as a never-matching
			// grant so it can never widen authorization.
			grants = append(grants, ProviderGrant{Provider: "\x00invalid", Region: "\x00invalid"})
			continue
		}
		grants = append(grants, grant)
	}
	return grants
}

// GrantsAllowed reports whether provider+region is covered by the allowlist
// entries. An empty allowlist allows everything. A bare family entry
// ("workbuddy") covers every region of that family; a region-scoped entry
// only covers accounts whose region matches. Malformed entries never match.
func GrantsAllowed(provider, region string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	family := NormalizeProviderFamily(provider)
	normalizedRegion := NormalizeRegion(region)
	for _, grant := range parseProviderGrantsLenient(allowed) {
		if grant.Provider != family {
			continue
		}
		if grant.Region == "" || grant.Region == normalizedRegion {
			return true
		}
	}
	return false
}

// grantsAllowFamily reports whether any entry of the allowlist grants some
// region of the given family (bare or region-scoped). It answers "may this key
// use this provider at all", used where only a family is known — for example
// a provider-prefixed model ID. Region narrowing still happens per candidate
// account inside the pool.
func grantsAllowFamily(provider string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	family := NormalizeProviderFamily(provider)
	for _, grant := range parseProviderGrantsLenient(allowed) {
		if grant.Provider == family {
			return true
		}
	}
	return false
}

// GrantedRegions returns the set of regions of the given family the allowlist
// grants, with a boolean telling whether every region is allowed (bare entry
// or empty allowlist). Unknown regions in stored entries are still returned
// so callers can reason about explicit grants; validation happens at write
// time.
func GrantedRegions(provider string, allowed []string) (regions []string, allRegions bool) {
	family := NormalizeProviderFamily(provider)
	if len(allowed) == 0 {
		return nil, true
	}
	seen := map[string]struct{}{}
	ordered := make([]string, 0, 2)
	for _, grant := range parseProviderGrantsLenient(allowed) {
		if grant.Provider != family {
			continue
		}
		if grant.Region == "" {
			return nil, true
		}
		if _, dup := seen[grant.Region]; dup {
			continue
		}
		seen[grant.Region] = struct{}{}
		ordered = append(ordered, grant.Region)
	}
	return ordered, false
}
