import { useLayoutEffect, useMemo, useRef, useState } from 'react'
import gsap from 'gsap'
import {
  Area,
  CartesianGrid,
  ComposedChart,
  Line,
  ReferenceArea,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { RequestStatsPoint } from '@/api/logs'
import { TrafficChartEmpty } from '@/components/overview/TrafficChartEmpty'
import { FilterToggle } from '@/components/ui/FilterToggle'
import { formatCompact, formatLatency } from '@/lib/format'
import { TYPE_SCALE_PX } from '@/lib/typeScale'

function hourLabel(value: string, daily: boolean, lang: 'en' | 'zh') {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return ''
  return new Intl.DateTimeFormat(lang === 'zh' ? 'zh-CN' : 'en-US', daily
    ? { month: 'short', day: 'numeric' }
    : { hour: '2-digit', minute: '2-digit', hour12: false }).format(date)
}

function niceMax(peak: number) {
  if (peak <= 1) return 1
  const exp = Math.floor(Math.log10(peak))
  const base = 10 ** exp
  const n = peak / base
  const nice = n <= 1 ? 1 : n <= 2 ? 2 : n <= 5 ? 5 : 10
  return nice * base
}

export type TrafficMetric = 'requests' | 'tokens' | 'latency'

type ChartRow = RequestStatsPoint & {
  okCount: number
  errorCount: number
  inputTokens: number
  outputTokens: number
  cacheTokens: number
  p50: number | null
  p95: number | null
}

type SeriesSpec = { key: keyof ChartRow; label: string; color: string; area?: boolean; width?: number; opacity?: number }

/** Derives the plotted fields for every metric from one stats point. */
function toChartRow(point: RequestStatsPoint): ChartRow {
  const okCount = Math.max(0, point.ok ?? (point.requests - point.error))
  const errorCount = Math.min(Math.max(0, point.requests - okCount), Math.max(0, point.requests))
  return {
    ...point,
    okCount,
    errorCount,
    inputTokens: point.prompt_tokens ?? 0,
    outputTokens: point.completion_tokens ?? 0,
    cacheTokens: point.cache_read_tokens ?? 0,
    p50: point.latency_p50_ms ?? null,
    p95: point.latency_p95_ms ?? null,
  }
}

function TrafficTooltip({
  active,
  payload,
  daily,
  lang,
  specs,
  format,
  totalLabel,
  total,
}: {
  active?: boolean
  payload?: Array<{ payload: ChartRow }>
  daily: boolean
  lang: 'en' | 'zh'
  specs: SeriesSpec[]
  format: (value: number | null) => string
  totalLabel?: string
  total?: (row: ChartRow) => number
}) {
  if (!active || !payload?.[0]) return null
  const point = payload[0].payload
  return (
    <div className="rounded-xl bg-overlay px-3 py-2 text-caption text-overlay-foreground shadow-overlay">
      <div className="mono text-muted">{hourLabel(point.at, daily, lang)}</div>
      <div className="mt-1.5 grid gap-1">
        {specs.map((spec) => (
          <div key={String(spec.key)} className="flex items-center justify-between gap-6">
            <span className="inline-flex items-center gap-1.5"><span className="size-1.5 rounded-full" style={{ background: spec.color }} />{spec.label}</span>
            <span className="mono font-medium">{format(point[spec.key] as number | null)}</span>
          </div>
        ))}
        {total && totalLabel ? (
          <div className="flex items-center justify-between gap-6 pt-1">
            <span className="text-muted">{totalLabel}</span>
            <span className="mono font-medium">{format(total(point))}</span>
          </div>
        ) : null}
      </div>
    </div>
  )
}

export function TrafficChart({
  series,
  lang,
  emptyLabel,
  okLabel,
  errorLabel,
  onSelectRange,
  selectHint,
  labels,
  initialMetric = 'requests',
}: {
  series: RequestStatsPoint[]
  lang: 'en' | 'zh'
  emptyLabel: string
  okLabel: string
  errorLabel: string
  /** Labels for the metric switch and its series. */
  labels?: {
    requests: string
    tokens: string
    latency: string
    input: string
    output: string
    cacheRead: string
    p50: string
    p95: string
    total: string
  }
  /** Initial metric (the switch is internal). */
  initialMetric?: TrafficMetric
  /** Called with an ISO [from, to) range after the user drag-selects buckets. */
  onSelectRange?: (from: string, to: string) => void
  selectHint?: string
}) {
  const [metric, setMetric] = useState<TrafficMetric>(initialMetric)
  const [dragStart, setDragStart] = useState<string | null>(null)
  const [dragEnd, setDragEnd] = useState<string | null>(null)
  // Draw the lines in once per mount; background refreshes must not replay it.
  const playedRef = useRef(false)
  const rootRef = useRef<HTMLDivElement>(null)
  // 修复：成功/失败折线必须各自取自己的计数。
  // 之前红色折线/面积用的是 requests（总请求数），与成功线在几乎无报错时重合，
  // 看起来就像"成功和失败数据一模一样"。这里显式派生两个序列：
  // - okCount: 成功数（后端 ok 字段；缺省时用 requests - error 兜底）
  // - errorCount: 未成功数 = requests - okCount（含 error/incomplete/canceled）。
  //   不能只用后端 error 字段：canceled（客户端断开）等状态会被顶掉，
  //   导致 tooltip 出现 total 37 / 成功 1 / 失败 0、红线却贴底的缺口。
  //   与概览卡片"成功率 = ok/total"的口径保持一致，红线垫平剩余部分。
  const data = useMemo<ChartRow[]>(() => series.map(toChartRow), [series])
  const first = data[0]
  const last = data[data.length - 1]
  const span = first && last ? Date.parse(last.at) - Date.parse(first.at) : 0
  const daily = span > 48 * 60 * 60 * 1000
  const l = labels ?? {
    requests: 'Requests', tokens: 'Tokens', latency: 'p95 latency',
    input: 'input', output: 'output', cacheRead: 'cache read', p50: 'p50', p95: 'p95', total: 'total',
  }
  const specs: SeriesSpec[] = metric === 'tokens'
    ? [
        { key: 'inputTokens', label: l.input, color: 'var(--foreground)', area: true, width: 1.5, opacity: 0.75 },
        { key: 'outputTokens', label: l.output, color: 'var(--success)', area: true, width: 1.5 },
        { key: 'cacheTokens', label: l.cacheRead, color: 'var(--muted)', width: 1.25, opacity: 0.8 },
      ]
    : metric === 'latency'
      ? [
          { key: 'p95', label: l.p95, color: 'var(--warning)', area: true, width: 1.75 },
          { key: 'p50', label: l.p50, color: 'var(--muted)', width: 1.25, opacity: 0.85 },
        ]
      : [
          { key: 'okCount', label: okLabel, color: 'var(--success)', area: true, width: 1.75 },
          { key: 'errorCount', label: errorLabel, color: 'var(--danger)', area: true, width: 1.25, opacity: 0.7 },
        ]
  const format = (value: number | null) => (value == null ? '—' : metric === 'latency' ? formatLatency(value) : formatCompact(value))
  const total = metric === 'requests'
    ? (row: ChartRow) => row.requests
    : metric === 'tokens'
      ? (row: ChartRow) => row.inputTokens + row.outputTokens
      : undefined
  const values = data.flatMap((row) => specs.map((spec) => Number(row[spec.key] ?? 0)))
  const peak = metric === 'requests' ? Math.max(0, ...data.map((point) => point.requests)) : Math.max(0, ...values)
  const yMax = niceMax(peak)
  const hasTraffic = data.some((point) => point.requests > 0)
  const signature = data.map((point) => `${point.at}:${point.requests}:${point.error}`).join('|')

  useLayoutEffect(() => {
    const root = rootRef.current
    if (!root || !hasTraffic) return

    if (playedRef.current) return
    let played = false
    let observer: MutationObserver | null = null
    const context = gsap.context(() => {
      const media = gsap.matchMedia()
      const play = () => {
        const fills = gsap.utils.toArray<SVGPathElement>('.recharts-area-area', root)
        const lines = gsap.utils.toArray<SVGPathElement>('.recharts-line-curve', root)
        if (!fills.length && !lines.length) return false

        media.add('(prefers-reduced-motion: reduce)', () => {
          gsap.set(lines, { clearProps: 'strokeDashoffset,strokeDasharray' })
          gsap.set(fills, { clearProps: 'autoAlpha' })
        })
        media.add('(prefers-reduced-motion: no-preference)', () => {
          gsap.fromTo(fills, { autoAlpha: 0 }, {
            autoAlpha: 1,
            duration: 0.45,
            ease: 'power2.out',
            stagger: 0.05,
          })
          lines.forEach((line, index) => {
            const length = line.getTotalLength()
            gsap.fromTo(line, { strokeDasharray: length, strokeDashoffset: length }, {
              strokeDashoffset: 0,
              duration: 0.7,
              delay: index * 0.05,
              ease: 'power2.out',
            })
          })
        })
        return true
      }

      if (play()) {
        played = true
        playedRef.current = true
        return
      }

      observer = new MutationObserver(() => {
        if (played) return
        if (play()) {
          played = true
          playedRef.current = true
          observer?.disconnect()
        }
      })
      observer.observe(root, { childList: true, subtree: true })
    }, root)

    return () => {
      observer?.disconnect()
      context.revert()
    }
  }, [hasTraffic, signature])

  const step = data.length > 1 ? Date.parse(data[1].at) - Date.parse(data[0].at) : 60 * 60 * 1000
  const finishDrag = () => {
    if (dragStart && dragEnd && onSelectRange) {
      const a = Date.parse(dragStart)
      const b = Date.parse(dragEnd)
      if (Number.isFinite(a) && Number.isFinite(b)) {
        const from = Math.min(a, b)
        const to = Math.max(a, b) + step
        onSelectRange(new Date(from).toISOString(), new Date(to).toISOString())
      }
    }
    setDragStart(null)
    setDragEnd(null)
  }
  const labelOf = (state: unknown) => {
    const label = (state as { activeLabel?: string | number } | null)?.activeLabel
    return label == null ? null : String(label)
  }

  if (!series.length || !hasTraffic) {
    return <TrafficChartEmpty emptyLabel={emptyLabel} />
  }

  return (
    <div ref={rootRef} className="min-w-0">
      <div className="mb-3 flex justify-end">
        <FilterToggle
          value={metric}
          onChange={(next) => setMetric(next as TrafficMetric)}
          ariaLabel={l.requests}
          options={[
            { id: 'requests', label: l.requests },
            { id: 'tokens', label: l.tokens },
            { id: 'latency', label: l.latency },
          ]}
        />
      </div>
      <div className="h-56 w-full">
        <ResponsiveContainer width="100%" height="100%">
          <ComposedChart
            data={data}
            margin={{ top: 8, right: 8, bottom: 0, left: 0 }}
            style={onSelectRange ? { cursor: 'crosshair', userSelect: 'none' } : undefined}
            onMouseDown={onSelectRange ? (state) => {
              const label = labelOf(state)
              if (label) {
                setDragStart(label)
                setDragEnd(label)
              }
            } : undefined}
            onMouseMove={onSelectRange ? (state) => {
              if (!dragStart) return
              const label = labelOf(state)
              if (label) setDragEnd(label)
            } : undefined}
            onMouseUp={onSelectRange ? finishDrag : undefined}
            onMouseLeave={onSelectRange ? () => { setDragStart(null); setDragEnd(null) } : undefined}
          >
            <defs>
              {specs.filter((spec) => spec.area).map((spec) => (
                <linearGradient key={String(spec.key)} id={`traffic-fill-${String(spec.key)}`} x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor={spec.color} stopOpacity={0.22} />
                  <stop offset="100%" stopColor={spec.color} stopOpacity={0.02} />
                </linearGradient>
              ))}
            </defs>
            <CartesianGrid vertical={false} stroke="var(--separator)" strokeDasharray="3 6" />
            <XAxis
              dataKey="at"
              tickLine={false}
              axisLine={false}
              minTickGap={28}
              tick={{ fill: 'var(--muted)', fontSize: TYPE_SCALE_PX.caption, fontFamily: 'var(--font-mono)' }}
              tickFormatter={(value: string) => hourLabel(value, daily, lang)}
            />
            <YAxis
              width={metric === 'latency' ? 44 : 36}
              domain={[0, yMax]}
              tickLine={false}
              axisLine={false}
              allowDecimals={false}
              tick={{ fill: 'var(--muted)', fontSize: TYPE_SCALE_PX.caption, fontFamily: 'var(--font-mono)' }}
              tickFormatter={(value: number) => (metric === 'latency' ? formatLatency(value) : formatCompact(value))}
            />
            <Tooltip
              cursor={{ stroke: 'var(--border)', strokeDasharray: '3 3' }}
              content={<TrafficTooltip daily={daily} lang={lang} specs={specs} format={format} totalLabel={l.total} total={total} />}
            />
            {specs.filter((spec) => spec.area).map((spec) => (
              <Area
                key={`area-${String(spec.key)}`}
                type="monotone"
                dataKey={spec.key as string}
                stroke="none"
                fill={`url(#traffic-fill-${String(spec.key)})`}
                isAnimationActive={false}
                activeDot={false}
                dot={false}
                connectNulls
              />
            ))}
            {specs.map((spec) => (
              <Line
                key={`line-${String(spec.key)}`}
                type="monotone"
                dataKey={spec.key as string}
                stroke={spec.color}
                strokeWidth={spec.width ?? 1.5}
                strokeOpacity={spec.opacity ?? 1}
                dot={false}
                isAnimationActive={false}
                connectNulls
                activeDot={{ r: 4, fill: 'var(--surface)', stroke: spec.color, strokeWidth: 1.5 }}
              />
            ))}
            {dragStart && dragEnd ? (
              <ReferenceArea x1={dragStart} x2={dragEnd} fill="var(--foreground)" fillOpacity={0.06} stroke="none" ifOverflow="visible" />
            ) : null}
          </ComposedChart>
        </ResponsiveContainer>
      </div>
      <div className="mt-2 flex min-h-5 items-center justify-between gap-3 text-caption text-muted">
        <div className="flex items-center gap-3">
          {specs.map((spec) => (
            <span key={String(spec.key)} className="inline-flex items-center gap-1.5"><span className="size-1.5 rounded-full" style={{ background: spec.color }} />{spec.label}</span>
          ))}
        </div>
        <span className="flex items-center gap-3">
          {onSelectRange && selectHint ? <span>{selectHint}</span> : null}
          <span className="mono">{format(peak)} peak</span>
        </span>
      </div>
    </div>
  )
}
