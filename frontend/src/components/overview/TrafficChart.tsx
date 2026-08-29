import { useLayoutEffect, useMemo, useRef, useState } from 'react'
import gsap from 'gsap'
import type { RequestStatsPoint } from '@/api/logs'
import { formatCompact } from '@/lib/format'

const WIDTH = 720
const HEIGHT = 280
const PAD = { top: 16, right: 12, bottom: 28, left: 40 }

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

function linePath(xs: number[], ys: number[]) {
  if (!xs.length) return ''
  return xs.map((x, i) => `${i ? 'L' : 'M'}${x.toFixed(2)} ${ys[i].toFixed(2)}`).join(' ')
}

function areaPath(xs: number[], ys: number[], baseline: number) {
  if (!xs.length) return ''
  return `${linePath(xs, ys)} L${xs[xs.length - 1].toFixed(2)} ${baseline.toFixed(2)} L${xs[0].toFixed(2)} ${baseline.toFixed(2)} Z`
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
  const [active, setActive] = useState<number | null>(null)
  const first = series[0]
  const last = series[series.length - 1]
  const span = first && last ? Date.parse(last.at) - Date.parse(first.at) : 0
  const daily = span > 48 * 60 * 60 * 1000
  const innerW = WIDTH - PAD.left - PAD.right
  const innerH = HEIGHT - PAD.top - PAD.bottom
  const peak = Math.max(0, ...series.map((point) => point.requests))
  const yMax = niceMax(peak)
  const ticks = [0, 0.5, 1].map((part) => Math.round(yMax * part))
  const hasTraffic = series.some((point) => point.requests > 0)

  const geometry = useMemo(() => {
    const count = Math.max(1, series.length)
    const step = count === 1 ? 0 : innerW / (count - 1)
    const xs = series.map((_, index) => PAD.left + index * step)
    const yFor = (value: number) => PAD.top + innerH - (value / Math.max(1, yMax)) * innerH
    const totalYs = series.map((point) => yFor(point.requests))
    const okYs = series.map((point) => yFor(Math.max(0, point.requests - point.error)))
    const baseline = yFor(0)
    return {
      xs,
      totalYs,
      okYs,
      baseline,
      totalLine: linePath(xs, totalYs),
      okArea: areaPath(xs, okYs, baseline),
      errorArea: areaPath(xs, totalYs, baseline),
    }
  }, [innerH, innerW, series, yMax])

  useLayoutEffect(() => {
    const root = rootRef.current
    if (!root || !hasTraffic) return
    const context = gsap.context(() => {
      const media = gsap.matchMedia()
      media.add('(prefers-reduced-motion: reduce)', () => {
        gsap.set('[data-traffic-draw]', { clearProps: 'strokeDashoffset,strokeDasharray' })
        gsap.set('[data-traffic-fill]', { clearProps: 'opacity,transform' })
        gsap.set('[data-traffic-dot]', { clearProps: 'transform,opacity' })
      })
      media.add('(prefers-reduced-motion: no-preference)', () => {
        const line = root.querySelector<SVGPathElement>('[data-traffic-draw]')
        const fills = gsap.utils.toArray<SVGPathElement>('[data-traffic-fill]')
        const dots = gsap.utils.toArray<SVGCircleElement>('[data-traffic-dot]')
        if (line) {
          const length = line.getTotalLength()
          gsap.fromTo(line, { strokeDasharray: length, strokeDashoffset: length }, {
            strokeDashoffset: 0,
            duration: 0.7,
            ease: 'power2.out',
          })
        }
        gsap.fromTo(fills, { autoAlpha: 0 }, {
          autoAlpha: 1,
          duration: 0.45,
          ease: 'power2.out',
          stagger: 0.06,
        })
        if (dots.length) {
          gsap.fromTo(dots, { scale: 0, transformOrigin: '50% 50%' }, {
            scale: 1,
            duration: 0.28,
            ease: 'back.out(1.6)',
            stagger: { amount: Math.min(0.28, dots.length * 0.02) },
            delay: 0.28,
          })
        }
      })
    }, root)
    return () => context.revert()
  }, [geometry.totalLine, hasTraffic])

  if (!series.length || !hasTraffic) {
    return (
      <div className="grid min-h-72 place-items-center rounded-lg border border-dashed border-[var(--app-line-strong)] px-4 text-sm text-[var(--app-faint)]">
        {emptyLabel}
      </div>
    )
  }

  const hover = active == null ? null : series[active]
  const hoverX = active == null ? 0 : geometry.xs[active]
  const hoverY = active == null ? 0 : geometry.totalYs[active]

  return (
    <div ref={rootRef} className="min-w-0">
      <svg
        viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
        className="h-72 w-full overflow-visible"
        role="img"
        aria-label={emptyLabel}
        onMouseLeave={() => setActive(null)}
      >
        {ticks.map((tick) => {
          const y = PAD.top + innerH - (tick / Math.max(1, yMax)) * innerH
          return (
            <g key={tick}>
              <line x1={PAD.left} x2={WIDTH - PAD.right} y1={y} y2={y} stroke="var(--app-line)" strokeWidth="1" />
              <text x={PAD.left - 8} y={y + 3} textAnchor="end" className="fill-[var(--app-faint)]" fontSize="10" fontFamily="ui-monospace, SFMono-Regular, Menlo, monospace">
                {formatCompact(tick)}
              </text>
            </g>
          )
        })}
        <path data-traffic-fill d={geometry.errorArea} fill="color-mix(in srgb, var(--app-danger) 22%, transparent)" />
        <path data-traffic-fill d={geometry.okArea} fill="color-mix(in srgb, var(--app-ok) 18%, transparent)" />
        <path data-traffic-draw d={geometry.totalLine} fill="none" stroke="var(--app-ok)" strokeWidth="1.75" strokeLinejoin="round" strokeLinecap="round" />
        {series.map((point, index) => (
          <circle
            key={point.at}
            data-traffic-dot
            cx={geometry.xs[index]}
            cy={geometry.totalYs[index]}
            r={series.length > 36 ? 1.4 : 2.1}
            fill={point.error && point.requests && point.error / point.requests >= 0.5 ? 'var(--app-danger)' : 'var(--app-ok)'}
          />
        ))}
        {series.map((point, index) => {
          const prev = index === 0 ? geometry.xs[0] : (geometry.xs[index - 1] + geometry.xs[index]) / 2
          const next = index === series.length - 1 ? geometry.xs[index] : (geometry.xs[index] + geometry.xs[index + 1]) / 2
          return (
            <rect
              key={`${point.at}-hit`}
              x={prev}
              y={PAD.top}
              width={Math.max(4, next - prev)}
              height={innerH}
              fill="transparent"
              onMouseEnter={() => setActive(index)}
            />
          )
        })}
        {hover ? (
          <g>
            <line x1={hoverX} x2={hoverX} y1={PAD.top} y2={PAD.top + innerH} stroke="var(--app-line-strong)" strokeWidth="1" strokeDasharray="3 3" />
            <circle cx={hoverX} cy={hoverY} r="4" fill="var(--app-surface)" stroke="var(--app-ok)" strokeWidth="1.5" />
          </g>
        ) : null}
        <text x={PAD.left} y={HEIGHT - 6} className="fill-[var(--app-faint)]" fontSize="10" fontFamily="ui-monospace, SFMono-Regular, Menlo, monospace">
          {first ? hourLabel(first.at, daily, lang) : ''}
        </text>
        <text x={WIDTH - PAD.right} y={HEIGHT - 6} textAnchor="end" className="fill-[var(--app-faint)]" fontSize="10" fontFamily="ui-monospace, SFMono-Regular, Menlo, monospace">
          {last ? hourLabel(last.at, daily, lang) : ''}
        </text>
      </svg>
      <div className="mt-2 flex min-h-5 items-center justify-between gap-3 text-[10px] text-[var(--app-faint)]">
        <div className="flex items-center gap-3">
          <span className="inline-flex items-center gap-1.5"><span className="size-1.5 rounded-full bg-[var(--app-ok)]" />{okLabel}</span>
          <span className="inline-flex items-center gap-1.5"><span className="size-1.5 rounded-full bg-[var(--app-danger)]" />{errorLabel}</span>
        </div>
        <span className="mono">
          {hover
            ? `${hourLabel(hover.at, daily, lang)} · ${hover.requests} · ${okLabel} ${hover.ok} · ${errorLabel} ${hover.error}`
            : `${formatCompact(peak)} peak`}
        </span>
      </div>
    </div>
  )
}
