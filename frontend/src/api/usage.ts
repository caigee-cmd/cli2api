import { api } from './client'

export type ModelPriceBand = {
  cache_hit: number
  cache_miss: number
  output: number
}

export type ModelPrice = {
  id: string
  label: string
  vendor: string
  currency: string
  off_peak: ModelPriceBand
  peak: ModelPriceBand
  peak_start_hour: number
  peak_end_hour: number
  peak_weekdays_only: boolean
  peak_note?: string
  source?: string
}

export type PricingCatalog = {
  currency: string
  models: ModelPrice[]
  note?: string
}

export type ModelCost = {
  id: string
  label: string
  vendor: string
  currency: string
  requests: number
  cache_hit: number
  cache_miss: number
  output: number
  total_tokens: number
  cost: number
  cost_off_peak: number
  cost_peak: number
  peak_requests: number
  credits: number
  priced: boolean
}

export type CostSeriesPoint = {
  at: string
  /** DeepSeek list-price CNY by model (priced models only; optional estimate). */
  per_model: Record<string, number>
  /** Subscription credits by model — every provider. */
  per_model_credits: Record<string, number>
  /** Token totals by model — every provider. */
  per_model_tokens: Record<string, number>
  tokens: number
  credits: number
}

export type CostBreakdown = {
  window: { from: string; to: string; hours: number }
  currency: string
  models: ModelCost[]
  totals: ModelCost
  series: CostSeriesPoint[]
  unpriced?: string[]
}

export function fetchUsagePricing() {
  return api<PricingCatalog>('/api/usage/pricing')
}

export function fetchUsageCost(query: { from?: string; to?: string; api_key?: string } = {}) {
  const params = new URLSearchParams()
  if (query.from) params.set('from', query.from)
  if (query.to) params.set('to', query.to)
  if (query.api_key) params.set('api_key', query.api_key)
  const suffix = params.toString() ? `?${params}` : ''
  return api<CostBreakdown>(`/api/usage/cost${suffix}`)
}
