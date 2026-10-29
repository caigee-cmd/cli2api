import { lazy, Suspense } from 'react'
import type { LiveSnapshot } from '@/api/live'
import type { LiveSample } from '@/hooks/useLive'

const LiveSparkline = lazy(() => import('@/components/overview/LiveSparkline').then((mod) => ({ default: mod.LiveSparkline })))

type T = (key: string, vars?: Record<string, string | number>) => string

/** Slim one-row live readout: concurrency/capacity, requests in last 60s, 60s trace. */
export function LiveStrip({
  live,
  samples,
  t,
  onConcurrencyClick,
}: {
  live: LiveSnapshot | null
  samples: LiveSample[]
  t: T
  /** Expand Overview InflightLiveCard from「当前并发」without a second fetch. */
  onConcurrencyClick?: () => void
}) {
  const inFlight = live?.in_flight ?? null
  const capacity = live?.capacity ?? 0
  const load = inFlight != null && capacity > 0 ? inFlight / capacity : 0
  const saturated = capacity > 0 && inFlight != null && inFlight >= capacity
  const concurrencyInner = (
    <>
      {t('liveConcurrency')}
      <span className="inline-flex items-center gap-2">
        <span className={`mono tabular text-body font-semibold ${saturated ? 'text-warning' : 'text-foreground'}`}>
          {inFlight ?? '—'}<span className="font-normal text-muted">/{live ? capacity : '—'}</span>
        </span>
        <span className="inline-block h-[3px] w-12 overflow-hidden rounded-[2px] bg-separator">
          <span className={`block h-full ${saturated ? 'bg-warning' : 'bg-foreground/60'}`} style={{ width: `${Math.min(100, load * 100)}%` }} />
        </span>
      </span>
    </>
  )
  return (
    <div
      data-testid="live-strip"
      className="flex min-h-11 flex-wrap items-center gap-x-5 gap-y-2 overflow-hidden rounded-lg bg-surface-secondary px-4 py-2.5 text-caption text-muted sm:h-11 sm:gap-x-6 sm:gap-y-1 sm:py-0"
    >
      <span className="inline-flex items-center">
        <span className="status-dot" data-state={inFlight == null ? '' : saturated ? 'warn' : inFlight > 0 ? 'ok' : ''} />
      </span>
      {onConcurrencyClick ? (
        <button
          type="button"
          data-testid="live-strip-concurrency"
          onClick={onConcurrencyClick}
          className="inline-flex flex-col gap-1 rounded-md text-left outline-none transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-focus sm:flex-row sm:items-center sm:gap-2"
          aria-label={t('inflightExpandFromConcurrency')}
        >
          {concurrencyInner}
        </button>
      ) : (
        <span className="inline-flex flex-col gap-1 sm:flex-row sm:items-center sm:gap-2">
          {concurrencyInner}
        </span>
      )}
      <span className="inline-flex flex-col gap-1 sm:flex-row sm:items-center sm:gap-2">
        {t('liveRpm')}
        <span className="mono tabular text-body font-semibold text-foreground">{live?.rpm ?? '—'}</span>
      </span>
      <span className="inline-flex flex-col gap-1 sm:flex-row sm:items-center sm:gap-2">
        {t('liveTrace')}
        <Suspense fallback={<span className="inline-block h-6 w-[120px]" />}>
          <LiveSparkline samples={samples} capacity={capacity} label={t('liveTrace')} />
        </Suspense>
      </span>
    </div>
  )
}
