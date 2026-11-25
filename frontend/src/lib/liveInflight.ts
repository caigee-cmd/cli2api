/**
 * Real inflight projection for Overview「实时进行中」.
 *
 * Everything rendered comes from /api/live: workers (account slots + their
 * in-flight cells), rate-limit/cooldown windows, the selecting queue, occupied
 * requests, sticky stats and RPM. There is no seed, no tick, and no demo mode:
 * when live data is unavailable the card shows an honest empty state.
 */

export type LiveJobKind = 'chat' | 'embed' | 'tools' | 'other'
export type LiveJobStatus = 'queued' | 'running' | 'done' | 'failed'

export type LiveJobPriority = 'high' | 'normal' | 'low'

/** Recent concurrency depth window kept for the sparkline. */
const DEPTH_SERIES_LEN = 24
/** Queue depth at which the waiting metric turns amber (UI guideline). */
const BACKPRESSURE_DEPTH = 6

export type LiveInflightJob = {
  id: string
  /** Short label — model / route style. */
  name: string
  kind: LiveJobKind
  status: LiveJobStatus
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
  priority?: LiveJobPriority
  /** Retry attempt count when failed job slides back to queue. */
  attempt?: number
}

/** One occupied concurrency cell on an account slot bar. */
export type LiveSlotCell = {
  id: string
  /** Requested model / short label. */
  name: string
  /** Wall-clock when this request entered the slot (Bind). */
  startedAtMs: number
  elapsedMs: number
  /**
   * Still on the selecting wait rail (display dwell) — keep cell DOM for the
   * queue→slot fly target, but InflightLiveCard must not show occupied chrome
   * (model + elapsed) until the inbound fly lands.
   */
  pendingInbound?: boolean
}

export type LiveAccountSlot = {
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
  cells?: LiveSlotCell[]
}

export type LiveRateLimitWindow = {
  id: string
  label: string
  /** Remaining cooldown / rate-limit window in ms. */
  remainingMs: number
  /** Depth-chart band: fraction along [0,1] series. */
  from: number
  until: number
}

/** One job that just landed on Done or Failed this tick (GSAP fly-out routing). */
export type LiveCompletion = {
  id: string
  name: string
  kind: LiveJobKind
  ok: boolean
}

export type LiveInflightSnapshot = {
  jobs: LiveInflightJob[]
  workers: LiveAccountSlot[]
  rateLimits: LiveRateLimitWindow[]
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
  completions?: LiveCompletion[]
}

export function cloneInflight(snap: LiveInflightSnapshot): LiveInflightSnapshot {
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

/** Honest empty board: no fabricated numbers, used until /api/live responds. */
export function emptyInflightSnapshot(): LiveInflightSnapshot {
  return {
    generatedAt: Date.now(),
    backpressure: BACKPRESSURE_DEPTH,
    throughputPerMin: 0,
    waitP95Ms: 0,
    totals: { done: 0, failed: 0 },
    workers: [],
    jobs: [],
    rateLimits: [],
    depthSeries: Array.from({ length: DEPTH_SERIES_LEN }, () => 0),
  }
}

/** Clamp account concurrency for slot-bar cells (UI + summary). */
export function clampSlotConcurrency(n: number | undefined): number {
  if (n == null || !Number.isFinite(n)) return 1
  return Math.max(1, Math.min(16, Math.floor(n)))
}

export function summarizeInflight(snap: LiveInflightSnapshot) {
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
export type LiveStickyStats = {
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
  /** RequestStatus* — UI maps selecting onto LiveJobStatus queued. */
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
 * UI uses per-cell model + elapsed, not %. Cells still on the selecting rail
 * (dwell) set `pendingInbound` so the slot can exist as a fly target without
 * showing occupied chrome until the inbound animation lands.
 */
export function applyLiveProjection(
  base: LiveInflightSnapshot,
  live: LiveProjectionInput,
): LiveInflightSnapshot {
  const now = Date.now()
  const prevById = new Map(base.jobs.map((j) => [j.id, j]))

  const n = DEPTH_SERIES_LEN
  const rateLimits: LiveRateLimitWindow[] = live.rate_limits.map((r, i) => {
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
  const queuedJobs: LiveInflightJob[] = (live.queue ?? []).map((q) => {
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

  const prevCellsByAccount = new Map<string, LiveSlotCell[]>()
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

  const workers: LiveAccountSlot[] = live.workers.map((w) => {
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
    const cells: LiveSlotCell[] = []

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
        // Dwell-bound: still on wait rail — reserve the cell, gate chrome until land.
        pendingInbound: queueIds.has(id),
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
  const runningJobs: LiveInflightJob[] = []
  for (const w of workers) {
    for (const c of w.cells ?? []) {
      if (queueIds.has(c.id)) continue // still selecting for display
      const prev = prevById.get(c.id)
      runningJobs.push({
        id: c.id,
        name: c.name,
        kind: (prev?.kind as LiveJobKind | undefined) ?? 'chat',
        status: 'running',
        accountId: w.id,
        progress: w.progress,
        startedAtMs: c.startedAtMs,
        elapsedMs: c.elapsedMs,
      })
    }
  }

  const jobs: LiveInflightJob[] = [...runningJobs, ...queuedJobs]

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
  const completions: LiveCompletion[] = []
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
