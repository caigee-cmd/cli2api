/**
 * Phase-1 fake snapshot for Overview「实时进行中」.
 * Shaped like cli2api runtime concepts (not a parallel control plane):
 *   jobs      ≈ in-flight / waiting proxy requests
 *   workers   ≈ account slots currently (or recently) hitting upstream
 *   rateLimits≈ cooldown / provider rate-limit windows
 *   depth     ≈ recent waiting + concurrency approx
 *   totals    ≈ recent request_logs Done/Failed (mock until Phase 2)
 * Stage-3 swaps workers/rateLimits/sticky onto /api/live (LiveWorker,
 * LiveRateLimit, LiveSticky); keep jobs/depth mock-only until request-level
 * inflight lands. Do not invent a parallel /api/inflight*.
 *
 * Live loop (`tickMockInflight`) mirrors public AffectUI Motion prose:
 * progress fill, queue→worker handoff, DONE/FAILED stack, depth scroll —
 * Stage-1 only; real /api/live projection stays read-only.
 */

export type MockJobKind = 'chat' | 'embed' | 'tools' | 'other'
export type MockJobStatus = 'queued' | 'running' | 'done' | 'failed'

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
  /** Sticky session hint (affinity hit). */
  sticky?: boolean
}

export type MockAccountSlot = {
  id: string
  /** Account display name. */
  name: string
  provider: string
  /** Busy with an upstream call. */
  busy: boolean
  /** 0–1 progress while busy; amber-frozen while rate-limited. */
  progress: number
  jobId?: string
  /** Slot paused by an active cooldown / rate-limit. */
  rateLimited?: boolean
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
}

/** Stable demo seed — deterministic for first paint / screenshots. */
export const MOCK_INFLIGHT: MockInflightSnapshot = {
  generatedAt: 0,
  backpressure: 6,
  throughputPerMin: 42,
  waitP95Ms: 2800,
  totals: { done: 186, failed: 4 },
  workers: [
    { id: 'acc-wb-a1', name: 'wb-a1', provider: 'workbuddy', busy: true, progress: 0.72, jobId: 'j1' },
    { id: 'acc-wb-a2', name: 'wb-a2', provider: 'workbuddy', busy: true, progress: 0.41, jobId: 'j2', rateLimited: true },
    { id: 'acc-qd-b1', name: 'qd-b1', provider: 'qoder', busy: true, progress: 0.88, jobId: 'j3' },
    { id: 'acc-qd-b2', name: 'qd-b2', provider: 'qoder', busy: false, progress: 0 },
    { id: 'acc-trae-c1', name: 'trae-c1', provider: 'trae', busy: true, progress: 0.23, jobId: 'j4' },
    { id: 'acc-codex-d1', name: 'codex-d1', provider: 'codex', busy: false, progress: 0, rateLimited: true },
  ],
  jobs: [
    { id: 'j1', name: 'chat · sonnet', kind: 'chat', status: 'running', accountId: 'acc-wb-a1', progress: 0.72, elapsedMs: 11_200, sticky: true },
    { id: 'j2', name: 'embed · batch', kind: 'embed', status: 'running', accountId: 'acc-wb-a2', progress: 0.41, elapsedMs: 9400 },
    { id: 'j3', name: 'chat · gpt', kind: 'chat', status: 'running', accountId: 'acc-qd-b1', progress: 0.88, elapsedMs: 6200 },
    { id: 'j4', name: 'tools · patch', kind: 'tools', status: 'running', accountId: 'acc-trae-c1', progress: 0.23, elapsedMs: 3100 },
    { id: 'j5', name: 'chat · wait', kind: 'chat', status: 'queued', progress: 0, elapsedMs: 7200 },
    { id: 'j6', name: 'retry · cool', kind: 'chat', status: 'queued', progress: 0, elapsedMs: 5100 },
    { id: 'j7', name: 'chat · sticky', kind: 'chat', status: 'queued', progress: 0, elapsedMs: 2800, sticky: true },
    { id: 'j8', name: 'embed · docs', kind: 'embed', status: 'queued', progress: 0, elapsedMs: 1400 },
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
    workers: snap.workers.map((w) => ({ ...w })),
    jobs: snap.jobs.map((j) => ({ ...j })),
    rateLimits: snap.rateLimits.map((r) => ({ ...r })),
    depthSeries: [...snap.depthSeries],
    totals: { ...snap.totals },
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

  // Progress + elapsed on busy non-frozen slots.
  for (const w of next.workers) {
    if (!w.busy || !w.jobId) continue
    const job = jobById.get(w.jobId)
    if (!job || job.status !== 'running') continue
    job.elapsedMs += dt
    if (w.rateLimited) {
      // Amber freeze — no progress (AffectUI: bars stop + amber).
      continue
    }
    // ~8–14s to complete from current progress, paced by dt.
    const speed = 0.045 + (w.id.charCodeAt(w.id.length - 1) % 5) * 0.008
    w.progress = Math.min(1, w.progress + speed * (dt / 1000))
    job.progress = w.progress
  }

  // Complete / fail when progress hits 1.
  const finished: Array<{ jobId: string; workerId: string; ok: boolean }> = []
  for (const w of next.workers) {
    if (!w.busy || !w.jobId || w.rateLimited) continue
    if (w.progress < 0.995) continue
    // ~12% fail flash (AffectUI shake path); rest → Done.
    const ok = (w.jobId.charCodeAt(1) + Math.floor(next.generatedAt / 1000)) % 9 !== 0
    finished.push({ jobId: w.jobId, workerId: w.id, ok })
  }
  for (const f of finished) {
    const job = jobById.get(f.jobId)
    const w = next.workers.find((x) => x.id === f.workerId)
    if (!job || !w) continue
    job.status = f.ok ? 'done' : 'failed'
    job.progress = 1
    job.accountId = undefined
    w.busy = false
    w.progress = 0
    w.jobId = undefined
    if (f.ok) next.totals.done += 1
    else next.totals.failed += 1
  }

  // Drop terminal jobs from the active list (keep queue + running only).
  next.jobs = next.jobs.filter((j) => j.status === 'queued' || j.status === 'running')

  // Waiting elapsed ticks up.
  for (const j of next.jobs) {
    if (j.status === 'queued') j.elapsedMs += dt
  }

  // Free slots pull front of queue (queue → worker handoff).
  const queue = next.jobs.filter((j) => j.status === 'queued')
  for (const w of next.workers) {
    if (w.busy || w.rateLimited) continue
    const job = queue.shift()
    if (!job) break
    job.status = 'running'
    job.accountId = w.id
    job.progress = 0.04
    job.elapsedMs = 0
    w.busy = true
    w.progress = 0.04
    w.jobId = job.id
  }

  // Soft enqueue so the waiting rail doesn’t drain dry.
  const waiting = next.jobs.filter((j) => j.status === 'queued').length
  if (waiting < 3 && next.generatedAt % 3200 < dt) {
    const pick = SEED_NAMES[seq % SEED_NAMES.length]
    seq += 1
    next.jobs.push({
      id: `j${seq}`,
      name: pick.name,
      kind: pick.kind,
      status: 'queued',
      progress: 0,
      elapsedMs: 200 + (seq % 7) * 400,
      sticky: pick.sticky,
    })
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

export function summarizeMockInflight(snap: MockInflightSnapshot) {
  const running = snap.jobs.filter((j) => j.status === 'running').length
  const waiting = snap.jobs.filter((j) => j.status === 'queued').length
  const busyWorkers = snap.workers.filter((w) => w.busy).length
  const depthNow = snap.depthSeries[snap.depthSeries.length - 1] ?? waiting
  return {
    running,
    waiting,
    busyWorkers,
    workerSlots: snap.workers.length,
    rateLimitActive: snap.rateLimits.length,
    depthNow,
  }
}
