import { api } from './client'

export type APIKeyRecord = {
  id: string
  name: string
  prefix: string
  providers: string[]
  enabled: boolean
  last_used_at?: string
  created_at: string
  updated_at: string
  secret?: string
  secret_once?: boolean
}

export type ConsoleKeyView = {
  prefix: string
  hint?: string
  rotated?: boolean
  secret?: string
}

export function fetchAPIKeys() {
  return api<{ object: string; data: APIKeyRecord[] }>('/api/keys')
}

export function createAPIKey(input: { name: string; providers: string[]; enabled?: boolean }) {
  return api<APIKeyRecord>('/api/keys', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export function updateAPIKey(id: string, input: { name?: string; providers?: string[]; enabled?: boolean }) {
  return api<APIKeyRecord>(`/api/keys/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(input),
  })
}

export function deleteAPIKey(id: string) {
  return api(`/api/keys/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export function fetchConsoleKey() {
  return api<ConsoleKeyView>('/api/system/console-key')
}

export function rotateConsoleKey() {
  return api<ConsoleKeyView>('/api/system/console-key', {
    method: 'POST',
    body: JSON.stringify({ rotate: true }),
  })
}
