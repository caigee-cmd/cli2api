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

function trimFixed(value: number) {
  const digits = Math.abs(value) >= 10 ? 0 : 1
  return Number(value.toFixed(digits)).toString()
}
