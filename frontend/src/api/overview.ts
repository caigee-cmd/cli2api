import { api } from './client'
import type { Overview } from './types'

export function fetchOverview() {
  return api<Overview>('/api/overview')
}

export function rewarmWorker() {
  return api('/api/rewarm', { method: 'POST', body: '{}' })
}

export function startDeviceLogin() {
  return api<{ authUrl?: string; status?: string; message?: string }>('/api/login/device', {
    method: 'POST',
    body: '{}',
  })
}

export function fetchLoginStatus() {
  return api<{ login?: any }>('/api/login/status')
}

export function loginWithPat(pat: string) {
  return api('/api/login/pat', {
    method: 'POST',
    body: JSON.stringify({ pat }),
  })
}

export function refreshModels() {
  return api<{ data?: Overview['models'] }>('/api/models?refresh=1')
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
