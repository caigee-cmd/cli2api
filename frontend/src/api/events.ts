import { ApiError } from './client'
import { readStoredApiKey } from '@/hooks/useApiKey'
import type { AccountQuota } from './types'

/** Event types published by the runtime event bus (`internal/events`). */
export type ServerEventType = 'account' | 'accounts' | 'quota' | 'checkin' | 'refresh' | 'bulk'

/** Real per-account payloads carried by each event type. */
export type AccountStateEvent = {
  account_id?: string
  runtime_state?: string
  ready?: boolean
  cooldown_until?: string
  last_error?: string
  last_kind?: string
}

export type QuotaStateEvent = {
  account_id?: string
  quota?: AccountQuota
}

export type CheckinStateEvent = {
  account_id?: string
  status?: string
  message?: string
  reward_credits?: number
  at?: string
}

export type RefreshStateEvent = {
  account_id?: string
  provider?: string
  region?: string
  ok?: boolean
  status?: string
  code?: string
  kind?: string
  message?: string
  http_status?: number
  quota?: AccountQuota
}

export type BulkEventData = {
  op_id?: string
  kind?: 'checkin' | 'refresh'
  phase?: 'start' | 'account' | 'done'
  total?: number
  done?: number
  account_id?: string
  result?: Record<string, unknown>
}

/** One SSE frame as delivered by GET /api/events. */
export type ServerEvent = {
  seq: number
  type: ServerEventType
  time: string
  account_id?: string
  op_id?: string
  data?: AccountStateEvent | QuotaStateEvent | CheckinStateEvent | RefreshStateEvent | BulkEventData
}

export type EventStreamStatus = 'connecting' | 'live' | 'offline'

type StreamOptions = {
  /** Last seen seq; the server replays newer ring events before streaming live. */
  lastSeq: number
  signal: AbortSignal
  onEvent: (event: ServerEvent) => void
  onOpen?: () => void
}

function splitFrames(buffer: string): { frames: string[]; rest: string } {
  const frames: string[] = []
  let rest = buffer.replace(/\r\n/g, '\n')
  let index = rest.indexOf('\n\n')
  while (index !== -1) {
    frames.push(rest.slice(0, index))
    rest = rest.slice(index + 2)
    index = rest.indexOf('\n\n')
  }
  return { frames, rest }
}

function parseFrame(frame: string): { id?: number; event?: string; data?: string } {
  const out: { id?: number; event?: string; data?: string } = {}
  const dataLines: string[] = []
  for (const line of frame.split('\n')) {
    if (!line || line.startsWith(':')) continue
    const colon = line.indexOf(':')
    const field = colon === -1 ? line : line.slice(0, colon)
    const rawValue = colon === -1 ? '' : line.slice(colon + 1)
    const value = rawValue.startsWith(' ') ? rawValue.slice(1) : rawValue
    if (field === 'id') {
      const id = Number(value)
      if (Number.isFinite(id)) out.id = id
    } else if (field === 'event') {
      out.event = value
    } else if (field === 'data') {
      dataLines.push(value)
    }
  }
  if (dataLines.length) out.data = dataLines.join('\n')
  return out
}

/**
 * Opens one console event stream and resolves when it ends (server close,
 * network failure, or abort). The console key travels in the Authorization
 * header — EventSource cannot do that, so the stream is read with fetch.
 */
export async function openEventStream(opts: StreamOptions): Promise<void> {
  const headers: Record<string, string> = { Accept: 'text/event-stream' }
  const key = readStoredApiKey().trim()
  if (key) headers.Authorization = `Bearer ${key}`
  if (opts.lastSeq > 0) headers['Last-Event-ID'] = String(opts.lastSeq)
  const url = opts.lastSeq > 0 ? `/api/events?last_seq=${opts.lastSeq}` : '/api/events'

  let res: Response
  try {
    res = await fetch(url, { headers, signal: opts.signal, cache: 'no-store' })
  } catch (error) {
    if (opts.signal.aborted) return
    throw error
  }
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`
    try {
      const body = await res.text()
      try {
        const parsed = JSON.parse(body)
        message = parsed?.error?.message || parsed?.message || message
      } catch {
        if (body) message = body.slice(0, 200)
      }
    } catch {
      /* keep status text */
    }
    throw new ApiError(message, res.status)
  }

  opts.onOpen?.()
  const reader = res.body?.getReader()
  if (!reader) throw new ApiError('event stream unavailable', res.status)

  const decoder = new TextDecoder()
  let buffer = ''
  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      const split = splitFrames(buffer)
      buffer = split.rest
      for (const frame of split.frames) {
        const parsed = parseFrame(frame)
        // Comments (heartbeats) and bare `id:` frames carry no state change.
        if (!parsed.data) continue
        let payload: ServerEvent
        try {
          payload = JSON.parse(parsed.data) as ServerEvent
        } catch {
          continue
        }
        if (typeof payload?.type !== 'string') continue
        if (!Number.isFinite(payload.seq) && parsed.id != null) payload.seq = parsed.id
        opts.onEvent(payload)
      }
      if (opts.signal.aborted) break
    }
  } finally {
    try {
      await reader.cancel()
    } catch {
      /* stream already closed */
    }
  }
}
