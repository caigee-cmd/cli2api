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
  members: AggregationMember[]
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

export const AGGREGATION_STRATEGIES = ['priority', 'round-robin', 'fill-first', 'fefo', 'least-quota-first', 'most-quota-first'] as const
