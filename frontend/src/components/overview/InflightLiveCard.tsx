import { useLayoutEffect, useMemo, useRef, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { Card } from '@heroui/react'
import { ArrowUpRight, Check, X } from '@phosphor-icons/react'
import CustomEase from 'gsap/CustomEase'
import {
  getMockInflightSnapshot,
  summarizeMockInflight,
  type MockAccountSlot,
  type MockInflightJob,
  type MockInflightSnapshot,
  type MockJobKind,
} from '@/lib/mockInflight'
import { duration, ease, gsap, motionDuration, prefersReducedMotion } from '@/lib/motion'

gsap.registerPlugin(CustomEase)
const queueEase =
  CustomEase.get('inflightQueue') ||
  CustomEase.create('inflightQueue', '0.22, 1, 0.36, 1')

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

/** Cold kind chips — identity via opacity / chart hue, not a second brand palette. */
function kindTone(kind: MockJobKind): string {
  switch (kind) {
    case 'chat':
      return 'bg-[var(--chart-1)]/55'
    case 'embed':
      return 'bg-[var(--chart-6)]/55'
    case 'tools':
      return 'bg-foreground/35'
    default:
      return 'bg-foreground/20'
  }
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
  const h = 44
  const step = series.length > 1 ? w / (series.length - 1) : w
  const points = series.map((v, i) => {
    const x = i * step
    const y = h - (v / peak) * (h - 6) - 3
    return [x, y] as const
  })
  const line = points.map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(' ')
  const area = `0,${h} ${line} ${w},${h}`
  const guideY = h - (backpressure / peak) * (h - 6) - 3

  return (
    <svg
      viewBox={`0 0 ${w} ${h}`}
      className="h-11 w-full"
      role="img"
      aria-label={label}
      preserveAspectRatio="none"
    >
      {bands.map((band, i) => {
        const x = band.from * w
        const width = Math.max(3, (band.until - band.from) * w)
        return (
          <rect
            key={i}
            x={x}
            y={0}
            width={width}
            height={h}
            fill="var(--warning)"
            opacity={0.18}
          />
        )
      })}
      <line
        x1={0}
        x2={w}
        y1={guideY}
        y2={guideY}
        stroke="var(--foreground)"
        strokeOpacity={0.22}
        strokeDasharray="3 3"
        strokeWidth={1}
        vectorEffect="non-scaling-stroke"
      />
      <polygon points={area} fill="var(--foreground)" opacity="0.08" />
      <polyline
        points={line}
        fill="none"
        stroke="var(--foreground)"
        strokeOpacity={0.7}
        strokeWidth="1.5"
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
  const pct = Math.max(slot.busy || frozen ? 8 : 0, Math.round(slot.progress * 100))
  const label = job?.name ?? (frozen ? pausedLabel : idleLabel)
  return (
    <div
      data-slot
      className={`relative flex min-w-0 items-center gap-2 overflow-hidden rounded-lg px-2 py-1.5 ${
        frozen
          ? 'bg-warning/10 ring-1 ring-inset ring-warning/35'
          : 'bg-surface-secondary'
      }`}
      title={`${slot.name} · ${label}`}
    >
      {frozen ? (
        <span
          aria-hidden
          className="pointer-events-none absolute inset-0 opacity-[0.35]"
          style={{
            backgroundImage:
              'repeating-linear-gradient(-45deg, transparent, transparent 4px, color-mix(in oklab, var(--warning) 28%, transparent) 4px, color-mix(in oklab, var(--warning) 28%, transparent) 5px)',
          }}
        />
      ) : null}
      {/* Job pill — AffectUI worker row: pill + growing bar + elapsed */}
      <span
        className={`relative z-[1] inline-flex max-w-[42%] shrink-0 items-center gap-1 truncate rounded-md px-1.5 py-0.5 text-[11px] leading-none ${
          frozen
            ? 'bg-warning/20 text-warning'
            : job
              ? 'bg-background/70 text-foreground/85'
              : 'bg-background/40 text-muted'
        }`}
      >
        {job ? (
          <span className={`size-1.5 shrink-0 rounded-[2px] ${kindTone(job.kind)}`} aria-hidden />
        ) : null}
        <span className="truncate">{label}</span>
      </span>
      <div className="relative z-[1] h-2 min-w-0 flex-1 overflow-hidden rounded-full bg-separator/80">
        <div
          data-slot-fill
          className={`relative h-full ${frozen ? 'bg-warning/80' : slot.busy ? 'bg-foreground/55' : 'bg-transparent'}`}
          style={{
            width: '100%',
            clipPath: `inset(0 ${100 - pct}% 0 0 round 999px)`,
            willChange: 'clip-path',
          }}
        >
          {slot.busy && !frozen ? (
            <>
              <span
                aria-hidden
                className="pointer-events-none absolute inset-y-0 w-2/5 bg-gradient-to-r from-transparent via-foreground/45 to-transparent motion-safe:animate-[inflight-shimmer_1.5s_linear_infinite]"
              />
              <span
                aria-hidden
                className="pointer-events-none absolute inset-y-0 right-0 w-[3px] bg-foreground/85 motion-safe:animate-[inflight-glow_1.1s_ease-in-out_infinite_alternate]"
                style={{ boxShadow: '0 0 7px 1px color-mix(in oklab, var(--foreground) 50%, transparent)' }}
              />
            </>
          ) : null}
        </div>
      </div>
      <span className="relative z-[1] mono w-[2.6rem] shrink-0 text-right text-[10px] leading-none tabular text-muted">
        {frozen ? (
          <span className="text-warning">{pausedLabel}</span>
        ) : job ? (
          formatElapsed(job.elapsedMs)
        ) : (
          <span className="text-muted/50">—</span>
        )}
      </span>
    </div>
  )
}

/**
 * Overview summary: Agent Queue feel over cli2api pool/inflight/cooldown concepts.
 * Read-only mock (Phase 1). Motion timings from public AffectUI demo CSS
 * (see docs/assets/overview-inflight-mock/affectui-public-motion-notes.md) —
 * not Pro source. Link targets `/live` later — no Pause/Cancel/stepper.
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
  const waitAmber = summary.waiting >= snap.backpressure

  useLayoutEffect(() => {
    const root = rootRef.current
    if (!root || prefersReducedMotion()) return
    const ctx = gsap.context(() => {
      const pills = root.querySelectorAll<HTMLElement>('[data-wait-pill]')
      const tokens = root.querySelectorAll<HTMLElement>('[data-wait-token]')
      const slots = root.querySelectorAll<HTMLElement>('[data-slot]')
      const fills = root.querySelectorAll<HTMLElement>('[data-slot-fill]')
      const outcomes = root.querySelectorAll<HTMLElement>('[data-outcome]')
      const depth = root.querySelectorAll<HTMLElement>('[data-depth]')

      if (pills.length) {
        gsap.fromTo(
          pills,
          { opacity: 0, y: -10, scale: 0.6 },
          {
            opacity: 1,
            y: 0,
            scale: 1,
            duration: motionDuration(0.52),
            ease: queueEase,
            stagger: 0.045,
          },
        )
      }
      if (tokens.length) {
        gsap.fromTo(
          tokens,
          { opacity: 0, scale: 0.6 },
          {
            opacity: 1,
            scale: 1,
            duration: motionDuration(0.42),
            ease: ease.soft,
            stagger: 0.02,
            delay: motionDuration(0.18),
          },
        )
      }
      if (slots.length) {
        gsap.fromTo(
          slots,
          { opacity: 0, x: -8 },
          {
            opacity: 1,
            x: 0,
            duration: motionDuration(0.42),
            ease: queueEase,
            stagger: 0.05,
            delay: motionDuration(0.06),
          },
        )
      }
      if (fills.length) {
        fills.forEach((el, i) => {
          const end = el.style.clipPath || 'inset(0 0% 0 0)'
          gsap.fromTo(
            el,
            { clipPath: 'inset(0 100% 0 0 round 999px)' },
            {
              clipPath: end,
              duration: motionDuration(0.56),
              ease: queueEase,
              delay: motionDuration(0.18 + i * 0.04),
            },
          )
        })
      }
      const doneEl = root.querySelector<HTMLElement>('[data-outcome="ok"]')
      const failEl = root.querySelector<HTMLElement>('[data-outcome="danger"]')
      if (doneEl) {
        gsap.fromTo(
          doneEl,
          { opacity: 0, y: 8, scale: 0.92, filter: 'brightness(1.6)' },
          {
            opacity: 1,
            y: 0,
            scale: 1,
            filter: 'brightness(1)',
            duration: motionDuration(0.48),
            ease: ease.soft,
            delay: motionDuration(0.42),
          },
        )
      }
      if (failEl) {
        gsap.fromTo(
          failEl,
          { opacity: 0, x: 0 },
          {
            opacity: 1,
            duration: motionDuration(0.28),
            delay: motionDuration(0.5),
            onComplete: () => {
              if (prefersReducedMotion()) return
              gsap.fromTo(
                failEl,
                { x: -3 },
                {
                  x: 3,
                  duration: motionDuration(0.07),
                  yoyo: true,
                  repeat: 5, // ~420ms total — public ui-agq-shake
                  ease: 'power1.inOut',
                  onComplete: () => {
                    gsap.set(failEl, { x: 0 })
                  },
                },
              )
            },
          },
        )
      }
      if (outcomes.length === 0) {
        /* no-op — outcomes animated above */
      }
      if (depth.length) {
        gsap.fromTo(
          depth,
          { opacity: 0, y: 4 },
          {
            opacity: 1,
            y: 0,
            duration: motionDuration(duration.base),
            ease: ease.out,
            delay: motionDuration(0.35),
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
      <style>{`
        @keyframes inflight-shimmer{0%{transform:translateX(-130%)}100%{transform:translateX(230%)}}
        @keyframes inflight-glow{from{opacity:.45}to{opacity:1}}
        @media (prefers-reduced-motion: reduce){
          .motion-safe\\:animate-\\[inflight-shimmer_1\\.5s_linear_infinite\\],
          .motion-safe\\:animate-\\[inflight-glow_1\\.1s_ease-in-out_infinite\\]{animation:none!important}
        }
      `}</style>
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
                <span
                  className={`mono inline-flex items-center gap-1 rounded-[3px] px-1.5 py-0.5 text-caption ${
                    rateActive
                      ? 'bg-warning/15 text-warning'
                      : 'bg-surface-secondary text-muted'
                  }`}
                >
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
              warn={waitAmber || Boolean(rateActive)}
            />
            <Metric
              label={t('inflightRunning')}
              value={`${summary.running}/${summary.workerSlots}`}
            />
            <Metric label={t('inflightThroughput')} value={`${snap.throughputPerMin}/min`} />
            <Metric label={t('inflightWaitP95')} value={formatElapsed(snap.waitP95Ms)} />
          </div>

          {/* Waiting → account-slot bars → Done/Failed  (AffectUI Agent Queue flow) */}
          <div className="grid gap-3 border-b border-separator px-4 py-3 lg:grid-cols-[minmax(0,0.95fr)_minmax(0,1.55fr)_auto]">
            <div className="min-w-0">
              <div className="mb-1.5 flex items-baseline justify-between gap-2">
                <span className="text-caption uppercase tracking-wide text-muted">{t('inflightQueue')}</span>
                <span className={`mono text-[10px] tabular ${waitAmber ? 'text-warning' : 'text-muted'}`}>
                  {summary.waiting}
                </span>
              </div>
              <div className="space-y-1">
                {waiting.slice(0, 3).map((job) => (
                  <div
                    key={job.id}
                    data-wait-pill
                    className="flex items-center justify-between gap-2 rounded-lg bg-surface-secondary px-2 py-1.5"
                  >
                    <span className="flex min-w-0 items-center gap-1.5 truncate text-[11px] text-foreground/85">
                      <span className={`size-1.5 shrink-0 rounded-[2px] ${kindTone(job.kind)}`} aria-hidden />
                      <span className="truncate">
                        {job.name}
                        {job.sticky ? (
                          <span className="mono ml-1 text-[10px] text-muted">{t('inflightSticky')}</span>
                        ) : null}
                      </span>
                    </span>
                    <span className="mono shrink-0 text-[10px] text-muted">{formatElapsed(job.elapsedMs)}</span>
                  </div>
                ))}
                {waiting.length > 3 ? (
                  <div className="flex flex-wrap items-center gap-1 pt-1">
                    {waiting.slice(3).map((job) => (
                      <span
                        key={job.id}
                        data-wait-token
                        data-wait-pill
                        className={`size-2 rounded-[2px] ${kindTone(job.kind)}`}
                        title={job.name}
                      />
                    ))}
                    {/* depth tokens — backlog feel */}
                    {Array.from({ length: Math.min(10, Math.max(0, summary.waiting * 2)) }).map((_, i) => (
                      <span
                        key={`tok-${i}`}
                        data-wait-token
                        className="size-2 rounded-[2px] bg-foreground/15"
                        aria-hidden
                      />
                    ))}
                    <span className="mono text-[10px] text-muted">+{waiting.length - 3}</span>
                  </div>
                ) : waiting.length ? (
                  <div className="flex flex-wrap gap-1 pt-1">
                    {waiting.map((job) => (
                      <span
                        key={`tok-${job.id}`}
                        data-wait-token
                        className={`size-2 rounded-[2px] ${kindTone(job.kind)}`}
                        title={job.name}
                      />
                    ))}
                    {Array.from({ length: 6 }).map((_, i) => (
                      <span
                        key={`pad-${i}`}
                        data-wait-token
                        className="size-2 rounded-[2px] bg-foreground/12"
                        aria-hidden
                      />
                    ))}
                  </div>
                ) : (
                  <div className="text-caption text-muted">{t('inflightQueueEmpty')}</div>
                )}
              </div>
            </div>

            <div className="min-w-0">
              <div className="mb-1.5 flex items-baseline justify-between gap-2">
                <span className="text-caption uppercase tracking-wide text-muted">{t('inflightSlots')}</span>
                <span className="mono text-[10px] tabular text-muted">
                  {summary.busyWorkers}/{summary.workerSlots}
                </span>
              </div>
              {/* Vertical worker rail — growing bars, AffectUI layout */}
              <div className="flex flex-col gap-1.5">
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

            <div className="flex gap-2 lg:w-[5.25rem] lg:flex-col">
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

          <div data-depth className="px-4 py-3">
            <div className="mb-1 flex items-baseline justify-between gap-2">
              <span className="text-caption uppercase tracking-wide text-muted">{t('inflightDepth')}</span>
              <span className="mono text-caption tabular text-muted">{depthLabel}</span>
            </div>
            <div className="rounded-lg bg-surface-secondary/60 px-1.5 pt-1.5 pb-1">
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
      data-outcome={tone}
      className={`flex min-w-[4.5rem] flex-1 flex-col justify-center rounded-lg px-2.5 py-2 ${
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
