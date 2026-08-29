export function isQoderProvider(provider?: string) {
  const value = String(provider || '').toLowerCase()
  return !value || value === 'qoder' || value === 'qoder-global' || value === 'qoder-cn'
}

export function isWorkBuddyProvider(provider?: string) {
  return String(provider || '').toLowerCase() === 'workbuddy'
}

export function isTraeProvider(provider?: string) {
  return String(provider || '').toLowerCase() === 'trae'
}

export function accountProviderFamilyLabel(
  provider: string | undefined,
  t: (key: string) => string,
) {
  const providerID = String(provider || '').toLowerCase()
  if (isWorkBuddyProvider(providerID)) return 'WorkBuddy'
  if (isTraeProvider(providerID)) return 'Trae'
  if (isQoderProvider(providerID)) return 'Qoder'
  return provider || t('account')
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
  if (isTraeProvider(providerID)) {
    return t('accountTypeTraeCN')
  }
  if (isQoderProvider(providerID)) {
    return regionID === 'cn' ? t('accountTypeQoderCN') : t('accountTypeQoderGlobal')
  }
  return provider || t('account')
}
