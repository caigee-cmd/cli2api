import { readStoredApiKey } from '@/hooks/useApiKey'

export class ApiError extends Error {
  status: number
  code?: string

  constructor(message: string, status: number, code?: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

/** RequestInit plus an opt-out for the console's default watchdog. */
export type ApiRequestInit = RequestInit & {
  /** Milliseconds before the request is aborted. 0 disables the watchdog. */
  timeoutMs?: number
}

const DEFAULT_TIMEOUT_MS = 180_000
/** Server-side batch jobs (probe, refresh-all, import) get a wider budget. */
export const LONG_REQUEST_TIMEOUT_MS = 15 * 60_000

export function isUnauthorized(err: unknown) {
  if (err instanceof ApiError) return err.status === 401 || err.code === 'invalid_api_key'
  const msg = err instanceof Error ? err.message : String(err || '')
  return /invalid_api_key|unauthorized|Missing\/invalid API key/i.test(msg)
}

export function isTimeout(err: unknown) {
  return err instanceof ApiError && err.code === 'timeout'
}

export async function api<T = any>(path: string, opts: ApiRequestInit = {}, keyOverride?: string): Promise<T> {
  const { timeoutMs = DEFAULT_TIMEOUT_MS, ...init } = opts
  const headers = new Headers(init.headers || {})
  if (init.body && !(init.body instanceof FormData) && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  const key = (keyOverride ?? readStoredApiKey()).trim()
  if (key && !headers.has('Authorization')) {
    headers.set('Authorization', `Bearer ${key}`)
  }

  // A hung console request must not leave a page spinning forever. Long
  // operations pass a wider budget; timeoutMs: 0 disables the watchdog.
  let signal = init.signal
  let timer: number | undefined
  let timedOut = false
  if (timeoutMs > 0 && !signal) {
    const controller = new AbortController()
    signal = controller.signal
    timer = window.setTimeout(() => {
      timedOut = true
      controller.abort()
    }, timeoutMs)
  }

  let res: Response
  let text: string
  try {
    res = await fetch(path, { ...init, headers, signal })
    // Keep the watchdog armed through the body read too: a server that sends
    // headers and then stalls must still fail instead of spinning forever.
    text = await res.text()
  } catch (error) {
    if (timedOut) {
      throw new ApiError(`Request timed out after ${Math.round(timeoutMs / 1000)}s`, 0, 'timeout')
    }
    throw error
  } finally {
    if (timer != undefined) window.clearTimeout(timer)
  }

  let data: any
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    data = text
  }
  if (!res.ok) {
    const raw = typeof data === 'string' ? data : text
    const looksHTML = /<!doctype html|<html[\s>]/i.test(raw)
    const msg = data?.error?.message || data?.message || (looksHTML ? `${res.status} ${res.statusText}` : (raw || res.statusText))
    throw new ApiError(msg, res.status, data?.error?.code)
  }
  return data as T
}
