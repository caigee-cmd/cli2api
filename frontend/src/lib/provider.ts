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

export function isDevinProvider(provider?: string) {
  return String(provider || '').toLowerCase() === 'devin'
}

export function accountProviderFamilyLabel(
  provider: string | undefined,
  t: (key: string) => string,
) {
  const providerID = String(provider || '').toLowerCase()
  if (isWorkBuddyProvider(providerID)) return 'WorkBuddy'
  if (isTraeProvider(providerID)) return 'Trae'
  if (isDevinProvider(providerID)) return 'Devin'
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
  if (isDevinProvider(providerID)) {
    return t('accountTypeDevinGlobal')
  }
  if (isQoderProvider(providerID)) {
    return regionID === 'cn' ? t('accountTypeQoderCN') : t('accountTypeQoderGlobal')
  }
  return provider || t('account')
}

// tieredTraeCaps resolves the fields that follow the Trae max-mode toggle for
// display: the context window, and — for models with a declared Max tier — the
// prompt and output ceilings. It reads the immutable catalog defaults plus the
// *_max tier and selects by maxMode, so it never depends on (or mutates) a
// previously rendered value: turning the toggle off always yields the default
// tier again.
export type TraeTierCaps = {
  context_length?: number
  prompt_max_tokens?: number
  max_output_tokens?: number
}

export function tieredTraeCaps(
  model: {
    catalog_context_length?: number
    default_context_length?: number
    context_length?: number
    catalog_context_length_max?: number
    max_mode?: boolean
    prompt_max_tokens?: number
    prompt_max_tokens_max?: number
    max_output_tokens?: number
    max_output_tokens_max?: number
  },
  maxMode?: boolean,
): TraeTierCaps {
  const on = maxMode ?? Boolean(model.max_mode)
  const dev = model.catalog_context_length || model.default_context_length || model.context_length || 0
  const max = model.catalog_context_length_max || 0
  if (on && max > 0) {
    return {
      context_length: max,
      prompt_max_tokens: model.prompt_max_tokens_max || model.prompt_max_tokens,
      max_output_tokens: model.max_output_tokens_max || model.max_output_tokens,
    }
  }
  return {
    context_length: dev,
    prompt_max_tokens: model.prompt_max_tokens,
    max_output_tokens: model.max_output_tokens,
  }
}
