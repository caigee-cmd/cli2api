import { api } from './client'

export type LiveAccount = { id: string; in_flight: number; max_inflight: number; enabled: boolean }

/** Account-pool slot projection — Stage-3 InflightLiveCard swaps mock workers onto this. */
export type LiveWorker = {
  id: string
  name: string
  provider: string
  busy: boolean
  in_flight: number
  max_inflight: number
  enabled: boolean
  rate_limited: boolean
  progress: number
  /** RFC3339 when the pool slot went busy (InFlight 0→1); omitted when idle. */
  started_at?: string
}

/** Active cooldown window from AccountView.DownUntil / model_cooldowns. */
export type LiveRateLimit = {
  id: string
  label: string
  account_id: string
  model?: string
  until: string
  remaining_ms: number
}

export type LiveSticky = {
  ttl_seconds: number
  capacity: number
  bindings: number
  hits: number
  misses: number
  escapes: number
  rebindings: number
  last_escape_at?: string
  last_escape_reason?: string
  last_miss_reason?: string
}

export type LiveSnapshot = {
  time: string
  in_flight: number
  capacity: number
  rpm: number
  accounts: LiveAccount[]
  /** Stage-3: map onto MockAccountSlot (busy / rate_limited / progress). */
  workers: LiveWorker[]
  /** Stage-3: map onto MockRateLimitWindow (remaining_ms; depth bands stay client-side). */
  rate_limits: LiveRateLimit[]
  sticky: LiveSticky
}

export function fetchLive() {
  return api<LiveSnapshot>('/api/live')
}
