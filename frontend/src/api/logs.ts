import { api } from './client'

export type RequestAttempt = {
  id: string
  request_id: string
  attempt_index: number
  account_id?: string
  started_at: string
  finished_at?: string | null
  status: string
  http_status?: number | null
  error_kind?: string
  error_message?: string
  latency_ms?: number | null
  prompt_tokens?: number | null
  completion_tokens?: number | null
  usage_source?: string
}

export type RequestLog = {
  id: string
  created_at: string
  finished_at?: string | null
  stream: boolean
  status: string
  requested_model: string
  mapped_model?: string
  account_id?: string
  provider?: string
  prompt_tokens?: number | null
  completion_tokens?: number | null
  cache_read_tokens?: number | null
  cache_write_tokens?: number | null
  usage_source?: string
  credits?: number | null
  latency_ms?: number | null
  ttfb_ms?: number | null
  error_kind?: string
  error_code?: string
  error_message?: string
  attempt_count: number
  attempts?: RequestAttempt[]
}

export type RequestLogList = {
  items: RequestLog[]
  total: number
  limit: number
  offset: number
}

export type RuntimeLogEntry = {
  id: number
  time: string
  level: string
  account_id?: string
  source?: string
  message: string
}

export type RuntimeLogList = {
  items: RuntimeLogEntry[]
  count: number
  total?: number
  limit?: number
  offset?: number
}

export type RequestStatsWindow = {
  from: string
  to: string
  hours: number
}

export type RequestStatsTotals = {
  requests: number
  ok: number
  error: number
  canceled: number
  streaming: number
  success_rate: number
}

export type RequestStatsLatency = {
  avg_ms?: number | null
  p50_ms?: number | null
  p95_ms?: number | null
  ttfb_avg_ms?: number | null
}

export type RequestStatsTokens = {
  prompt: number
  completion: number
  cache_read: number
  total: number
}

export type RequestStatsBucket = {
  key: string
  count: number
}

export type RequestStatsNamed = {
  key: string
  count: number
  ok: number
  error: number
  latency_avg_ms?: number | null
}

export type RequestStatsPoint = {
  at: string
  requests: number
  ok: number
  error: number
}

export type RequestStats = {
  window: RequestStatsWindow
  totals: RequestStatsTotals
  latency: RequestStatsLatency
  tokens: RequestStatsTokens
  status: RequestStatsBucket[]
  errors: RequestStatsBucket[]
  models: RequestStatsNamed[]
  accounts: RequestStatsNamed[]
  providers: RequestStatsNamed[]
  series: RequestStatsPoint[]
}

export type RequestLogQuery = {
  account?: string
  status?: string
  stream?: boolean
  error_kind?: string
  model?: string
  id?: string
  q?: string
  from?: string
  to?: string
  limit?: number
  offset?: number
}

export function fetchRequestStats(query: { hours?: number; from?: string; to?: string } = {}) {
  const params = new URLSearchParams()
  if (query.hours != null) params.set('hours', String(query.hours))
  if (query.from) params.set('from', query.from)
  if (query.to) params.set('to', query.to)
  const suffix = params.toString() ? `?${params}` : ''
  return api<RequestStats>(`/api/logs/stats${suffix}`)
}

export function fetchRequestLogs(query: RequestLogQuery = {}) {
  const params = new URLSearchParams()
  if (query.account) params.set('account', query.account)
  if (query.status) params.set('status', query.status)
  if (query.stream != null) params.set('stream', query.stream ? '1' : '0')
  if (query.error_kind) params.set('error_kind', query.error_kind)
  if (query.model) params.set('model', query.model)
  if (query.id) params.set('id', query.id)
  if (query.q) params.set('q', query.q)
  if (query.from) params.set('from', query.from)
  if (query.to) params.set('to', query.to)
  if (query.limit != null) params.set('limit', String(query.limit))
  if (query.offset != null) params.set('offset', String(query.offset))
  const suffix = params.toString() ? `?${params}` : ''
  return api<RequestLogList>(`/api/logs/requests${suffix}`)
}

export function fetchRequestLog(id: string) {
  return api<RequestLog>(`/api/logs/requests/${encodeURIComponent(id)}`)
}

export function clearRequestLogs() {
  return api<{ ok: boolean; deleted: number }>('/api/logs/requests', { method: 'DELETE' })
}

export function fetchRuntimeLogs(query: { after?: number; limit?: number; offset?: number; level?: string; q?: string; account?: string } = {}) {
  const params = new URLSearchParams()
  if (query.after != null) params.set('after', String(query.after))
  if (query.limit != null) params.set('limit', String(query.limit))
  if (query.offset != null) params.set('offset', String(query.offset))
  if (query.level) params.set('level', query.level)
  if (query.q) params.set('q', query.q)
  if (query.account) params.set('account', query.account)
  const suffix = params.toString() ? `?${params}` : ''
  return api<RuntimeLogList>(`/api/logs/runtime${suffix}`)
}
