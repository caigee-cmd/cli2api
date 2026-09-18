package accounts

// ProviderAllowed reports whether an account family may be used under an API
// key allowlist — i.e. whether any entry (bare or region-scoped) grants some
// region of the family. An empty allowlist means every family. An empty
// provider is treated as Qoder, the same default PickRoute uses. Region-level
// narrowing happens per candidate account via ProviderRegionAllowed.
func ProviderAllowed(provider string, allowed []string) bool {
	return grantsAllowFamily(provider, allowed)
}

// ProviderRegionAllowed reports whether a specific account (provider family
// plus region) is covered by an API key allowlist. This is the fail-closed
// gate every routed candidate must pass; ProviderAllowed alone answers only
// the family-level question and must not be used to admit an account.
func ProviderRegionAllowed(provider, region string, allowed []string) bool {
	return GrantsAllowed(provider, region, allowed)
}
