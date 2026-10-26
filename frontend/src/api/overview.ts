import { api, ApiError } from './client'
import { readStoredApiKey } from '@/hooks/useApiKey'
import type { CheckinRecord, Overview } from './types'

export function fetchOverviewSummary(keyOverride?: string) {
  return api<Overview>('/api/overview/summary', {}, keyOverride)
}

export function fetchAccounts(refresh = false) {
  return api<{ object?: string; data?: NonNullable<Overview['accounts']> }>(
    `/api/accounts?refresh=${refresh ? '1' : '0'}`,
  )
}

export function startDeviceLogin(accountId?: string, loginMethod?: string) {
  if (!accountId) throw new Error('account id required')
  const body: Record<string, string> = {}
  if (loginMethod) body.login_method = loginMethod
  return api<{ authUrl?: string; status?: string; message?: string }>(`/api/accounts/${encodeURIComponent(accountId)}/login/device`, {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

export function fetchLoginStatus(accountId?: string) {
  if (!accountId) throw new Error('account id required')
  return api<{ login?: any }>(`/api/accounts/${encodeURIComponent(accountId)}/login/status`)
}

export function completeLoginCallback(accountId: string, callbackUrl: string) {
  if (!accountId) throw new Error('account id required')
  return api(`/api/accounts/${encodeURIComponent(accountId)}/login/callback`, {
    method: 'POST',
    body: JSON.stringify({ callback_url: callbackUrl }),
  })
}

export function loginWithPat(pat: string, accountId?: string) {
  if (!accountId) throw new Error('account id required')
  return api(`/api/accounts/${encodeURIComponent(accountId)}/login/pat`, {
    method: 'POST',
    body: JSON.stringify({ pat }),
  })
}

type ModelsResponse = { data?: Overview['models'] }

type ModelsMemoryEntry = {
  data: ModelsResponse
  at: number
  pending?: Promise<ModelsResponse>
}

const modelsMemoryTTL = 30_000
const modelsMemoryCache = new Map<string, ModelsMemoryEntry>()

function modelsMemoryKey(accountId?: string, view?: 'regional') {
  return `${accountId || '*'}@${view || 'merged'}`
}

export function fetchModels(accountId?: string, refresh = false, view?: 'regional') {
  const q = new URLSearchParams()
  if (refresh) q.set('refresh', '1')
  if (accountId) q.set('account', accountId)
  if (view) q.set('view', view)
  const query = q.toString()
  return api<ModelsResponse>(`/api/models${query ? `?${query}` : ''}`)
}

export function fetchModelsCached(accountId?: string, view?: 'regional') {
  const key = modelsMemoryKey(accountId, view)
  const cached = modelsMemoryCache.get(key)
  if (cached && Date.now() - cached.at < modelsMemoryTTL) {
    return Promise.resolve(cached.data)
  }
  if (cached?.pending) return cached.pending
  const pending = fetchModels(accountId, false, view).then((data) => {
    modelsMemoryCache.set(key, { data, at: Date.now() })
    return data
  }).finally(() => {
    const current = modelsMemoryCache.get(key)
    if (current?.pending === pending) {
      modelsMemoryCache.set(key, { data: current.data, at: current.at })
    }
  })
  modelsMemoryCache.set(key, { data: cached?.data || {}, at: cached?.at || 0, pending })
  return pending
}

export function refreshModels(accountId?: string, view?: 'regional') {
  const key = modelsMemoryKey(accountId, view)
  modelsMemoryCache.delete(key)
  return fetchModels(accountId, true, view).then((data) => {
    modelsMemoryCache.set(key, { data, at: Date.now() })
    return data
  })
}

export function updateModelContext(modelKey: string, contextLength: number) {
  return api<{
    model: string
    context_length: number
    default_context_length: number
    context_custom: boolean
  }>(`/api/models/${encodeURIComponent(modelKey)}`, {
    method: 'PATCH',
    body: JSON.stringify({ context_length: contextLength }),
  })
}

export function updateTraeMaxMode(modelKey: string, maxMode: boolean) {
  return updateProviderMaxMode('trae', modelKey, maxMode)
}

export function updateProviderMaxMode(provider: string, modelKey: string, maxMode: boolean, contextWindow?: number | null) {
  return api<{
    model: string
    provider: string
    max_mode: boolean
    reasoning_effort?: string
    context_custom: boolean
    context_length?: number
    default_context_length?: number
  }>(`/api/models/${encodeURIComponent(provider)}/${encodeURIComponent(modelKey)}`, {
    method: 'PATCH',
    // Qoder has no is_max_mode upstream: its toggle maps onto the numeric
    // window, so send the target window explicitly. Trae ignores context_length
    // and switches on max_mode alone.
    body: JSON.stringify(
      provider === 'qoder' && maxMode && contextWindow ? { max_mode: maxMode, context_length: contextWindow } : { max_mode: maxMode },
    ),
  })
}

export function updateProviderReasoning(provider: 'trae' | 'workbuddy' | 'kimi', modelKey: string, reasoningEffort: string) {
  return api<{
    model: string
    provider: string
    max_mode?: boolean
    reasoning_effort?: string
    context_custom: boolean
  }>(`/api/models/${provider}/${encodeURIComponent(modelKey)}`, {
    method: 'PATCH',
    body: JSON.stringify({ reasoning_effort: reasoningEffort }),
  })
}

export function refreshAccount(accountId: string, options?: { quota?: boolean }) {
  if (!accountId) throw new Error('account id required')
  const query = options?.quota ? '?quota=1' : ''
  return api(`/api/accounts/${encodeURIComponent(accountId)}/refresh${query}`, {
    method: 'POST',
    body: '{}',
  })
}

export function testChat(model: string, content: string, accountId?: string) {
  const headers: Record<string, string> = {}
  if (accountId) headers['X-Qoder-Account'] = accountId
  return api('/api/chat', {
    method: 'POST',
    headers,
    body: JSON.stringify({
      model,
      stream: false,
      messages: [{ role: 'user', content }],
    }),
  })
}

export type StreamChatContentPart =
  | { type: 'text'; text: string }
  | { type: 'image_url'; image_url: { url: string } }

export type StreamChatMessage = {
  role: 'user' | 'assistant' | 'system' | 'tool'
  content: string | StreamChatContentPart[]
  name?: string
  tool_call_id?: string
  tool_calls?: StreamToolCall[]
  reasoning_content?: string
}

export type StreamToolCall = {
  index: number
  id?: string
  type?: string
  function?: {
    name?: string
    arguments?: string
  }
}

export type StreamChatUsage = {
  prompt_tokens?: number
  completion_tokens?: number
  total_tokens?: number
  /** Subscription credit spend when the upstream reports it. */
  credits?: number
  source?: string
}

export type StreamChatHandlers = {
  onContent?: (chunk: string, full: string) => void
  onReasoning?: (chunk: string, full: string) => void
  onToolCalls?: (toolCalls: StreamToolCall[]) => void
  onUsage?: (usage: StreamChatUsage) => void
  signal?: AbortSignal
}

function mergeToolCallDeltas(existing: StreamToolCall[], patches: unknown): StreamToolCall[] {
  if (!Array.isArray(patches)) return existing
  const next = existing.map((item) => ({
    ...item,
    function: item.function ? { ...item.function } : undefined,
  }))
  for (const patch of patches) {
    if (!patch || typeof patch !== 'object') continue
    const raw = patch as Record<string, unknown>
    const index = typeof raw.index === 'number' ? raw.index : next.length
    while (next.length <= index) {
      next.push({ index: next.length, function: { name: '', arguments: '' } })
    }
    const target = next[index]
    target.index = index
    if (typeof raw.id === 'string' && raw.id) target.id = raw.id
    if (typeof raw.type === 'string' && raw.type) target.type = raw.type
    const fn = raw.function
    if (fn && typeof fn === 'object') {
      const fnRaw = fn as Record<string, unknown>
      if (!target.function) target.function = { name: '', arguments: '' }
      if (typeof fnRaw.name === 'string' && fnRaw.name) target.function.name = fnRaw.name
      if (typeof fnRaw.arguments === 'string' && fnRaw.arguments) {
        target.function.arguments = (target.function.arguments || '') + fnRaw.arguments
      }
    }
  }
  return next
}

function normalizeStreamMessages(content: string | StreamChatMessage[]): StreamChatMessage[] {
  if (typeof content === 'string') return [{ role: 'user', content }]
  return content
}

/** Streaming Chat Completions via console `/api/chat` (SSE). `content` may be a string or a messages array. */
export async function streamChat(
  model: string,
  content: string | StreamChatMessage[],
  accountId: string | undefined,
  handlers: StreamChatHandlers = {},
): Promise<string> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  if (accountId) headers['X-Qoder-Account'] = accountId
  const key = readStoredApiKey().trim()
  if (key) headers.Authorization = `Bearer ${key}`

  const messages = normalizeStreamMessages(content)

  const res = await fetch('/api/chat', {
    method: 'POST',
    headers,
    body: JSON.stringify({
      model,
      stream: true,
      messages,
    }),
    signal: handlers.signal,
  })

  if (!res.ok) {
    const text = await res.text()
    let data: any
    try {
      data = text ? JSON.parse(text) : null
    } catch {
      data = text
    }
    const raw = typeof data === 'string' ? data : text
    const looksHTML = /<!doctype html|<html[\s>]/i.test(raw)
    const msg = data?.error?.message || data?.message || (looksHTML ? `${res.status} ${res.statusText}` : (raw || res.statusText))
    throw new ApiError(msg, res.status, data?.error?.code)
  }

  const reader = res.body?.getReader()
  if (!reader) throw new Error('No response body')

  const decoder = new TextDecoder()
  let buffer = ''
  let full = ''
  let reasoning = ''
  let toolCalls: StreamToolCall[] = []

  const readUsageCredits = (raw: Record<string, unknown>): number | undefined => {
    for (const key of ['credits', 'credit', 'consumed_credits', 'ConsumedCredits'] as const) {
      const value = raw[key]
      if (typeof value === 'number' && Number.isFinite(value)) return value
    }
    return undefined
  }

  const consumeLine = (line: string) => {
    const trimmed = line.trim()
    if (!trimmed.startsWith('data:')) return false
    const payload = trimmed.slice(5).trim()
    if (!payload) return false
    if (payload === '[DONE]') return true
    try {
      const json = JSON.parse(payload)
      const usageRaw = json?.usage
      if (usageRaw && typeof usageRaw === 'object') {
        const raw = usageRaw as Record<string, unknown>
        const usage: StreamChatUsage = {
          prompt_tokens: typeof raw.prompt_tokens === 'number' ? raw.prompt_tokens : undefined,
          completion_tokens: typeof raw.completion_tokens === 'number' ? raw.completion_tokens : undefined,
          total_tokens: typeof raw.total_tokens === 'number' ? raw.total_tokens : undefined,
          credits: readUsageCredits(raw),
          source: typeof raw.source === 'string' ? raw.source : undefined,
        }
        handlers.onUsage?.(usage)
      }
      const delta = json?.choices?.[0]?.delta
      if (!delta || typeof delta !== 'object') return false
      const contentDelta = delta.content
      if (typeof contentDelta === 'string' && contentDelta) {
        full += contentDelta
        handlers.onContent?.(contentDelta, full)
      }
      const reasoningDelta = delta.reasoning_content
      if (typeof reasoningDelta === 'string' && reasoningDelta) {
        reasoning += reasoningDelta
        handlers.onReasoning?.(reasoningDelta, reasoning)
      }
      if (delta.tool_calls) {
        toolCalls = mergeToolCallDeltas(toolCalls, delta.tool_calls)
        handlers.onToolCalls?.(toolCalls)
      }
    } catch {
      // ignore incomplete JSON frames
    }
    return false
  }

  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    const lines = buffer.split('\n')
    buffer = lines.pop() || ''
    for (const line of lines) {
      if (consumeLine(line)) return full
    }
  }
  if (buffer.trim()) consumeLine(buffer)
  return full
}


export type ProviderDescriptor = {
  id: string
  label: string
  runtime: string
  capabilities: {
    browser_login: boolean
    pat_login: boolean
    import_export: boolean
  }
  regions: Array<{ id: string; label: string; checkin?: { timezone: string } }>
  default_region: string
}

export function fetchProviders() {
  return api<{ data?: ProviderDescriptor[] }>('/api/providers')
}

export function createAccount(
  name: string,
  provider = 'qoder',
  region = 'global',
  options?: {
    max_inflight?: number
    priority?: number
    drop_system_prompt?: boolean
    workbuddy_auto_checkin?: boolean
    workbuddy_checkin_time?: string
    auto_checkin?: boolean
    checkin_time?: string
    proxy_url?: string
  },
) {
  return api('/api/accounts', {
    method: 'POST',
    body: JSON.stringify({
      name,
      provider,
      region,
      enabled: true,
      max_inflight: options?.max_inflight ?? 4,
      priority: options?.priority ?? 50,
      drop_system_prompt: options?.drop_system_prompt,
      workbuddy_auto_checkin: options?.workbuddy_auto_checkin,
      workbuddy_checkin_time: options?.workbuddy_checkin_time,
      auto_checkin: options?.auto_checkin,
      checkin_time: options?.checkin_time,
      proxy_url: options?.proxy_url,
    }),
  })
}

export function checkinAccount(accountId: string) {
  return api(`/api/accounts/${encodeURIComponent(accountId)}/checkin`, {
    method: 'POST',
    body: '{}',
  })
}

export function fetchCheckinRecords(accountId: string) {
  return api<{ object?: string; data?: CheckinRecord[] }>(`/api/accounts/${encodeURIComponent(accountId)}/checkins`)
}

export function updateAccount(accountId: string, input: Record<string, unknown>) {
  return api(`/api/accounts/${encodeURIComponent(accountId)}`, { method: 'PATCH', body: JSON.stringify(input) })
}

export function deleteAccount(accountId: string) {
  return api(`/api/accounts/${encodeURIComponent(accountId)}`, { method: 'DELETE' })
}

export function importAccount(bundle: Record<string, unknown>) {
  return api('/api/accounts/import', { method: 'POST', body: JSON.stringify(bundle) })
}

export function exportAccount(accountId: string) {
  return api<Record<string, unknown>>(`/api/accounts/${encodeURIComponent(accountId)}/export`)
}
