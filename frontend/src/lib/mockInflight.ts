/**
 * Phase-1 fake snapshot for Overview「实时进行中」.
 * Shaped like cli2api runtime concepts (not a parallel control plane):
 *   jobs      ≈ in-flight / waiting proxy requests
 *   workers   ≈ account slots currently (or recently) hitting upstream
 *   rateLimits≈ cooldown / provider rate-limit windows
 *   depth     ≈ recent waiting + concurrency approx
 *   totals    ≈ session Done/Failed while viewing (mock tick; live bumps)
 * Stage-3 swaps workers/rateLimits/sticky/queue onto /api/live (LiveWorker,
 * LiveRateLimit, LiveSticky, LiveJob); depth stays client-rolled. Do not invent
 * a parallel /api/inflight*.
 *
 * Live loop (`tickMockInflight`) mirrors public AffectUI Motion prose:
 * progress fill, queue→worker handoff, DONE/FAILED stack, depth scroll —
 * Stage-1 only; real /api/live projection stays read-only.
 */

export type MockJobKind = 'chat' | 'embed' | 'tools' | 'other'
export type MockJobStatus = 'queued' | 'running' | 'done' | 'failed'

export type MockJobPriority = 'high' | 'normal' | 'low'

export type MockInflightJob = {
  id: string
  /** Short label — model / route style. */
  name: string
  kind: MockJobKind
  status: MockJobStatus
  /** Account id holding the request when running. */
  accountId?: string
  progress: number
  /** Elapsed wait or run, ms. */
  elapsedMs: number
  /** Wall-clock start (epoch ms) for live/mock elapsed ticking. */
  startedAtMs?: number
  /** Sticky session hint (affinity hit). */
  sticky?: boolean
  /** AffectUI priority — high lands at back then glides past. */
  priority?: MockJobPriority
  /** Retry attempt count when failed job slides back to queue. */
  attempt?: number
}

/** One occupied concurrency cell on an account slot bar. */
export type MockSlotCell = {
  id: string
  /** Requested model / short label. */
  name: string
  /** Wall-clock when this request entered the slot (Bind). */
  startedAtMs: number
  elapsedMs: number
}

export type MockAccountSlot = {
  id: string
  /** Account display name. */
  name: string
  provider: string
  /** Busy with an upstream call. */
  busy: boolean
  /** Internal mock completion driver (0–1). Busy UI uses indeterminate shimmer, not this %. */
  progress: number
  jobId?: string
  /** Slot paused by an active cooldown / rate-limit. */
  rateLimited?: boolean
  /**
   * Account max concurrency (max_inflight). Slot bar divides into this many
   * equal cells. Clamp in UI (min 1).
   */
  concurrency?: number
  /** Current occupied concurrency cells (in_flight). */
  inFlight?: number
  /** Per-cell model + elapsed (length ≤ inFlight). */
  cells?: MockSlotCell[]
}

export type MockRateLimitWindow = {
  id: string
  label: string
  /** Remaining cooldown / rate-limit window in ms. */
  remainingMs: number
  /** Depth-chart band: fraction along [0,1] series. */
  from: number
  until: number
}

/** One job that just landed on Done or Failed this tick (GSAP fly-out routing). */
export type MockCompletion = {
  id: string
  name: string
  kind: MockJobKind
  ok: boolean
}

export type MockInflightSnapshot = {
  jobs: MockInflightJob[]
  workers: MockAccountSlot[]
  rateLimits: MockRateLimitWindow[]
  /** Recent waiting+concurrency depth (oldest → newest). */
  depthSeries: number[]
  /** Soft backpressure guide on the depth chart. */
  backpressure: number
  totals: { done: number; failed: number }
  /** Throughput mock: completed / min. */
  throughputPerMin: number
  waitP95Ms: number
  generatedAt: number
  /**
   * Ephemeral per-tick terminal outcomes for GSAP Done/Failed routing.
   * Not cloned across ticks unless the producer sets them again.
   * Live `totals` are session counters (since expand/mount), bumped from
   * completions — not historical DB totals. priorIsLive uses waitP95Ms === 0.
   */
  completions?: MockCompletion[]
}

/** Stable demo seed — deterministic for first paint / screenshots. */
export const MOCK_INFLIGHT: MockInflightSnapshot = {
  generatedAt: 0,
  backpressure: 6,
  throughputPerMin: 42,
  waitP95Ms: 2800,
  totals: { done: 186, failed: 4 },
  workers: [
    {
      id: 'acc-wb-a1', name: 'wb-a1', provider: 'workbuddy', busy: true, progress: 0.72, jobId: 'j1', concurrency: 4, inFlight: 3,
      cells: [
        { id: 'j1', name: 'chat · sonnet', startedAtMs: 0, elapsedMs: 11_200 },
        { id: 'j1b', name: 'chat · opus', startedAtMs: 0, elapsedMs: 8400 },
        { id: 'j1c', name: 'embed · idx', startedAtMs: 0, elapsedMs: 4100 },
      ],
    },
    {
      id: 'acc-wb-a2', name: 'wb-a2', provider: 'workbuddy', busy: true, progress: 0.41, jobId: 'j2', rateLimited: true, concurrency: 2, inFlight: 1,
      cells: [{ id: 'j2', name: 'embed · batch', startedAtMs: 0, elapsedMs: 9400 }],
    },
    {
      id: 'acc-qd-b1', name: 'qd-b1', provider: 'qoder', busy: true, progress: 0.88, jobId: 'j3', concurrency: 1, inFlight: 1,
      cells: [{ id: 'j3', name: 'chat · gpt', startedAtMs: 0, elapsedMs: 6200 }],
    },
    { id: 'acc-qd-b2', name: 'qd-b2', provider: 'qoder', busy: false, progress: 0, concurrency: 4, inFlight: 0, cells: [] },
    {
      id: 'acc-trae-c1', name: 'trae-c1', provider: 'trae', busy: true, progress: 0.23, jobId: 'j4', concurrency: 3, inFlight: 2,
      cells: [
        { id: 'j4', name: 'tools · patch', startedAtMs: 0, elapsedMs: 3100 },
        { id: 'j4b', name: 'chat · flash', startedAtMs: 0, elapsedMs: 1800 },
      ],
    },
    { id: 'acc-codex-d1', name: 'codex-d1', provider: 'codex', busy: false, progress: 0, rateLimited: true, concurrency: 2, inFlight: 0, cells: [] },
  ],
  jobs: [
    { id: 'j1', name: 'chat · sonnet', kind: 'chat', status: 'running', accountId: 'acc-wb-a1', progress: 0.72, elapsedMs: 11_200, sticky: true },
    { id: 'j1b', name: 'chat · opus', kind: 'chat', status: 'running', accountId: 'acc-wb-a1', progress: 0.55, elapsedMs: 8400 },
    { id: 'j1c', name: 'embed · idx', kind: 'embed', status: 'running', accountId: 'acc-wb-a1', progress: 0.3, elapsedMs: 4100 },
    { id: 'j2', name: 'embed · batch', kind: 'embed', status: 'running', accountId: 'acc-wb-a2', progress: 0.41, elapsedMs: 9400 },
    { id: 'j3', name: 'chat · gpt', kind: 'chat', status: 'running', accountId: 'acc-qd-b1', progress: 0.88, elapsedMs: 6200 },
    { id: 'j4', name: 'tools · patch', kind: 'tools', status: 'running', accountId: 'acc-trae-c1', progress: 0.23, elapsedMs: 3100 },
    { id: 'j4b', name: 'chat · flash', kind: 'chat', status: 'running', accountId: 'acc-trae-c1', progress: 0.15, elapsedMs: 1800 },
    { id: 'j5', name: 'chat · wait', kind: 'chat', status: 'queued', progress: 0, elapsedMs: 7200 },
    { id: 'j6', name: 'retry · cool', kind: 'chat', status: 'queued', progress: 0, elapsedMs: 5100 },
    { id: 'j7', name: 'chat · sticky', kind: 'chat', status: 'queued', progress: 0, elapsedMs: 2800, sticky: true, priority: 'high' },
    { id: 'j8', name: 'embed · docs', kind: 'embed', status: 'queued', progress: 0, elapsedMs: 1400 },
    { id: 'j9', name: 'tools · apply', kind: 'tools', status: 'queued', progress: 0, elapsedMs: 900 },
    { id: 'j10', name: 'chat · flash', kind: 'chat', status: 'queued', progress: 0, elapsedMs: 600 },
    { id: 'j11', name: 'embed · idx', kind: 'embed', status: 'queued', progress: 0, elapsedMs: 400 },
    { id: 'j12', name: 'chat · opus', kind: 'chat', status: 'queued', progress: 0, elapsedMs: 200, priority: 'high' },
  ],
  rateLimits: [
    { id: 'rl1', label: 'wb-a2 限流', remainingMs: 97_000, from: 0.55, until: 0.72 },
    { id: 'rl2', label: 'codex-d1 冷却', remainingMs: 41_000, from: 0.82, until: 1 },
  ],
  // Soft hump under rate-limit, then partial drain.
  depthSeries: [1, 1, 2, 2, 3, 4, 5, 6, 7, 6, 5, 4, 3, 4, 5, 6, 5, 4, 3, 3, 2, 3, 4, 3],
}

const SEED_NAMES: Array<{ name: string; kind: MockJobKind; sticky?: boolean }> = [
  { name: 'chat · opus', kind: 'chat' },
  { name: 'embed · idx', kind: 'embed' },
  { name: 'tools · apply', kind: 'tools' },
  { name: 'chat · flash', kind: 'chat', sticky: true },
  { name: 'chat · wait', kind: 'chat' },
  { name: 'embed · docs', kind: 'embed' },
]

let seq = 100

export function cloneMockInflight(snap: MockInflightSnapshot = MOCK_INFLIGHT): MockInflightSnapshot {
  return {
    ...snap,
    workers: snap.workers.map((w) => ({
      ...w,
      cells: w.cells?.map((c) => ({ ...c })),
    })),
    jobs: snap.jobs.map((j) => ({ ...j })),
    rateLimits: snap.rateLimits.map((r) => ({ ...r })),
    depthSeries: [...snap.depthSeries],
    totals: { ...snap.totals },
    // Completions are ephemeral — never inherit a prior tick's fly-out list.
    completions: undefined,
  }
}

export function getMockInflightSnapshot(): MockInflightSnapshot {
  return cloneMockInflight()
}

/**
 * Advance the Stage-1 mock one frame. Mirrors public AffectUI loop:
 * progress fills (frozen under rate-limit), completed jobs → Done/Failed,
 * free slots pull the front of the waiting queue, depth series scrolls,
 * rate-limit remaining counts down. Deterministic-ish for a given dt.
 */
export function tickMockInflight(prev: MockInflightSnapshot, dtMs: number): MockInflightSnapshot {
  const dt = Math.max(16, Math.min(500, dtMs))
  const next = cloneMockInflight(prev)
  next.generatedAt = (prev.generatedAt || 0) + dt

  // Countdown rate-limit windows; clear expired → unfreeze matching slots.
  next.rateLimits = next.rateLimits
    .map((r) => ({ ...r, remainingMs: Math.max(0, r.remainingMs - dt) }))
    .filter((r) => r.remainingMs > 0)
  // Keep rateLimited flag in sync with remaining windows (seed label → slot).
  for (const w of next.workers) {
    if (w.id === 'acc-wb-a2') w.rateLimited = next.rateLimits.some((r) => r.label.includes('wb-a2'))
    else if (w.id === 'acc-codex-d1') w.rateLimited = next.rateLimits.some((r) => r.label.includes('codex-d1'))
  }

  const jobById = new Map(next.jobs.map((j) => [j.id, j]))

  // Per-cell elapsed + independent progress (rate-limit freezes the account).
  for (const w of next.workers) {
    if (w.cells?.length) {
      for (const c of w.cells) c.elapsedMs += dt
    }
    if (w.rateLimited) continue
    let maxP = 0
    for (const c of w.cells ?? []) {
      const job = jobById.get(c.id)
      if (!job || job.status !== 'running') continue
      job.elapsedMs = c.elapsedMs
      // ~8–14s per cell from current progress, paced by dt + cell id.
      const speed = 0.045 + (c.id.charCodeAt(c.id.length - 1) % 5) * 0.008
      job.progress = Math.min(1, job.progress + speed * (dt / 1000))
      maxP = Math.max(maxP, job.progress)
    }
    if ((w.cells?.length ?? 0) > 0) {
      w.progress = maxP
      w.busy = true
      w.jobId = w.cells![0].id
      w.inFlight = w.cells!.length
    }
  }

  // Finish ONE ready cell per worker per tick so each concurrency cell gets
  // its own Done/Failed fly-out (do not clear the whole account at once).
  const finished: Array<{ jobId: string; workerId: string; ok: boolean }> = []
  for (const w of next.workers) {
    if (w.rateLimited || !(w.cells?.length)) continue
    const ready = w.cells.find((c) => {
      const j = jobById.get(c.id)
      return Boolean(j && j.status === 'running' && j.progress >= 0.995)
    })
    if (!ready) continue
    const ok = (ready.id.charCodeAt(1) + Math.floor(next.generatedAt / 1000)) % 9 !== 0
    finished.push({ jobId: ready.id, workerId: w.id, ok })
  }
  for (const f of finished) {
    const job = jobById.get(f.jobId)
    const w = next.workers.find((x) => x.id === f.workerId)
    if (!job || !w) continue
    w.cells = (w.cells ?? []).filter((c) => c.id !== f.jobId)
    w.inFlight = w.cells.length
    w.jobId = w.cells[0]?.id
    w.busy = w.cells.length > 0
    w.progress = w.busy
      ? Math.max(0.04, ...w.cells.map((c) => jobById.get(c.id)?.progress ?? 0.04))
      : 0
    job.accountId = undefined
    if (f.ok) {
      job.status = 'done'
      job.progress = 1
      next.totals.done += 1
      continue
    }
    // AffectUI: failed shakes — or retry slides back to queue (same id).
    const attempt = (job.attempt ?? 1) + 1
    const canRetry = attempt <= 3 && (f.jobId.charCodeAt(0) + Math.floor(next.generatedAt / 900)) % 2 === 0
    if (canRetry) {
      job.status = 'queued'
      job.progress = 0
      job.elapsedMs = 80
      job.attempt = attempt
      job.priority = 'high'
      continue
    }
    job.status = 'failed'
    job.progress = 1
    next.totals.failed += 1
  }

  // Per-job outcomes for GSAP (retry back to queue is not a sink landing).
  const completions: MockCompletion[] = []
  for (const f of finished) {
    const job = jobById.get(f.jobId)
    if (!job) continue
    if (job.status === 'done' || job.status === 'failed') {
      completions.push({ id: job.id, name: job.name, kind: job.kind, ok: job.status === 'done' })
    }
  }
  next.completions = completions.length > 0 ? completions : undefined

  // Drop terminal jobs from the active list (keep queue + running only).
  next.jobs = next.jobs.filter((j) => j.status === 'queued' || j.status === 'running')

  // Waiting elapsed ticks up.
  for (const j of next.jobs) {
    if (j.status === 'queued') j.elapsedMs += dt
  }

  // Free concurrency cells pull front of queue (partially busy accounts included).
  const queue = next.jobs.filter((j) => j.status === 'queued')
  for (const w of next.workers) {
    if (w.rateLimited) continue
    const cap = clampSlotConcurrency(w.concurrency ?? 1)
    w.cells = w.cells ?? []
    while (w.cells.length < cap) {
      const job = queue.shift()
      if (!job) break
      job.status = 'running'
      job.accountId = w.id
      job.progress = 0.04
      job.elapsedMs = 0
      const cell = { id: job.id, name: job.name, startedAtMs: Date.now(), elapsedMs: 0 }
      w.cells = [...w.cells.filter((c) => c.id !== job.id), cell].slice(0, cap)
      w.busy = true
      w.progress = Math.max(w.progress, 0.04)
      w.jobId = w.cells[0]?.id
      w.inFlight = w.cells.length
    }
  }

  // Soft enqueue so the waiting rail doesn’t drain dry.
  const waiting = next.jobs.filter((j) => j.status === 'queued').length
  if (waiting < 6 && next.generatedAt % 2800 < dt) {
    const pick = SEED_NAMES[seq % SEED_NAMES.length]
    seq += 1
    // Every ~4th job is high priority (lands at back, then glides past).
    const priority = seq % 4 === 0 ? 'high' as const : 'normal' as const
    next.jobs.push({
      id: `j${seq}`,
      name: pick.name,
      kind: pick.kind,
      status: 'queued',
      progress: 0,
      elapsedMs: 200 + (seq % 7) * 400,
      sticky: pick.sticky,
      priority,
    })
  }

  // High-priority: after a brief back-of-queue land (~400ms), glide past to front.
  {
    const running = next.jobs.filter((j) => j.status === 'running')
    const queued = next.jobs.filter((j) => j.status === 'queued')
    const promote = queued.filter((j) => j.priority === 'high' && j.elapsedMs >= 400)
    const rest = queued.filter((j) => !(j.priority === 'high' && j.elapsedMs >= 400))
    // Stable: already-promoted highs keep relative order at front; new promotes append after them.
    const highs = promote
    next.jobs = [...running, ...highs, ...rest]
  }

  // Depth chart scrolls: shift left, append current waiting+busy approx.
  const busy = next.workers.filter((w) => w.busy).length
  const waitNow = next.jobs.filter((j) => j.status === 'queued').length
  const depthNow = Math.max(1, waitNow + Math.max(0, busy - 2))
  const series = next.depthSeries.slice(1)
  series.push(depthNow)
  next.depthSeries = series

  // Throughput / p95 drift gently.
  next.throughputPerMin = Math.max(28, Math.min(58, next.throughputPerMin + ((seq % 3) - 1)))
  next.waitP95Ms = Math.max(900, Math.min(5200, next.waitP95Ms + (waitNow > 4 ? 40 : -20)))

  return next
}

/** Clamp account concurrency for slot-bar cells (UI + summary). */
export function clampSlotConcurrency(n: number | undefined): number {
  if (n == null || !Number.isFinite(n)) return 1
  return Math.max(1, Math.min(16, Math.floor(n)))
}

export function summarizeMockInflight(snap: MockInflightSnapshot) {
  const running = snap.jobs.filter((j) => j.status === 'running').length
  const waiting = snap.jobs.filter((j) => j.status === 'queued').length
  const busyWorkers = snap.workers.filter((w) => w.busy).length
  // Capacity / occupancy from per-account concurrency (max_inflight / in_flight).
  let workerSlots = 0
  let occupiedSlots = 0
  for (const w of snap.workers) {
    const cap = clampSlotConcurrency(w.concurrency ?? 1)
    workerSlots += cap
    const occ =
      w.inFlight != null
        ? Math.max(0, Math.min(cap, Math.floor(w.inFlight)))
        : w.busy
          ? 1
          : 0
    occupiedSlots += occ
  }
  const depthNow = snap.depthSeries[snap.depthSeries.length - 1] ?? waiting
  return {
    running,
    waiting,
    busyWorkers,
    occupiedSlots,
    workerSlots,
    rateLimitActive: snap.rateLimits.length,
    depthNow,
  }
}

/** Optional sticky projection from /api/live (Stage-3 wire). */
export type MockStickyStats = {
  bindings: number
  hits: number
  misses: number
  capacity: number
  ttlSeconds: number
  escapes?: number
}

export type LiveWorkerLike = {
  id: string
  name: string
  provider: string
  busy: boolean
  progress: number
  rate_limited: boolean
  /** Account max concurrency — drives slot-bar cell count. */
  max_inflight?: number
  /** Occupied concurrency cells on this account. */
  in_flight?: number
  /** RFC3339 from pool InFlightSince when available. */
  started_at?: string
  /** Fallback fail signal when recent_completions missed the finish. */
  last_error_kind?: string
}

export type LiveCompletionLike = {
  /** Open request id — match per-cell Done/Failed when present. */
  request_id?: string
  account_id: string
  status: string
}

export type LiveRateLimitLike = {
  id: string
  label: string
  remaining_ms: number
}

export type LiveStickyLike = {
  bindings: number
  hits: number
  misses: number
  capacity: number
  ttl_seconds: number
  escapes?: number
}

export type LiveQueueJobLike = {
  id: string
  name: string
  /** RequestStatus* — UI maps selecting onto MockJobStatus queued. */
  status: string
  started_at?: string
  /** Bind target while dwell still projects onto the waiting rail. */
  account_id?: string
}

export type LiveProjectionInput = {
  in_flight: number
  rpm: number
  workers: LiveWorkerLike[]
  rate_limits: LiveRateLimitLike[]
  sticky: LiveStickyLike
  recent_completions?: LiveCompletionLike[]
  /** Selecting requests (started, unbound) from /api/live queue. */
  queue?: LiveQueueJobLike[]
  /** Bound open requests — per concurrency cell (model + Bind started_at). */
  occupancy?: LiveQueueJobLike[]
}

/**
 * Merge /api/live workers + rate_limits (+ sticky meta) onto a snapshot.
 * Busy workers → synthetic running jobs. When `live` is provided, never inherit
 * mock throughput / p95 / depth: zeros unless the API supplies a value
 * (`rpm` → throughput; no p95 fields yet). Session `totals` carry across live
 * polls and bump from recent_completions / inferred completions (reset when
 * the card starts a new viewing session). depthSeries rolls prior *live*
 * samples only — mock seed/tick series are discarded on first projection.
 * Per-cell elapsed comes from occupancy `started_at` (Bind / BoundAt); falls
 * back to worker InFlightSince / prior cell stamp. Progress is occupancy only —
 * UI uses per-cell model + elapsed, not %.
 */
export function applyLiveProjection(
  base: MockInflightSnapshot,
  live: LiveProjectionInput,
): MockInflightSnapshot {
  const now = Date.now()
  const prevById = new Map(base.jobs.map((j) => [j.id, j]))

  const n = Math.max(1, MOCK_INFLIGHT.depthSeries.length)
  const rateLimits: MockRateLimitWindow[] = live.rate_limits.map((r, i) => {
    const span = 0.14
    const from = Math.min(0.86, ((i * 0.22) % 0.86))
    return {
      id: r.id,
      label: r.label,
      remainingMs: Math.max(0, r.remaining_ms),
      from,
      until: Math.min(1, from + span),
    }
  })

  // Selecting queue first — ids still on wait rail (incl. dwell-bound).
  const queuedJobs: MockInflightJob[] = (live.queue ?? []).map((q) => {
    const id = (q.id || '').trim()
    const prev = prevById.get(id)
    let startedAtMs: number | undefined
    const raw = q.started_at?.trim()
    if (raw) {
      const parsed = Date.parse(raw)
      if (!Number.isNaN(parsed)) startedAtMs = parsed
    }
    if (startedAtMs == null && prev?.startedAtMs != null) {
      startedAtMs = prev.startedAtMs
    }
    if (startedAtMs == null) {
      startedAtMs = now
    }
    const name = (q.name || '').trim() || id
    const accountId = (q.account_id || '').trim() || undefined
    return {
      id,
      name,
      kind: 'chat' as const,
      status: 'queued' as const,
      accountId,
      progress: 0,
      startedAtMs,
      elapsedMs: Math.max(0, now - startedAtMs),
    }
  })
  const queueIds = new Set(queuedJobs.map((j) => j.id))

  const dwellNameByAccount = new Map<string, string>()
  for (const q of queuedJobs) {
    const acc = (q.accountId || '').trim()
    if (acc && q.name && !dwellNameByAccount.has(acc)) dwellNameByAccount.set(acc, q.name)
  }

  // Occupancy grouped by account (FIFO from API BoundRequests).
  const occByAccount = new Map<string, LiveQueueJobLike[]>()
  for (const o of live.occupancy ?? []) {
    const acc = (o.account_id || '').trim()
    const id = (o.id || '').trim()
    if (!acc || !id) continue
    const list = occByAccount.get(acc) ?? []
    list.push(o)
    occByAccount.set(acc, list)
  }

  const prevCellsByAccount = new Map<string, MockSlotCell[]>()
  for (const w of base.workers) {
    if (w.cells?.length) prevCellsByAccount.set(w.id, w.cells)
  }

  const parseStarted = (
    raw: string | undefined,
    prevMs: number | undefined,
    fallback: number,
  ): number => {
    const t = raw?.trim()
    if (t) {
      const parsed = Date.parse(t)
      if (!Number.isNaN(parsed)) return parsed
    }
    if (prevMs != null) return prevMs
    return fallback
  }

  const workers: MockAccountSlot[] = live.workers.map((w) => {
    const concurrency = clampSlotConcurrency(w.max_inflight ?? 4)
    const rawIn =
      w.in_flight != null && Number.isFinite(w.in_flight)
        ? Math.max(0, Math.floor(w.in_flight))
        : w.busy
          ? 1
          : 0
    const inFlight = Math.min(concurrency, rawIn)
    const prevCells = prevCellsByAccount.get(w.id) ?? []
    const prevByCellId = new Map(prevCells.map((c) => [c.id, c]))
    const occ = occByAccount.get(w.id) ?? []
    const cells: MockSlotCell[] = []

    for (const o of occ) {
      if (cells.length >= inFlight) break
      const id = (o.id || '').trim()
      if (!id) continue
      const prevCell = prevByCellId.get(id)
      const prevJob = prevById.get(id)
      const startedAtMs = parseStarted(
        o.started_at,
        prevCell?.startedAtMs ?? prevJob?.startedAtMs,
        now,
      )
      const name = (o.name || '').trim() || id
      cells.push({
        id,
        name,
        startedAtMs,
        elapsedMs: Math.max(0, now - startedAtMs),
      })
    }

    // Pool says more occupied than recorder — pad anonymous cells (account started_at).
    const liveWorker = live.workers.find((lw) => lw.id === w.id)
    const accountStarted = parseStarted(liveWorker?.started_at, undefined, now)
    while (cells.length < inFlight) {
      const idx = cells.length
      // First synthetic cell keeps legacy `live-${accountId}` id for fly-out routing.
      const synthId = idx === 0 ? `live-${w.id}` : `live-${w.id}-${idx}`
      const prevCell = prevByCellId.get(synthId) ?? prevCells[idx]
      const startedAtMs = prevCell?.startedAtMs ?? accountStarted
      cells.push({
        id: synthId,
        name: dwellNameByAccount.get(w.id) ?? w.name,
        startedAtMs,
        elapsedMs: Math.max(0, now - startedAtMs),
      })
    }

    const busy = w.busy || inFlight > 0
    return {
      id: w.id,
      name: w.name,
      provider: w.provider,
      busy,
      progress: busy ? Math.max(0.04, Math.min(1, w.progress || 0)) : 0,
      rateLimited: w.rate_limited,
      jobId: cells[0]?.id,
      concurrency,
      inFlight,
      cells,
    }
  })

  // Running jobs = occupancy cells not still on the wait rail (dwell).
  const runningJobs: MockInflightJob[] = []
  for (const w of workers) {
    for (const c of w.cells ?? []) {
      if (queueIds.has(c.id)) continue // still selecting for display
      const prev = prevById.get(c.id)
      runningJobs.push({
        id: c.id,
        name: c.name,
        kind: (prev?.kind as MockJobKind | undefined) ?? 'chat',
        status: 'running',
        accountId: w.id,
        progress: w.progress,
        startedAtMs: c.startedAtMs,
        elapsedMs: c.elapsedMs,
      })
    }
  }

  const jobs: MockInflightJob[] = [...runningJobs, ...queuedJobs]

  // Continue rolling depth only after a prior live projection (cleared p95).
  // Mock seed/tick always leaves waitP95Ms > 0 — start zeros. Session totals
  // may be > 0 while viewing live; do not key priorIsLive on them.
  const priorIsLive = base.waitP95Ms === 0
  const series = priorIsLive
    ? base.depthSeries.slice(1)
    : Array.from({ length: Math.max(0, n - 1) }, () => 0)
  series.push(Math.max(0, live.in_flight))
  while (series.length < n) series.unshift(0)

  // Match busy→idle jobs to drained finishes: request_id first (per-cell),
  // then account FIFO, then last_error_kind.
  const pendingByRequest = new Map<string, boolean>()
  const pendingByAccount = new Map<string, boolean[]>()
  for (const c of live.recent_completions ?? []) {
    const ok = (c.status || '').trim() === 'ok'
    const rid = (c.request_id || '').trim()
    if (rid) pendingByRequest.set(rid, ok)
    const acc = (c.account_id || '').trim()
    if (!acc) continue
    const list = pendingByAccount.get(acc) ?? []
    list.push(ok)
    pendingByAccount.set(acc, list)
  }
  const stillIds = new Set(jobs.map((j) => j.id))
  const completions: MockCompletion[] = []
  for (const prev of base.jobs) {
    if (prev.status !== 'running') continue
    if (stillIds.has(prev.id)) continue
    const acc = (prev.accountId || prev.id.replace(/^live-/, '')).trim()
    let ok = true
    if (pendingByRequest.has(prev.id)) {
      ok = pendingByRequest.get(prev.id)!
      pendingByRequest.delete(prev.id)
    } else {
      const queue = pendingByAccount.get(acc)
      if (queue && queue.length > 0) {
        ok = queue.shift()!
      } else {
        const lw = live.workers.find((w) => w.id === acc)
        if (lw?.last_error_kind && lw.last_error_kind.trim() !== '') {
          ok = false
        }
      }
    }
    completions.push({
      id: prev.id,
      name: prev.name,
      kind: prev.kind,
      ok,
    })
  }

  // Session Done/Failed while viewing live — not historical DB totals.
  // First live projection (from mock / new expand) starts at 0 and does not
  // count mock jobs vanishing as completions. Later polls carry + bump.
  let done = 0
  let failed = 0
  if (priorIsLive) {
    done = base.totals.done
    failed = base.totals.failed
    const recents = live.recent_completions ?? []
    if (recents.length > 0) {
      for (const c of recents) {
        if ((c.status || '').trim() === 'ok') done += 1
        else failed += 1
      }
    } else {
      for (const c of completions) {
        if (c.ok) done += 1
        else failed += 1
      }
    }
  }

  return {
    generatedAt: now,
    backpressure: base.backpressure,
    // API: rpm only. No p95 yet → 0 (do not spread mock base).
    throughputPerMin: Math.max(0, live.rpm),
    waitP95Ms: 0,
    totals: { done, failed },
    workers,
    rateLimits,
    jobs,
    depthSeries: series.slice(-n),
    completions: completions.length > 0 ? completions : undefined,
  }
}

/** True when live has something to show (slots or cooldowns or concurrency). */
export function liveHasSignal(live: LiveProjectionInput): boolean {
  return (
    live.workers.length > 0 ||
    live.rate_limits.length > 0 ||
    live.in_flight > 0 ||
    (live.queue?.length ?? 0) > 0 ||
    (live.occupancy?.length ?? 0) > 0 ||
    (live.sticky?.bindings ?? 0) > 0
  )
}
