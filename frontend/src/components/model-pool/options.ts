import type { ProviderDescriptor } from '@/api/overview'
import type { ModelInfo, Overview } from '@/api/types'
import type { FilterSelectOption } from '@/components/ui/FilterSelect'

export type AccountRow = NonNullable<Overview['accounts']>[number]
type TFunc = (key: string, vars?: Record<string, string | number>) => string

export function normalizeProvider(value?: string) {
  return String(value || 'qoder').trim().toLowerCase()
}

export function normalizeRegion(value?: string) {
  const region = String(value || '').trim().toLowerCase()
  return region === '' || region === 'gl' ? 'global' : region
}

export function providerOptions(descriptors: ProviderDescriptor[], accounts: AccountRow[]): FilterSelectOption[] {
  const connected = new Set(accounts.map((account) => normalizeProvider(account.provider)))
  return descriptors
    .filter((descriptor) => connected.has(normalizeProvider(descriptor.id)))
    .map((descriptor) => ({ id: descriptor.id, label: descriptor.label }))
}

export function regionOptions(descriptors: ProviderDescriptor[], accounts: AccountRow[], provider: string): FilterSelectOption[] {
  const descriptor = descriptors.find((item) => item.id === provider)
  if (!descriptor) return []
  const connected = new Set(
    accounts
      .filter((account) => normalizeProvider(account.provider) === normalizeProvider(provider))
      .map((account) => normalizeRegion(account.region)),
  )
  return (descriptor.regions || [])
    .filter((region) => connected.has(normalizeRegion(region.id)))
    .map((region) => ({ id: region.id, label: region.label }))
}

export function modelOptions(models: ModelInfo[], provider: string, region: string, t: TFunc): FilterSelectOption[] {
  const seen = new Set<string>()
  const out: FilterSelectOption[] = []
  const wantedProvider = String(provider || '').trim().toLowerCase()
  const wantedRegion = String(region || '').trim() ? normalizeRegion(region) : ''
  for (const model of models) {
    const taggedProvider = String(model.provider || model.owned_by || '').trim().toLowerCase()
    if (taggedProvider && taggedProvider !== wantedProvider) continue
    if (wantedRegion) {
      const modelRegions = [model.region, ...(model.regions || [])]
        .map((value) => normalizeRegion(value))
        .filter(Boolean)
      if (modelRegions.length && !modelRegions.includes(wantedRegion)) continue
    }
    const id = model.id
    if (!id || seen.has(id)) continue
    seen.add(id)
    const label = model.display_name || model.route_display_name || id
    out.push({ id, label: `${label} (${id})` })
  }
  if (!out.length) out.push({ id: '', label: t('modelPoolNoModels') })
  return out
}

export function modelTitle(models: ModelInfo[], id: string) {
  const match = models.find((model) => model.id === id)
  return match?.display_name || match?.route_display_name || id
}
