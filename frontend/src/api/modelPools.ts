import { api } from './client'

export type AggregationMember = {
  id?: number
  provider: string
  region?: string
  model: string
  account_id?: string
  priority: number
  weight: number
}

export type AggregationRecord = {
  id: string
  name: string
  enabled: boolean
  strategy: string
  allow_fallbacks: boolean
  max_attempts: number
  context_length?: number
  max_output_tokens?: number
  reasoning_options?: string[]
  reasoning_effort?: string
  document?: string
  members: AggregationMember[]
  created_at: string
  updated_at: string
}

export type AggregationInput = {
  id?: string
  name: string
  enabled?: boolean
  strategy: string
  allow_fallbacks?: boolean
  max_attempts: number
  context_length?: number
  max_output_tokens?: number
  reasoning_options?: string[]
  reasoning_effort?: string
  document?: object
  members: AggregationMember[]
}

export type AggregationPreviewAttempt = {
  id: string
  model: string
  provider: string
  region?: string
  status: string
  reason?: string
}

export function fetchAggregations() {
  return api<{ object: string; data: AggregationRecord[] }>('/api/aggregations')
}

export function createAggregation(input: AggregationInput) {
  return api<AggregationRecord>('/api/aggregations', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export function updateAggregation(id: string, input: Partial<AggregationInput>) {
  return api<AggregationRecord>(`/api/aggregations/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(input),
  })
}

export function deleteAggregation(id: string) {
  return api(`/api/aggregations/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export function previewAggregation(input: { id?: string; strategy: string; document: object; message: string }) {
  return api<{ path: string; attempts: AggregationPreviewAttempt[] }>('/api/aggregations/preview', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export const AGGREGATION_STRATEGIES = ['priority', 'round-robin', 'fill-first', 'fefo', 'least-quota-first', 'most-quota-first'] as const
