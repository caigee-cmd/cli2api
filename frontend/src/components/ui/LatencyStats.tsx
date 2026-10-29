import { formatLatency, computeOutputTokensPerSecond, formatTokensPerSecond } from '@/lib/format'

export type LatencyStatsLabels = {
  /** Optional block heading (e.g. 延迟). Column headers already cover this in tables. */
  heading?: string
  ttft: string
  total: string
  tps: string
}

export type LatencyStatsProps = {
  latencyMs?: number | null
  ttfbMs?: number | null
  completionTokens?: number | null
  /** When true/undefined, prefer generation window (latency − TTFT). */
  stream?: boolean | null
  labels: LatencyStatsLabels
  className?: string
  /** block = three-row Logs layout; inline = one compact Access line. */
  variant?: 'block' | 'inline'
}

/**
 * Latency: TTFT / total / output TPS.
 * Grayscale Studio Admin tokens — muted labels, mono values.
 */
export function LatencyStats({
  latencyMs,
  ttfbMs,
  completionTokens,
  stream,
  labels,
  className = '',
  variant = 'block',
}: LatencyStatsProps) {
  const tps = formatTokensPerSecond(
    computeOutputTokensPerSecond({
      completionTokens,
      latencyMs,
      ttfbMs,
      stream,
    }),
  )
  const ttft = formatLatency(ttfbMs)
  const total = formatLatency(latencyMs)

  if (variant === 'inline') {
    return (
      <div className={`mono text-caption leading-4 text-muted ${className}`.trim()}>
        <span>{labels.ttft} {ttft}</span>
        <span className="mx-1.5 text-separator">·</span>
        <span>{labels.total} {total}</span>
        <span className="mx-1.5 text-separator">·</span>
        <span>{tps}</span>
      </div>
    )
  }

  const rows: Array<[string, string]> = [
    [labels.ttft, ttft],
    [labels.total, total],
    [labels.tps, tps],
  ]

  return (
    <div className={className}>
      {labels.heading ? (
        <div className="mb-1 text-caption font-medium text-muted">{labels.heading}</div>
      ) : null}
      <div className="border-l border-separator pl-2 leading-4">
        {rows.map(([label, value]) => (
          <div key={label} className="flex items-baseline gap-2 py-px">
            <span className="shrink-0 text-caption text-muted">{label}</span>
            <span className="mono text-xs text-foreground">{value}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
