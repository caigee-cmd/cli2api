import { useLayoutEffect, useRef } from 'react'
import gsap from 'gsap'
import type { RequestStatsPoint } from '@/api/logs'

function hourLabel(value: string, daily: boolean, lang: 'en' | 'zh') {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return ''
  return new Intl.DateTimeFormat(lang === 'zh' ? 'zh-CN' : 'en-US', daily
    ? { month: 'short', day: 'numeric' }
    : { hour: '2-digit', minute: '2-digit', hour12: false }).format(date)
}

export function TrafficChart({
  series,
  lang,
  emptyLabel,
}: {
  series: RequestStatsPoint[]
  lang: 'en' | 'zh'
  emptyLabel: string
}) {
  const rootRef = useRef<HTMLDivElement>(null)
  const peak = Math.max(1, ...series.map((point) => point.requests))
  const first = series[0]
  const last = series[series.length - 1]
  const span = first && last ? Date.parse(last.at) - Date.parse(first.at) : 0
  const daily = span > 48 * 60 * 60 * 1000

  useLayoutEffect(() => {
    const root = rootRef.current
    if (!root) return
    const context = gsap.context(() => {
      const media = gsap.matchMedia()
      media.add('(prefers-reduced-motion: reduce)', () => {
        gsap.set('[data-bar]', { clearProps: 'transform' })
      })
      media.add('(prefers-reduced-motion: no-preference)', () => {
        const bars = gsap.utils.toArray<HTMLElement>('[data-bar]')
        if (!bars.length) return
        gsap.fromTo(bars, { scaleY: 0 }, {
          scaleY: 1,
          duration: 0.42,
          ease: 'power2.out',
          stagger: { amount: Math.min(0.45, bars.length * 0.012) },
          transformOrigin: '50% 100%',
        })
      })
    }, root)
    return () => context.revert()
  }, [series])

  if (!series.length || series.every((point) => point.requests === 0)) {
    return (
      <div className="grid min-h-36 place-items-center rounded-lg border border-dashed border-[var(--app-line-strong)] px-4 text-sm text-[var(--app-faint)]">
        {emptyLabel}
      </div>
    )
  }

  return (
    <div ref={rootRef} className="min-w-0">
      <div className="flex h-36 items-end gap-px">
        {series.map((point) => {
          const height = Math.max(point.requests ? 8 : 2, Math.round((point.requests / peak) * 100))
          const failed = point.requests > 0 && point.error / point.requests >= 0.5
          return (
            <div
              key={point.at}
              className="group relative flex min-w-0 flex-1 items-end"
              title={`${hourLabel(point.at, daily, lang)} · ${point.requests}`}
            >
              <span
                data-bar
                className={`block w-full origin-bottom rounded-[2px] ${failed ? 'bg-[var(--app-danger)]' : 'bg-[var(--app-ok)]'} ${point.requests ? 'opacity-90' : 'bg-[var(--app-line-strong)] opacity-50'}`}
                style={{ height: `${height}%` }}
              />
            </div>
          )
        })}
      </div>
      <div className="mt-2 flex items-center justify-between text-[10px] text-[var(--app-faint)]">
        <span className="mono">{first ? hourLabel(first.at, daily, lang) : ''}</span>
        <span className="mono">{last ? hourLabel(last.at, daily, lang) : ''}</span>
      </div>
    </div>
  )
}
