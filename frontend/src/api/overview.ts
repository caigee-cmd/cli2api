import { api } from './client'
import type { CheckinRecord, Overview } from './types'

export function fetchOverview(keyOverride?: string, options?: { refreshQuota?: boolean }) {
  const path = options?.refreshQuota ? '/api/overview?refresh=1' : '/api/overview'
  return api<Overview>(path, {}, keyOverride)
}

export function fetchOverviewSummary(keyOverride?: string) {
  return api<Overview>('/api/overview/summary', {}, keyOverride)
}

export function fetchAccounts(refresh = false) {
  return api<{ object?: string; data?: NonNullable<Overview['accounts']> }>(
    `/api/accounts?refresh=${refresh ? '1' : '0'}`,
  )
}

export function startDeviceLogin(accountId?: string) {
  if (!accountId) throw new Error('account id required')
  return api<{ authUrl?: string; status?: string; message?: string }>(`/api/accounts/${encodeURIComponent(accountId)}/login/device`, {
    method: 'POST',
    body: '{}',
  })
}

export function fetchLoginStatus(accountId?: string) {
  if (!accountId) throw new Error('account id required')
  return api<{ login?: any }>(`/api/accounts/${encodeURIComponent(accountId)}/login/status`)
}

export function completeLoginCallback(accountId: string, callbackUrl: string) {
  if (!accountId) throw new Error('account id required')
  return api(`/api/accounts/${encodeURIComponent(accountId)}/login/callback`, {
    method: 'POST',
    body: JSON.stringify({ callback_url: callbackUrl }),
  })
}

export function loginWithPat(pat: string, accountId?: string) {
  if (!accountId) throw new Error('account id required')
  return api(`/api/accounts/${encodeURIComponent(accountId)}/login/pat`, {
    method: 'POST',
    body: JSON.stringify({ pat }),
  })
}

type ModelsResponse = { data?: Overview['models'] }

type ModelsMemoryEntry = {
  data: ModelsResponse
  at: number
  pending?: Promise<ModelsResponse>
}

const modelsMemoryTTL = 30_000
const modelsMemoryCache = new Map<string, ModelsMemoryEntry>()

function modelsMemoryKey(accountId?: string) {
  return accountId || '*'
}

export function fetchModels(accountId?: string, refresh = false) {
  const q = new URLSearchParams()
  if (refresh) q.set('refresh', '1')
  if (accountId) q.set('account', accountId)
  const query = q.toString()
  return api<ModelsResponse>(`/api/models${query ? `?${query}` : ''}`)
}

export function fetchModelsCached(accountId?: string) {
  const key = modelsMemoryKey(accountId)
  const cached = modelsMemoryCache.get(key)
  if (cached && Date.now() - cached.at < modelsMemoryTTL) {
    return Promise.resolve(cached.data)
  }
  if (cached?.pending) return cached.pending
  const pending = fetchModels(accountId).then((data) => {
    modelsMemoryCache.set(key, { data, at: Date.now() })
    return data
  }).finally(() => {
    const current = modelsMemoryCache.get(key)
    if (current?.pending === pending) {
      modelsMemoryCache.set(key, { data: current.data, at: current.at })
    }
  })
  modelsMemoryCache.set(key, { data: cached?.data || {}, at: cached?.at || 0, pending })
  return pending
}

export function refreshModels(accountId?: string) {
  const key = modelsMemoryKey(accountId)
  modelsMemoryCache.delete(key)
  return fetchModels(accountId, true).then((data) => {
    modelsMemoryCache.set(key, { data, at: Date.now() })
    return data
  })
}

export function updateModelContext(modelKey: string, contextLength: number) {
  return api<{
    model: string
    context_length: number
    default_context_length: number
    context_custom: boolean
  }>(`/api/models/${encodeURIComponent(modelKey)}`, {
    method: 'PATCH',
    body: JSON.stringify({ context_length: contextLength }),
  })
}

export function updateTraeMaxMode(modelKey: string, maxMode: boolean) {
  return api<{
    model: string
    provider: string
    max_mode: boolean
    reasoning_effort?: string
    context_custom: boolean
  }>(`/api/models/trae/${encodeURIComponent(modelKey)}`, {
    method: 'PATCH',
    body: JSON.stringify({ max_mode: maxMode }),
  })
}

export function updateProviderReasoning(provider: 'trae' | 'workbuddy', modelKey: string, reasoningEffort: string) {
  return api<{
    model: string
    provider: string
    max_mode: boolean
    reasoning_effort?: string
    context_custom: boolean
  }>(`/api/models/${provider}/${encodeURIComponent(modelKey)}`, {
    method: 'PATCH',
    body: JSON.stringify({ reasoning_effort: reasoningEffort }),
  })
}

export function refreshAccount(accountId: string, options?: { quota?: boolean }) {
  if (!accountId) throw new Error('account id required')
  const query = options?.quota ? '?quota=1' : ''
  return api(`/api/accounts/${encodeURIComponent(accountId)}/refresh${query}`, {
    method: 'POST',
    body: '{}',
  })
}

export function rewarmWorker(accountId?: string) {
  if (!accountId) throw new Error('account id required')
  return api(`/api/accounts/${encodeURIComponent(accountId)}/rewarm`, { method: 'POST', body: '{}' })
}

export function testChat(model: string, content: string, accountId?: string) {
  const headers: Record<string, string> = {}
  if (accountId) headers['X-Qoder-Account'] = accountId
  return api('/api/chat', {
    method: 'POST',
    headers,
    body: JSON.stringify({
      model,
      stream: false,
      messages: [{ role: 'user', content }],
    }),
  })
}


export type ProviderDescriptor = {
  id: string
  label: string
  runtime: string
  capabilities: {
    browser_login: boolean
    pat_login: boolean
    import_export: boolean
  }
  regions: Array<{ id: string; label: string }>
  default_region: string
}

export function fetchProviders() {
  return api<{ data?: ProviderDescriptor[] }>('/api/providers')
}

export function createAccount(
  name: string,
  provider = 'qoder',
  region = 'global',
  options?: {
    max_inflight?: number
    priority?: number
    drop_system_prompt?: boolean
    workbuddy_auto_checkin?: boolean
  },
) {
  return api('/api/accounts', {
    method: 'POST',
    body: JSON.stringify({
      name,
      provider,
      region,
      enabled: true,
      max_inflight: options?.max_inflight ?? 4,
      priority: options?.priority ?? 50,
      drop_system_prompt: options?.drop_system_prompt,
      workbuddy_auto_checkin: options?.workbuddy_auto_checkin,
    }),
  })
}

export function checkinAccount(accountId: string) {
  return api(`/api/accounts/${encodeURIComponent(accountId)}/checkin`, {
    method: 'POST',
    body: '{}',
  })
}

export function fetchCheckinRecords(accountId: string) {
  return api<{ object?: string; data?: CheckinRecord[] }>(`/api/accounts/${encodeURIComponent(accountId)}/checkins`)
}

export function updateAccount(accountId: string, input: Record<string, unknown>) {
  return api(`/api/accounts/${encodeURIComponent(accountId)}`, { method: 'PATCH', body: JSON.stringify(input) })
}

export function deleteAccount(accountId: string) {
  return api(`/api/accounts/${encodeURIComponent(accountId)}`, { method: 'DELETE' })
}

export function importAccount(bundle: Record<string, unknown>) {
  return api('/api/accounts/import', { method: 'POST', body: JSON.stringify(bundle) })
}

export function exportAccount(accountId: string) {
  return api<Record<string, unknown>>(`/api/accounts/${encodeURIComponent(accountId)}/export`)
}
