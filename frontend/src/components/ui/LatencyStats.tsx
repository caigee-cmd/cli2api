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
}

/**
 * Three-row latency block: TTFT / total / output TPS.
 * Grayscale Studio Admin tokens — muted labels, mono values, soft left rule.
 */
export function LatencyStats({
  latencyMs,
  ttfbMs,
  completionTokens,
  stream,
  labels,
  className = '',
}: LatencyStatsProps) {
  const tps = formatTokensPerSecond(
    computeOutputTokensPerSecond({
      completionTokens,
      latencyMs,
      ttfbMs,
      stream,
    }),
  )
  const rows: Array<[string, string]> = [
    [labels.ttft, formatLatency(ttfbMs)],
    [labels.total, formatLatency(latencyMs)],
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
