export function isQoderGlobalProvider(provider?: string) {
  const value = String(provider || '').toLowerCase()
  return !value || value === 'qoder' || value === 'qoder-global'
}

export function isWorkBuddyProvider(provider?: string) {
  return String(provider || '').toLowerCase() === 'workbuddy'
}

export function accountProviderLabel(
  provider: string | undefined,
  region: string | undefined,
  t: (key: string) => string,
) {
  const providerID = String(provider || '').toLowerCase()
  const regionID = String(region || '').toLowerCase()
  if (isWorkBuddyProvider(providerID)) {
    return regionID === 'global' ? t('accountTypeWorkBuddyGlobal') : t('accountTypeWorkBuddyCN')
  }
  if (isQoderGlobalProvider(providerID)) return t('accountTypeQoderGlobal')
  return provider || t('account')
}
