import { api } from './client'

export type ProviderPolicy = {
  provider: string
  region: string
  credit_floor?: number | null
  percent_floor?: number | null
  proxy_url?: string
  drop_system_prompt?: boolean
  model_aliases?: Record<string, string>
  enabled_models?: string[]
  models_restricted?: boolean
  checkin_time?: string
  checkin_auto?: boolean
}

export type ProviderCatalogEntry = {
  id: string
  display_name?: string
}

export function fetchProviderPolicies() {
  return api<{ data?: ProviderPolicy[] }>('/api/provider-settings')
}

export function updateProviderPolicy(policy: ProviderPolicy) {
  return api<{ data?: ProviderPolicy }>('/api/provider-settings', {
    method: 'PATCH',
    body: JSON.stringify(policy),
  })
}

export function fetchProviderCatalog(provider: string, region: string) {
  const query = new URLSearchParams({ provider, region })
  return api<{ data?: ProviderCatalogEntry[] }>(`/api/provider-settings/catalog?${query.toString()}`)
}
