import { api } from './client'
import type { Overview } from './types'

export function fetchOverview(keyOverride?: string) {
  return api<Overview>('/api/overview', {}, keyOverride)
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

export function loginWithPat(pat: string, accountId?: string) {
  if (!accountId) throw new Error('account id required')
  return api(`/api/accounts/${encodeURIComponent(accountId)}/login/pat`, {
    method: 'POST',
    body: JSON.stringify({ pat }),
  })
}

export function refreshModels(accountId?: string) {
  const q = new URLSearchParams({ refresh: '1' })
  if (accountId) q.set('account', accountId)
  return api<{ data?: Overview['models'] }>(`/api/models?${q.toString()}`)
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


export function createAccount(name: string) {
  return api('/api/accounts', { method: 'POST', body: JSON.stringify({ name, enabled: true, max_inflight: 4 }) })
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
