import { useEffect, useRef, useState, type ReactNode } from 'react'
import { ProviderMark } from '@/components/ProviderMark'
import type { QueueJob, QueueJobKind, QueueRateLimit, QueueWorker } from '@/components/overview/AgentQueue'
import { clampSlotConcurrency, type MockAccountSlot, type MockInflightJob, type MockInflightSnapshot, type MockJobKind, type MockSlotCell } from '@/lib/mockInflight'

/** Keep the worker row long enough for the bar to land, then fly into Done or Failed. */
const HOLD_WORKER_MS = 800
/** Finished jobs stay in the model so the depth chart and per-minute throughput can see them. */
const KEEP_TERMINAL_MS = 90_000
/** The flow is one row per worker. Keep it to a short rail: one row per account, then extra in-flight cells. */
const MAX_ROWS = 8

export function queueKind(kind: MockJobKind): QueueJobKind {
  if (kind === 'tools') return 'tool'
  if (kind === 'embed') return 'agent'
  if (kind === 'chat') return 'llm'
  return 'human'
}

type Stamp = { enqueuedAt: number; startedAt?: number }

type RunningMeta = {
  accountId: string
  workerId: string
  workerName: string
  provider: string
  enqueuedAt: number
  startedAt: number
  rateLimited: boolean
}

type TerminalJob = {
  id: string
  name: string
  kind: MockJobKind
  ok: boolean
  endedAt: number
  enqueuedAt: number
  startedAt: number
  workerId: string
  workerName: string
  provider: string
  rateLimited: boolean
}

function workerIdFor(job: MockInflightJob) {
  return job.accountId ? `${job.accountId}::${job.id}` : job.id
}

function remember(job: MockInflightJob, now: number, stamps: Map<string, Stamp>) {
  let stamp = stamps.get(job.id)
  const startedAtMs = job.startedAtMs && job.startedAtMs > 0 ? job.startedAtMs : undefined
  if (!stamp) {
    if (job.status === 'queued') {
      stamp = { enqueuedAt: startedAtMs ?? now - Math.max(0, job.elapsedMs) }
    } else {
      const startedAt = startedAtMs ?? now - Math.max(0, job.elapsedMs)
      stamp = { enqueuedAt: startedAt, startedAt }
    }
    stamps.set(job.id, stamp)
    return stamp
  }
  if (job.status === 'running' && stamp.startedAt == null) stamp.startedAt = startedAtMs ?? now
  if (job.status === 'queued') stamp.startedAt = undefined
  return stamp
}

const EMPTY = {
  jobs: [] as QueueJob[],
  workers: [] as QueueWorker[],
  rateLimits: [] as QueueRateLimit[],
}

/**
 * Project the overview inflight snapshot onto AgentQueue.
 * One worker row per in-flight request so account concurrency stays visible.
 * A rate limit pauses that account's rows. Chart bands do not pause the board.
 */
export function useInflightQueueModel(
  snap: MockInflightSnapshot,
  now: number,
  active: boolean,
  stickyLabel?: string,
) {
  const stamps = useRef(new Map<string, Stamp>())
  const runningMeta = useRef(new Map<string, RunningMeta>())
  const [terminals, setTerminals] = useState<TerminalJob[]>([])

  useEffect(() => {
    if (!active) {
      stamps.current.clear()
      runningMeta.current.clear()
      setTerminals((list) => (list.length ? [] : list))
      return
    }
    const prev = runningMeta.current
    const comps = snap.completions ?? []
    const tnow = Date.now()
    setTerminals((list) => {
      const liveIds = new Set(snap.jobs.map((job) => job.id))
      const keep = list.filter((item) => tnow - item.endedAt < KEEP_TERMINAL_MS && !liveIds.has(item.id))
      const ids = new Set(keep.map((item) => item.id))
      for (const completion of comps) {
        if (ids.has(completion.id)) continue
        const meta = prev.get(completion.id)
        const account = snap.workers.find((worker) => worker.id === meta?.accountId)
        keep.push({
          id: completion.id,
          name: completion.name,
          kind: completion.kind,
          ok: completion.ok,
          endedAt: tnow,
          enqueuedAt: meta?.enqueuedAt ?? tnow - 1000,
          startedAt: meta?.startedAt ?? tnow - 500,
          workerId: meta?.workerId ?? meta?.accountId ?? completion.id,
          workerName: account?.name ?? meta?.workerName ?? '',
          provider: account?.provider ?? meta?.provider ?? '',
          rateLimited: account?.rateLimited ?? meta?.rateLimited ?? false,
        })
      }
      if (
        keep.length === list.length &&
        comps.length === 0 &&
        keep.every((item, index) => item.id === list[index]?.id)
      ) {
        return list
      }
      return keep
    })
    const next = new Map<string, RunningMeta>()
    for (const job of snap.jobs) {
      if (job.status !== 'running' || !job.accountId) continue
      const stamp = stamps.current.get(job.id)
      const account = snap.workers.find((worker) => worker.id === job.accountId)
      next.set(job.id, {
        accountId: job.accountId,
        workerId: workerIdFor(job),
        workerName: account?.name ?? '',
        provider: account?.provider ?? '',
        enqueuedAt: stamp?.enqueuedAt ?? tnow,
        startedAt: stamp?.startedAt ?? tnow,
        rateLimited: Boolean(account?.rateLimited),
      })
    }
    runningMeta.current = next
  }, [active, snap])

  if (!active) return EMPTY

  const keepIds = new Set(snap.jobs.map((job) => job.id))
  for (const item of terminals) keepIds.add(item.id)
  for (const job of snap.jobs) remember(job, now, stamps.current)
  for (const id of stamps.current.keys()) {
    if (!keepIds.has(id)) stamps.current.delete(id)
  }

  const iconFor = (provider: string): ReactNode => <ProviderMark provider={provider} size={14} />
  const activeCells = (account: MockAccountSlot) => (account.cells ?? []).filter((cell) => !cell.pendingInbound)
  const rowId = (account: MockAccountSlot, cell?: MockSlotCell) => (cell ? `${account.id}::${cell.id}` : account.id)
  const detailFor = (account: MockAccountSlot) => {
    const cap = clampSlotConcurrency(account.concurrency ?? 1)
    const occ = activeCells(account).length
    return cap > 1 ? `${occ}/${cap}` : undefined
  }
  const rank = (account: MockAccountSlot) => (activeCells(account).length ? 0 : account.rateLimited ? 1 : 2)
  const ordered = [...snap.workers].sort((a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name))

  type Plan = { id: string; account: MockAccountSlot; cell?: MockSlotCell }
  const plans: Plan[] = []
  const planIds = new Set<string>()
  const take = (account: MockAccountSlot, cell?: MockSlotCell) => {
    const id = rowId(account, cell)
    if (planIds.has(id) || plans.length >= MAX_ROWS) return
    planIds.add(id)
    plans.push({ id, account, cell })
  }
  for (const account of ordered) take(account, activeCells(account)[0])
  let added = true
  while (added && plans.length < MAX_ROWS) {
    added = false
    for (const account of ordered) {
      if (plans.length >= MAX_ROWS) break
      const cells = activeCells(account)
      const taken = plans.filter((plan) => plan.account.id === account.id).length
      if (taken < cells.length) {
        take(account, cells[taken])
        added = true
      }
    }
  }

  const rows: QueueWorker[] = plans.map((plan) => ({
    id: plan.id,
    name: plan.account.name,
    detail: detailFor(plan.account),
    rateLimited: plan.account.rateLimited,
    icon: iconFor(plan.account.provider),
  }))
  const seen = new Set(rows.map((row) => row.id))
  const push = (row: QueueWorker) => {
    if (seen.has(row.id) || rows.length >= MAX_ROWS) return
    seen.add(row.id)
    rows.push(row)
  }

  const jobs: QueueJob[] = []
  for (const job of snap.jobs) {
    if (job.status !== 'queued' && job.status !== 'running') continue
    const stamp = stamps.current.get(job.id) ?? remember(job, now, stamps.current)
    const worker = job.status === 'running' && job.accountId ? workerIdFor(job) : undefined
    if (worker && !seen.has(worker)) {
      const account = snap.workers.find((item) => item.id === job.accountId)
      push({
        id: worker,
        name: account?.name ?? job.name,
        rateLimited: account?.rateLimited,
        icon: iconFor(account?.provider ?? ''),
      })
    }
    jobs.push({
      id: job.id,
      name: job.name,
      kind: queueKind(job.kind),
      status: job.status,
      priority: job.priority,
      enqueuedAt: stamp.enqueuedAt,
      startedAt: job.status === 'running' ? stamp.startedAt : undefined,
      worker,
      progress: job.status === 'running' ? job.progress : undefined,
      attempt: job.attempt,
      detail: job.sticky && stickyLabel ? stickyLabel : undefined,
    })
  }

  for (const item of terminals) {
    if (snap.jobs.some((job) => job.id === item.id)) continue
    if (now - item.endedAt < HOLD_WORKER_MS) {
      push({
        id: item.workerId,
        name: item.workerName || item.name,
        rateLimited: item.rateLimited,
        icon: iconFor(item.provider),
      })
    }
    jobs.push({
      id: item.id,
      name: item.name,
      kind: queueKind(item.kind),
      status: item.ok ? 'done' : 'failed',
      enqueuedAt: item.enqueuedAt,
      startedAt: item.startedAt,
      endedAt: item.endedAt,
      worker: item.workerId,
      progress: 1,
    })
  }

  const pinnedIds = new Set<string>()
  for (const job of jobs) {
    if (job.status === 'running' && job.worker) pinnedIds.add(job.worker)
  }
  for (const item of terminals) {
    if (now - item.endedAt < HOLD_WORKER_MS) pinnedIds.add(item.workerId)
  }
  const pinned = rows.filter((row) => pinnedIds.has(row.id) || row.rateLimited)
  const rest = rows.filter((row) => !pinnedIds.has(row.id) && !row.rateLimited)
  const workers = pinned.length >= MAX_ROWS ? pinned : [...pinned, ...rest.slice(0, MAX_ROWS - pinned.length)]
  const workerIds = new Set(workers.map((worker) => worker.id))
  const visibleJobs = jobs.filter((job) => job.status !== 'running' || (job.worker != null && workerIds.has(job.worker)))

  const rateLimits: QueueRateLimit[] = snap.rateLimits
    .filter((limit) => limit.remainingMs > 0)
    .map((limit) => ({
      from: now - 8_000,
      until: now + limit.remainingMs,
      label: limit.label,
      bandOnly: true,
    }))

  return { jobs: visibleJobs, workers, rateLimits }
}
