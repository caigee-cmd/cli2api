import { useLayoutEffect, useMemo, useRef, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { Card } from '@heroui/react'
import { ArrowUpRight, Check, X } from '@phosphor-icons/react'
import {
  getMockInflightSnapshot,
  summarizeMockInflight,
  type MockAccountSlot,
  type MockInflightJob,
  type MockInflightSnapshot,
} from '@/lib/mockInflight'
import { duration, ease, gsap, motionDuration, prefersReducedMotion } from '@/lib/motion'

type T = (key: string, vars?: Record<string, string | number>) => string

function formatRemain(ms: number) {
  const sec = Math.max(0, Math.round(ms / 1000))
  const m = Math.floor(sec / 60)
  const s = sec % 60
  if (m) return `${m}m ${String(s).padStart(2, '0')}s`
  return `${s}s`
}

function formatElapsed(ms: number) {
  const sec = Math.max(0, ms / 1000)
  if (sec >= 10) return `${sec.toFixed(0)}s`
  return `${sec.toFixed(1)}s`
}

function DepthChart({
  series,
  bands,
  backpressure,
  label,
}: {
  series: number[]
  bands: Array<{ from: number; until: number }>
  backpressure: number
  label: string
}) {
  const peak = Math.max(backpressure, ...series, 1)
  const w = 320
  const h = 36
  const step = series.length > 1 ? w / (series.length - 1) : w
  const points = series.map((v, i) => {
    const x = i * step
    const y = h - (v / peak) * (h - 4) - 2
    return [x, y] as const
  })
  const line = points.map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(' ')
  const area = `0,${h} ${line} ${w},${h}`
  const guideY = h - (backpressure / peak) * (h - 4) - 2

  return (
    <svg
      viewBox={`0 0 ${w} ${h}`}
      className="h-9 w-full text-foreground/55"
      role="img"
      aria-label={label}
      preserveAspectRatio="none"
    >
      {bands.map((band, i) => {
        const x = band.from * w
        const width = Math.max(2, (band.until - band.from) * w)
        return (
          <rect
            key={i}
            x={x}
            y={0}
            width={width}
            height={h}
            className="fill-warning/20"
          />
        )
      })}
      <line
        x1={0}
        x2={w}
        y1={guideY}
        y2={guideY}
        stroke="currentColor"
        strokeOpacity={0.25}
        strokeDasharray="3 3"
        strokeWidth={1}
        vectorEffect="non-scaling-stroke"
      />
      <polygon points={area} fill="currentColor" opacity="0.1" />
      <polyline
        points={line}
        fill="none"
        stroke="currentColor"
        strokeWidth="1.4"
        strokeLinejoin="round"
        strokeLinecap="round"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  )
}

function SlotBar({
  slot,
  job,
  idleLabel,
  pausedLabel,
}: {
  slot: MockAccountSlot
  job?: MockInflightJob
  idleLabel: string
  pausedLabel: string
}) {
  const frozen = Boolean(slot.rateLimited)
  return (
    <div
      data-slot
      className={`relative min-w-0 overflow-hidden rounded-md bg-surface-secondary px-2 py-1.5 ${
        frozen ? 'ring-1 ring-inset ring-warning/40' : ''
      }`}
      title={slot.name}
    >
      <div className="flex items-center justify-between gap-1">
        <span className="mono truncate text-[10px] leading-none text-muted">{slot.name}</span>
        {frozen ? (
          <span className="mono shrink-0 text-[10px] leading-none text-warning">{pausedLabel}</span>
        ) : job ? (
          <span className="mono shrink-0 text-[10px] leading-none text-muted">{formatElapsed(job.elapsedMs)}</span>
        ) : (
          <span className="mono shrink-0 text-[10px] leading-none text-muted/60">—</span>
        )}
      </div>
      <div className="mt-1 truncate text-[11px] leading-tight text-foreground/80">
        {job?.name ?? (frozen ? pausedLabel : idleLabel)}
      </div>
      <div className="mt-1.5 h-1 overflow-hidden rounded-[2px] bg-separator">
        <div
          data-slot-fill
          className={`relative h-full origin-left rounded-[2px] ${
            frozen ? 'bg-warning/70' : slot.busy ? 'bg-foreground/55' : 'bg-transparent'
          }`}
          style={{ width: `${Math.max(slot.busy || frozen ? 8 : 0, slot.progress * 100)}%` }}
        >
          {slot.busy && !frozen ? (
            <span
              aria-hidden
              className="pointer-events-none absolute inset-y-0 w-1/3 bg-gradient-to-r from-transparent via-foreground/35 to-transparent motion-safe:animate-[inflight-shimmer_1.5s_linear_infinite]"
            />
          ) : null}
        </div>
      </div>
    </div>
  )
}

/**
 * Overview summary: Agent Queue feel over cli2api pool/inflight/cooldown concepts.
 * Read-only mock (Phase 1). Click opens `/live` later — no Pause/Cancel/stepper.
 */
export function InflightLiveCard({
  t,
  snapshot,
}: {
  t: T
  snapshot?: MockInflightSnapshot
}) {
  const snap = snapshot ?? getMockInflightSnapshot()
  const summary = useMemo(() => summarizeMockInflight(snap), [snap])
  const rootRef = useRef<HTMLDivElement>(null)
  const jobById = useMemo(() => new Map(snap.jobs.map((j) => [j.id, j])), [snap.jobs])
  const waiting = snap.jobs.filter((j) => j.status === 'queued')
  const rateActive = snap.rateLimits[0]

  useLayoutEffect(() => {
    const root = rootRef.current
    if (!root || prefersReducedMotion()) return
    const ctx = gsap.context(() => {
      const pills = root.querySelectorAll<HTMLElement>('[data-wait-pill]')
      const slots = root.querySelectorAll<HTMLElement>('[data-slot]')
      const fills = root.querySelectorAll<HTMLElement>('[data-slot-fill]')
      if (pills.length) {
        gsap.fromTo(
          pills,
          { opacity: 0, y: 8, scale: 0.96 },
          {
            opacity: 1,
            y: 0,
            scale: 1,
            duration: motionDuration(0.52),
            ease: ease.emphasized,
            stagger: 0.04,
          },
        )
      }
      if (slots.length) {
        gsap.fromTo(
          slots,
          { opacity: 0, x: -10 },
          {
            opacity: 1,
            x: 0,
            duration: motionDuration(0.56),
            ease: ease.emphasized,
            stagger: 0.05,
          },
        )
      }
      if (fills.length) {
        gsap.fromTo(
          fills,
          { scaleX: 0 },
          {
            scaleX: 1,
            duration: motionDuration(duration.base),
            ease: ease.out,
            stagger: 0.04,
            transformOrigin: '0% 50%',
          },
        )
      }
    }, root)
    return () => ctx.revert()
  }, [snap])

  const depthLabel = t('inflightDepthHint', { n: summary.depthNow })

  return (
    <Card
      data-testid="inflight-live-card"
      data-gsap-reveal
      className="group overflow-hidden p-0 transition-[box-shadow] duration-200"
    >
      <style>{`@keyframes inflight-shimmer{0%{transform:translateX(-120%)}100%{transform:translateX(220%)}}`}</style>
      <Link
        to="/live"
        className="block outline-none focus-visible:ring-2 focus-visible:ring-focus focus-visible:ring-offset-2 focus-visible:ring-offset-background"
        aria-label={t('inflightCardAria')}
      >
        <div ref={rootRef}>
          <div className="flex items-start justify-between gap-3 border-b border-separator px-4 py-3">
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <h3 className="font-semibold">{t('inflightTitle')}</h3>
                <span className="mono inline-flex items-center gap-1 rounded-[3px] bg-surface-secondary px-1.5 py-0.5 text-caption text-muted">
                  <span className="status-dot" data-state={rateActive ? 'warn' : 'ok'} />
                  {rateActive
                    ? t('inflightRatePill', { remain: formatRemain(rateActive.remainingMs) })
                    : t('inflightMockBadge')}
                </span>
              </div>
              <p className="mt-0.5 text-caption text-muted">{t('inflightHint')}</p>
            </div>
            <span className="inline-flex shrink-0 items-center gap-1 text-caption text-muted transition-colors group-hover:text-foreground">
              {t('inflightOpen')}
              <ArrowUpRight size={14} aria-hidden />
            </span>
          </div>

          <div className="grid grid-cols-2 gap-3 border-b border-separator px-4 py-3 sm:grid-cols-4">
            <Metric
              label={t('inflightWaiting')}
              value={String(summary.waiting)}
              warn={summary.waiting >= snap.backpressure}
            />
            <Metric
              label={t('inflightRunning')}
              value={`${summary.running}/${summary.workerSlots}`}
            />
            <Metric label={t('inflightThroughput')} value={`${snap.throughputPerMin}/min`} />
            <Metric label={t('inflightWaitP95')} value={formatElapsed(snap.waitP95Ms)} />
          </div>

          {/* Waiting → account slots → Done/Failed */}
          <div className="grid gap-3 border-b border-separator px-4 py-3 lg:grid-cols-[minmax(0,.9fr)_minmax(0,1.4fr)_auto]">
            <div className="min-w-0">
              <div className="mb-1.5 text-caption uppercase tracking-wide text-muted">{t('inflightQueue')}</div>
              <div className="space-y-1">
                {waiting.slice(0, 3).map((job) => (
                  <div
                    key={job.id}
                    data-wait-pill
                    className="flex items-center justify-between gap-2 rounded-md bg-surface-secondary px-2 py-1.5"
                  >
                    <span className="truncate text-[11px] text-foreground/85">
                      {job.name}
                      {job.sticky ? (
                        <span className="mono ml-1 text-[10px] text-muted">{t('inflightSticky')}</span>
                      ) : null}
                    </span>
                    <span className="mono shrink-0 text-[10px] text-muted">{formatElapsed(job.elapsedMs)}</span>
                  </div>
                ))}
                {waiting.length > 3 ? (
                  <div className="flex flex-wrap gap-1 pt-0.5">
                    {waiting.slice(3).map((job) => (
                      <span
                        key={job.id}
                        data-wait-pill
                        className="size-2 rounded-[2px] bg-foreground/25"
                        title={job.name}
                      />
                    ))}
                    <span className="mono text-[10px] text-muted">+{waiting.length - 3}</span>
                  </div>
                ) : null}
                {!waiting.length ? (
                  <div className="text-caption text-muted">{t('inflightQueueEmpty')}</div>
                ) : null}
              </div>
            </div>

            <div className="min-w-0">
              <div className="mb-1.5 text-caption uppercase tracking-wide text-muted">{t('inflightSlots')}</div>
              <div className="grid grid-cols-2 gap-1.5 sm:grid-cols-3">
                {snap.workers.map((slot) => (
                  <SlotBar
                    key={slot.id}
                    slot={slot}
                    job={slot.jobId ? jobById.get(slot.jobId) : undefined}
                    idleLabel={t('inflightSlotIdle')}
                    pausedLabel={t('inflightSlotPaused')}
                  />
                ))}
              </div>
            </div>

            <div className="flex gap-2 lg:flex-col">
              <Outcome
                label={t('inflightDone')}
                value={snap.totals.done}
                tone="ok"
                icon={<Check size={12} weight="bold" aria-hidden />}
              />
              <Outcome
                label={t('inflightFailed')}
                value={snap.totals.failed}
                tone="danger"
                icon={<X size={12} weight="bold" aria-hidden />}
              />
            </div>
          </div>

          <div className="px-4 py-3">
            <div className="mb-1 flex items-baseline justify-between gap-2">
              <span className="text-caption uppercase tracking-wide text-muted">{t('inflightDepth')}</span>
              <span className="mono text-caption tabular text-muted">{depthLabel}</span>
            </div>
            <div className="rounded-[4px] bg-surface-secondary/50 px-1 pt-1">
              <DepthChart
                series={snap.depthSeries}
                bands={snap.rateLimits.map((r) => ({ from: r.from, until: r.until }))}
                backpressure={snap.backpressure}
                label={depthLabel}
              />
            </div>
            {rateActive ? (
              <div className="mono mt-1.5 text-[10px] text-warning">
                {t('inflightRateBand', {
                  label: rateActive.label,
                  remain: formatRemain(rateActive.remainingMs),
                })}
              </div>
            ) : null}
          </div>
        </div>
      </Link>
    </Card>
  )
}

function Metric({ label, value, warn }: { label: string; value: string; warn?: boolean }) {
  return (
    <div className="min-w-0">
      <div className="text-caption uppercase tracking-wide text-muted">{label}</div>
      <div className={`mono mt-1 text-section font-semibold tabular ${warn ? 'text-warning' : ''}`}>{value}</div>
    </div>
  )
}

function Outcome({
  label,
  value,
  tone,
  icon,
}: {
  label: string
  value: number
  tone: 'ok' | 'danger'
  icon: ReactNode
}) {
  return (
    <div
      className={`flex min-w-[4.5rem] flex-1 flex-col justify-center rounded-md px-2.5 py-2 ${
        tone === 'ok' ? 'bg-success/10' : 'bg-danger/10'
      }`}
    >
      <div className="flex items-center gap-1 text-caption text-muted">
        <span className={tone === 'ok' ? 'text-success' : 'text-danger'}>{icon}</span>
        {label}
      </div>
      <div className={`mono mt-0.5 text-section font-semibold tabular ${tone === 'ok' ? 'text-success' : 'text-danger'}`}>
        {value}
      </div>
    </div>
  )
}
