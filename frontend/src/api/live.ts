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
  /** Account last_error_kind — fallback for busy→idle fail routing. */
  last_error_kind?: string
}

/** Terminal request finish drained this /api/live poll (Done vs Failed fly-out). */
export type LiveCompletion = {
  account_id: string
  /** ok | error | incomplete | canceled */
  status: string
}

/**
 * Selecting (选号) request for the InflightLiveCard waiting rail.
 * status reuses request_logs RequestStatus* (typically started) — not a new machine.
 */
export type LiveJob = {
  id: string
  name: string
  /** RequestStatus* from recorder (started while selecting). */
  status: string
  /** RFC3339 Prepare/Start time. */
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
  /** Terminal finishes since last poll — InflightLiveCard routes fly-outs. */
  recent_completions?: LiveCompletion[]
  /** Prepare→Acquire 选号 (RequestStatusStarted, no pool bind). */
  queue?: LiveJob[]
}

export function fetchLive() {
  return api<LiveSnapshot>('/api/live')
}
