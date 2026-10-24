import { useMemo, useRef, useLayoutEffect } from 'react'
import gsap from 'gsap'
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { CostSeriesPoint, ModelCost } from '@/api/usage'
import { formatCNY } from '@/lib/format'

// Stacked bands need distinct, sober hues. Row 1 uses the console accent ink
// (charcoal / snow); deeper rows fall back to muted steel and the two status
// hues so the stack stays readable without inventing a second palette. Order is
// stable by cost (the caller sorts models), so the biggest spender is always ink.
const SERIES_COLORS = ['var(--accent)', 'var(--muted)', 'var(--success)', 'var(--warning)']

function bucketLabel(value: string, daily: boolean, lang: 'en' | 'zh') {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return ''
  return new Intl.DateTimeFormat(lang === 'zh' ? 'zh-CN' : 'en-US', daily
    ? { month: 'short', day: 'numeric' }
    : { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false }).format(date)
}

type ChartRow = { at: string } & Record<string, string | number>

function CostTooltip({
  active,
  payload,
  label,
  daily,
  lang,
  models,
  totalLabel,
}: {
  active?: boolean
  payload?: Array<{ dataKey: string; value: number }>
  label?: string
  daily: boolean
  lang: 'en' | 'zh'
  models: ModelCost[]
  totalLabel: string
}) {
  if (!active || !payload?.length) return null
  const labelById = new Map(models.map((model) => [model.id, model.label]))
  const rows = payload.filter((item) => (item.value ?? 0) > 0).sort((a, b) => b.value - a.value)
  const total = payload.reduce((sum, item) => sum + (item.value ?? 0), 0)
  return (
    <div className="rounded-xl bg-overlay px-3 py-2 text-caption text-overlay-foreground shadow-overlay">
      <div className="mono text-muted">{bucketLabel(String(label), daily, lang)}</div>
      <div className="mt-1.5 grid gap-1">
        {rows.map((item) => (
          <div key={item.dataKey} className="flex items-center justify-between gap-6">
            <span className="inline-flex items-center gap-1.5">
              <span className="size-1.5 rounded-full" style={{ background: SERIES_COLORS[models.findIndex((m) => m.id === item.dataKey) % SERIES_COLORS.length] }} />
              {labelById.get(item.dataKey) || item.dataKey}
            </span>
            <span className="mono font-medium">{formatCNY(item.value)}</span>
          </div>
        ))}
        <div className="flex items-center justify-between gap-6 border-t border-separator pt-1">
          <span className="text-muted">{totalLabel}</span>
          <span className="mono font-medium">{formatCNY(total)}</span>
        </div>
      </div>
    </div>
  )
}

export function CostChart({
  series,
  models,
  lang,
  emptyLabel,
  totalLabel,
}: {
  series: CostSeriesPoint[]
  models: ModelCost[]
  lang: 'en' | 'zh'
  emptyLabel: string
  totalLabel: string
}) {
  const rootRef = useRef<HTMLDivElement>(null)
  const ordered = useMemo(() => models.map((model) => model.id), [models])
  const data = useMemo<ChartRow[]>(() => series.map((point) => {
    const row: ChartRow = { at: point.at }
    for (const id of ordered) row[id] = Number((point.per_model[id] ?? 0).toFixed(4))
    return row
  }), [series, ordered])
  const peak = Math.max(0, ...data.map((row) => ordered.reduce((sum, id) => sum + Number(row[id] ?? 0), 0)))
  const hasCost = peak > 0
  const span = data.length > 1 ? Date.parse(data[data.length - 1].at) - Date.parse(data[0].at) : 0
  const daily = span > 48 * 60 * 60 * 1000
  const signature = data.map((row) => `${row.at}:${ordered.map((id) => Number(row[id])).join(',')}`).join('|')

  useLayoutEffect(() => {
    const root = rootRef.current
    if (!root || !hasCost) return
    const media = gsap.matchMedia()
    media.add('(prefers-reduced-motion: no-preference)', () => {
      const fills = gsap.utils.toArray<SVGPathElement>('.recharts-area-area', root)
      gsap.fromTo(fills, { autoAlpha: 0 }, {
        autoAlpha: 1,
        duration: 0.5,
        ease: 'power2.out',
        stagger: 0.05,
      })
    }, root)
    return () => media.revert()
  }, [hasCost, signature])

  if (!series.length || !hasCost) {
    return (
      <div className="grid min-h-56 place-items-center px-6 py-12 text-center text-sm text-muted">
        {emptyLabel}
      </div>
    )
  }

  return (
    <div ref={rootRef} className="min-w-0">
      <div className="h-72 w-full">
        <ResponsiveContainer width="100%" height="100%">
          <AreaChart data={data} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
            <defs>
              {ordered.map((id, index) => (
                <linearGradient key={id} id={`cost-${id}`} x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor={SERIES_COLORS[index % SERIES_COLORS.length]} stopOpacity={0.32} />
                  <stop offset="100%" stopColor={SERIES_COLORS[index % SERIES_COLORS.length]} stopOpacity={0.04} />
                </linearGradient>
              ))}
            </defs>
            <CartesianGrid vertical={false} stroke="var(--separator)" strokeDasharray="3 6" />
            <XAxis
              dataKey="at"
              tickLine={false}
              axisLine={false}
              minTickGap={28}
              tick={{ fill: 'var(--muted)', fontSize: 10, fontFamily: 'var(--font-mono)' }}
              tickFormatter={(value: string) => bucketLabel(value, daily, lang)}
            />
            <YAxis
              width={48}
              tickLine={false}
              axisLine={false}
              tick={{ fill: 'var(--muted)', fontSize: 10, fontFamily: 'var(--font-mono)' }}
              tickFormatter={(value: number) => `¥${value >= 10 ? Math.round(value) : Number(value.toFixed(1))}`}
            />
            <Tooltip
              cursor={{ stroke: 'var(--border)', strokeDasharray: '3 3' }}
              content={<CostTooltip daily={daily} lang={lang} models={models} totalLabel={totalLabel} />}
            />
            {ordered.map((id, index) => (
              <Area
                key={id}
                type="monotone"
                dataKey={id}
                stackId="cost"
                stroke={SERIES_COLORS[index % SERIES_COLORS.length]}
                strokeWidth={1.5}
                fill={`url(#cost-${id})`}
                isAnimationActive={false}
                dot={false}
                activeDot={{ r: 3, strokeWidth: 1.2 }}
              />
            ))}
          </AreaChart>
        </ResponsiveContainer>
      </div>
      <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-[10px] text-muted">
        {models.map((model, index) => (
          <span key={model.id} className="inline-flex items-center gap-1.5">
            <span className="size-1.5 rounded-full" style={{ background: SERIES_COLORS[index % SERIES_COLORS.length] }} />
            {model.label}
          </span>
        ))}
      </div>
    </div>
  )
}
