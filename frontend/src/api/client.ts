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

export function isUnauthorized(err: unknown) {
  if (err instanceof ApiError) return err.status === 401 || err.code === 'invalid_api_key'
  const msg = err instanceof Error ? err.message : String(err || '')
  return /invalid_api_key|unauthorized|Missing\/invalid PROXY_API_KEY/i.test(msg)
}

export async function api<T = any>(path: string, opts: RequestInit = {}, keyOverride?: string): Promise<T> {
  const headers = new Headers(opts.headers || {})
  if (opts.body && !(opts.body instanceof FormData) && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  const key = (keyOverride ?? readStoredApiKey()).trim()
  if (key && !headers.has('Authorization')) {
    headers.set('Authorization', `Bearer ${key}`)
  }
  const res = await fetch(path, { ...opts, headers })
  const text = await res.text()
  let data: any
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    data = text
  }
  if (!res.ok) {
    const msg = data?.error?.message || data?.message || text || res.statusText
    throw new ApiError(msg, res.status, data?.error?.code)
  }
  return data as T
}
