import { api } from './client'
import type { Overview } from './types'

export function fetchOverview() {
  return api<Overview>('/api/overview')
}

export function startDeviceLogin(accountId?: string) {
  const q = accountId ? `?account=${encodeURIComponent(accountId)}` : ''
  return api<{ authUrl?: string; status?: string; message?: string }>(`/api/login/device${q}`, {
    method: 'POST',
    body: '{}',
  })
}

export function fetchLoginStatus(accountId?: string) {
  const q = accountId ? `?account=${encodeURIComponent(accountId)}` : ''
  return api<{ login?: any }>(`/api/login/status${q}`)
}

export function loginWithPat(pat: string, accountId?: string) {
  const q = accountId ? `?account=${encodeURIComponent(accountId)}` : ''
  return api(`/api/login/pat${q}`, {
    method: 'POST',
    body: JSON.stringify({ pat }),
  })
}

export function refreshModels(accountId?: string) {
  const q = new URLSearchParams({ refresh: '1' })
  if (accountId) q.set('account', accountId)
  return api<{ data?: Overview['models'] }>(`/api/models?${q.toString()}`)
}

export function rewarmWorker(accountId?: string) {
  const q = accountId ? `?account=${encodeURIComponent(accountId)}` : ''
  return api(`/api/rewarm${q}`, { method: 'POST', body: '{}' })
}

export function testChat(model: string, content: string) {
  return api('/api/chat', {
    method: 'POST',
    body: JSON.stringify({
      model,
      stream: false,
      messages: [{ role: 'user', content }],
    }),
  })
}
