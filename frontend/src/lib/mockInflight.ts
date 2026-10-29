/**
 * Phase-1 fake snapshot for Overview「实时进行中」.
 * Shaped like cli2api runtime concepts (not a parallel control plane):
 *   jobs      ≈ in-flight / waiting proxy requests
 *   workers   ≈ account slots currently (or recently) hitting upstream
 *   rateLimits≈ cooldown / provider rate-limit windows
 *   depth     ≈ recent waiting + concurrency approx
 *   totals    ≈ recent request_logs Done/Failed (mock until Phase 2)
 * Swap the source later; keep this file mock-only until real inflight lands.
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

/** Stable demo — deterministic for screenshots. */
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

export function getMockInflightSnapshot(): MockInflightSnapshot {
  return MOCK_INFLIGHT
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
