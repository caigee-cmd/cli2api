import { useLayoutEffect, useMemo, useRef, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { Card } from '@heroui/react'
import { ArrowUpRight, Check, X } from '@phosphor-icons/react'
import CustomEase from 'gsap/CustomEase'
import {
  Area,
  AreaChart,
  ReferenceArea,
  ReferenceLine,
  ResponsiveContainer,
  XAxis,
  YAxis,
} from 'recharts'
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

/**
 * Kind identity from public AffectUI demo hierarchy (llm/tool/agent washes),
 * remapped to project chart / foreground tokens — never a second brand palette.
 */
function kindDot(kind: MockJobKind): string {
  switch (kind) {
    case 'chat':
      return 'bg-[var(--chart-1)]'
    case 'embed':
      return 'bg-[var(--chart-6)]'
    case 'tools':
      return 'bg-foreground/45'
    default:
      return 'bg-foreground/25'
  }
}

function kindPill(kind: MockJobKind): string {
  switch (kind) {
    case 'chat':
      return 'bg-[color-mix(in_oklab,var(--chart-1)_14%,var(--surface))] text-foreground/90'
    case 'embed':
      return 'bg-[color-mix(in_oklab,var(--chart-6)_14%,var(--surface))] text-foreground/90'
    case 'tools':
      return 'bg-foreground/[0.06] text-foreground/85'
    default:
      return 'bg-surface-tertiary text-foreground/80'
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
  const data = useMemo(
    () => series.map((v, i) => ({ i, v, bp: backpressure })),
    [series, backpressure],
  )
  const bandRows = useMemo(
    () =>
      bands.map((band, idx) => {
        const n = Math.max(1, series.length - 1)
        return {
          key: idx,
          x1: band.from * n,
          x2: Math.max(band.from * n + 0.35, band.until * n),
        }
      }),
    [bands, series.length],
  )

  return (
    <div className="h-11 w-full" role="img" aria-label={label} data-depth-chart>
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={data} margin={{ top: 4, right: 2, bottom: 2, left: 2 }}>
          <YAxis hide domain={[0, peak]} />
          <XAxis hide dataKey="i" type="number" domain={[0, Math.max(0, series.length - 1)]} />
          {bandRows.map((b) => (
            <ReferenceArea
              key={b.key}
              x1={b.x1}
              x2={b.x2}
              y1={0}
              y2={peak}
              fill="var(--il-warn, var(--warning))"
              fillOpacity={0.14}
              ifOverflow="extendDomain"
            />
          ))}
          <ReferenceLine
            y={backpressure}
            stroke="var(--il-faint, var(--foreground))"
            strokeOpacity={0.35}
            strokeDasharray="3 3"
            ifOverflow="extendDomain"
          />
          <defs>
            <linearGradient id="il-depth-fill" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="var(--il-depth, var(--chart-1))" stopOpacity={0.28} />
              <stop offset="100%" stopColor="var(--il-depth, var(--chart-1))" stopOpacity={0.04} />
            </linearGradient>
          </defs>
          <Area
            type="monotone"
            dataKey="v"
            stroke="var(--il-depth, var(--chart-1))"
            strokeOpacity={0.95}
            strokeWidth={1.85}
            fill="url(#il-depth-fill)"
            fillOpacity={1}
            isAnimationActive={!prefersReducedMotion()}
            animationDuration={Math.round(motionDuration(0.56) * 1000)}
            animationEasing="ease-out"
            dot={false}
            activeDot={false}
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
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
          ? 'bg-[var(--il-warn-bg)] shadow-[inset_0_0_0_1px_var(--il-warn-edge)]'
          : slot.busy
            ? 'bg-[var(--il-chip)] shadow-[inset_0_0_0_1px_var(--il-line)]'
            : 'bg-[var(--il-well)] shadow-[inset_0_0_0_1px_var(--il-line-soft)]'
      }`}
      title={`${slot.name} · ${label}`}
    >
      {frozen ? (
        <span
          aria-hidden
          className="pointer-events-none absolute inset-0 opacity-[0.22]"
          style={{
            backgroundImage:
              'repeating-linear-gradient(-45deg, transparent, transparent 5px, var(--il-warn-hatch) 5px, var(--il-warn-hatch) 6px)',
          }}
        />
      ) : null}
      <span className="relative z-[1] mono w-[3.4rem] shrink-0 truncate text-[10px] leading-none text-muted" title={slot.name}>
        {slot.name}
      </span>
      {/* Job pill — AffectUI worker row: kind wash + growing bar + elapsed */}
      <span
        className={`relative z-[1] inline-flex max-w-[38%] shrink-0 items-center gap-1 truncate rounded-md px-1.5 py-0.5 text-[11px] leading-none ${
          frozen
            ? 'bg-[color-mix(in_oklab,var(--warning)_18%,var(--surface))] font-medium text-[var(--il-warn-fg)] shadow-[inset_0_0_0_1px_var(--il-warn-edge)]'
            : job
              ? `${kindPill(job.kind)} shadow-[inset_0_0_0_1px_var(--il-line-soft)]`
              : 'bg-background/45 text-muted'
        }`}
      >
        {job ? (
          <span className={`size-1.5 shrink-0 rounded-[2px] ${kindDot(job.kind)}`} aria-hidden />
        ) : null}
        <span className="truncate">{label}</span>
      </span>
      <div className="relative z-[1] h-2 min-w-0 flex-1 overflow-hidden rounded-full bg-[var(--il-track)]">
        <div
          data-slot-fill
          className={`relative h-full ${
            frozen ? 'bg-[var(--il-warn)]' : slot.busy ? 'bg-[var(--il-run)]' : 'bg-transparent'
          }`}
          style={{
            width: '100%',
            clipPath: `inset(0 ${100 - pct}% 0 0 round 999px)`,
          }}
        >
          {slot.busy && !frozen ? (
            <>
              <span
                aria-hidden
                className="pointer-events-none absolute inset-y-0 w-2/5 bg-gradient-to-r from-transparent via-[color-mix(in_oklab,var(--surface)_55%,transparent)] to-transparent motion-safe:animate-[inflight-shimmer_1.5s_linear_infinite]"
              />
              <span
                aria-hidden
                className="pointer-events-none absolute inset-y-0 right-0 w-[3px] bg-[var(--il-run-edge)] motion-safe:animate-[inflight-glow_1.1s_ease-in-out_infinite_alternate]"
                style={{ boxShadow: '0 0 8px 1px var(--il-run-glow)' }}
              />
            </>
          ) : null}
        </div>
      </div>
      <span className="relative z-[1] mono w-[2.6rem] shrink-0 text-right text-[10px] leading-none tabular text-muted">
        {frozen ? (
          <span className="font-medium text-[var(--il-warn-fg)]">{pausedLabel}</span>
        ) : job ? (
          formatElapsed(job.elapsedMs)
        ) : (
          <span className="text-muted/45">—</span>
        )}
      </span>
    </div>
  )
}

/**
 * Overview summary: Agent Queue feel over cli2api pool/inflight/cooldown concepts.
 * Read-only mock (Phase 1). Motion timings + color hierarchy from public
 * AffectUI Agent Queue demo CSS vars (mapped to project tokens via color-mix;
 * see docs/assets/overview-inflight-mock/affectui-public-motion-notes.md) —
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
            force3D: true,
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
            force3D: true,
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
          { opacity: 0, y: 6 },
          {
            opacity: 1,
            y: 0,
            duration: motionDuration(duration.base),
            ease: queueEase,
            delay: motionDuration(0.35),
            force3D: true,
          },
        )
      }
      const depthPaths = root.querySelectorAll<SVGPathElement>('[data-depth-chart] .recharts-area-curve, [data-depth-chart] .recharts-area-area')
      if (depthPaths.length) {
        gsap.fromTo(
          depthPaths,
          { opacity: 0 },
          {
            opacity: 1,
            duration: motionDuration(0.56),
            ease: queueEase,
            delay: motionDuration(0.4),
            stagger: 0.04,
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
        [data-testid="inflight-live-card"]{
          /* Public AffectUI hierarchy → project tokens (color-mix only).
             Card/well/chip stay cooler surface mixes — chart-3 (orange) is
             categorical series only; amber (--warning) is reserved for
             rate-limit / cooldown accents. Depth curve uses chart-1. */
          --il-well: color-mix(in oklab, var(--foreground) 4%, var(--surface));
          --il-chip: color-mix(in oklab, var(--foreground) 8%, var(--surface));
          --il-line: color-mix(in oklab, var(--foreground) 13%, transparent);
          --il-line-soft: color-mix(in oklab, var(--foreground) 8%, transparent);
          --il-track: color-mix(in oklab, var(--foreground) 14%, transparent);
          --il-run: color-mix(in oklab, var(--foreground) 84%, transparent);
          --il-run-edge: var(--foreground);
          --il-run-glow: color-mix(in oklab, var(--foreground) 52%, transparent);
          --il-warn: var(--warning);
          --il-warn-fg: var(--warning);
          --il-warn-bg: color-mix(in oklab, var(--warning) 16%, var(--surface));
          --il-warn-edge: color-mix(in oklab, var(--warning) 55%, transparent);
          --il-warn-hatch: color-mix(in oklab, var(--warning) 28%, transparent);
          --il-ok-bg: color-mix(in oklab, var(--success) 14%, var(--surface));
          --il-ok-edge: color-mix(in oklab, var(--success) 50%, transparent);
          --il-ok-fg: var(--success);
          --il-err-bg: color-mix(in oklab, var(--danger) 14%, var(--surface));
          --il-err-edge: color-mix(in oklab, var(--danger) 50%, transparent);
          --il-err-fg: var(--danger);
          --il-depth: var(--chart-1);
          --il-depth-fill: color-mix(in oklab, var(--chart-1) 20%, transparent);
          --il-faint: color-mix(in oklab, var(--foreground) 42%, transparent);
        }
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
                      ? 'bg-[var(--il-warn-bg)] font-medium text-[var(--il-warn-fg)] shadow-[inset_0_0_0_1px_var(--il-warn-edge)]'
                      : 'bg-[var(--il-chip)] text-muted shadow-[inset_0_0_0_1px_var(--il-line-soft)]'
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
                <span className={`mono text-[10px] tabular ${waitAmber ? 'text-[var(--il-warn-fg)]' : 'text-muted'}`}>
                  {summary.waiting}
                </span>
              </div>
              <div className="space-y-1">
                {waiting.slice(0, 3).map((job) => (
                  <div
                    key={job.id}
                    data-wait-pill
                    className="flex items-center justify-between gap-2 rounded-lg bg-[var(--il-chip)] px-2 py-1.5 shadow-[inset_0_0_0_1px_var(--il-line-soft)]"
                  >
                    <span className="flex min-w-0 items-center gap-1.5 truncate text-[11px] text-foreground/90">
                      <span className={`size-1.5 shrink-0 rounded-[2px] ${kindDot(job.kind)}`} aria-hidden />
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
                        className={`size-2 rounded-[2px] ${kindDot(job.kind)}`}
                        title={job.name}
                      />
                    ))}
                    {/* depth tokens — backlog feel */}
                    {Array.from({ length: Math.min(10, Math.max(0, summary.waiting * 2)) }).map((_, i) => (
                      <span
                        key={`tok-${i}`}
                        data-wait-token
                        className="size-2 rounded-[2px] bg-foreground/18"
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
                        className={`size-2 rounded-[2px] ${kindDot(job.kind)}`}
                        title={job.name}
                      />
                    ))}
                    {Array.from({ length: 6 }).map((_, i) => (
                      <span
                        key={`pad-${i}`}
                        data-wait-token
                        className="size-2 rounded-[2px] bg-foreground/16"
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
            <div className="rounded-lg bg-[var(--il-well)] px-1.5 pt-1.5 pb-1 shadow-[inset_0_0_0_1px_var(--il-line-soft)]">
              <DepthChart
                series={snap.depthSeries}
                bands={snap.rateLimits.map((r) => ({ from: r.from, until: r.until }))}
                backpressure={snap.backpressure}
                label={depthLabel}
              />
            </div>
            {rateActive ? (
              <div className="mono mt-1.5 text-[10px] font-medium text-[var(--il-warn-fg)]">
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
      <div className={`mono mt-1 text-section font-semibold tabular ${warn ? 'text-[var(--il-warn-fg)]' : 'text-foreground'}`}>{value}</div>
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
  const ok = tone === 'ok'
  return (
    <div
      data-outcome={tone}
      className={`flex min-w-[4.5rem] flex-1 flex-col justify-center rounded-lg px-2.5 py-2 ${
        ok
          ? 'bg-[var(--il-ok-bg)] shadow-[inset_0_0_0_1px_var(--il-ok-edge)]'
          : 'bg-[var(--il-err-bg)] shadow-[inset_0_0_0_1px_var(--il-err-edge)]'
      }`}
    >
      <div className="flex items-center gap-1.5 text-caption text-muted">
        <span
          className={`inline-flex size-[1.125rem] items-center justify-center rounded-[5px] ${
            ok
              ? 'bg-[color-mix(in_oklab,var(--success)_18%,var(--surface))] text-[var(--il-ok-fg)] shadow-[inset_0_0_0_1px_var(--il-ok-edge)]'
              : 'bg-[color-mix(in_oklab,var(--danger)_18%,var(--surface))] text-[var(--il-err-fg)] shadow-[inset_0_0_0_1px_var(--il-err-edge)]'
          }`}
        >
          {icon}
        </span>
        {label}
      </div>
      <div
        className={`mono mt-0.5 text-section font-semibold tabular ${
          ok ? 'text-[var(--il-ok-fg)]' : 'text-[var(--il-err-fg)]'
        }`}
      >
        {value}
      </div>
    </div>
  )
}
