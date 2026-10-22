import { lazy, Suspense } from 'react'
import { Card } from '@heroui/react'
import { Broadcast, Gauge, Pulse } from '@phosphor-icons/react'
import type { LiveSnapshot } from '@/api/live'
import type { LiveSample } from '@/hooks/useLive'

const LiveSparkline = lazy(() => import('@/components/overview/LiveSparkline').then((mod) => ({ default: mod.LiveSparkline })))

type T = (key: string, vars?: Record<string, string | number>) => string

/** Live strip: current concurrency vs capacity, requests in the last 60s, 60s trace. */
export function LiveStrip({ live, samples, t }: { live: LiveSnapshot | null; samples: LiveSample[]; t: T }) {
  const inFlight = live?.in_flight ?? null
  const capacity = live?.capacity ?? 0
  const load = inFlight != null && capacity > 0 ? inFlight / capacity : 0
  const saturated = capacity > 0 && inFlight != null && inFlight >= capacity
  const peak = samples.reduce((max, s) => Math.max(max, s.value), 0)
  return (
    <Card className="gap-0 p-0" data-testid="live-strip">
      <div className="grid gap-px sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,2fr)]">
        <div className="px-4 py-3">
          <div className="flex items-center gap-2 text-caption text-muted">
            <Gauge size={13} />
            {t('liveConcurrency')}
            <span className="status-dot ml-auto" data-state={inFlight == null ? '' : saturated ? 'warn' : inFlight > 0 ? 'ok' : ''} />
          </div>
          <div className="mt-1.5 flex items-baseline gap-1.5">
            <span className="tabular text-metric font-medium leading-none">{inFlight ?? '—'}</span>
            <span className="mono text-body text-muted">/ {live ? capacity : '—'}</span>
          </div>
          <div className="mt-2 h-1 overflow-hidden rounded-[2px] bg-separator">
            <div className={`h-full ${saturated ? 'bg-warning' : 'bg-foreground/60'}`} style={{ width: `${Math.min(100, load * 100)}%` }} />
          </div>
        </div>
        <div className="px-4 py-3">
          <div className="flex items-center gap-2 text-caption text-muted">
            <Pulse size={13} />
            {t('liveRpm')}
          </div>
          <div className="mt-1.5 flex items-baseline gap-1.5">
            <span className="tabular text-metric font-medium leading-none">{live?.rpm ?? '—'}</span>
            <span className="mono text-body text-muted">{t('liveRpmUnit')}</span>
          </div>
          <div className="mono mt-2 text-caption text-muted">{t('liveRpmHint')}</div>
        </div>
        <div className="min-w-0 px-4 py-3">
          <div className="flex items-center gap-2 text-caption text-muted">
            <Broadcast size={13} />
            {t('liveTrace')}
            <span className="mono ml-auto">{t('livePeak', { n: peak })}</span>
          </div>
          <div className="mt-1.5">
            <Suspense fallback={<div className="h-10" />}>
              <LiveSparkline samples={samples} capacity={capacity} label={t('liveTrace')} />
            </Suspense>
          </div>
        </div>
      </div>
    </Card>
  )
}
