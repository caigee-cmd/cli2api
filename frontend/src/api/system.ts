import { api } from './client'

export type UpdateAgentStatus = {
  protocol_version?: number
  available: boolean
  staged_update?: boolean
  state: string
  job_id?: string
  current_version?: string
  target_version?: string
  backup_path?: string
  error?: string
  started_at?: string
  finished_at?: string
}

export type UpdatePreparationStatus = {
  job_id: string
  agent_job_id?: string
  state: string
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
  skipped_versions?: string[]
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
  update?: UpdatePreparationStatus
}

export type SystemSettings = {
  cross_provider_model_pool: boolean
  routing_strategy: 'round-robin' | 'weighted-round-robin' | 'fill-first'
  session_affinity?: {
    ttl_seconds?: number
    capacity?: number
    bindings?: number
    hits?: number
    misses?: number
    escapes?: number
    rebindings?: number
    last_escape_at?: string
    last_escape_reason?: string
    last_miss_reason?: string
  }
}

export type StartUpdateResult = {
  job_id: string
  current_version?: string
  target_version?: string
  backup?: {
    name: string
    created_at: string
  }
}

export function fetchSystemUpdate(force = false) {
  return api<SystemUpdateInfo>(`/api/system/update${force ? '?force=1' : ''}`)
}

export function fetchSystemSettings() {
  return api<SystemSettings>('/api/system/settings')
}

export function updateSystemSettings(input: { cross_provider_model_pool?: boolean; routing_strategy?: SystemSettings['routing_strategy'] }) {
  return api<SystemSettings>('/api/system/settings', {
    method: 'PATCH',
    body: JSON.stringify(input),
  })
}

export function startSystemUpdate() {
  return api<StartUpdateResult>('/api/system/update/prepare', { method: 'POST', body: '{}' })
}

export function applyPreparedSystemUpdate() {
  return api<StartUpdateResult>('/api/system/update/apply', { method: 'POST', body: '{}' })
}
