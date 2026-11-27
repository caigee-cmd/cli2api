import { useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type KeyboardEvent, type ReactNode } from 'react'
import { ArrowClockwise, ArrowUp, Check, Cube, Minus, Pause, Play, Plus, Sparkle, Stack, User, Warning, Wrench, X } from '@phosphor-icons/react'

/**
 * Queue flow for the overview live panel.
 * Jobs move left to right: wait, a free worker pulls the front one, then Done, Failed, or back into the queue.
 * Motion is CSS transitions plus one animation frame loop for the ticking numbers.
 * Colors follow the console tokens. One account rate limit pauses that worker, not the whole board.
 */

export type QueueJobKind = 'llm' | 'tool' | 'agent' | 'human'
export type QueueJobStatus = 'queued' | 'running' | 'done' | 'failed'
export type QueueJobPriority = 'high' | 'normal' | 'low'

export type QueueJob = {
  id: string
  name: string
  kind: QueueJobKind
  status: QueueJobStatus
  /** High jumps ahead of the others, low waits behind them (default "normal") */
  priority?: QueueJobPriority
  /** When it (last) entered the queue – ms, on the same clock as `now` */
  enqueuedAt: number
  /** When a worker picked it up – clear it when the job goes back into the queue */
  startedAt?: number
  /** When it finished or failed */
  endedAt?: number
  /** The id of the worker that runs it */
  worker?: string
  /** 0–1 while it runs – leave it out and it's estimated from `estimate` */
  progress?: number
  /** How long it's expected to run, in ms */
  estimate?: number
  /** 1 for the first try, 2 for the first retry … */
  attempt?: number
  maxAttempts?: number
  /** A few muted words – the source, the target, the model */
  detail?: string
  /** A small mark, e.g. the tool's logo – defaults to a glyph for the kind */
  icon?: ReactNode
  /** What went wrong – shown in its card */
  error?: string
}

export type QueueWorker = {
  id: string
  name: string
  /** One muted line under the name, e.g. the model or "Worker 2" */
  detail?: string
  /** A small mark, e.g. the model maker's logo – defaults to the initials */
  icon?: ReactNode
  /** This slot is paused. Does not pause the other workers. */
  rateLimited?: boolean
}

export type QueueRateLimit = {
  from: number
  until: number
  /** e.g. "Token limit · 2M/min" – shown with the countdown */
  label?: string
  /** Pause this worker only. Omit to pause every worker. */
  workerId?: string
  /** Draw the chart band without pausing workers. */
  bandOnly?: boolean
}

export type AgentQueueLabels = {
  waiting: string
  running: string
  throughput: string
  waitP95: string
  queue: string
  workers: string
  nothingWaiting: string
  done: string
  failed: string
  depth: string
  idle: string
  paused: string
  live: string
  rateLimited: string
  now: string
  ago: string
  more: string
  high: string
  low: string
  normal: string
  priority: string
  attempt: string
  position: string
  waited: string
  worker: string
  close: string
  kind: Record<QueueJobKind, string>
  status: Record<QueueJobStatus, string>
  formatPosition: (index: number, total: number) => string
  formatAttempt: (attempt: number, max?: number) => string
  announceFailed: (name: string, error?: string) => string
  announceRetry: (name: string, attempt: number, max?: number) => string
  announceLimited: (seconds: number) => string
  announceResumed: string
  announcePressure: (count: string, line: number) => string
}

export type AgentQueueProps = {
  /** Waiting and running jobs, plus finished ones for as long as the chart should count them */
  jobs: QueueJob[]
  /** The worker slots – their number is the concurrency */
  workers: QueueWorker[]
  /** The clock in ms – without it, the component keeps its own (Date.now) while there is work */
  now?: number
  /** Stretches when the provider rate-limits – while a global one is active the workers show as paused */
  rateLimits?: QueueRateLimit[]
  /** The queue is paused – no new jobs start */
  paused?: boolean
  /** Shows a Pause/Resume button */
  onPausedChange?: (paused: boolean) => void
  /** Shows a – N + stepper for the number of workers */
  onConcurrencyChange?: (workers: number) => void
  minConcurrency?: number
  maxConcurrency?: number
  /** A depth that means trouble – drawn as a dashed line on the chart; above it Waiting turns amber */
  backpressure?: number
  /** How far back the chart looks, in ms (default 2 minutes) */
  window?: number
  /** Counts since the start, when `jobs` only holds the recent ones */
  totals?: { done?: number; failed?: number }
  /** Controlled id of the job whose card is pinned */
  openId?: string | null
  defaultOpenId?: string | null
  onOpenChange?: (id: string | null, job?: QueueJob) => void
  /** Shows "Move to front" in a waiting job's card */
  onPromote?: (job: QueueJob) => void
  /** Shows "Cancel" in a waiting or running job's card */
  onCancel?: (job: QueueJob) => void
  title?: string
  /** The queue's name, muted next to the title */
  name?: string
  className?: string
  /** Sit inside another card: no second shadow, no second title row */
  embedded?: boolean
  labels?: Partial<AgentQueueLabels>
}

const DEFAULT_LABELS: AgentQueueLabels = {
  waiting: 'Waiting',
  running: 'Running',
  throughput: 'Throughput',
  waitP95: 'Wait p95',
  queue: 'Queue',
  workers: 'Workers',
  nothingWaiting: 'Nothing waiting',
  done: 'Done',
  failed: 'Failed',
  depth: 'Queue depth',
  idle: 'Idle',
  paused: 'Paused',
  live: 'Live',
  rateLimited: 'Rate limited',
  now: 'now',
  ago: 'ago',
  more: 'more',
  high: 'High',
  low: 'Low',
  normal: 'Normal',
  priority: 'Priority',
  attempt: 'Attempt',
  position: 'Position',
  waited: 'Waited',
  worker: 'Worker',
  close: 'Close details',
  kind: { llm: 'LLM call', tool: 'Tool call', agent: 'Sub-agent', human: 'Human review' },
  status: { queued: 'Waiting', running: 'Running', done: 'Done', failed: 'Failed' },
  formatPosition: (index, total) => `${index} of ${total}`,
  formatAttempt: (attempt, max) => `${attempt}${max ? ` of ${max}` : ''}`,
  announceFailed: (name, error) => (error ? `${name} failed: ${error}` : `${name} failed`),
  announceRetry: (name, attempt, max) => `${name} failed – retrying, attempt ${attempt}${max ? ` of ${max}` : ''}`,
  announceLimited: (seconds) => `Rate limited – workers paused for ${seconds} seconds`,
  announceResumed: 'Rate limit lifted – workers resumed',
  announcePressure: (count, line) => `${count} jobs waiting – above the backpressure line of ${line}`,
}

function labelsOf(partial?: Partial<AgentQueueLabels>): AgentQueueLabels {
  return {
    ...DEFAULT_LABELS,
    ...partial,
    kind: { ...DEFAULT_LABELS.kind, ...partial?.kind },
    status: { ...DEFAULT_LABELS.status, ...partial?.status },
  }
}

const RANK: Record<QueueJobPriority, number> = { high: 0, normal: 1, low: 2 }
/** Motion: how long a job takes to move, to land in a bin, and how long a fresh high-priority job waits before it jumps */
const MOVE = 560
const LAND = 1100
const FLASH = 1500
const JUMP = 520
/** A finished job shows its result in its slot for a moment before it flies – a failure a little longer */
const HOLD_DONE = 200
const HOLD_FAIL = 520
const MINUTE = 60_000
/** Heights the chart's scale snaps to */
const SCALES = [4, 6, 8, 10, 12, 16, 20, 24, 30, 40, 50, 60, 80, 100, 120, 160, 200, 300, 400, 500, 600, 800, 1000, 1500, 2000]

const took = (ms: number) => {
  const v = Math.max(0, ms)
  if (v < 10_000) return `${(v / 1000).toFixed(1)}s`
  const s = Math.round(v / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ${String(s % 60).padStart(2, '0')}s`
  const h = Math.floor(m / 60)
  return h < 24 ? `${h}h ${String(m % 60).padStart(2, '0')}m` : `${Math.floor(h / 24)}d ${h % 24}h`
}
const int = (n: number) => Math.round(n).toLocaleString('en-US')
/** Counts that must fit a small box: exact up to 99,999, then "128K", "1.3M" – the exact number goes in the title */
const compact = new Intl.NumberFormat('en-US', { notation: 'compact', maximumFractionDigits: 1 })
const count = (n: number) => (Math.round(n) < 100_000 ? int(n) : compact.format(Math.round(n)))
const pct = (list: number[], p: number) => {
  if (!list.length) return 0
  const s = [...list].sort((a, b) => a - b)
  return s[Math.min(s.length - 1, Math.floor(p * s.length))]
}
const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, v))
const initials = (name: string) => {
  const words = name.trim().split(/\s+/).filter((w) => /^[\p{L}\p{N}]/u.test(w))
  const marks = words.slice(0, 2).map((w) => Array.from(w)[0])
  return (marks.length ? marks.join('') : (Array.from(name.trim())[0] ?? '')).toUpperCase()
}
const reduced = () => typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches
const r2 = (n: number) => Math.round(n * 100) / 100

/** A smooth line through the points that never overshoots (monotone cubic) – x in px, y in queue depth */
const smooth = (p: [number, number][]) => {
  const n = p.length
  if (n < 2) return ''
  const dx: number[] = []
  const m: number[] = []
  for (let i = 0; i < n - 1; i++) {
    dx[i] = p[i + 1][0] - p[i][0] || 1e-6
    m[i] = (p[i + 1][1] - p[i][1]) / dx[i]
  }
  const t: number[] = [m[0]]
  for (let i = 1; i < n - 1; i++) t[i] = m[i - 1] * m[i] <= 0 ? 0 : (3 * (dx[i - 1] + dx[i])) / ((2 * dx[i] + dx[i - 1]) / m[i - 1] + (dx[i] + 2 * dx[i - 1]) / m[i])
  t[n - 1] = m[n - 2]
  let d = `M${r2(p[0][0])},${r2(p[0][1])}`
  for (let i = 0; i < n - 1; i++) {
    const h = dx[i] / 3
    d += `C${r2(p[i][0] + h)},${r2(p[i][1] + t[i] * h)} ${r2(p[i + 1][0] - h)},${r2(p[i + 1][1] - t[i + 1] * h)} ${r2(p[i + 1][0])},${r2(p[i + 1][1])}`
  }
  return d
}

type Box = { x: number; y: number; w: number; h: number }
type Layout = {
  narrow: boolean
  height: number
  pad: number
  heads: { queue: Box; workers: Box }
  chips: Box[]
  well: Box
  grid: { x: number; y: number; cols: number; rows: number; step: number }
  slots: Box[]
  rail: { x: number; y: number; w: number; h: number }
  done: Box
  failed: Box
}
const SQ = 12
const STEP = 16
const CHIP = 28
const SLOT = 40
const GAP = 6
const WELL = 10
const GUTTER = 26
const gridIn = (well: Box, rows: number) => {
  const cols = Math.max(2, Math.floor((well.w - 2 * WELL + STEP - SQ) / STEP))
  const h = rows * STEP - (STEP - SQ)
  return { x: well.x + WELL, y: well.y + (well.h - h) / 2, cols, rows, step: (well.w - 2 * WELL - SQ) / (cols - 1) }
}

const layoutFor = (W: number, n: number): Layout => {
  const narrow = W < 600
  const countN = Math.max(1, n)
  const slotsH = countN * SLOT + (countN - 1) * GAP
  if (!narrow) {
    const pad = 16
    const top = 14
    const y0 = top + 16 + 10
    const qw = clamp(Math.round(W * 0.26), 188, 236)
    const bw = 92
    const g = 24
    const content = Math.max(slotsH, 4 * SLOT + 3 * GAP)
    const qx = pad
    const wx = pad + qw + g
    const ww = W - 2 * pad - qw - bw - 2 * g
    const bx = W - pad - bw
    const chips = [0, 1, 2].map((i) => ({ x: qx, y: y0 + i * (CHIP + 4), w: qw, h: CHIP }))
    const gy = y0 + 3 * (CHIP + 4) + 2
    const rows = Math.max(1, Math.floor((y0 + content - gy - 2 * WELL + STEP - SQ) / STEP))
    const well = { x: qx, y: gy, w: qw, h: y0 + content - gy }
    const iw = clamp(Math.round(ww * 0.4), 112, 164)
    // Bins stay a short pair. A long worker list must not drop the count to mid-column.
    const binContent = Math.min(content, 4 * SLOT + 3 * GAP)
    const half = (binContent - 8) / 2
    return {
      narrow,
      height: y0 + content + 18,
      pad,
      heads: { queue: { x: qx, y: top, w: qw, h: 16 }, workers: { x: wx, y: top, w: ww, h: 16 } },
      chips,
      well,
      grid: gridIn(well, rows),
      slots: Array.from({ length: n }, (_, i) => ({ x: wx, y: y0 + i * (SLOT + GAP), w: ww, h: SLOT })),
      rail: { x: iw, y: 6, w: ww - iw - 6, h: CHIP },
      done: { x: bx, y: y0, w: bw, h: half },
      failed: { x: bx, y: y0 + half + 8, w: bw, h: half },
    }
  }
  const pad = 12
  const fw = W - 2 * pad
  let y = 12
  const queueHead = { x: pad, y, w: fw, h: 16 }
  y += 16 + 8
  const chips = [0, 1].map((i) => ({ x: pad, y: y + i * (CHIP + 4), w: fw, h: CHIP }))
  const gy = y + 2 * (CHIP + 4) + 2
  const well = { x: pad, y: gy, w: fw, h: 2 * STEP - (STEP - SQ) + 2 * WELL }
  y = gy + well.h
  y += 18
  const workersHead = { x: pad, y, w: fw, h: 16 }
  y += 16 + 8
  const sy = y
  y += slotsH
  y += 12
  const iw = 40
  const half = (fw - 8) / 2
  return {
    narrow,
    height: y + 40 + 12,
    pad,
    heads: { queue: queueHead, workers: workersHead },
    chips,
    well,
    grid: gridIn(well, 2),
    slots: Array.from({ length: n }, (_, i) => ({ x: pad, y: sy + i * (SLOT + GAP), w: fw, h: SLOT })),
    rail: { x: iw, y: 6, w: fw - iw - 6, h: CHIP },
    done: { x: pad, y, w: half, h: 40 },
    failed: { x: pad + half + 8, y, w: half, h: 40 },
  }
}

const KindGlyph = ({ kind }: { kind: QueueJobKind }) => {
  const props = { size: 12, weight: 'regular' as const }
  if (kind === 'llm') return <Sparkle {...props} />
  if (kind === 'tool') return <Wrench {...props} />
  if (kind === 'agent') return <Cube {...props} />
  return <User {...props} />
}

type Shape = 'chip' | 'square' | 'bar' | 'gone'
type Placed = { job: QueueJob; shape: Shape; box: Box; index?: number; flash: boolean; p?: number }
type Num = { el: HTMLElement | null; shown: number; target: number; fmt: (n: number) => string }

export function AgentQueue({
  jobs,
  workers,
  now: nowProp,
  rateLimits = [],
  paused = false,
  onPausedChange,
  onConcurrencyChange,
  minConcurrency = 1,
  maxConcurrency = 8,
  backpressure,
  window: span = 2 * MINUTE,
  totals,
  openId,
  defaultOpenId = null,
  onOpenChange,
  onPromote,
  onCancel,
  title = 'Queue',
  name,
  className,
  embedded = false,
  labels: labelsProp,
}: AgentQueueProps) {
  const labels = labelsOf(labelsProp)
  const rootRef = useRef<HTMLDivElement>(null)
  const cardRef = useRef<HTMLDivElement>(null)
  const popRef = useRef<HTMLDivElement>(null)
  const jobEls = useRef(new Map<string, HTMLButtonElement>())
  const gradId = 'ui-agqc-' + useId().replace(/[^a-zA-Z0-9_-]/g, '')

  const [W, setW] = useState(0)
  useLayoutEffect(() => {
    const el = cardRef.current
    if (!el) return
    setW(el.clientWidth)
    const ro = new ResizeObserver(() => setW(el.clientWidth))
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  const width = W || 812

  const [tick, setTick] = useState(() => Date.now())
  const now = nowProp ?? tick
  const activeLimit = rateLimits.find((l) => !l.workerId && !l.bandOnly && l.from <= now && now < l.until)
  const limited = !!activeLimit
  const workerHalted = (id: string) => {
    if (limited) return true
    if (workers.some((worker) => worker.id === id && worker.rateLimited)) return true
    return rateLimits.some((l) => l.workerId === id && !l.bandOnly && l.from <= now && now < l.until)
  }
  const queuedAll = jobs.filter((j) => j.status === 'queued')
  const runningAll = jobs.filter((j) => j.status === 'running')
  const needClock = nowProp === undefined && (queuedAll.length > 0 || runningAll.length > 0 || limited)

  const mem = useRef({
    seen: new Map<string, { status: QueueJobStatus; attempt: number; first: number; fresh: boolean; worker?: string }>(),
    gone: new Map<string, number>(),
    flash: new Map<string, number>(),
    limited: false,
    over: false,
    msg: '',
    msgAt: 0,
    mounted: false,
  })
  const real = typeof performance !== 'undefined' ? performance.now() : 0
  {
    const m = mem.current
    const say = (text: string) => {
      m.msg = text
      m.msgAt = real
    }
    for (const j of jobs) {
      const attempt = j.attempt ?? 1
      const prev = m.seen.get(j.id)
      if (!prev) {
        m.seen.set(j.id, { status: j.status, attempt, first: real, fresh: m.mounted, worker: j.worker })
        continue
      }
      if (j.status === 'running' && j.worker) prev.worker = j.worker
      if (prev.status === j.status && prev.attempt === attempt) continue
      const wasLive = prev.status === 'running' || prev.status === 'queued'
      if ((j.status === 'done' || j.status === 'failed') && wasLive) m.gone.set(j.id, real)
      if (j.status === 'failed' && wasLive) say(labels.announceFailed(j.name, j.error))
      if (j.status === 'queued' && prev.status === 'running') {
        m.flash.set(j.id, real)
        if (attempt > prev.attempt) say(labels.announceRetry(j.name, attempt, j.maxAttempts))
      }
      prev.status = j.status
      prev.attempt = attempt
    }
    if (limited !== m.limited) {
      if (m.mounted) say(limited ? labels.announceLimited(Math.ceil((activeLimit!.until - now) / 1000)) : labels.announceResumed)
      m.limited = limited
    }
    const overNow = backpressure !== undefined && queuedAll.length > backpressure
    if (overNow !== m.over) {
      if (overNow && m.mounted && backpressure !== undefined) say(labels.announcePressure(int(queuedAll.length), backpressure))
      m.over = overNow
    }
    for (const [id, at] of m.gone) if (real - at > LAND) m.gone.delete(id)
    for (const [id, at] of m.flash) if (real - at > FLASH) m.flash.delete(id)
    if (m.seen.size > jobs.length + 64) {
      const ids = new Set(jobs.map((j) => j.id))
      for (const id of m.seen.keys()) if (!ids.has(id)) m.seen.delete(id)
    }
  }
  const [announce, setAnnounce] = useState('')
  useEffect(() => {
    mem.current.mounted = true
    if (mem.current.msg && mem.current.msg !== announce) setAnnounce(mem.current.msg)
  })

  const L = layoutFor(width, workers.length)
  const rankOf = (j: QueueJob) => {
    const r = RANK[j.priority ?? 'normal']
    const s = mem.current.seen.get(j.id)
    return r === 0 && s?.fresh && real - s.first < JUMP ? 1 : r
  }
  const order = [...queuedAll].sort((a, b) => rankOf(a) - rankOf(b) || a.enqueuedAt - b.enqueuedAt || (a.id < b.id ? -1 : 1))
  const K = L.chips.length
  const capFull = L.grid.cols * L.grid.rows
  const overflow = order.length > K + capFull
  const cap = overflow ? capFull - 3 : capFull
  const hiddenCount = Math.max(0, order.length - K - cap)
  const slotOf = new Map(workers.map((w, i) => [w.id, i]))

  const placed: Placed[] = []
  const barIn = (slot: Box): Box => ({ x: slot.x + L.rail.x, y: slot.y + L.rail.y, w: L.rail.w, h: L.rail.h })
  const heldSlot = (j: QueueJob, since: number | undefined, hold: number) => {
    if (since === undefined || real - since >= hold) return undefined
    const w = mem.current.seen.get(j.id)?.worker ?? j.worker
    const s = w !== undefined ? slotOf.get(w) : undefined
    return s === undefined ? undefined : L.slots[s]
  }
  let queuedIndex = 0
  order.forEach((j) => {
    const flash = mem.current.flash.has(j.id)
    const held = heldSlot(j, mem.current.flash.get(j.id), HOLD_FAIL)
    if (held) {
      placed.push({ job: j, shape: 'bar', box: barIn(held), flash: true, p: 1 })
      return
    }
    const i = queuedIndex++
    if (i < K) placed.push({ job: j, shape: 'chip', box: L.chips[i], index: i, flash })
    else if (i < K + cap) {
      const g = i - K
      placed.push({
        job: j,
        shape: 'square',
        box: { x: L.grid.x + (g % L.grid.cols) * L.grid.step, y: L.grid.y + Math.floor(g / L.grid.cols) * STEP, w: SQ, h: SQ },
        index: i,
        flash,
      })
    }
  })
  for (const j of runningAll) {
    const s = j.worker !== undefined ? slotOf.get(j.worker) : undefined
    if (s === undefined) continue
    placed.push({ job: j, shape: 'bar', box: barIn(L.slots[s]), flash: false })
  }
  for (const j of jobs) {
    if (!mem.current.gone.has(j.id) || (j.status !== 'done' && j.status !== 'failed')) continue
    const held = heldSlot(j, mem.current.gone.get(j.id), j.status === 'done' ? HOLD_DONE : HOLD_FAIL)
    if (held) {
      placed.push({ job: j, shape: 'bar', box: barIn(held), flash: false, p: 1 })
      continue
    }
    const bin = j.status === 'done' ? L.done : L.failed
    placed.push({ job: j, shape: 'gone', box: { x: bin.x + 17, y: bin.y + (L.narrow ? 15 : 17), w: 10, h: 10 }, flash: false })
  }
  placed.sort((a, b) => (a.job.id < b.job.id ? -1 : 1))

  const doneCount = totals?.done ?? jobs.filter((j) => j.status === 'done').length
  const failedCount = totals?.failed ?? jobs.filter((j) => j.status === 'failed').length
  const perMinute = jobs.filter((j) => j.status === 'done' && j.endedAt !== undefined && j.endedAt > now - MINUTE && j.endedAt <= now).length
  const waits = [
    ...jobs.filter((j) => j.startedAt !== undefined && j.status !== 'queued' && j.startedAt > now - MINUTE).map((j) => j.startedAt! - j.enqueuedAt),
    ...queuedAll.map((j) => now - j.enqueuedAt),
  ]
  const p95 = pct(waits, 0.95)
  const over = backpressure !== undefined && queuedAll.length > backpressure
  const state = limited ? 'limited' : paused ? 'paused' : queuedAll.length || runningAll.length ? 'live' : 'idle'

  const t0 = now - span
  const stepT = span / 96
  const { starts, ends } = useMemo(() => {
    const startAt: number[] = []
    const endAt: number[] = []
    for (const j of jobs) {
      const left = j.status === 'queued' ? Infinity : (j.startedAt ?? j.endedAt ?? Infinity)
      startAt.push(j.enqueuedAt)
      endAt.push(Math.max(j.enqueuedAt, left))
    }
    startAt.sort((a, b) => a - b)
    endAt.sort((a, b) => a - b)
    return { starts: startAt, ends: endAt }
  }, [jobs])
  let si = 0
  let ei = 0
  const depthAt = (t: number) => {
    while (si < starts.length && starts[si] <= t) si++
    while (ei < ends.length && ends[ei] <= t) ei++
    return si - ei
  }
  const samples: [number, number][] = [[t0, depthAt(t0)]]
  for (let t = Math.ceil(t0 / stepT) * stepT; t < now - stepT * 0.25; t += stepT) samples.push([t, depthAt(t)])
  samples.push([now, queuedAll.length])
  const peak = Math.max(0, ...samples.map((s) => s[1]))
  const want = Math.max(peak * 1.15, (backpressure ?? 0) * 1.2, 4)
  const ymax = SCALES.find((v) => v >= want) ?? Math.ceil(want / 500) * 500
  const gutter = Math.max(GUTTER, 10 + int(ymax).length * 7)
  const plotW = Math.max(40, width - 2 * L.pad - gutter)
  const plotH = L.narrow ? 88 : 112
  const xOf = (t: number) => ((t - t0) / span) * plotW
  const pts = samples.map(([t, d]) => [xOf(t), d] as [number, number])
  const line = smooth(pts)
  const area = line ? `${line}L${r2(plotW)},0L0,0Z` : ''
  const avg = samples.reduce((s, p) => s + p[1], 0) / samples.length
  const bands = rateLimits.filter((l) => l.until > t0 && l.from < now).map((l) => ({ l, x1: xOf(Math.max(l.from, t0)), x2: xOf(Math.min(l.until, now)) }))
  const minutes = span / MINUTE
  const spanLabel =
    minutes >= 2 * 24 * 60 ? `${+(minutes / 1440).toFixed(1)} days` : minutes >= 120 ? `${+(minutes / 60).toFixed(1)} h` : minutes >= 2 ? `${+minutes.toFixed(1)} min` : `${Math.round(span / 1000)}s`
  const summary = `${labels.depth}, ${spanLabel}: ${queuedAll.length}, peak ${peak}, average ${avg.toFixed(1)}${backpressure !== undefined ? `, ${backpressure}` : ''}.`

  const nums = useRef(new Map<string, Num>())
  const num = (key: string, value: number, fmt: (n: number) => string) => {
    let n = nums.current.get(key)
    if (!n) {
      n = { el: null, shown: value, target: value, fmt }
      nums.current.set(key, n)
    }
    n.target = value
    n.fmt = fmt
    const rec = n
    return (
      <span
        className="ui-agqc__num"
        ref={(el) => {
          rec.el = el
          if (el && !el.textContent) el.textContent = rec.fmt(rec.shown)
        }}
      />
    )
  }
  const live = useRef({ needClock, ymax })
  live.current = { needClock, ymax }
  const wake = useRef<() => void>(() => {})
  useEffect(() => {
    let raf = 0
    let visible = true
    let last = performance.now()
    let lastClock = 0
    let shownMax = live.current.ymax
    rootRef.current?.style.setProperty('--ui-agqc-ymax', String(shownMax))
    const frame = (t: number) => {
      raf = 0
      const dt = Math.min(64, t - last)
      last = t
      const instant = reduced()
      const k = 1 - Math.exp(-dt / 110)
      let busy = false
      for (const n of nums.current.values()) {
        if (n.shown === n.target) continue
        const d = n.target - n.shown
        n.shown = instant || Math.abs(d) < 0.02 * Math.max(1, Math.abs(n.target)) * 0.05 + 0.004 ? n.target : n.shown + d * k
        if (n.el) n.el.textContent = n.fmt(n.shown)
        busy = true
      }
      const goal = live.current.ymax
      if (shownMax !== goal) {
        const d = goal - shownMax
        shownMax = instant || Math.abs(d) < 0.01 ? goal : shownMax + d * (1 - Math.exp(-dt / 160))
        rootRef.current?.style.setProperty('--ui-agqc-ymax', String(shownMax))
        busy = true
      }
      if (live.current.needClock) {
        busy = true
        if (t - lastClock > 33) {
          lastClock = t
          setTick(Date.now())
        }
      }
      if (busy && visible && !document.hidden) raf = requestAnimationFrame(frame)
    }
    const start = () => {
      if (raf || !visible || document.hidden) return
      last = performance.now()
      raf = requestAnimationFrame(frame)
    }
    wake.current = start
    const io = new IntersectionObserver(([e]) => {
      visible = e.isIntersecting
      if (visible) start()
    })
    if (rootRef.current) io.observe(rootRef.current)
    document.addEventListener('visibilitychange', start)
    start()
    return () => {
      cancelAnimationFrame(raf)
      io.disconnect()
      document.removeEventListener('visibilitychange', start)
    }
  }, [])
  useEffect(() => wake.current())
  useLayoutEffect(() => {
    rootRef.current?.style.setProperty('--ui-agqc-ymax', String(live.current.ymax))
  }, [])

  const [, bump] = useState(0)
  const pending = mem.current.gone.size > 0 || mem.current.flash.size > 0 || queuedAll.some((j) => j.priority === 'high' && real - (mem.current.seen.get(j.id)?.first ?? 0) < JUMP)
  useEffect(() => {
    if (!pending) return
    const t = window.setTimeout(() => bump((b) => b + 1), 120)
    return () => window.clearTimeout(t)
  })

  const [innerOpen, setInnerOpen] = useState<string | null>(defaultOpenId)
  const wantOpen = openId === undefined ? innerOpen : openId
  const setOpen = (id: string | null) => {
    setInnerOpen(id)
    onOpenChange?.(id, id ? jobs.find((j) => j.id === id) : undefined)
  }
  const [hover, setHover] = useState<string | null>(null)
  const live2 = placed.filter((p) => p.shape !== 'gone')
  const openPlaced = wantOpen ? live2.find((p) => p.job.id === wantOpen) : undefined
  const hoverPlaced = !openPlaced && hover ? live2.find((p) => p.job.id === hover) : undefined
  const cardOf = openPlaced ?? hoverPlaced
  useEffect(() => {
    if (wantOpen && !openPlaced) setOpen(null)
  })
  useEffect(() => {
    if (!openPlaced) return
    const away = (e: PointerEvent) => {
      const t = e.target as Node
      if (popRef.current?.contains(t) || [...jobEls.current.values()].some((el) => el.contains(t))) return
      setOpen(null)
    }
    document.addEventListener('pointerdown', away)
    return () => document.removeEventListener('pointerdown', away)
  })

  const [popH, setPopH] = useState(120)
  useLayoutEffect(() => {
    const h = popRef.current?.offsetHeight
    if (h && Math.abs(h - popH) > 1) setPopH(h)
  })

  const walkOrder = [
    ...runningAll.filter((j) => j.worker !== undefined && slotOf.has(j.worker)).sort((a, b) => slotOf.get(a.worker!)! - slotOf.get(b.worker!)!),
    ...order.slice(0, K + cap),
  ]
  const [active, setActive] = useState<string | null>(null)
  const activeId = active && walkOrder.some((j) => j.id === active) ? active : walkOrder[0]?.id
  const focusJob = (j?: QueueJob) => {
    if (!j) return
    setActive(j.id)
    jobEls.current.get(j.id)?.focus()
  }
  const onKey = (e: KeyboardEvent<HTMLButtonElement>, j: QueueJob) => {
    const i = walkOrder.findIndex((x) => x.id === j.id)
    const keys: Record<string, () => void> = {
      ArrowDown: () => focusJob(walkOrder[i + 1]),
      ArrowRight: () => focusJob(walkOrder[i + 1]),
      ArrowUp: () => focusJob(walkOrder[i - 1]),
      ArrowLeft: () => focusJob(walkOrder[i - 1]),
      Home: () => focusJob(walkOrder[0]),
      End: () => focusJob(walkOrder[walkOrder.length - 1]),
      Escape: () => wantOpen && setOpen(null),
    }
    if (!keys[e.key]) return
    e.preventDefault()
    keys[e.key]()
  }

  const progressOf = (j: QueueJob) =>
    j.status === 'done' || j.status === 'failed' ? 1 : j.status !== 'running' ? 0 : clamp(j.progress ?? (j.startedAt !== undefined && j.estimate ? (now - j.startedAt) / j.estimate : 0.5), 0.015, 1)
  const describe = (p: Placed) => {
    const j = p.job
    const bits = [j.name, labels.kind[j.kind]]
    if (j.status === 'queued') bits.push(`${labels.waiting} ${took(now - j.enqueuedAt)}`, labels.formatPosition((p.index ?? 0) + 1, order.length))
    if (j.status === 'running') {
      const halted = j.worker ? workerHalted(j.worker) : limited
      bits.push(halted ? labels.paused : `${labels.running} ${took(now - (j.startedAt ?? now))}, ${Math.round(progressOf(j) * 100)}%`)
    }
    if (j.priority === 'high') bits.push(labels.high)
    if (j.priority === 'low') bits.push(labels.low)
    if ((j.attempt ?? 1) > 1) bits.push(labels.formatAttempt(j.attempt ?? 1, j.maxAttempts))
    return bits.join(', ')
  }
  const job = (p: Placed) => {
    const j = p.job
    const gone = p.shape === 'gone'
    const attempt = j.attempt ?? 1
    const halted = j.status === 'running' && (j.worker ? workerHalted(j.worker) : limited)
    const meta =
      j.status === 'queued' ? took(now - j.enqueuedAt) : j.status === 'running' ? (halted ? labels.paused : took(now - (j.startedAt ?? now))) : j.status === 'done' ? labels.done : labels.failed
    const s = mem.current.seen.get(j.id)
    const enter = s && !s.fresh ? Math.min(420, (p.index ?? 0) * 22) : 0
    return (
      <button
        key={j.id}
        ref={(el) => {
          if (el) jobEls.current.set(j.id, el)
          else jobEls.current.delete(j.id)
        }}
        type="button"
        className="ui-agqc__job"
        data-shape={p.shape}
        data-kind={j.kind}
        data-status={j.status}
        data-flash={p.flash ? '' : undefined}
        data-halted={halted ? '' : undefined}
        data-priority={j.priority && j.priority !== 'normal' ? j.priority : undefined}
        data-open={wantOpen === j.id ? '' : undefined}
        tabIndex={!gone && activeId === j.id ? 0 : -1}
        aria-hidden={gone || undefined}
        aria-label={describe(p)}
        aria-haspopup="dialog"
        aria-expanded={wantOpen === j.id}
        style={{ translate: `${p.box.x}px ${p.box.y}px`, width: p.box.w, height: p.box.h, '--p': p.p ?? progressOf(j), animationDelay: `${enter}ms` } as CSSProperties}
        onPointerEnter={() => !gone && setHover(j.id)}
        onPointerLeave={() => setHover((h) => (h === j.id ? null : h))}
        onFocus={() => setActive(j.id)}
        onClick={() => {
          if (gone) return
          setActive(j.id)
          setOpen(wantOpen === j.id ? null : j.id)
        }}
        onKeyDown={(e) => onKey(e, j)}
      >
        <span className="ui-agqc__fill" aria-hidden="true" />
        <span className="ui-agqc__job-in" aria-hidden="true">
          <span className="ui-agqc__job-icon" data-logo={j.icon ? '' : undefined}>
            {j.icon ?? <KindGlyph kind={j.kind} />}
          </span>
          <span className="ui-agqc__job-name" dir="auto">
            {j.name}
          </span>
          {j.priority === 'high' && (
            <span className="ui-agqc__badge" data-kind="high" title={labels.high}>
              <ArrowUp size={10} weight="bold" />
            </span>
          )}
          {attempt > 1 && (
            <span className="ui-agqc__badge" data-kind="retry" title={labels.formatAttempt(attempt, j.maxAttempts)}>
              <ArrowClockwise size={10} weight="bold" />
              {attempt}
            </span>
          )}
          <span className="ui-agqc__job-meta">{meta}</span>
        </span>
      </button>
    )
  }

  const pop = (p: Placed) => {
    const j = p.job
    const pinned = !!openPlaced
    const attempt = j.attempt ?? 1
    const worker = j.worker ? workers.find((w) => w.id === j.worker) : undefined
    const rows: [string, string][] = [
      j.status === 'queued' ? [labels.waiting, took(now - j.enqueuedAt)] : [labels.waited, took((j.startedAt ?? now) - j.enqueuedAt)],
      ...(j.status === 'running' ? [[labels.running, took(now - (j.startedAt ?? now))] as [string, string]] : []),
      ...(j.status === 'queued' ? [[labels.position, labels.formatPosition((p.index ?? 0) + 1, order.length)] as [string, string]] : []),
      ...(j.status === 'running' && worker ? [[labels.worker, worker.name] as [string, string]] : []),
      [labels.priority, j.priority === 'high' ? labels.high : j.priority === 'low' ? labels.low : labels.normal],
      [labels.attempt, labels.formatAttempt(attempt, j.maxAttempts)],
    ]
    const w = Math.min(L.narrow ? width - 2 * L.pad : 280, width - 16)
    const below = p.box.y + p.box.h + 6
    const y = below + popH > L.height + 120 && p.box.y - popH - 6 > 0 ? p.box.y - popH - 6 : below
    const x = clamp(p.box.x, 8, width - w - 8)
    const halted = j.status === 'running' && (j.worker ? workerHalted(j.worker) : limited)
    const pill = halted ? labels.paused : labels.status[j.status]
    return (
      <div
        key={j.id}
        ref={popRef}
        className="ui-agqc__pop"
        role={pinned ? 'dialog' : 'tooltip'}
        aria-label={j.name}
        data-pinned={pinned ? '' : undefined}
        data-kind={j.kind}
        data-status={halted ? 'halted' : j.status}
        style={{ translate: `${x}px ${y}px`, width: w }}
        onKeyDown={(e) => {
          if (e.key !== 'Escape') return
          e.stopPropagation()
          setOpen(null)
          jobEls.current.get(j.id)?.focus()
        }}
      >
        <div className="ui-agqc__pop-head">
          <span className="ui-agqc__pop-icon" data-logo={j.icon ? '' : undefined}>
            {j.icon ?? <KindGlyph kind={j.kind} />}
          </span>
          <span className="ui-agqc__pop-who">
            <span className="ui-agqc__pop-name" dir="auto">{j.name}</span>
            <span className="ui-agqc__pop-kind" dir="auto">
              {labels.kind[j.kind]}
              {j.detail ? ` · ${j.detail}` : ''}
            </span>
          </span>
          <span className="ui-agqc__pill" data-status={halted ? 'halted' : j.status}>{pill}</span>
          {pinned && (
            <button
              type="button"
              className="ui-agqc__close"
              aria-label={labels.close}
              onClick={() => {
                setOpen(null)
                jobEls.current.get(j.id)?.focus()
              }}
            >
              <X size={14} />
            </button>
          )}
        </div>
        <dl className="ui-agqc__pop-stats">
          {rows.map(([k, v]) => (
            <div key={k}>
              <dt>{k}</dt>
              <dd>{v}</dd>
            </div>
          ))}
        </dl>
        {j.error && (
          <p className="ui-agqc__pop-error">
            <Warning size={12} />
            <span dir="auto">{j.error}</span>
          </p>
        )}
        {pinned && (onPromote || onCancel) && (j.status === 'queued' || j.status === 'running') && (
          <div className="ui-agqc__pop-actions">
            {onPromote && j.status === 'queued' && (p.index ?? 0) > 0 && (
              <button type="button" className="ui-agqc__btn" onClick={() => onPromote(j)}>
                <ArrowUp size={12} weight="bold" />
                {labels.high}
              </button>
            )}
            {onCancel && (
              <button
                type="button"
                className="ui-agqc__btn"
                data-kind="danger"
                onClick={() => {
                  onCancel(j)
                  setOpen(null)
                }}
              >
                <X size={10} />
                {labels.failed}
              </button>
            )}
          </div>
        )}
      </div>
    )
  }

  const at = (b: Box): CSSProperties => ({ translate: `${b.x}px ${b.y}px`, width: b.w, height: b.h })
  const busyCount = runningAll.filter((j) => j.worker !== undefined && slotOf.has(j.worker)).length
  const stats: { label: string; value: ReactNode; kind?: string }[] = [
    { label: labels.waiting, value: num('waiting', queuedAll.length, int), kind: over ? 'warn' : undefined },
    {
      label: labels.running,
      value: (
        <>
          {num('running', busyCount, int)}
          <span className="ui-agqc__of">/{workers.length}</span>
        </>
      ),
    },
    {
      label: labels.throughput,
      value: (
        <>
          {num('rate', perMinute, int)}
          <span className="ui-agqc__of">/min</span>
        </>
      ),
    },
    { label: labels.waitP95, value: num('p95', p95, took) },
  ]
  const left = activeLimit ? Math.max(0, Math.ceil((activeLimit.until - now) / 1000)) : 0
  const pillText = state === 'paused' ? labels.paused : state === 'live' ? labels.live : labels.idle
  const canLess = workers.length > minConcurrency
  const canMore = workers.length < maxConcurrency

  return (
    <div
      ref={rootRef}
      className={['ui-agqc', className].filter(Boolean).join(' ')}
      data-state={state}
      data-embedded={embedded ? '' : undefined}
    >
      <style href="ui-agent-queue-clean" precedence="default">{css}</style>

      <header className="ui-agqc__head">
        <span className="ui-agqc__mark"><Stack size={16} /></span>
        <span className="ui-agqc__title" title={title}>{title}</span>
        {name && <span className="ui-agqc__name">{name}</span>}
        <span className="ui-agqc__pill" data-status={state} role={state === 'limited' ? 'status' : undefined} title={state === 'limited' ? activeLimit?.label : undefined}>
          {(state === 'live' || state === 'limited') && <span className="ui-agqc__pulse" aria-hidden="true" />}
          {state === 'limited' ? (
            <>
              {labels.rateLimited} · <span className="ui-agqc__num">{left < 100 ? `${left}s` : took(left * 1000)}</span>
            </>
          ) : (
            pillText
          )}
        </span>
        <span className="ui-agqc__tools">
          {onPausedChange && (
            <button type="button" className="ui-agqc__btn" aria-pressed={paused} aria-label={paused ? labels.live : labels.paused} onClick={() => onPausedChange(!paused)}>
              {paused ? <Play size={14} /> : <Pause size={14} />}
              <span className="ui-agqc__btn-text">{paused ? labels.live : labels.paused}</span>
            </button>
          )}
          {onConcurrencyChange && (
            <span className="ui-agqc__stepper" role="group" aria-label={labels.workers}>
              <button type="button" aria-label={labels.workers} disabled={!canLess} onClick={() => canLess && onConcurrencyChange(workers.length - 1)}>
                <Minus size={14} />
              </button>
              <output className="ui-agqc__stepper-value" aria-live="polite">{workers.length}</output>
              <button type="button" aria-label={labels.workers} disabled={!canMore} onClick={() => canMore && onConcurrencyChange(workers.length + 1)}>
                <Plus size={14} />
              </button>
            </span>
          )}
        </span>
      </header>

      <div ref={cardRef} className="ui-agqc__card">
        <dl className="ui-agqc__stats">
          {stats.map((s) => (
            <div key={s.label} className="ui-agqc__stat" data-kind={s.kind}>
              <dt>{s.label}</dt>
              <dd>{s.value}</dd>
            </div>
          ))}
        </dl>

        <div className="ui-agqc__flow" style={{ height: L.height }} data-narrow={L.narrow ? '' : undefined}>
          <span className="ui-agqc__col-head" style={at(L.heads.queue)}>
            <span>{labels.queue}</span>
            <b>{num('depth', queuedAll.length, int)}</b>
            {hiddenCount > 0 && L.narrow && <span className="ui-agqc__col-more">+{int(hiddenCount)} {labels.more}</span>}
          </span>
          <span className="ui-agqc__col-head" style={at(L.heads.workers)}>
            <span>{labels.workers}</span>
            <b>{busyCount}/{workers.length}</b>
          </span>

          <span className="ui-agqc__well" aria-hidden="true" data-empty={order.length <= K ? '' : undefined} style={at(L.well)} />
          {order.length === 0 && (
            <span className="ui-agqc__empty" style={at({ x: L.chips[0].x, y: L.chips[0].y, w: L.chips[0].w, h: L.well.y + L.well.h - L.chips[0].y })}>
              {labels.nothingWaiting}
            </span>
          )}
          {overflow && (
            <span
              className="ui-agqc__more"
              aria-hidden="true"
              style={at({
                x: L.grid.x + ((capFull - 3) % L.grid.cols) * L.grid.step,
                y: L.grid.y + Math.floor((capFull - 3) / L.grid.cols) * STEP,
                w: 2 * L.grid.step + SQ,
                h: SQ,
              })}
            >
              +{count(hiddenCount)}
            </span>
          )}

          {workers.map((w, i) => {
            const slot = L.slots[i]
            const busy = runningAll.some((j) => j.worker === w.id)
            const slotLimited = workerHalted(w.id)
            return (
              <div key={w.id} className="ui-agqc__slot" data-busy={busy ? '' : undefined} data-limited={slotLimited ? '' : undefined} style={at(slot)}>
                <span className="ui-agqc__who" style={{ width: L.rail.x - 16 }}>
                  <span className="ui-agqc__logo" data-logo={w.icon ? '' : undefined}>
                    {w.icon ?? initials(w.name)}
                  </span>
                  <span className="ui-agqc__who-text">
                    <span className="ui-agqc__who-name">{w.name}</span>
                    {w.detail && <span className="ui-agqc__who-detail">{w.detail}</span>}
                  </span>
                </span>
                <span className="ui-agqc__rail" style={{ left: L.rail.x, top: L.rail.y, width: L.rail.w, height: L.rail.h }}>
                  {!busy && <span>{slotLimited || paused ? labels.paused : labels.idle}</span>}
                </span>
              </div>
            )
          })}

          {(['done', 'failed'] as const).map((k) => {
            const n = k === 'done' ? doneCount : failedCount
            return (
              <div key={k} className="ui-agqc__bin" data-kind={k} style={at(k === 'done' ? L.done : L.failed)}>
                <span className="ui-agqc__bin-icon">{k === 'done' ? <Check size={12} weight="bold" /> : <X size={12} weight="bold" />}</span>
                <span className="ui-agqc__bin-label">{k === 'done' ? labels.done : labels.failed}</span>
                <span className="ui-agqc__bin-count" title={int(n)}>{num(`bin-${k}`, n, count)}</span>
                <span key={n} className="ui-agqc__bin-hit" aria-hidden="true" />
              </div>
            )
          })}

          <div className="ui-agqc__jobs" role="group" aria-label={`${labels.running} ${busyCount}, ${labels.waiting} ${order.length}`}>
            {placed.map(job)}
          </div>
          {cardOf && pop(cardOf)}
        </div>

        <div className="ui-agqc__chart" style={{ '--ui-agqc-gutter': `${gutter}px` } as CSSProperties}>
          <div className="ui-agqc__chart-head">
            <span className="ui-agqc__chart-title">{labels.depth}</span>
          </div>
          <div className="ui-agqc__plot" role="img" aria-label={summary} style={{ height: plotH }}>
            {[0, ymax].map((v) => (
              <span key={v} className="ui-agqc__grid" data-zero={v === 0 ? '' : undefined} style={{ '--v': v } as CSSProperties} aria-hidden="true">
                {(backpressure === undefined || Math.abs(v - backpressure) > ymax * 0.1) && <span>{int(v)}</span>}
              </span>
            ))}
            <svg className="ui-agqc__svg" width={plotW} height={plotH} viewBox={`0 0 ${plotW} ${plotH}`} aria-hidden="true">
              <defs>
                <linearGradient id={gradId} x1="0" y1="1" x2="0" y2="0">
                  <stop offset="0" className="ui-agqc__stop" data-at="top" />
                  <stop offset="1" className="ui-agqc__stop" data-at="bottom" />
                </linearGradient>
              </defs>
              {bands.map(({ l, x1, x2 }) => (
                <g key={`${l.from}-${l.label ?? ''}`} className="ui-agqc__band">
                  <rect x={x1} y={0} width={Math.max(1, x2 - x1)} height={plotH} />
                </g>
              ))}
              <g className="ui-agqc__scaled" style={{ '--h': plotH } as CSSProperties}>
                <path className="ui-agqc__area" d={area} fill={`url(#${gradId})`} />
                <path className="ui-agqc__line" d={line} />
                {backpressure !== undefined && <line className="ui-agqc__bp" x1={0} x2={plotW} y1={backpressure} y2={backpressure} />}
              </g>
            </svg>
            {backpressure !== undefined && (
              <span className="ui-agqc__bp-label" style={{ '--v': backpressure } as CSSProperties} aria-hidden="true">{backpressure}</span>
            )}
            <span className="ui-agqc__dot" style={{ '--v': queuedAll.length } as CSSProperties} aria-hidden="true" />
          </div>
          <div className="ui-agqc__axis" aria-hidden="true">
            <span>{spanLabel} {labels.ago}</span>
            <span>{labels.now}</span>
          </div>
        </div>
      </div>

      <div className="ui-agqc__sr" aria-live="polite">{announce}</div>
    </div>
  )
}

const css = /* css */ `
@property --ui-agqc-ymax { syntax: "<number>"; inherits: true; initial-value: 10; }
.ui-agqc {
  --aui-radius: var(--radius, 8px);
  --aui-radius-lg: calc(var(--radius, 8px) + 2px);
  --aui-radius-sm: max(4px, calc(var(--radius, 8px) - 2px));
  --aui-font: inherit;
  --aui-surface: var(--surface);
  --aui-overlay: var(--overlay, var(--surface));
  --aui-subtle: color-mix(in oklab, var(--foreground) 4%, transparent);
  --aui-ink: var(--foreground);
  --aui-muted: var(--muted);
  --aui-placeholder: color-mix(in oklab, var(--muted) 72%, transparent);
  --aui-border: var(--border);
  --aui-focus: var(--focus, var(--foreground));
  --ui-agqc-card: var(--surface);
  --ui-agqc-strong: var(--foreground);
  --ui-agqc-ink: var(--foreground);
  --ui-agqc-text: color-mix(in oklab, var(--foreground) 78%, transparent);
  --ui-agqc-muted: var(--muted);
  --ui-agqc-faint: color-mix(in oklab, var(--muted) 70%, transparent);
  --ui-agqc-line: var(--border);
  --ui-agqc-grid: color-mix(in oklab, var(--foreground) 5%, transparent);
  --ui-agqc-fill: color-mix(in oklab, var(--foreground) 6%, transparent);
  --ui-agqc-wash: color-mix(in oklab, var(--surface-secondary, var(--foreground)) 42%, var(--surface));
  --ui-agqc-accent: var(--foreground);
  --ui-agqc-depth: var(--chart-1);
  --ui-agqc-depth-fill: color-mix(in oklab, var(--chart-1) 18%, transparent);
  --ui-agqc-depth-fade: color-mix(in oklab, var(--chart-1) 0%, transparent);
  --ui-agqc-agent-bg: color-mix(in oklab, var(--chart-6) 12%, transparent);
  --ui-agqc-agent-edge: color-mix(in oklab, var(--chart-6) 32%, transparent);
  --ui-agqc-agent-ink: color-mix(in oklab, var(--chart-6) 62%, var(--foreground));
  --ui-agqc-agent-sq: color-mix(in oklab, var(--chart-6) 28%, transparent);
  --ui-agqc-llm-bg: color-mix(in oklab, var(--chart-1) 12%, transparent);
  --ui-agqc-llm-edge: color-mix(in oklab, var(--chart-1) 32%, transparent);
  --ui-agqc-llm-ink: color-mix(in oklab, var(--chart-1) 68%, var(--foreground));
  --ui-agqc-llm-sq: color-mix(in oklab, var(--chart-1) 26%, transparent);
  --ui-agqc-tool-bg: color-mix(in oklab, var(--chart-5) 12%, transparent);
  --ui-agqc-tool-edge: color-mix(in oklab, var(--chart-5) 32%, transparent);
  --ui-agqc-tool-ink: color-mix(in oklab, var(--chart-5) 62%, var(--foreground));
  --ui-agqc-tool-sq: color-mix(in oklab, var(--chart-5) 26%, transparent);
  --ui-agqc-human-bg: color-mix(in oklab, var(--foreground) 6%, transparent);
  --ui-agqc-human-edge: color-mix(in oklab, var(--foreground) 16%, transparent);
  --ui-agqc-human-ink: var(--muted);
  --ui-agqc-human-sq: color-mix(in oklab, var(--foreground) 14%, transparent);
  --ui-agqc-error-bg: color-mix(in oklab, var(--danger) 10%, transparent);
  --ui-agqc-error-edge: color-mix(in oklab, var(--danger) 34%, transparent);
  --ui-agqc-error-ink: var(--danger);
  --ui-agqc-ok-bg: color-mix(in oklab, var(--success) 12%, transparent);
  --ui-agqc-ok-edge: color-mix(in oklab, var(--success) 32%, transparent);
  --ui-agqc-ok-fg: var(--success);
  --ui-agqc-warn: var(--warning);
  --ui-agqc-warn-bg: color-mix(in oklab, var(--warning) 16%, transparent);
  --ui-agqc-warn-soft: color-mix(in oklab, var(--warning) 8%, transparent);
  --ui-agqc-warn-edge: color-mix(in oklab, var(--warning) 36%, transparent);
  --ui-agqc-warn-fg: var(--warning);
  --ui-agqc-brand-bg: color-mix(in oklab, var(--foreground) 8%, transparent);
  --ui-agqc-brand-fg: var(--foreground);
  --ui-agqc-focus: var(--focus, var(--foreground));
  --ui-agqc-shadow: 0 0 0 0.5px var(--border), 0 1px 2px color-mix(in oklab, var(--foreground) 6%, transparent);
  --ui-agqc-lift: 0 0 0 0.5px var(--border), 0 1px 2px color-mix(in oklab, var(--foreground) 5%, transparent);
  --ui-agqc-pop: 0 0 0 0.5px var(--border), 0 12px 28px -10px color-mix(in oklab, var(--foreground) 18%, transparent);
  --ui-agqc-ease: cubic-bezier(0.32, 0.72, 0.24, 1);
  position: relative;
  box-sizing: border-box;
  width: 100%;
  border-radius: calc(var(--aui-radius-lg, 10px) + 2px);
  background: var(--ui-agqc-card);
  box-shadow: var(--ui-agqc-shadow);
  container: ui-agqc / inline-size;
  font-family: inherit;
  font-size: 13px;
  font-weight: 400;
  line-height: 20px;
  color: var(--ui-agqc-text);
  text-align: left;
  -webkit-font-smoothing: antialiased;
}
.ui-agqc[data-embedded] { background: transparent; box-shadow: none; border-radius: 0; }
.ui-agqc[data-embedded] .ui-agqc__head { display: none; }
.ui-agqc *, .ui-agqc *::before, .ui-agqc *::after { box-sizing: border-box; }
:where(.ui-agqc) button { margin: 0; padding: 0; border: 0; background: none; font: inherit; color: inherit; text-align: inherit; cursor: pointer; -webkit-tap-highlight-color: transparent; }
.ui-agqc button:focus-visible { outline: 2px solid var(--ui-agqc-focus); outline-offset: 2px; }
.ui-agqc :is(p, dl, dd) { margin: 0; }
.ui-agqc svg { display: block; flex: none; }
.ui-agqc__num { font-variant-numeric: tabular-nums; }
.ui-agqc__head { display: flex; align-items: center; gap: 8px; min-width: 0; min-height: 52px; padding: 12px 12px 12px 16px; border-bottom: 0.5px solid var(--ui-agqc-line); }
.ui-agqc__mark { display: grid; flex: none; place-items: center; width: 16px; color: var(--ui-agqc-accent); }
.ui-agqc__title { flex: 0 1 auto; min-width: 0; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; font-weight: 500; color: var(--ui-agqc-strong); }
.ui-agqc__name { flex: 0 1000 auto; min-width: 0; overflow: hidden; color: var(--ui-agqc-muted); white-space: nowrap; text-overflow: ellipsis; }
.ui-agqc__pill { display: inline-flex; flex: none; align-items: center; gap: 5px; height: 20px; padding: 0 7px; border-radius: var(--aui-radius-sm, 5px); background: var(--ui-agqc-fill); font-size: 12px; font-weight: 500; line-height: 16px; color: var(--ui-agqc-text); white-space: nowrap; }
.ui-agqc__pill > svg { width: 11px; height: 11px; }
.ui-agqc__pill:is([data-status="live"], [data-status="running"]) { background: var(--ui-agqc-brand-bg); color: var(--ui-agqc-brand-fg); }
.ui-agqc__pill[data-status="limited"], .ui-agqc__pill[data-status="halted"] { background: var(--ui-agqc-warn-bg); color: var(--ui-agqc-warn-fg); }
.ui-agqc__pill[data-status="done"] { background: var(--ui-agqc-ok-bg); color: var(--ui-agqc-ok-fg); }
.ui-agqc__pill[data-status="failed"] { background: var(--ui-agqc-error-bg); color: var(--ui-agqc-error-ink); }
.ui-agqc__pulse { width: 6px; height: 6px; border-radius: 50%; background: currentColor; animation: ui-agqc-pulse 1400ms ease-in-out infinite; }
.ui-agqc__tools { display: flex; flex: none; align-items: center; gap: 8px; margin-left: auto; }
.ui-agqc__btn { display: inline-flex; align-items: center; gap: 6px; height: 28px; padding: 0 10px 0 6px; border-radius: calc(var(--aui-radius, 7px) - 1px); background: var(--surface); box-shadow: var(--ui-agqc-shadow); font-size: 13px; font-weight: 500; color: var(--ui-agqc-strong); white-space: nowrap; transition: background-color 140ms ease; }
.ui-agqc__btn > svg { width: 16px; height: 16px; color: var(--muted); }
.ui-agqc__btn:hover { background: color-mix(in oklab, var(--foreground) 3%, var(--surface)); }
.ui-agqc__btn[aria-pressed="true"], .ui-agqc__btn[aria-pressed="true"] > svg { color: var(--ui-agqc-brand-fg); }
.ui-agqc__btn[data-kind="danger"], .ui-agqc__btn[data-kind="danger"] > svg { color: var(--ui-agqc-error-ink); }
.ui-agqc__stepper { display: inline-flex; align-items: center; height: 28px; padding: 0 2px 0 10px; border-radius: calc(var(--aui-radius, 7px) - 1px); background: var(--surface); box-shadow: var(--ui-agqc-shadow); }
.ui-agqc__stepper button { display: grid; place-items: center; width: 24px; height: 24px; border-radius: calc(var(--aui-radius-sm, 5px) - 1px); color: var(--muted); transition: background-color 140ms ease, color 140ms ease; }
.ui-agqc__stepper button:hover:not(:disabled) { background: var(--ui-agqc-fill); color: var(--ui-agqc-ink); }
.ui-agqc__stepper button:disabled { opacity: 0.35; cursor: default; }
.ui-agqc__stepper-value { min-width: 18px; font-weight: 500; color: var(--ui-agqc-strong); text-align: center; font-variant-numeric: tabular-nums; }
.ui-agqc__card { position: relative; }
.ui-agqc__stats { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); padding: 12px 0; border-bottom: 0.5px solid var(--ui-agqc-line); }
.ui-agqc__stat { display: grid; gap: 2px; min-width: 0; padding: 0 16px; }
.ui-agqc__stat:first-child { padding-left: 22px; }
.ui-agqc__stat:last-child { padding-right: 10px; }
.ui-agqc__stat + .ui-agqc__stat { border-left: 0.5px solid var(--ui-agqc-line); }
.ui-agqc__stat dt { overflow: hidden; font-size: 10px; font-weight: 500; line-height: 16px; letter-spacing: 0.5px; text-transform: uppercase; color: var(--ui-agqc-muted); white-space: nowrap; text-overflow: ellipsis; }
.ui-agqc__stat dd { overflow: hidden; font-size: 15px; font-weight: 500; line-height: 24px; color: var(--ui-agqc-ink); white-space: nowrap; font-variant-numeric: tabular-nums; transition: color 240ms ease; }
.ui-agqc__of { margin-left: 1px; font-size: 13px; color: var(--ui-agqc-muted); }
.ui-agqc__stat[data-kind="warn"] dd { color: var(--ui-agqc-warn-fg); }
.ui-agqc__flow { position: relative; border-bottom: 0.5px solid var(--ui-agqc-line); transition: height 360ms var(--ui-agqc-ease); }
.ui-agqc__flow > *, .ui-agqc__jobs > * { position: absolute; top: 0; left: 0; }
.ui-agqc__col-head { display: flex; align-items: center; gap: 6px; font-size: 10px; font-weight: 500; line-height: 16px; letter-spacing: 0.5px; text-transform: uppercase; color: var(--ui-agqc-muted); white-space: nowrap; }
.ui-agqc__col-head > b { font-size: 12px; font-weight: 500; letter-spacing: 0; color: var(--ui-agqc-ink); font-variant-numeric: tabular-nums; }
.ui-agqc__col-head > .ui-agqc__col-more { margin-left: auto; letter-spacing: 0.3px; }
.ui-agqc__well { border-radius: 6px; background: var(--ui-agqc-wash); box-shadow: inset 0 0 0 0.5px var(--ui-agqc-line); transition: opacity 300ms var(--ui-agqc-ease); }
.ui-agqc__well[data-empty] { opacity: 0; }
.ui-agqc__empty { display: grid; place-items: center; color: var(--ui-agqc-faint); animation: ui-agqc-calm 300ms var(--ui-agqc-ease) both; }
@keyframes ui-agqc-calm { from { opacity: 0; } }
.ui-agqc__more { display: grid; place-items: center; border-radius: 3px; background: var(--ui-agqc-fill); font-size: 10px; font-weight: 500; line-height: 12px; color: var(--ui-agqc-text); font-variant-numeric: tabular-nums; }
.ui-agqc__slot { border-radius: 8px; background: var(--ui-agqc-wash); box-shadow: inset 0 0 0 0.5px var(--ui-agqc-line); transition: translate ${MOVE}ms var(--ui-agqc-ease), width ${MOVE}ms var(--ui-agqc-ease), background-color 300ms ease, box-shadow 300ms ease; animation: ui-agqc-slot 420ms var(--ui-agqc-ease) both; }
.ui-agqc__slot[data-limited] { background: var(--ui-agqc-warn-soft); box-shadow: inset 0 0 0 0.5px var(--ui-agqc-warn-edge); }
.ui-agqc__who { position: absolute; top: 0; bottom: 0; left: 8px; display: flex; align-items: center; gap: 8px; min-width: 0; }
.ui-agqc__logo { display: grid; flex: none; place-items: center; width: 24px; height: 24px; overflow: hidden; border-radius: 5px; background: var(--ui-agqc-card); box-shadow: var(--ui-agqc-shadow); font-size: 10px; font-weight: 500; color: var(--ui-agqc-text); }
.ui-agqc__logo > svg { width: 14px; height: 14px; }
.ui-agqc__who-text { display: grid; min-width: 0; }
.ui-agqc__who-name { overflow: hidden; font-weight: 500; line-height: 16px; color: var(--ui-agqc-strong); white-space: nowrap; text-overflow: ellipsis; }
.ui-agqc__who-detail { overflow: hidden; font-size: 11px; line-height: 14px; color: var(--ui-agqc-muted); white-space: nowrap; text-overflow: ellipsis; }
.ui-agqc__rail { position: absolute; display: flex; align-items: center; padding-left: 10px; border-radius: 6px; box-shadow: inset 0 0 0 0.5px var(--ui-agqc-line); background-image: repeating-linear-gradient(-45deg, transparent 0 6px, color-mix(in oklab, var(--foreground) 4%, transparent) 6px 6.5px); font-size: 12px; color: var(--ui-agqc-faint); }
.ui-agqc__bin { z-index: 1; padding: 12px; border-radius: 8px; background: var(--ui-agqc-wash); box-shadow: inset 0 0 0 0.5px var(--ui-agqc-line); }
.ui-agqc__bin-icon { position: absolute; top: 12px; left: 12px; display: grid; place-items: center; width: 20px; height: 20px; border-radius: 5px; }
.ui-agqc__bin[data-kind="done"] .ui-agqc__bin-icon { background: var(--ui-agqc-ok-bg); color: var(--ui-agqc-ok-fg); }
.ui-agqc__bin[data-kind="failed"] .ui-agqc__bin-icon { background: var(--ui-agqc-error-bg); color: var(--ui-agqc-error-ink); }
.ui-agqc__bin-label { position: absolute; top: 14px; left: 40px; font-size: 10px; font-weight: 500; line-height: 16px; letter-spacing: 0.5px; text-transform: uppercase; color: var(--ui-agqc-muted); }
.ui-agqc__bin-count { position: absolute; left: 12px; bottom: 10px; font-size: 20px; font-weight: 500; line-height: 28px; letter-spacing: -0.2px; color: var(--ui-agqc-strong); }
.ui-agqc__bin[data-kind="failed"] .ui-agqc__bin-count { color: var(--ui-agqc-error-ink); }
.ui-agqc__bin-hit { position: absolute; top: 12px; left: 12px; width: 20px; height: 20px; border-radius: 5px; opacity: 0; pointer-events: none; animation: ui-agqc-hit 700ms ease-out 380ms; }
.ui-agqc__bin[data-kind="done"] .ui-agqc__bin-hit { box-shadow: 0 0 0 1px var(--ui-agqc-ok-fg); }
.ui-agqc__bin[data-kind="failed"] .ui-agqc__bin-hit { box-shadow: 0 0 0 1px var(--ui-agqc-error-ink); }
.ui-agqc__flow[data-narrow] .ui-agqc__who-text { display: none; }
.ui-agqc__flow[data-narrow] .ui-agqc__bin { display: flex; align-items: center; gap: 6px; padding: 0 10px; }
.ui-agqc__flow[data-narrow] :is(.ui-agqc__bin-icon, .ui-agqc__bin-label, .ui-agqc__bin-count) { position: static; }
.ui-agqc__flow[data-narrow] .ui-agqc__bin-icon { flex: none; }
.ui-agqc__flow[data-narrow] .ui-agqc__bin-count { order: 1; font-size: 15px; line-height: 24px; }
.ui-agqc__flow[data-narrow] .ui-agqc__bin-label { order: 2; min-width: 0; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
.ui-agqc__flow[data-narrow] .ui-agqc__bin-hit { top: 10px; }
.ui-agqc [data-kind] { --k-bg: var(--ui-agqc-agent-bg); --k-edge: var(--ui-agqc-agent-edge); --k-ink: var(--ui-agqc-agent-ink); --k-sq: var(--ui-agqc-agent-sq); }
.ui-agqc [data-kind="llm"] { --k-bg: var(--ui-agqc-llm-bg); --k-edge: var(--ui-agqc-llm-edge); --k-ink: var(--ui-agqc-llm-ink); --k-sq: var(--ui-agqc-llm-sq); }
.ui-agqc [data-kind="tool"] { --k-bg: var(--ui-agqc-tool-bg); --k-edge: var(--ui-agqc-tool-edge); --k-ink: var(--ui-agqc-tool-ink); --k-sq: var(--ui-agqc-tool-sq); }
.ui-agqc [data-kind="human"] { --k-bg: var(--ui-agqc-human-bg); --k-edge: var(--ui-agqc-human-edge); --k-ink: var(--ui-agqc-human-ink); --k-sq: var(--ui-agqc-human-sq); }
.ui-agqc .ui-agqc__job:is([data-status="failed"], [data-flash]) { --k-bg: var(--ui-agqc-error-bg); --k-edge: var(--ui-agqc-error-edge); --k-ink: var(--ui-agqc-error-ink); --k-sq: var(--ui-agqc-error-edge); }
.ui-agqc .ui-agqc__job[data-status="done"] { --k-bg: var(--ui-agqc-ok-bg); --k-edge: var(--ui-agqc-ok-edge); --k-ink: var(--ui-agqc-ok-fg); --k-sq: var(--ui-agqc-ok-edge); }
.ui-agqc__jobs { position: absolute; inset: 0; pointer-events: none; }
.ui-agqc__job {
  z-index: 1;
  display: block;
  overflow: hidden;
  border-radius: 6px;
  background: var(--ui-agqc-card);
  box-shadow: var(--ui-agqc-lift);
  pointer-events: auto;
  transition: translate ${MOVE}ms var(--ui-agqc-ease), width ${MOVE}ms var(--ui-agqc-ease), height ${MOVE}ms var(--ui-agqc-ease), border-radius ${MOVE}ms var(--ui-agqc-ease), background-color 320ms ease, box-shadow 320ms ease, opacity 240ms ease;
  animation: ui-agqc-drop 520ms var(--ui-agqc-ease) backwards;
}
.ui-agqc__job:hover, .ui-agqc__job[data-open] { z-index: 2; box-shadow: 0 0 0 1px var(--k-edge), 0 2px 8px -2px color-mix(in oklab, var(--foreground) 10%, transparent); }
.ui-agqc__job:focus-visible { z-index: 2; outline: 2px solid var(--ui-agqc-focus); outline-offset: 2px; }
.ui-agqc__job[data-flash] { background: var(--k-bg); box-shadow: 0 0 0 0.5px var(--k-edge); animation: ui-agqc-drop 520ms var(--ui-agqc-ease) backwards, ui-agqc-shake 420ms ease; }
.ui-agqc__job[data-shape="square"] { border-radius: 3px; background: var(--k-sq); box-shadow: inset 0 0 0 0.5px var(--k-edge); }
.ui-agqc__job[data-shape="square"][data-priority="high"] { box-shadow: inset 0 0 0 1.5px var(--ui-agqc-warn); }
.ui-agqc__job[data-shape="square"]:hover { box-shadow: inset 0 0 0 1px var(--k-ink); }
.ui-agqc__job[data-shape="bar"] { box-shadow: var(--ui-agqc-shadow); }
.ui-agqc__job[data-shape="gone"] { z-index: 0; border-radius: 3px; background: var(--k-sq); box-shadow: none; opacity: 0; pointer-events: none; transition: translate 480ms var(--ui-agqc-ease), width 480ms var(--ui-agqc-ease), height 480ms var(--ui-agqc-ease), border-radius 480ms ease, background-color 200ms ease, opacity 200ms ease 220ms; }
.ui-agqc__job-in { position: absolute; inset: 0; display: flex; align-items: center; gap: 6px; padding: 0 8px 0 6px; white-space: nowrap; opacity: 1; transition: opacity 200ms ease 160ms; }
.ui-agqc__job:is([data-shape="square"], [data-shape="gone"]) .ui-agqc__job-in { opacity: 0; transition: opacity 120ms ease; }
.ui-agqc__job-icon { display: grid; flex: none; place-items: center; width: 18px; height: 18px; border-radius: 4px; background: var(--k-bg); color: var(--k-ink); }
.ui-agqc__job-icon[data-logo] { background: var(--ui-agqc-card); box-shadow: var(--ui-agqc-shadow); }
.ui-agqc__job-icon > svg { width: 11px; height: 11px; }
.ui-agqc__job-name { flex: 1; min-width: 0; overflow: hidden; font-size: 12px; line-height: 16px; color: var(--ui-agqc-ink); text-overflow: ellipsis; }
.ui-agqc__badge { display: inline-flex; flex: none; align-items: center; gap: 2px; height: 16px; padding: 0 4px; border-radius: calc(var(--aui-radius-sm, 5px) - 1px); font-size: 10px; font-weight: 500; line-height: 16px; font-variant-numeric: tabular-nums; }
.ui-agqc__badge[data-kind="high"] { width: 16px; padding: 0; justify-content: center; background: var(--ui-agqc-warn-bg); color: var(--ui-agqc-warn-fg); }
.ui-agqc__badge[data-kind="retry"] { background: var(--ui-agqc-error-bg); color: var(--ui-agqc-error-ink); }
.ui-agqc__job-meta { flex: none; font-size: 12px; line-height: 16px; color: var(--ui-agqc-muted); font-variant-numeric: tabular-nums; }
.ui-agqc__job[data-status="running"] .ui-agqc__job-meta { color: var(--k-ink); }
.ui-agqc__job[data-flash] :is(.ui-agqc__job-name, .ui-agqc__job-meta) { color: var(--ui-agqc-error-ink); }
.ui-agqc__fill { position: absolute; inset: 0; border-radius: inherit; background: var(--k-bg); box-shadow: inset 0 0 0 0.5px var(--k-edge); clip-path: inset(0 calc((1 - var(--p)) * 100%) 0 0 round 6px); opacity: 0; transition: clip-path 260ms linear, opacity 300ms var(--ui-agqc-ease), background-color 300ms ease, box-shadow 300ms ease; }
.ui-agqc__job[data-shape="bar"] .ui-agqc__fill { opacity: 1; animation: ui-agqc-grow 560ms var(--ui-agqc-ease) backwards; }
.ui-agqc__job[data-shape="bar"] .ui-agqc__fill::before { transition: opacity 300ms var(--ui-agqc-ease); content: ""; position: absolute; inset: 0; background: linear-gradient(90deg, transparent, var(--ui-agqc-card), transparent) -60px 0 / 60px 100% no-repeat; opacity: 0.7; animation: ui-agqc-sweep 1500ms linear infinite; }
.ui-agqc__job[data-shape="bar"] .ui-agqc__fill::after { transition: opacity 300ms var(--ui-agqc-ease); content: ""; position: absolute; top: 0; bottom: 0; left: calc(var(--p) * 100% - 28px); width: 28px; background: linear-gradient(90deg, transparent, var(--k-edge)); animation: ui-agqc-glow 1100ms ease-in-out infinite alternate; }
.ui-agqc__job[data-shape="gone"] .ui-agqc__fill { opacity: 0; }
.ui-agqc__pop { z-index: 5; display: grid; grid-template-columns: minmax(0, 1fr); gap: 10px; padding: 12px; border-radius: var(--aui-radius-lg, 10px); background: var(--overlay, var(--ui-agqc-card)); box-shadow: var(--ui-agqc-pop); pointer-events: none; transition: translate ${MOVE}ms var(--ui-agqc-ease); animation: ui-agqc-open 200ms var(--ui-agqc-ease) both; }
.ui-agqc__pop[data-pinned] { pointer-events: auto; }
.ui-agqc__pop-head { display: flex; align-items: center; min-width: 0; }
.ui-agqc__pop-icon { display: grid; flex: none; place-items: center; width: 28px; height: 28px; margin-right: 10px; border-radius: 6px; background: var(--k-bg); color: var(--k-ink); }
.ui-agqc__pop-icon[data-logo] { background: var(--ui-agqc-card); box-shadow: var(--ui-agqc-shadow); }
.ui-agqc__pop-icon > svg { width: 15px; height: 15px; }
.ui-agqc__pop-who { display: grid; flex: 1; min-width: 0; }
.ui-agqc__pop-name { font-weight: 500; line-height: 18px; color: var(--ui-agqc-strong); overflow-wrap: anywhere; }
.ui-agqc__pop-kind { display: -webkit-box; overflow: hidden; font-size: 12px; line-height: 16px; color: var(--ui-agqc-muted); overflow-wrap: anywhere; -webkit-line-clamp: 2; -webkit-box-orient: vertical; }
.ui-agqc__pop-head .ui-agqc__pill { height: 18px; margin-left: 8px; padding: 0 6px; border-radius: calc(var(--aui-radius-sm, 5px) - 1px); font-size: 11px; }
.ui-agqc__close { display: grid; flex: none; place-items: center; width: 28px; height: 28px; margin: -2px -4px 0 4px; border-radius: calc(var(--aui-radius-sm, 5px) + 1px); color: var(--ui-agqc-muted); transition: background-color 140ms ease, color 140ms ease; }
.ui-agqc__close:hover { background: var(--ui-agqc-fill); color: var(--ui-agqc-ink); }
.ui-agqc__pop-stats { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px 12px; padding: 8px 10px; border-radius: 8px; background: color-mix(in oklab, var(--foreground) 3%, transparent); box-shadow: inset 0 0 0 0.5px var(--ui-agqc-line); }
.ui-agqc__pop-stats dt { font-size: 10px; font-weight: 500; line-height: 16px; letter-spacing: 0.5px; text-transform: uppercase; color: var(--ui-agqc-muted); }
.ui-agqc__pop-stats dd { overflow: hidden; font-weight: 500; line-height: 20px; color: var(--ui-agqc-ink); white-space: nowrap; text-overflow: ellipsis; font-variant-numeric: tabular-nums; }
.ui-agqc__pop-error { display: flex; gap: 8px; min-width: 0; overflow-wrap: anywhere; padding: 8px 10px; border-radius: 8px; background: var(--ui-agqc-error-bg); font-size: 12px; line-height: 16px; color: var(--ui-agqc-error-ink); }
.ui-agqc__pop-error > svg { margin-top: 2px; }
.ui-agqc__pop-actions { display: flex; gap: 8px; }
.ui-agqc__pop-actions .ui-agqc__btn > svg { width: 12px; height: 12px; }
.ui-agqc__chart { padding: 12px 16px 12px; }
.ui-agqc__chart-head { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; line-height: 16px; white-space: nowrap; }
.ui-agqc__chart-title { font-size: 10px; font-weight: 500; letter-spacing: 0.5px; text-transform: uppercase; color: var(--ui-agqc-muted); }
.ui-agqc__plot { position: relative; margin-left: var(--ui-agqc-gutter, ${GUTTER}px); }
.ui-agqc__grid { position: absolute; left: -6px; right: 0; bottom: calc(var(--v) / var(--ui-agqc-ymax) * 100%); height: 0.5px; background: var(--ui-agqc-line); }
.ui-agqc__grid[data-zero] { background: color-mix(in oklab, var(--foreground) 14%, transparent); }
.ui-agqc__grid > span { position: absolute; right: calc(100% + 8px); top: -8px; font-size: 12px; line-height: 16px; color: var(--ui-agqc-muted); font-variant-numeric: tabular-nums; }
.ui-agqc__svg { position: absolute; inset: 0; overflow: hidden; }
.ui-agqc__scaled { transform: translateY(calc(var(--h) * 1px)) scaleY(calc(-1 * var(--h) / var(--ui-agqc-ymax))); }
.ui-agqc__stop[data-at="top"] { stop-color: var(--ui-agqc-depth-fade); }
.ui-agqc__stop[data-at="bottom"] { stop-color: var(--ui-agqc-depth-fill); }
.ui-agqc__line { fill: none; stroke: var(--ui-agqc-depth); stroke-width: 1.25; stroke-linejoin: round; vector-effect: non-scaling-stroke; }
.ui-agqc__bp { stroke: var(--ui-agqc-faint); stroke-width: 1; stroke-dasharray: 3 3; vector-effect: non-scaling-stroke; }
.ui-agqc__band rect { fill: var(--ui-agqc-warn); opacity: 0.12; }
.ui-agqc__bp-label { position: absolute; right: calc(100% + 8px); bottom: calc(var(--v) / var(--ui-agqc-ymax) * 100% - 8px); font-size: 12px; line-height: 16px; color: var(--ui-agqc-muted); font-variant-numeric: tabular-nums; }
.ui-agqc__dot { position: absolute; right: -3px; bottom: calc(var(--v) / var(--ui-agqc-ymax) * 100% - 3px); width: 6px; height: 6px; border-radius: 50%; background: var(--ui-agqc-depth); box-shadow: 0 0 0 3px var(--ui-agqc-card); }
.ui-agqc[data-state="live"] .ui-agqc__dot::after, .ui-agqc[data-state="limited"] .ui-agqc__dot::after { content: ""; position: absolute; inset: 0; border-radius: 50%; background: var(--ui-agqc-depth); animation: ui-agqc-ping 1600ms ease-out infinite; }
.ui-agqc__axis { display: flex; justify-content: space-between; margin: 6px 0 0 var(--ui-agqc-gutter, ${GUTTER}px); font-size: 12px; line-height: 16px; color: var(--ui-agqc-muted); font-variant-numeric: tabular-nums; }
.ui-agqc__sr { position: absolute; width: 1px; height: 1px; margin: -1px; padding: 0; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; border: 0; }
@container ui-agqc (max-width: 560px) {
  .ui-agqc__head { flex-wrap: wrap; row-gap: 10px; padding: 10px 12px; }
  .ui-agqc__name { display: none; }
  .ui-agqc__stats { grid-template-columns: repeat(2, minmax(0, 1fr)); row-gap: 10px; padding: 10px 0; }
  .ui-agqc__stat { padding: 0 12px; }
  .ui-agqc__stat:nth-child(odd) { padding-left: 16px; }
  .ui-agqc__stat:nth-child(3) { border-left: 0; }
  .ui-agqc__chart { padding: 12px 12px 10px; }
}
@container ui-agqc (max-width: 440px) {
  .ui-agqc__tools { flex-basis: 100%; justify-content: space-between; margin-left: 0; }
  .ui-agqc__stat dt { letter-spacing: 0.2px; }
}
.ui-agqc__job[data-halted] .ui-agqc__fill { opacity: 0.55; }
.ui-agqc__job[data-halted] .ui-agqc__fill::before, .ui-agqc__job[data-halted] .ui-agqc__fill::after { opacity: 0; animation-play-state: paused; }
.ui-agqc__job[data-halted] .ui-agqc__job-meta { color: var(--ui-agqc-muted); }
@keyframes ui-agqc-drop { from { opacity: 0; transform: translateY(-10px) scale(0.6); } }
@keyframes ui-agqc-slot { from { opacity: 0; transform: translateX(-8px); } }
@keyframes ui-agqc-grow { from { clip-path: inset(0 100% 0 0 round 6px); } }
@keyframes ui-agqc-sweep { to { background-position: calc(100% + 60px) 0; } }
@keyframes ui-agqc-glow { from { filter: opacity(0.45); } to { filter: opacity(1); } }
@keyframes ui-agqc-pulse { 50% { opacity: 0.35; } }
@keyframes ui-agqc-open { from { opacity: 0; transform: translateY(-4px) scale(0.97); } }
@keyframes ui-agqc-hit { 0% { opacity: 0.45; transform: scale(1); } 100% { opacity: 0; transform: scale(1.45); } }
@keyframes ui-agqc-ping { 0% { opacity: 0.5; transform: scale(1); } 100% { opacity: 0; transform: scale(2.8); } }
@keyframes ui-agqc-shake { 20% { transform: translateX(-3px); } 40% { transform: translateX(3px); } 60% { transform: translateX(-2px); } 80% { transform: translateX(1px); } }
@media (prefers-reduced-motion: reduce) {
  .ui-agqc *, .ui-agqc *::before, .ui-agqc *::after { animation: none !important; transition: none !important; }
}
`
