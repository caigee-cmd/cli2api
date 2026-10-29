import assert from 'node:assert/strict'
import {
  applyLiveProjection,
  cloneMockInflight,
  getIdleInflightSnapshot,
  getMockInflightSnapshot,
  isIdleInflightSnapshot,
  liveHasSignal,
  MOCK_INFLIGHT,
  tickMockInflight,
} from '../src/lib/mockInflight.ts'

function run(name: string, fn: () => void) {
  try {
    fn()
    console.log(`ok  ${name}`)
  } catch (error) {
    console.error(`FAIL ${name}`)
    throw error
  }
}

run('applyLiveProjection uses started_at for elapsed', () => {
  const started = new Date(Date.now() - 12_400).toISOString()
  const snap = applyLiveProjection(getMockInflightSnapshot(), {
    in_flight: 1,
    rpm: 3,
    workers: [
      {
        id: 'acc-1',
        name: 'wb-a1',
        provider: 'workbuddy',
        busy: true,
        progress: 0.5,
        rate_limited: false,
        started_at: started,
      },
      {
        id: 'acc-2',
        name: 'idle',
        provider: 'qoder',
        busy: false,
        progress: 0,
        rate_limited: false,
      },
    ],
    rate_limits: [],
    sticky: { bindings: 0, hits: 0, misses: 0, capacity: 64, ttl_seconds: 600 },
  })
  assert.equal(snap.jobs.length, 1)
  assert.equal(snap.totals.done, 0)
  assert.equal(snap.waitP95Ms, 0)
  const job = snap.jobs[0]
  assert.ok(job.startedAtMs != null)
  assert.ok(job.elapsedMs >= 12_000 && job.elapsedMs < 20_000, `elapsed=${job.elapsedMs}`)
  const idle = snap.workers.find((w) => w.id === 'acc-2')
  assert.equal(idle?.busy, false)
  assert.equal(idle?.progress, 0)
  assert.equal(idle?.jobId, undefined)
})

run('applyLiveProjection keeps startedAtMs across polls without started_at', () => {
  const first = applyLiveProjection(cloneMockInflight(MOCK_INFLIGHT), {
    in_flight: 1,
    rpm: 0,
    workers: [
      {
        id: 'acc-1',
        name: 'wb-a1',
        provider: 'workbuddy',
        busy: true,
        progress: 0.2,
        rate_limited: false,
      },
    ],
    rate_limits: [],
    sticky: { bindings: 0, hits: 0, misses: 0, capacity: 64, ttl_seconds: 600 },
  })
  const stamped = first.jobs[0].startedAtMs
  assert.ok(stamped != null)
  const second = applyLiveProjection(first, {
    in_flight: 1,
    rpm: 0,
    workers: [
      {
        id: 'acc-1',
        name: 'wb-a1',
        provider: 'workbuddy',
        busy: true,
        progress: 0.2,
        rate_limited: false,
      },
    ],
    rate_limits: [],
    sticky: { bindings: 0, hits: 0, misses: 0, capacity: 64, ttl_seconds: 600 },
  })
  assert.equal(second.jobs[0].startedAtMs, stamped)
})

run('idle live does not bleed mock totals', () => {
  const snap = applyLiveProjection(getMockInflightSnapshot(), {
    in_flight: 0,
    rpm: 0,
    workers: [],
    rate_limits: [],
    sticky: { bindings: 0, hits: 0, misses: 0, capacity: 64, ttl_seconds: 600 },
  })
  assert.equal(snap.totals.done, 0)
  assert.equal(snap.totals.failed, 0)
  assert.equal(snap.throughputPerMin, 0)
  assert.equal(snap.waitP95Ms, 0)
})


run('applyLiveProjection routes fail via recent_completions', () => {
  const busy = applyLiveProjection(cloneMockInflight(MOCK_INFLIGHT), {
    in_flight: 1,
    rpm: 0,
    workers: [
      {
        id: 'acc-1',
        name: 'wb-a1',
        provider: 'workbuddy',
        busy: true,
        progress: 0.5,
        rate_limited: false,
      },
    ],
    rate_limits: [],
    sticky: { bindings: 0, hits: 0, misses: 0, capacity: 64, ttl_seconds: 600 },
  })
  assert.equal(busy.jobs.length, 1)
  const idleFail = applyLiveProjection(busy, {
    in_flight: 0,
    rpm: 1,
    workers: [
      {
        id: 'acc-1',
        name: 'wb-a1',
        provider: 'workbuddy',
        busy: false,
        progress: 0,
        rate_limited: false,
        last_error_kind: 'unavailable',
      },
    ],
    rate_limits: [],
    sticky: { bindings: 0, hits: 0, misses: 0, capacity: 64, ttl_seconds: 600 },
    recent_completions: [{ account_id: 'acc-1', status: 'error' }],
  })
  assert.equal(idleFail.jobs.length, 0)
  assert.equal(idleFail.totals.done, 0)
  assert.equal(idleFail.totals.failed, 1)
  assert.ok(idleFail.completions && idleFail.completions.length === 1)
  assert.equal(idleFail.completions[0].ok, false)
  assert.equal(idleFail.completions[0].id, 'live-acc-1')
})

run('applyLiveProjection last_error_kind fallback without completions', () => {
  const busy = applyLiveProjection(cloneMockInflight(MOCK_INFLIGHT), {
    in_flight: 1,
    rpm: 0,
    workers: [
      {
        id: 'acc-9',
        name: 'x',
        provider: 'qoder',
        busy: true,
        progress: 0.2,
        rate_limited: false,
      },
    ],
    rate_limits: [],
    sticky: { bindings: 0, hits: 0, misses: 0, capacity: 64, ttl_seconds: 600 },
  })
  const idle = applyLiveProjection(busy, {
    in_flight: 0,
    rpm: 0,
    workers: [
      {
        id: 'acc-9',
        name: 'x',
        provider: 'qoder',
        busy: false,
        progress: 0,
        rate_limited: false,
        last_error_kind: 'auth',
      },
    ],
    rate_limits: [],
    sticky: { bindings: 0, hits: 0, misses: 0, capacity: 64, ttl_seconds: 600 },
  })
  assert.ok(idle.completions && idle.completions.length === 1)
  assert.equal(idle.completions[0].ok, false)
  assert.equal(idle.totals.failed, 1)
  assert.equal(idle.totals.done, 0)
})

run('applyLiveProjection success completion flies ok', () => {
  const busy = applyLiveProjection(cloneMockInflight(MOCK_INFLIGHT), {
    in_flight: 1,
    rpm: 0,
    workers: [
      {
        id: 'acc-ok',
        name: 'ok',
        provider: 'qoder',
        busy: true,
        progress: 0.9,
        rate_limited: false,
      },
    ],
    rate_limits: [],
    sticky: { bindings: 0, hits: 0, misses: 0, capacity: 64, ttl_seconds: 600 },
  })
  const idle = applyLiveProjection(busy, {
    in_flight: 0,
    rpm: 2,
    workers: [
      {
        id: 'acc-ok',
        name: 'ok',
        provider: 'qoder',
        busy: false,
        progress: 0,
        rate_limited: false,
      },
    ],
    rate_limits: [],
    sticky: { bindings: 0, hits: 0, misses: 0, capacity: 64, ttl_seconds: 600 },
    recent_completions: [{ account_id: 'acc-ok', status: 'ok' }],
  })
  assert.ok(idle.completions && idle.completions[0].ok === true)
  assert.equal(idle.totals.done, 1)
  assert.equal(idle.totals.failed, 0)
  const again = applyLiveProjection(idle, {
    in_flight: 0,
    rpm: 3,
    workers: [
      {
        id: 'acc-ok',
        name: 'ok',
        provider: 'qoder',
        busy: false,
        progress: 0,
        rate_limited: false,
      },
    ],
    rate_limits: [],
    sticky: { bindings: 0, hits: 0, misses: 0, capacity: 64, ttl_seconds: 600 },
    recent_completions: [
      { account_id: 'acc-ok', status: 'ok' },
      { account_id: 'acc-ok', status: 'error' },
    ],
  })
  assert.equal(again.totals.done, 2)
  assert.equal(again.totals.failed, 1)
})


run('applyLiveProjection maps max_inflight / in_flight to slot cells', () => {
  const snap = applyLiveProjection(getMockInflightSnapshot(), {
    in_flight: 3,
    rpm: 1,
    workers: [
      {
        id: 'acc-4',
        name: 'wb-x',
        provider: 'workbuddy',
        busy: true,
        progress: 0.5,
        rate_limited: false,
        max_inflight: 4,
        in_flight: 3,
      },
      {
        id: 'acc-1',
        name: 'solo',
        provider: 'qoder',
        busy: true,
        progress: 0.2,
        rate_limited: false,
        max_inflight: 1,
        in_flight: 1,
      },
      {
        id: 'acc-idle',
        name: 'idle',
        provider: 'trae',
        busy: false,
        progress: 0,
        rate_limited: false,
        max_inflight: 4,
        in_flight: 0,
      },
    ],
    rate_limits: [],
    sticky: { bindings: 0, hits: 0, misses: 0, capacity: 64, ttl_seconds: 600 },
  })
  const multi = snap.workers.find((w) => w.id === 'acc-4')
  assert.equal(multi?.concurrency, 4)
  assert.equal(multi?.inFlight, 3)
  assert.equal(multi?.busy, true)
  const solo = snap.workers.find((w) => w.id === 'acc-1')
  assert.equal(solo?.concurrency, 1)
  assert.equal(solo?.inFlight, 1)
  const idle = snap.workers.find((w) => w.id === 'acc-idle')
  assert.equal(idle?.concurrency, 4)
  assert.equal(idle?.inFlight, 0)
  assert.equal(idle?.busy, false)
})

run('applyLiveProjection maps queue onto queued MockJobStatus', () => {
  const started = new Date(Date.now() - 5_000).toISOString()
  const snap = applyLiveProjection(getMockInflightSnapshot(), {
    in_flight: 1,
    rpm: 2,
    workers: [
      {
        id: 'acc-1',
        name: 'wb-a1',
        provider: 'workbuddy',
        busy: true,
        progress: 0.4,
        rate_limited: false,
      },
    ],
    rate_limits: [],
    sticky: { bindings: 0, hits: 0, misses: 0, capacity: 64, ttl_seconds: 600 },
    queue: [
      { id: 'req-1', name: 'chat · wait', status: 'started', started_at: started },
      { id: 'req-2', name: 'embed · docs', status: 'started' },
    ],
  })
  const queued = snap.jobs.filter((j) => j.status === 'queued')
  const running = snap.jobs.filter((j) => j.status === 'running')
  assert.equal(running.length, 1)
  assert.equal(queued.length, 2)
  assert.equal(queued[0].id, 'req-1')
  assert.equal(queued[0].name, 'chat · wait')
  assert.ok(queued[0].elapsedMs >= 4_000 && queued[0].elapsedMs < 15_000)
  assert.equal(queued[1].id, 'req-2')
})

run('applyLiveProjection keeps queue accountId for slot handoff', () => {
  const snap = applyLiveProjection(getMockInflightSnapshot(), {
    in_flight: 1,
    rpm: 1,
    workers: [
      {
        id: 'acc-x',
        name: 'wb-x',
        provider: 'workbuddy',
        busy: true,
        progress: 0.5,
        rate_limited: false,
      },
    ],
    rate_limits: [],
    sticky: { bindings: 0, hits: 0, misses: 0, capacity: 64, ttl_seconds: 600 },
    queue: [
      { id: 'req-dwell', name: 'gpt · dwell', status: 'started', account_id: 'acc-x' },
    ],
  })
  const queued = snap.jobs.filter((j) => j.status === 'queued')
  assert.equal(queued.length, 1)
  assert.equal(queued[0].id, 'req-dwell')
  assert.equal(queued[0].accountId, 'acc-x')
  const running = snap.jobs.filter((j) => j.status === 'running')
  assert.equal(running.length, 1)
  assert.equal(running[0].name, 'gpt · dwell')
})

run('liveHasSignal true when only queue present', () => {
  assert.equal(
    liveHasSignal({
      in_flight: 0,
      rpm: 0,
      workers: [],
      rate_limits: [],
      sticky: { bindings: 0, hits: 0, misses: 0, capacity: 0, ttl_seconds: 0 },
      queue: [{ id: 'r1', name: 'm', status: 'started' }],
    }),
    true,
  )
})

run('applyLiveProjection occupancy maps per-cell model and started_at', () => {
  const t0 = new Date(Date.now() - 8_000).toISOString()
  const t1 = new Date(Date.now() - 3_000).toISOString()
  const snap = applyLiveProjection(getMockInflightSnapshot(), {
    in_flight: 2,
    rpm: 1,
    workers: [
      {
        id: 'acc-m',
        name: 'wb-m',
        provider: 'workbuddy',
        busy: true,
        progress: 0.5,
        rate_limited: false,
        max_inflight: 4,
        in_flight: 2,
      },
    ],
    rate_limits: [],
    sticky: { bindings: 0, hits: 0, misses: 0, capacity: 64, ttl_seconds: 600 },
    occupancy: [
      { id: 'req-a', name: 'claude-sonnet', status: 'started', started_at: t0, account_id: 'acc-m' },
      { id: 'req-b', name: 'gpt-4.1', status: 'started', started_at: t1, account_id: 'acc-m' },
    ],
  })
  const w = snap.workers.find((x) => x.id === 'acc-m')
  assert.ok(w)
  assert.equal(w.cells?.length, 2)
  assert.equal(w.cells?.[0].id, 'req-a')
  assert.equal(w.cells?.[0].name, 'claude-sonnet')
  assert.equal(w.cells?.[1].id, 'req-b')
  assert.equal(w.cells?.[1].name, 'gpt-4.1')
  assert.ok((w.cells?.[0].elapsedMs ?? 0) >= 7_000)
  assert.ok((w.cells?.[1].elapsedMs ?? 0) >= 2_000)
  assert.ok((w.cells?.[0].elapsedMs ?? 0) > (w.cells?.[1].elapsedMs ?? 0))
  const running = snap.jobs.filter((j) => j.status === 'running')
  assert.equal(running.length, 2)
  assert.equal(running[0].id, 'req-a')
  assert.equal(running[1].name, 'gpt-4.1')
})

run('applyLiveProjection occupancy dwell stays on queue not double-running', () => {
  const snap = applyLiveProjection(getMockInflightSnapshot(), {
    in_flight: 1,
    rpm: 1,
    workers: [
      {
        id: 'acc-d',
        name: 'wb-d',
        provider: 'workbuddy',
        busy: true,
        progress: 0.4,
        rate_limited: false,
        max_inflight: 2,
        in_flight: 1,
      },
    ],
    rate_limits: [],
    sticky: { bindings: 0, hits: 0, misses: 0, capacity: 64, ttl_seconds: 600 },
    queue: [{ id: 'req-dwell', name: 'sonnet', status: 'started', account_id: 'acc-d' }],
    occupancy: [{ id: 'req-dwell', name: 'sonnet', status: 'started', account_id: 'acc-d' }],
  })
  const queued = snap.jobs.filter((j) => j.status === 'queued')
  const running = snap.jobs.filter((j) => j.status === 'running')
  assert.equal(queued.length, 1)
  assert.equal(queued[0].id, 'req-dwell')
  assert.equal(running.length, 0)
  const w = snap.workers.find((x) => x.id === 'acc-d')
  assert.equal(w?.cells?.length, 1)
  assert.equal(w?.cells?.[0].id, 'req-dwell')
  assert.equal(w?.cells?.[0].name, 'sonnet')
  // Dwell-bound cell is a fly target but chrome stays gated until land.
  assert.equal(w?.cells?.[0].pendingInbound, true)
})

run('applyLiveProjection occupancy without queue is not pendingInbound', () => {
  const snap = applyLiveProjection(getMockInflightSnapshot(), {
    in_flight: 1,
    rpm: 1,
    workers: [
      {
        id: 'acc-p',
        name: 'wb-p',
        provider: 'workbuddy',
        busy: true,
        progress: 0.5,
        rate_limited: false,
        max_inflight: 2,
        in_flight: 1,
      },
    ],
    rate_limits: [],
    sticky: { bindings: 0, hits: 0, misses: 0, capacity: 64, ttl_seconds: 600 },
    occupancy: [{ id: 'req-run', name: 'opus', status: 'started', account_id: 'acc-p' }],
  })
  const w = snap.workers.find((x) => x.id === 'acc-p')
  assert.equal(w?.cells?.[0].id, 'req-run')
  assert.equal(w?.cells?.[0].pendingInbound, false)
  assert.equal(snap.jobs.filter((j) => j.status === 'running').length, 1)
})

run('applyLiveProjection request_id completion matches that cell', () => {
  const busy = applyLiveProjection(cloneMockInflight(MOCK_INFLIGHT), {
    in_flight: 2,
    rpm: 1,
    workers: [
      {
        id: 'acc-x',
        name: 'wb-x',
        provider: 'workbuddy',
        busy: true,
        progress: 0.5,
        rate_limited: false,
        max_inflight: 4,
        in_flight: 2,
      },
    ],
    rate_limits: [],
    sticky: { bindings: 0, hits: 0, misses: 0, capacity: 64, ttl_seconds: 600 },
    occupancy: [
      { id: 'req-keep', name: 'keep', status: 'started', account_id: 'acc-x' },
      { id: 'req-gone', name: 'gone', status: 'started', account_id: 'acc-x' },
    ],
  })
  assert.equal(busy.jobs.filter((j) => j.status === 'running').length, 2)
  const after = applyLiveProjection(busy, {
    in_flight: 1,
    rpm: 1,
    workers: [
      {
        id: 'acc-x',
        name: 'wb-x',
        provider: 'workbuddy',
        busy: true,
        progress: 0.5,
        rate_limited: false,
        max_inflight: 4,
        in_flight: 1,
      },
    ],
    rate_limits: [],
    sticky: { bindings: 0, hits: 0, misses: 0, capacity: 64, ttl_seconds: 600 },
    occupancy: [{ id: 'req-keep', name: 'keep', status: 'started', account_id: 'acc-x' }],
    recent_completions: [{ request_id: 'req-gone', account_id: 'acc-x', status: 'error' }],
  })
  assert.equal(after.jobs.filter((j) => j.status === 'running').length, 1)
  assert.equal(after.jobs.find((j) => j.status === 'running')?.id, 'req-keep')
  assert.ok(after.completions?.some((c) => c.id === 'req-gone' && c.ok === false))
  assert.equal(after.workers.find((w) => w.id === 'acc-x')?.cells?.length, 1)
})

run('tickMockInflight finishes one cell without clearing siblings', () => {
  const base = cloneMockInflight(MOCK_INFLIGHT)
  const w = base.workers.find((x) => x.id === 'acc-wb-a1')
  assert.ok(w && (w.cells?.length ?? 0) >= 2)
  const siblingIds = (w!.cells ?? []).map((c) => c.id).filter((id) => id !== 'j1')
  assert.ok(siblingIds.length >= 1)
  // Force first cell to completion threshold; leave siblings mid-flight.
  for (const j of base.jobs) {
    if (j.id === 'j1') j.progress = 0.999
    else if (j.accountId === 'acc-wb-a1') j.progress = 0.4
  }
  const next = tickMockInflight(base, 300)
  const w2 = next.workers.find((x) => x.id === 'acc-wb-a1')
  assert.ok(w2)
  // Finished cell is gone (may refill from queue, so length can stay the same).
  assert.ok(!(w2!.cells ?? []).some((c) => c.id === 'j1'))
  // At least one prior sibling still occupies a cell — account not wiped.
  assert.ok(siblingIds.some((id) => (w2!.cells ?? []).some((c) => c.id === id)))
  assert.ok((w2!.inFlight ?? 0) >= 1)
  // One completion (or retry) for j1 only — not a bulk clear of all cells.
  const terminal = next.completions?.filter((c) => c.id === 'j1') ?? []
  const retried = next.jobs.some((j) => j.id === 'j1' && j.status === 'queued')
  assert.ok(terminal.length === 1 || retried)
})

console.log('all mockInflight tests passed')

run('idle snapshot is empty calm board', () => {
  const idle = getIdleInflightSnapshot()
  assert.equal(idle.jobs.length, 0)
  assert.equal(idle.workers.length, 0)
  assert.equal(idle.throughputPerMin, 0)
  assert.equal(idle.waitP95Ms, 0)
  assert.equal(idle.totals.done, 0)
  assert.equal(idle.totals.failed, 0)
  assert.ok(isIdleInflightSnapshot(idle))
  assert.equal(isIdleInflightSnapshot(getMockInflightSnapshot()), false)
})
