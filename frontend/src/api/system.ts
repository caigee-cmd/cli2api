import { api } from './client'

export type UpdateAgentStatus = {
  protocol_version?: number
  available: boolean
  state: string
  job_id?: string
  current_version?: string
  target_version?: string
  backup_path?: string
  error?: string
  started_at?: string
  finished_at?: string
}

export type SystemUpdateInfo = {
  current_version: string
  next_version?: string
  has_update: boolean
  managed: boolean
  cached: boolean
  warning?: string
  release?: {
    tag_name: string
    name?: string
    body?: string
    published_at?: string
    html_url?: string
  }
  agent: UpdateAgentStatus
}

export type StartUpdateResult = {
  job_id: string
  current_version: string
  target_version: string
  backup: {
    name: string
    created_at: string
  }
}

export function fetchSystemUpdate(force = false) {
  return api<SystemUpdateInfo>(`/api/system/update${force ? '?force=1' : ''}`)
}

export function startSystemUpdate() {
  return api<StartUpdateResult>('/api/system/update', { method: 'POST', body: '{}' })
}
