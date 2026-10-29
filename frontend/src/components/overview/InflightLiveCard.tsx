import { useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { Card } from '@heroui/react'
import { ArrowUpRight, CaretRight, Check, X } from '@phosphor-icons/react'
import type { LiveSnapshot, LiveSticky } from '@/api/live'
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
  applyLiveProjection,
  cloneMockInflight,
  getMockInflightSnapshot,
  summarizeMockInflight,
  tickMockInflight,
  type MockAccountSlot,
  type MockInflightJob,
  type MockInflightSnapshot,
  type MockJobKind,
} from '@/lib/mockInflight'
import { CountUp } from '@/components/overview/CountUp'
import { duration, ease, gsap, motionDuration, prefersReducedMotion } from '@/lib/motion'

gsap.registerPlugin(CustomEase)
const queueEase =
  CustomEase.get('inflightQueue') ||
  CustomEase.create('inflightQueue', '0.22, 1, 0.36, 1')

/** AffectUI overflow: first N queue pills; rest as kind-colored dots. */
const QUEUE_PILL_MAX = 3
/** AffectUI: show a short worker rail (busy/rate-limited first), not every account. */
const WORKER_VISIBLE_MAX = 4

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
  // Continuous glide: keep a stable x domain (0..n-1) while values scroll;
  // ease Y peak so scale changes don’t jump. Tick cadence ~260ms → linear ~560ms morph.
  const rawPeak = Math.max(backpressure, ...series, 1)
  const peakRef = useRef(rawPeak)
  const [peak, setPeak] = useState(rawPeak)
  useEffect(() => {
    if (prefersReducedMotion()) {
      peakRef.current = rawPeak
      setPeak(rawPeak)
      return
    }
    const from = peakRef.current
    const to = rawPeak
    if (Math.abs(to - from) < 0.01) return
    let raf = 0
    const t0 = performance.now()
    const dur = motionDuration(0.56) * 1000
    const tick = (now: number) => {
      const t = Math.min(1, (now - t0) / dur)
      // queue ease approx: 1 - (1-t)^3
      const e = 1 - Math.pow(1 - t, 3)
      const v = from + (to - from) * e
      peakRef.current = v
      setPeak(v)
      if (t < 1) raf = requestAnimationFrame(tick)
    }
    raf = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(raf)
  }, [rawPeak])

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
  const yMax = Math.max(peak, backpressure, 1)

  return (
    <div className="h-11 w-full overflow-hidden" role="img" aria-label={label} data-depth-chart>
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={data} margin={{ top: 4, right: 2, bottom: 2, left: 2 }}>
          <YAxis hide domain={[0, yMax]} allowDataOverflow />
          <XAxis hide dataKey="i" type="number" domain={[0, Math.max(0, series.length - 1)]} />
          {bandRows.map((b) => (
            <ReferenceArea
              key={b.key}
              x1={b.x1}
              x2={b.x2}
              y1={0}
              y2={yMax}
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
            animationEasing="linear"
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
      data-slot-id={slot.id}
      data-slot-job={slot.jobId ?? ''}
      data-il-glass={frozen ? undefined : ''}
      className={`relative flex min-w-0 items-center gap-2 overflow-hidden rounded-lg px-2 py-1.5 ${
        frozen
          ? 'bg-[var(--il-warn-bg)] shadow-[inset_0_0_0_1px_var(--il-warn-edge)]'
          : ''
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
        data-slot-job-pill=""
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
          className={`relative h-full transition-[clip-path] duration-[260ms] ease-linear ${
            frozen ? 'bg-[var(--il-warn)]' : slot.busy ? 'bg-[var(--il-run)]' : 'bg-transparent'
          }`}
          style={{
            width: '100%',
            clipPath: `inset(0 ${100 - pct}% 0 0 round 999px)`,
          }}
        >
          {slot.busy && !frozen ? (
            <>
              {/* Stronger running 流光 — AffectUI sheen 1500ms + glow 1100ms */}
              <span
                aria-hidden
                className="pointer-events-none absolute inset-y-0 w-1/2 bg-gradient-to-r from-transparent via-[color-mix(in_oklab,var(--surface)_72%,transparent)] to-transparent motion-safe:animate-[inflight-shimmer_1.5s_linear_infinite]"
              />
              <span
                aria-hidden
                className="pointer-events-none absolute inset-y-0 w-1/3 bg-gradient-to-r from-transparent via-[color-mix(in_oklab,var(--foreground)_28%,transparent)] to-transparent motion-safe:animate-[inflight-shimmer_2.2s_linear_infinite]"
                style={{ animationDelay: '-0.7s' }}
              />
              <span
                aria-hidden
                className="pointer-events-none absolute inset-y-0 right-0 w-[3px] bg-[var(--il-run-edge)] motion-safe:animate-[inflight-glow_1.1s_ease-in-out_infinite_alternate]"
                style={{ boxShadow: '0 0 10px 2px var(--il-run-glow)' }}
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
 * Default collapsed — no /api/live consume and no mock loop until expanded
 * (toggle or LiveStrip「当前并发」). When open, project live workers /
 * rate_limits / sticky; mock tick only as empty-demo fallback.
 * Motion timings from public AffectUI Agent Queue demo (mapped to project
 * tokens). Link targets `/live` later — no Pause/Cancel/stepper.
 */
export function InflightLiveCard({
  t,
  snapshot,
  live = null,
  expanded: expandedProp,
  onExpandedChange,
  defaultExpanded = false,
}: {
  t: T
  snapshot?: MockInflightSnapshot
  /** Parent /api/live snapshot — only passed while expanded so the card does not drive fetches. */
  live?: LiveSnapshot | null
  expanded?: boolean
  onExpandedChange?: (open: boolean) => void
  defaultExpanded?: boolean
}) {
  const controlled = snapshot !== undefined
  const [uncontrolledOpen, setUncontrolledOpen] = useState(defaultExpanded)
  const expanded = expandedProp ?? uncontrolledOpen
  const setExpanded = (open: boolean) => {
    onExpandedChange?.(open)
    if (expandedProp === undefined) setUncontrolledOpen(open)
  }

  const [liveSnap, setLiveSnap] = useState<MockInflightSnapshot>(() =>
    cloneMockInflight(snapshot ?? getMockInflightSnapshot()),
  )
  const [useMockFallback, setUseMockFallback] = useState(false)
  const [sticky, setSticky] = useState<LiveSticky | null>(null)
  const snap = controlled ? snapshot! : liveSnap
  const summary = useMemo(() => summarizeMockInflight(snap), [snap])
  const rootRef = useRef<HTMLDivElement>(null)
  const enteredRef = useRef(false)
  const prevRef = useRef<MockInflightSnapshot>(snap)
  /** Last painted rects — handoff flies read these after the source unmounts. */
  const rectCache = useRef<{
    wait: Record<string, DOMRect>
    slot: Record<string, DOMRect>
    outcome: { ok?: DOMRect; danger?: DOMRect }
  }>({ wait: {}, slot: {}, outcome: {} })
  const jobById = useMemo(() => new Map(snap.jobs.map((j) => [j.id, j])), [snap.jobs])
  const waiting = snap.jobs.filter((j) => j.status === 'queued')
  const queuePills = waiting.slice(0, QUEUE_PILL_MAX)
  const queueOverflow = waiting.slice(QUEUE_PILL_MAX)
  // AffectUI: short worker rail — busy / rate-limited first, cap at N (not every account).
  const visibleWorkers = useMemo(() => {
    const preferred = snap.workers.filter((w) => w.busy || w.rateLimited)
    const rest = snap.workers.filter((w) => !w.busy && !w.rateLimited)
    const out: typeof snap.workers = []
    const seen = new Set<string>()
    for (const w of [...preferred, ...rest]) {
      if (seen.has(w.id)) continue
      seen.add(w.id)
      out.push(w)
      if (out.length >= WORKER_VISIBLE_MAX) break
    }
    return out
  }, [snap.workers])
  const rateActive = snap.rateLimits[0]
  const waitAmber = summary.waiting >= snap.backpressure

  // External snapshot (tests) wins. Otherwise: when expanded, project /api/live
  // (even idle — no mock totals/throughput/p95/depth bleed). Mock tick only if
  // live is null. useLayoutEffect so the first expanded paint is already clean.
  useLayoutEffect(() => {
    if (controlled) {
      setLiveSnap(cloneMockInflight(snapshot!))
      setUseMockFallback(false)
      setSticky(null)
      return
    }
    if (!expanded) {
      setUseMockFallback(false)
      return
    }
    if (live !== null) {
      setUseMockFallback(false)
      setSticky(live.sticky)
      setLiveSnap((prev) => applyLiveProjection(prev, live))
      return
    }
    // live === null → Stage-1 mock loop only (offline / no parent poll).
    setSticky(null)
    setUseMockFallback(true)
  }, [controlled, snapshot, expanded, live])

  // Mock raf loop — gated on useMockFallback (live === null only).
  useEffect(() => {
    if (controlled || !expanded || !useMockFallback || live !== null) return
    if (prefersReducedMotion()) return
    let raf = 0
    let last = performance.now()
    let alive = true
    const step = (now: number) => {
      if (!alive) return
      const dt = now - last
      if (!document.hidden && dt >= 260) {
        last = now
        setLiveSnap((prev) => tickMockInflight(prev, dt))
      }
      raf = requestAnimationFrame(step)
    }
    raf = requestAnimationFrame(step)
    return () => {
      alive = false
      cancelAnimationFrame(raf)
    }
  }, [controlled, expanded, useMockFallback, live])

  useEffect(() => {
    if (!expanded) enteredRef.current = false
  }, [expanded])

  /** Entrance once — public ui-agq-drop / slot / grow timings. */
  useLayoutEffect(() => {
    const root = rootRef.current
    if (!root || prefersReducedMotion() || enteredRef.current) return
    enteredRef.current = true
    const ctx = gsap.context(() => {
      const pills = root.querySelectorAll<HTMLElement>('[data-wait-pill]')
      const tokens = root.querySelectorAll<HTMLElement>('[data-wait-token]')
      const slots = root.querySelectorAll<HTMLElement>('[data-slot]')
      const fills = root.querySelectorAll<HTMLElement>('[data-slot-fill]')
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
                  repeat: 5,
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
    }, root)
    return () => ctx.revert()
    // Re-run when body mounts (default collapsed).
  }, [expanded])

  /** Live handoffs: queue→worker fly, worker→DONE/FAILED fly (AffectUI prose). */
  useLayoutEffect(() => {
    const root = rootRef.current
    const prev = prevRef.current
    prevRef.current = snap
    const refreshCache = () => {
      if (!root) return
      const wait: Record<string, DOMRect> = {}
      root.querySelectorAll<HTMLElement>('[data-wait-id]').forEach((el) => {
        const id = el.getAttribute('data-wait-id')
        if (id) wait[id] = el.getBoundingClientRect()
      })
      const slotR: Record<string, DOMRect> = {}
      root.querySelectorAll<HTMLElement>('[data-slot-id]').forEach((el) => {
        const id = el.getAttribute('data-slot-id')
        if (id) slotR[id] = el.getBoundingClientRect()
      })
      rectCache.current = {
        wait,
        slot: slotR,
        outcome: {
          ok: root.querySelector<HTMLElement>('[data-outcome="ok"]')?.getBoundingClientRect(),
          danger: root.querySelector<HTMLElement>('[data-outcome="danger"]')?.getBoundingClientRect(),
        },
      }
    }
    if (!root || prefersReducedMotion() || !enteredRef.current) {
      refreshCache()
      return
    }
    if (prev === snap) {
      refreshCache()
      return
    }

    const prevJobs = new Map(prev.jobs.map((j) => [j.id, j]))
    const nextJobs = new Map(snap.jobs.map((j) => [j.id, j]))
    const prevWorkerOf = new Map(
      prev.workers.filter((w) => w.jobId).map((w) => [w.jobId!, w.id]),
    )
    const nextWorkerOf = new Map(
      snap.workers.filter((w) => w.jobId).map((w) => [w.jobId!, w.id]),
    )

    const flyGhost = (
      from: DOMRect | HTMLElement | null | undefined,
      to: DOMRect | HTMLElement | null | undefined,
      label: string,
      kind: MockJobKind,
      tone: 'run' | 'ok' | 'danger' | 'retry',
      beatEl?: HTMLElement | null,
      onLand?: () => void,
    ) => {
      const a = from instanceof HTMLElement ? from.getBoundingClientRect() : from
      const b = to instanceof HTMLElement ? to.getBoundingClientRect() : to
      if (!a || !b || a.width < 2 || b.width < 2) return

      // Opaque solid-glass pill clone (AffectUI: full job pill flies into slot,
      // not a faint label). Stay readable mid-flight; trail is separate afterimages.
      const kindWash =
        kind === 'chat'
          ? 'color-mix(in oklab, var(--chart-1) 14%, var(--surface))'
          : kind === 'embed'
            ? 'color-mix(in oklab, var(--chart-6) 14%, var(--surface))'
            : kind === 'tools'
              ? 'color-mix(in oklab, var(--foreground) 6%, var(--surface))'
              : 'var(--surface)'
      const bg =
        tone === 'ok'
          ? 'color-mix(in oklab, var(--success) 18%, var(--surface))'
          : tone === 'danger'
            ? 'color-mix(in oklab, var(--danger) 18%, var(--surface))'
            : tone === 'retry'
              ? 'color-mix(in oklab, var(--warning) 14%, var(--surface))'
              : kindWash
      const fg =
        tone === 'ok'
          ? 'var(--success)'
          : tone === 'danger'
            ? 'var(--danger)'
            : tone === 'retry'
              ? 'var(--warning)'
              : 'var(--foreground)'
      const dotColor =
        kind === 'chat'
          ? 'var(--chart-1)'
          : kind === 'embed'
            ? 'var(--chart-6)'
            : 'color-mix(in oklab, var(--foreground) 45%, transparent)'
      const glassShadow =
        'inset 0 1px 0 var(--surface-rim), inset 0 0 0 1px var(--surface-edge), var(--surface-contact), 0 8px 24px color-mix(in oklab, var(--foreground) 14%, transparent)'

      const w = Math.max(a.width, 88)
      const h = Math.max(a.height, 26)
      const endLeft = b.left + Math.max(0, (b.width - w * 0.92) / 2)
      const endTop = b.top + Math.max(0, (b.height - h) / 2)
      // Slightly longer than layout 560ms so the horizontal path reads clearly.
      const flyDur = motionDuration(0.72)
      const holdDur = motionDuration(0.16)
      const fadeDur = motionDuration(0.14)

      const makePill = (opts: { opacity: number; asTrail?: boolean }) => {
        const el = document.createElement('div')
        el.setAttribute('aria-hidden', 'true')
        Object.assign(el.style, {
          position: 'fixed',
          left: `${a.left}px`,
          top: `${a.top}px`,
          width: `${w}px`,
          height: `${h}px`,
          zIndex: opts.asTrail ? '78' : '80',
          pointerEvents: 'none',
          display: 'flex',
          alignItems: 'center',
          gap: '6px',
          padding: '0 8px',
          borderRadius: '8px',
          fontSize: '11px',
          fontWeight: '500',
          lineHeight: '1',
          color: fg,
          background: bg,
          boxShadow: glassShadow,
          whiteSpace: 'nowrap',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          opacity: String(opts.opacity),
          willChange: 'transform, left, top, opacity',
          // Stretch residual slightly so the trail reads as motion, not a second pill.
          transform: opts.asTrail ? 'scaleX(1.08)' : 'none',
          filter: opts.asTrail
            ? 'blur(0.35px)'
            : 'none',
        } as CSSStyleDeclaration)
        const dot = document.createElement('span')
        Object.assign(dot.style, {
          width: '6px',
          height: '6px',
          borderRadius: '2px',
          flexShrink: '0',
          background: dotColor,
        })
        const text = document.createElement('span')
        text.textContent = label
        Object.assign(text.style, {
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
          minWidth: '0',
          flex: '1',
        })
        el.append(dot, text)
        document.body.appendChild(el)
        return el
      }

      const ghost = makePill({ opacity: 1 })
      // 3 residual afterimages — stagger behind the lead pill (AffectUI trail).
      const trails = [0.55, 0.32, 0.16].map((op, i) => {
        const t = makePill({ opacity: op, asTrail: true })
        gsap.to(t, {
          left: endLeft,
          top: endTop,
          width: w * 0.92,
          scaleX: 1.2 + i * 0.08,
          opacity: 0,
          duration: flyDur,
          delay: motionDuration(0.045 + i * 0.04),
          ease: queueEase,
          force3D: true,
          onComplete: () => t.remove(),
        })
        return t
      })
      void trails

      gsap.to(ghost, {
        left: endLeft,
        top: endTop,
        width: w * 0.92,
        scale: tone === 'run' || tone === 'retry' ? 0.96 : 0.88,
        // Keep the lead pill opaque for the whole flight — only fade after a short land hold.
        opacity: 1,
        duration: flyDur,
        ease: queueEase,
        force3D: true,
        onComplete: () => {
          // Land first so the real slot/queue pill can occupy while the
          // opaque ghost still holds, then fade the clone out.
          onLand?.()
          gsap.to(ghost, {
            opacity: 0,
            scale: tone === 'run' || tone === 'retry' ? 0.9 : 0.82,
            duration: fadeDur,
            delay: holdDur,
            ease: 'power1.out',
            onComplete: () => ghost.remove(),
          })
        },
      })

      const beat = beatEl
      if (beat && (tone === 'ok' || tone === 'danger')) {
        gsap.fromTo(
          beat,
          { filter: 'brightness(1.55)', scale: 1.04 },
          {
            filter: 'brightness(1)',
            scale: 1,
            duration: motionDuration(0.42),
            ease: ease.soft,
            delay: motionDuration(0.36),
          },
        )
      }
      if (beat && tone === 'danger') {
        gsap.fromTo(
          beat,
          { x: -3 },
          {
            x: 3,
            duration: motionDuration(0.07),
            yoyo: true,
            repeat: 5,
            ease: 'power1.inOut',
            delay: motionDuration(0.36),
            onComplete: () => gsap.set(beat, { x: 0 }),
          },
        )
      }
    }

    const cache = rectCache.current

    // Queue → worker: was queued, now running on a slot.
    const flewFromQueue = new Set<string>()
    for (const [id, job] of nextJobs) {
      const before = prevJobs.get(id)
      if (!before || before.status !== 'queued' || job.status !== 'running') continue
      const workerId = nextWorkerOf.get(id)
      if (!workerId) continue
      flewFromQueue.add(id)
      const fromRect = cache.wait[id]
      const toEl = root.querySelector<HTMLElement>(`[data-slot-id="${CSS.escape(workerId)}"]`)
      const toRect = toEl?.getBoundingClientRect() ?? cache.slot[workerId]
      // Hide occupy chrome until land (AffectUI receive/occupy on land).
      const landPill = toEl?.querySelector<HTMLElement>('[data-slot-job-pill]') ?? null
      const landFill = toEl?.querySelector<HTMLElement>('[data-slot-fill]') ?? null
      const endClip = landFill?.style.clipPath || 'inset(0 96% 0 0 round 999px)'
      if (landPill) gsap.set(landPill, { opacity: 0, scale: 0.85 })
      if (landFill) gsap.set(landFill, { clipPath: 'inset(0 100% 0 0 round 999px)' })
      flyGhost(fromRect, toRect, job.name, job.kind, 'run', null, () => {
        if (!toEl) return
        gsap.fromTo(
          toEl,
          { filter: 'brightness(1.35)' },
          { filter: 'brightness(1)', duration: motionDuration(0.42), ease: ease.soft },
        )
        if (landPill) {
          gsap.to(landPill, {
            opacity: 1,
            scale: 1,
            duration: motionDuration(0.42),
            ease: queueEase,
          })
        }
        if (landFill) {
          gsap.to(landFill, {
            clipPath: endClip,
            duration: motionDuration(0.56),
            ease: queueEase,
          })
        }
      })
    }

    // Queue vacancy FLIP — remaining pills/dots close the gap after fly-out (560ms).
    if (flewFromQueue.size) {
      const waitEls = root.querySelectorAll<HTMLElement>('[data-wait-pill][data-wait-id]')
      waitEls.forEach((el) => {
        const id = el.dataset.waitId
        if (!id || flewFromQueue.has(id)) return
        const first = cache.wait[id]
        if (!first) return
        const last = el.getBoundingClientRect()
        const dx = first.left - last.left
        const dy = first.top - last.top
        if (Math.abs(dx) < 0.5 && Math.abs(dy) < 0.5) return
        gsap.fromTo(
          el,
          { x: dx, y: dy },
          { x: 0, y: 0, duration: motionDuration(0.56), ease: queueEase, force3D: true },
        )
      })
      // Overflow dots that promote into pill slots also FLIP from token rects.
      const tokens = root.querySelectorAll<HTMLElement>('[data-wait-token][data-wait-id]')
      tokens.forEach((el) => {
        const id = el.dataset.waitId
        if (!id) return
        const first = cache.wait[id]
        if (!first) return
        const last = el.getBoundingClientRect()
        const dx = first.left - last.left
        const dy = first.top - last.top
        if (Math.abs(dx) < 0.5 && Math.abs(dy) < 0.5) return
        gsap.fromTo(
          el,
          { x: dx, y: dy, scale: 1.15 },
          { x: 0, y: 0, scale: 1, duration: motionDuration(0.56), ease: queueEase },
        )
      })
    }

    // Worker → retry queue (same id back to queued) OR DONE/FAILED.
    for (const [id, before] of prevJobs) {
      if (before.status !== 'running') continue
      const still = nextJobs.get(id)
      const workerId = prevWorkerOf.get(id)
      const fromRect = workerId ? cache.slot[workerId] : undefined

      // Retry: running → queued (AffectUI: retry slides back).
      if (still && still.status === 'queued') {
        const toEl =
          root.querySelector<HTMLElement>(`[data-wait-id="${CSS.escape(id)}"]`) ??
          root.querySelector<HTMLElement>('[data-queue-col]')
        const toRect = toEl?.getBoundingClientRect()
        flyGhost(fromRect, toRect, before.name, before.kind, 'retry', null, () => {
          const pill = root.querySelector<HTMLElement>(`[data-wait-id="${CSS.escape(id)}"]`)
          if (!pill) return
          gsap.fromTo(
            pill,
            { opacity: 0.4, scale: 0.85 },
            { opacity: 1, scale: 1, duration: motionDuration(0.42), ease: queueEase },
          )
        })
        continue
      }

      if (still && still.status === 'running') continue
      const okBump = snap.totals.done > prev.totals.done
      const failBump = snap.totals.failed > prev.totals.failed
      const tone: 'ok' | 'danger' = failBump && !okBump ? 'danger' : 'ok'
      const beat = root.querySelector<HTMLElement>(
        tone === 'ok' ? '[data-outcome="ok"]' : '[data-outcome="danger"]',
      )
      const toRect = beat?.getBoundingClientRect() ?? cache.outcome[tone]
      flyGhost(fromRect, toRect, before.name, before.kind, tone, beat)
    }

    // New wait pills drop in (AffectUI: new jobs drop at the back).
    // High-priority later glides past via queue-order FLIP below.
    for (const [id, job] of nextJobs) {
      if (job.status !== 'queued') continue
      if (prevJobs.has(id)) continue
      const el = root.querySelector<HTMLElement>(`[data-wait-id="${CSS.escape(id)}"]`)
      if (!el) continue
      gsap.fromTo(
        el,
        { opacity: 0, y: -10, scale: 0.6 },
        {
          opacity: 1,
          y: 0,
          scale: 1,
          duration: motionDuration(0.52),
          ease: queueEase,
        },
      )
    }

    // Priority glide-past + general queue reorder FLIP (high moved toward front).
    {
      const prevQueued = prev.jobs.filter((j) => j.status === 'queued').map((j) => j.id)
      const nextQueued = snap.jobs.filter((j) => j.status === 'queued').map((j) => j.id)
      const orderChanged =
        prevQueued.length === nextQueued.length &&
        prevQueued.some((id, i) => id !== nextQueued[i])
      if (orderChanged) {
        for (const id of nextQueued) {
          const el = root.querySelector<HTMLElement>(`[data-wait-id="${CSS.escape(id)}"]`)
          if (!el) continue
          const first = cache.wait[id]
          if (!first) continue
          const last = el.getBoundingClientRect()
          const dx = first.left - last.left
          const dy = first.top - last.top
          if (Math.abs(dx) < 0.5 && Math.abs(dy) < 0.5) continue
          gsap.fromTo(
            el,
            { x: dx, y: dy },
            { x: 0, y: 0, duration: motionDuration(0.56), ease: queueEase, force3D: true },
          )
        }
      }
    }

    // Dot → pill promotion when overflow slides up into the visible N.
    {
      const prevQueued = prev.jobs.filter((j) => j.status === 'queued').map((j) => j.id)
      const nextQueued = snap.jobs.filter((j) => j.status === 'queued').map((j) => j.id)
      for (let i = 0; i < Math.min(QUEUE_PILL_MAX, nextQueued.length); i++) {
        const id = nextQueued[i]
        const prevIdx = prevQueued.indexOf(id)
        if (prevIdx < QUEUE_PILL_MAX) continue // already was a pill (or new)
        if (prevIdx < 0) continue
        const el = root.querySelector<HTMLElement>(`[data-wait-id="${CSS.escape(id)}"]`)
        if (!el || el.dataset.waitToken) continue
        // Was a dot, now a pill — slide-up expand.
        gsap.fromTo(
          el,
          { opacity: 0.5, y: 12, scale: 0.7 },
          { opacity: 1, y: 0, scale: 1, duration: motionDuration(0.52), ease: queueEase },
        )
      }
    }

    refreshCache()
  }, [snap, expanded])

  const depthLabel = t('inflightDepthHint', { n: summary.depthNow })
  const liveBadge = !useMockFallback && Boolean(live)
  const stickyBindings = sticky?.bindings ?? 0

  return (
    <Card
      data-testid="inflight-live-card"
      data-expanded={expanded ? 'true' : 'false'}
      data-gsap-reveal
      className="group overflow-hidden p-0 transition-[box-shadow] duration-200"
    >
      <style>{`
        [data-testid="inflight-live-card"]{
          /* Public AffectUI hierarchy → project tokens.
             Queue pills + account slots reuse solid-glass (top-edge rim +
             edge + contact shadow) like Overview cards / modals / supplier
             tiles — not frosted glass, not gray wells. chart-3 stays out of
             surfaces; amber (--warning) only for rate-limit / cooldown.
             Depth curve uses chart-1. */
          --il-well: var(--surface);
          --il-chip: var(--surface);
          --il-line: var(--surface-edge);
          --il-line-soft: var(--surface-edge);
          --il-glass:
            inset 0 1px 0 var(--surface-rim),
            inset 0 0 0 1px var(--surface-edge),
            var(--surface-contact);
          --il-track: color-mix(in oklab, var(--foreground) 10%, var(--surface-secondary));
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
        [data-testid="inflight-live-card"] [data-il-glass]{
          background: var(--surface);
          box-shadow: var(--il-glass);
        }
        @keyframes inflight-shimmer{0%{transform:translateX(-130%)}100%{transform:translateX(230%)}}
        @keyframes inflight-queue-shimmer{0%{transform:translateX(-140%);opacity:.35}50%{opacity:.7}100%{transform:translateX(240%);opacity:.35}}
        @keyframes inflight-glow{from{opacity:.45}to{opacity:1}}
        @media (prefers-reduced-motion: reduce){
          .motion-safe\\:animate-\\[inflight-shimmer_1\\.5s_linear_infinite\\],
          .motion-safe\:animate-\[inflight-shimmer_2\.2s_linear_infinite\],
          .motion-safe\:animate-\[inflight-queue-shimmer_2\.8s_linear_infinite\],
          .motion-safe\\:animate-\\[inflight-glow_1\\.1s_ease-in-out_infinite\\]{animation:none!important}
        }
      `}</style>

      <div className="flex items-start justify-between gap-3 border-b border-separator px-4 py-3">
        <button
          type="button"
          data-testid="inflight-live-toggle"
          aria-expanded={expanded}
          aria-controls="inflight-live-body"
          onClick={() => setExpanded(!expanded)}
          className="flex min-w-0 flex-1 items-start gap-2 text-left outline-none focus-visible:ring-2 focus-visible:ring-focus focus-visible:ring-offset-2 focus-visible:ring-offset-background"
        >
          <CaretRight
            size={14}
            aria-hidden
            className={`mt-1 shrink-0 text-muted transition-transform duration-200 ${expanded ? 'rotate-90' : ''}`}
          />
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h3 className="font-semibold">{t('inflightTitle')}</h3>
              <span
                data-il-glass={rateActive && expanded ? undefined : ''}
                className={`mono inline-flex items-center gap-1 rounded-[3px] px-1.5 py-0.5 text-caption ${
                  rateActive && expanded
                    ? 'bg-[var(--il-warn-bg)] font-medium text-[var(--il-warn-fg)] shadow-[inset_0_0_0_1px_var(--il-warn-edge)]'
                    : 'text-muted'
                }`}
              >
                <span className="status-dot" data-state={rateActive && expanded ? 'warn' : liveBadge ? 'ok' : ''} />
                {expanded && rateActive
                  ? t('inflightRatePill', { remain: formatRemain(rateActive.remainingMs) })
                  : liveBadge
                    ? t('inflightLiveBadge')
                    : t('inflightMockBadge')}
              </span>
              {expanded && stickyBindings > 0 ? (
                <span className="mono text-caption text-muted">
                  {t('inflightStickyBindings', { n: stickyBindings })}
                </span>
              ) : null}
            </div>
            <p className="mt-0.5 text-caption text-muted">{t('inflightHint')}</p>
          </div>
        </button>
        {expanded ? (
          <Link
            to="/live"
            className="inline-flex shrink-0 items-center gap-1 text-caption text-muted transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-focus"
            aria-label={t('inflightCardAria')}
            onClick={(e) => e.stopPropagation()}
          >
            {t('inflightOpen')}
            <ArrowUpRight size={14} aria-hidden />
          </Link>
        ) : (
          <button
            type="button"
            className="inline-flex shrink-0 items-center gap-1 text-caption text-muted transition-colors hover:text-foreground"
            onClick={() => setExpanded(true)}
          >
            {t('inflightExpand')}
            <CaretRight size={14} aria-hidden />
          </button>
        )}
      </div>

      {expanded ? (
        <div id="inflight-live-body" ref={rootRef}>
          <div className="grid grid-cols-2 gap-3 border-b border-separator px-4 py-3 sm:grid-cols-4">
            <Metric
              label={t('inflightWaiting')}
              value={summary.waiting}
              warn={waitAmber || Boolean(rateActive)}
            />
            <Metric
              label={t('inflightRunning')}
              value={summary.running}
              suffix={`/${summary.workerSlots}`}
            />
            <Metric label={t('inflightThroughput')} value={snap.throughputPerMin} suffix="/min" />
            <Metric label={t('inflightWaitP95')} value={snap.waitP95Ms} kind="ms" />
          </div>

          {/* Waiting → account-slot bars → Done/Failed  (AffectUI Agent Queue flow) */}
          <div className="grid gap-3 border-b border-separator px-4 py-3 lg:grid-cols-[minmax(0,0.95fr)_minmax(0,1.55fr)_auto]">
            <div className="min-w-0" data-queue-col>
              <div className="mb-1.5 flex items-baseline justify-between gap-2">
                <span className="text-caption uppercase tracking-wide text-muted">{t('inflightQueue')}</span>
                <span className={`mono text-[10px] tabular ${waitAmber ? 'text-[var(--il-warn-fg)]' : 'text-muted'}`}>
                  {summary.waiting}
                </span>
              </div>
              <div className="space-y-1">
                {queuePills.map((job) => (
                  <div
                    key={job.id}
                    data-wait-pill
                    data-wait-id={job.id}
                    data-priority={job.priority ?? 'normal'}
                    data-il-glass=""
                    className="relative flex items-center justify-between gap-2 overflow-hidden rounded-lg px-2 py-1.5"
                  >
                    <span
                      aria-hidden
                      className="pointer-events-none absolute inset-y-0 w-2/5 bg-gradient-to-r from-transparent via-[color-mix(in_oklab,var(--foreground)_10%,transparent)] to-transparent motion-safe:animate-[inflight-queue-shimmer_2.8s_linear_infinite]"
                    />
                    <span className="relative z-[1] flex min-w-0 items-center gap-1.5 truncate text-[11px] text-foreground/90">
                      <span className={`size-1.5 shrink-0 rounded-[2px] ${kindDot(job.kind)}`} aria-hidden />
                      <span className="truncate">
                        {job.name}
                        {job.priority === 'high' ? (
                          <span className="mono ml-1 text-[10px] text-muted">↑</span>
                        ) : null}
                        {job.sticky ? (
                          <span className="mono ml-1 text-[10px] text-muted">{t('inflightSticky')}</span>
                        ) : null}
                      </span>
                    </span>
                    <span className="relative z-[1] mono shrink-0 text-[10px] text-muted">{formatElapsed(job.elapsedMs)}</span>
                  </div>
                ))}
                {queueOverflow.length ? (
                  <div className="flex flex-wrap items-center gap-1 pt-1" data-queue-overflow>
                    {queueOverflow.map((job) => (
                      <span
                        key={job.id}
                        data-wait-token
                        data-wait-id={job.id}
                        data-priority={job.priority ?? 'normal'}
                        className={`size-2 rounded-[2px] ${kindDot(job.kind)} ${
                          job.priority === 'high' ? 'ring-1 ring-foreground/35' : ''
                        }`}
                        title={job.name}
                      />
                    ))}
                    <span className="mono text-[10px] text-muted">+{queueOverflow.length}</span>
                  </div>
                ) : queuePills.length === 0 ? (
                  <div className="text-caption text-muted">{t('inflightQueueEmpty')}</div>
                ) : null}
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
                {visibleWorkers.map((slot) => (
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
            <div data-il-glass="" className="rounded-lg px-1.5 pt-1.5 pb-1">
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
      ) : null}
    </Card>
  )
}

function Metric({
  label,
  value,
  suffix,
  kind = 'int',
  warn,
}: {
  label: string
  value: number
  suffix?: string
  kind?: 'int' | 'ms'
  warn?: boolean
}) {
  return (
    <div className="min-w-0">
      <div className="text-caption uppercase tracking-wide text-muted">{label}</div>
      <div
        className={`mono mt-1 flex items-baseline gap-0.5 text-section font-semibold tabular ${
          warn ? 'text-[var(--il-warn-fg)]' : 'text-foreground'
        }`}
      >
        <CountUp value={value} kind={kind} duration={560} />
        {suffix ? <span className="text-[0.92em] opacity-80">{suffix}</span> : null}
      </div>
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
        <CountUp value={value} kind="int" duration={560} />
      </div>
    </div>
  )
}
