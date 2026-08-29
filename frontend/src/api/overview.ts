import { api } from './client'
import type { Overview } from './types'

export function fetchOverview(keyOverride?: string, options?: { refreshQuota?: boolean }) {
  const path = options?.refreshQuota ? '/api/overview?refresh=1' : '/api/overview'
  return api<Overview>(path, {}, keyOverride)
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

export function fetchModels(accountId?: string, refresh = false) {
  const q = new URLSearchParams()
  if (refresh) q.set('refresh', '1')
  if (accountId) q.set('account', accountId)
  const query = q.toString()
  return api<{ data?: Overview['models'] }>(`/api/models${query ? `?${query}` : ''}`)
}

export function refreshModels(accountId?: string) {
  return fetchModels(accountId, true)
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
  options?: { max_inflight?: number; priority?: number; drop_system_prompt?: boolean },
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
    }),
  })
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
