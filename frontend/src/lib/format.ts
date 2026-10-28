export function formatCompact(value: number) {
  if (!Number.isFinite(value)) return '—'
  const abs = Math.abs(value)
  if (abs >= 1_000_000) return `${trimFixed(value / 1_000_000)}M`
  if (abs >= 1000) return `${trimFixed(value / 1000)}k`
  return String(Math.round(value))
}

export function formatLatency(ms: number | null | undefined) {
  if (ms == null || !Number.isFinite(ms)) return '—'
  if (ms >= 1000) return `${trimFixed(ms / 1000)}s`
  return `${Math.round(ms)}ms`
}

export function formatBytes(bytes: number | null | undefined) {
  if (bytes == null || !Number.isFinite(bytes) || bytes < 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  return `${trimFixed(value)} ${units[unit]}`
}

export function formatPercent(rate: number | null | undefined) {
  if (rate == null || !Number.isFinite(rate)) return '—'
  const pct = rate * 100
  return `${pct >= 10 ? pct.toFixed(0) : pct.toFixed(1)}%`
}

export function formatCountKind(value: number, kind: 'int' | 'compact' | 'percent' | 'ms') {
  if (kind === 'compact') return formatCompact(value)
  if (kind === 'percent') return formatPercent(value)
  if (kind === 'ms') return formatLatency(value)
  return String(Math.round(value))
}

/** Official WorkBuddy catalog credits text, when present. */
export function modelCreditsText(model: { credits?: string | null }) {
  const credits = (model.credits || '').trim()
  return credits || ''
}

export function modelIsFree(model: { free?: boolean | null; credits?: string | null }) {
  if (model.free) return true
  const credits = modelCreditsText(model).toLowerCase()
  if (!credits) return false
  const match = credits.match(/(\d+(?:\.\d+)?)/)
  return Boolean(match && Number(match[1]) === 0)
}

function trimFixed(value: number) {
  const digits = Math.abs(value) >= 10 ? 0 : 1
  return Number(value.toFixed(digits)).toString()
}

export function formatCNY(value: number | null | undefined) {
  if (value == null || !Number.isFinite(value)) return '—'
  return `¥${value.toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
}

/** Raw unit price (already in CNY per 1M tokens); trims needless decimals. */
export function formatUnitPrice(value: number | null | undefined) {
  if (value == null || !Number.isFinite(value)) return '—'
  return `¥${Number(value.toFixed(4))}`
}

/** Subscription quota spent, in the provider's own credit unit. */
export function formatCredits(value: number | null | undefined) {
  if (value == null || !Number.isFinite(value)) return '—'
  if (value >= 10_000) return formatCompact(value)
  return value.toLocaleString('zh-CN', { minimumFractionDigits: value < 100 ? 2 : 0, maximumFractionDigits: 2 })
}

/** Output generation rate (tok/s). Prefer post-TTFT window on streams. */
export function computeOutputTokensPerSecond(opts: {
  completionTokens?: number | null
  latencyMs?: number | null
  ttfbMs?: number | null
  stream?: boolean | null
}): number | null {
  const completion = opts.completionTokens
  const latency = opts.latencyMs
  const ttfb = opts.ttfbMs
  if (completion == null || !Number.isFinite(completion) || completion <= 0) return null
  if (latency == null || !Number.isFinite(latency) || latency <= 0) return null

  const preferGeneration =
    opts.stream === true &&
    ttfb != null &&
    Number.isFinite(ttfb) &&
    ttfb >= 0 &&
    latency - ttfb > 0

  const seconds = preferGeneration ? (latency - (ttfb as number)) / 1000 : latency / 1000
  if (!(seconds > 0)) return null
  const rate = completion / seconds
  return Number.isFinite(rate) && rate > 0 ? rate : null
}

/** e.g. `39.7 tok/s`, integer when large; missing → `—`. */
export function formatTokensPerSecond(rate: number | null | undefined) {
  if (rate == null || !Number.isFinite(rate) || rate <= 0) return '—'
  const text = rate >= 100 ? String(Math.round(rate)) : Number(rate.toFixed(1)).toString()
  return `${text} tok/s`
}
