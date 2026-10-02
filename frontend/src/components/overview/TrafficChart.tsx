import { useLayoutEffect, useMemo, useRef } from 'react'
import gsap from 'gsap'
import {
  Area,
  CartesianGrid,
  ComposedChart,
  Line,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { RequestStatsPoint } from '@/api/logs'
import { TrafficChartEmpty } from '@/components/overview/TrafficChartEmpty'
import { formatCompact } from '@/lib/format'

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

type ChartRow = RequestStatsPoint & { okCount: number; errorCount: number }

function TrafficTooltip({
  active,
  payload,
  daily,
  lang,
  okLabel,
  errorLabel,
}: {
  active?: boolean
  payload?: Array<{ payload: ChartRow }>
  daily: boolean
  lang: 'en' | 'zh'
  okLabel: string
  errorLabel: string
}) {
  if (!active || !payload?.[0]) return null
  const point = payload[0].payload
  return (
    <div className="rounded-xl border border-border bg-overlay px-3 py-2 text-[11px] text-overlay-foreground shadow-overlay">
      <div className="mono text-muted">{hourLabel(point.at, daily, lang)}</div>
      <div className="mt-1.5 grid gap-1">
        <div className="flex items-center justify-between gap-6">
          <span className="inline-flex items-center gap-1.5"><span className="size-1.5 rounded-full bg-success" />{okLabel}</span>
          <span className="mono font-medium">{point.okCount}</span>
        </div>
        <div className="flex items-center justify-between gap-6">
          <span className="inline-flex items-center gap-1.5"><span className="size-1.5 rounded-full bg-danger" />{errorLabel}</span>
          {/* 与红色折线同口径：未成功数（error + incomplete + canceled） */}
          <span className="mono font-medium">{point.errorCount}</span>
        </div>
        <div className="flex items-center justify-between gap-6 border-t border-separator pt-1">
          <span className="text-muted">total</span>
          <span className="mono font-medium">{point.requests}</span>
        </div>
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
}: {
  series: RequestStatsPoint[]
  lang: 'en' | 'zh'
  emptyLabel: string
  okLabel: string
  errorLabel: string
}) {
  const rootRef = useRef<HTMLDivElement>(null)
  // 修复：成功/失败折线必须各自取自己的计数。
  // 之前红色折线/面积用的是 requests（总请求数），与成功线在几乎无报错时重合，
  // 看起来就像"成功和失败数据一模一样"。这里显式派生两个序列：
  // - okCount: 成功数（后端 ok 字段；缺省时用 requests - error 兜底）
  // - errorCount: 未成功数 = requests - okCount（含 error/incomplete/canceled）。
  //   不能只用后端 error 字段：canceled（客户端断开）等状态会被顶掉，
  //   导致 tooltip 出现 total 37 / 成功 1 / 失败 0、红线却贴底的缺口。
  //   与概览卡片"成功率 = ok/total"的口径保持一致，红线垫平剩余部分。
  const data = useMemo<ChartRow[]>(() => series.map((point) => {
    const okCount = Math.max(0, point.ok ?? (point.requests - point.error))
    const errorCount = Math.min(Math.max(0, point.requests - okCount), Math.max(0, point.requests))
    return {
      ...point,
      okCount,
      errorCount,
    }
  }), [series])
  const first = data[0]
  const last = data[data.length - 1]
  const span = first && last ? Date.parse(last.at) - Date.parse(first.at) : 0
  const daily = span > 48 * 60 * 60 * 1000
  const peak = Math.max(0, ...data.map((point) => point.requests))
  const yMax = niceMax(peak)
  const hasTraffic = data.some((point) => point.requests > 0)
  const signature = data.map((point) => `${point.at}:${point.requests}:${point.error}`).join('|')

  useLayoutEffect(() => {
    const root = rootRef.current
    if (!root || !hasTraffic) return

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
        return
      }

      observer = new MutationObserver(() => {
        if (played) return
        if (play()) {
          played = true
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

  if (!series.length || !hasTraffic) {
    return <TrafficChartEmpty emptyLabel={emptyLabel} />
  }

  return (
    <div ref={rootRef} className="min-w-0">
      <div className="h-72 w-full">
        <ResponsiveContainer width="100%" height="100%">
          <ComposedChart
            data={data}
            margin={{ top: 8, right: 8, bottom: 0, left: 0 }}
          >
            <defs>
              <linearGradient id="traffic-ok" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="var(--success)" stopOpacity={0.28} />
                <stop offset="100%" stopColor="var(--success)" stopOpacity={0.02} />
              </linearGradient>
              <linearGradient id="traffic-error" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="var(--danger)" stopOpacity={0.22} />
                <stop offset="100%" stopColor="var(--danger)" stopOpacity={0.02} />
              </linearGradient>
            </defs>
            <CartesianGrid vertical={false} stroke="var(--separator)" strokeDasharray="3 6" />
            <XAxis
              dataKey="at"
              tickLine={false}
              axisLine={false}
              minTickGap={28}
              tick={{ fill: 'var(--muted)', fontSize: 10, fontFamily: 'var(--font-mono)' }}
              tickFormatter={(value: string) => hourLabel(value, daily, lang)}
            />
            <YAxis
              width={36}
              domain={[0, yMax]}
              tickLine={false}
              axisLine={false}
              allowDecimals={false}
              tick={{ fill: 'var(--muted)', fontSize: 10, fontFamily: 'var(--font-mono)' }}
              tickFormatter={(value: number) => formatCompact(value)}
            />
            <Tooltip
              cursor={{ stroke: 'var(--border)', strokeDasharray: '3 3' }}
              content={<TrafficTooltip daily={daily} lang={lang} okLabel={okLabel} errorLabel={errorLabel} />}
            />
            {/* 面积：先画成功（较大，作为底层），再画失败（叠加在底部，避免被完全遮住） */}
            <Area
              type="monotone"
              dataKey="okCount"
              stroke="none"
              fill="url(#traffic-ok)"
              isAnimationActive={false}
              activeDot={false}
              dot={false}
              className="traffic-ok-area"
            />
            <Area
              type="monotone"
              dataKey="errorCount"
              stroke="none"
              fill="url(#traffic-error)"
              isAnimationActive={false}
              activeDot={false}
              dot={false}
              className="traffic-error-area"
            />
            <Line
              type="monotone"
              dataKey="okCount"
              stroke="var(--success)"
              strokeWidth={1.75}
              dot={false}
              isAnimationActive={false}
              activeDot={{ r: 4, fill: 'var(--surface)', stroke: 'var(--success)', strokeWidth: 1.5 }}
              className="traffic-ok-line"
            />
            <Line
              type="monotone"
              dataKey="errorCount"
              stroke="var(--danger)"
              strokeWidth={1.25}
              strokeOpacity={0.7}
              dot={false}
              isAnimationActive={false}
              activeDot={{ r: 4, fill: 'var(--surface)', stroke: 'var(--danger)', strokeWidth: 1.5 }}
              className="traffic-error-line"
            />
          </ComposedChart>
        </ResponsiveContainer>
      </div>
      <div className="mt-2 flex min-h-5 items-center justify-between gap-3 text-[10px] text-muted">
        <div className="flex items-center gap-3">
          <span className="inline-flex items-center gap-1.5"><span className="size-1.5 rounded-full bg-success" />{okLabel}</span>
          <span className="inline-flex items-center gap-1.5"><span className="size-1.5 rounded-full bg-danger" />{errorLabel}</span>
        </div>
        <span className="mono">{formatCompact(peak)} peak</span>
      </div>
    </div>
  )
}
