import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { Card } from '@heroui/react'
import { ArrowUpRight, CaretRight } from '@phosphor-icons/react'
import type { LiveSnapshot, LiveSticky } from '@/api/live'
import {
  applyLiveProjection,
  cloneMockInflight,
  getIdleInflightSnapshot,
  getMockInflightSnapshot,
  isIdleInflightSnapshot,
  tickMockInflight,
  type MockInflightSnapshot,
} from '@/lib/mockInflight'
import { AgentQueue, type AgentQueueLabels } from '@/components/overview/AgentQueue'
import { useInflightQueueModel } from '@/components/overview/projectInflightQueue'
import { CompactSwitch } from '@/components/ui/CompactSwitch'
import { prefersReducedMotion } from '@/lib/motion'

const MOCK_DEMO_STORAGE_KEY = 'cli2api_inflight_mock_demo'

function readMockDemoPref(): boolean {
  try {
    return localStorage.getItem(MOCK_DEMO_STORAGE_KEY) === '1'
  } catch {
    return false
  }
}

function writeMockDemoPref(on: boolean) {
  try {
    localStorage.setItem(MOCK_DEMO_STORAGE_KEY, on ? '1' : '0')
  } catch {
    /* ignore quota / private mode */
  }
}

type T = (key: string, vars?: Record<string, string | number>) => string

/**
 * Overview live panel. The queue motion is AgentQueue.
 * Collapsed by default — no /api/live consume and no mock loop until expanded.
 * Mock tick only when the demo switch is on and live is null.
 * Pause, concurrency, and cancel stay off: the gateway does not expose them.
 */
export function InflightLiveCard({
  t,
  snapshot,
  live = null,
  expanded: expandedProp,
  onExpandedChange,
  defaultExpanded = false,
  forceMockDemo = false,
  mockPace = 1,
  bare = false,
}: {
  t: T
  snapshot?: MockInflightSnapshot
  /** Parent /api/live snapshot — only passed while expanded so the card does not drive fetches. */
  live?: LiveSnapshot | null
  expanded?: boolean
  onExpandedChange?: (open: boolean) => void
  defaultExpanded?: boolean
  /** Login showcase: play the mock loop without reading or writing the overview demo preference. */
  forceMockDemo?: boolean
  /** Scale mock time. Login uses a slower pace; overview stays at 1. */
  mockPace?: number
  /** Login showcase: the queue card only — no title row, demo switch, or view-all link. */
  bare?: boolean
}) {
  const controlled = snapshot !== undefined
  const [uncontrolledOpen, setUncontrolledOpen] = useState(defaultExpanded)
  const expanded = expandedProp ?? uncontrolledOpen
  const setExpanded = (open: boolean) => {
    onExpandedChange?.(open)
    if (expandedProp === undefined) setUncontrolledOpen(open)
  }

  const [mockDemoEnabled, setMockDemo] = useState(() => forceMockDemo || readMockDemoPref())
  const setMockDemoPref = (on: boolean) => {
    setMockDemo(on)
    if (!forceMockDemo) writeMockDemoPref(on)
  }
  const [liveSnap, setLiveSnap] = useState<MockInflightSnapshot>(() =>
    cloneMockInflight(snapshot ?? getIdleInflightSnapshot()),
  )
  const [useMockFallback, setUseMockFallback] = useState(false)
  const [sticky, setSticky] = useState<LiveSticky | null>(null)
  const [clockMs, setClockMs] = useState(() => Date.now())
  const snap = controlled ? snapshot! : liveSnap
  const viewingSessionRef = useRef(false)

  useLayoutEffect(() => {
    if (controlled) {
      setLiveSnap(cloneMockInflight(snapshot!))
      setUseMockFallback(false)
      setSticky(null)
      viewingSessionRef.current = false
      return
    }
    if (!expanded) {
      setUseMockFallback(false)
      viewingSessionRef.current = false
      return
    }
    if (live !== null) {
      setUseMockFallback(false)
      setSticky(live.sticky)
      setClockMs(Date.now())
      setLiveSnap((prev) => {
        const base = !viewingSessionRef.current
          ? { ...cloneMockInflight(prev), waitP95Ms: 1, totals: { done: 0, failed: 0 } }
          : prev
        viewingSessionRef.current = true
        return applyLiveProjection(base, live)
      })
      return
    }
    setSticky(null)
    viewingSessionRef.current = false
    if (mockDemoEnabled) {
      setUseMockFallback(true)
      setLiveSnap((prev) =>
        isIdleInflightSnapshot(prev) ? cloneMockInflight(getMockInflightSnapshot()) : prev,
      )
    } else {
      setUseMockFallback(false)
      setLiveSnap(cloneMockInflight(getIdleInflightSnapshot()))
    }
  }, [controlled, snapshot, expanded, live, mockDemoEnabled])

  useEffect(() => {
    if (controlled || !expanded || !useMockFallback || !mockDemoEnabled || live !== null) return
    if (prefersReducedMotion()) return
    let raf = 0
    let last = performance.now()
    let alive = true
    const step = (now: number) => {
      if (!alive) return
      const dt = now - last
      if (!document.hidden && dt >= 260) {
        last = now
        const pace = Number.isFinite(mockPace) ? Math.min(1, Math.max(0.2, mockPace ?? 1)) : 1
        setLiveSnap((prev) => tickMockInflight(prev, dt * pace))
      }
      raf = requestAnimationFrame(step)
    }
    raf = requestAnimationFrame(step)
    return () => {
      alive = false
      cancelAnimationFrame(raf)
    }
  }, [controlled, expanded, useMockFallback, mockDemoEnabled, live, mockPace])

  useEffect(() => {
    if (!expanded) return
    if (prefersReducedMotion()) {
      setClockMs(Date.now())
      return
    }
    let raf = 0
    let last = 0
    let alive = true
    const step = (now: number) => {
      if (!alive) return
      if (!document.hidden && now - last >= 150) {
        last = now
        setClockMs(Date.now())
      }
      raf = requestAnimationFrame(step)
    }
    setClockMs(Date.now())
    raf = requestAnimationFrame(step)
    return () => {
      alive = false
      cancelAnimationFrame(raf)
    }
  }, [expanded])

  const model = useInflightQueueModel(snap, clockMs, expanded, t('inflightSticky'))
  const labels = useMemo<Partial<AgentQueueLabels>>(() => ({
    waiting: t('inflightWaiting'),
    running: t('inflightRunning'),
    throughput: t('inflightThroughput'),
    waitP95: t('inflightWaitP95'),
    queue: t('inflightQueue'),
    workers: t('inflightSlots'),
    nothingWaiting: t('inflightQueueEmpty'),
    done: t('inflightDone'),
    failed: t('inflightFailed'),
    depth: t('inflightDepth'),
    idle: t('inflightSlotIdle'),
    paused: t('inflightSlotPaused'),
    live: t('inflightLiveBadge'),
    rateLimited: t('cooldown'),
    now: t('inflightNow'),
    ago: t('inflightAgo'),
    more: t('inflightMore'),
    high: t('inflightHigh'),
    low: t('inflightLow'),
    normal: t('inflightNormal'),
    priority: t('inflightPriority'),
    attempt: t('inflightAttempt'),
    position: t('inflightPosition'),
    waited: t('inflightWaited'),
    worker: t('account'),
    close: t('inflightClose'),
    kind: {
      llm: t('inflightKindModel'),
      tool: t('inflightKindTool'),
      agent: t('inflightKindEmbed'),
      human: t('inflightKindOther'),
    },
    status: {
      queued: t('inflightWaiting'),
      running: t('inflightRunning'),
      done: t('inflightDone'),
      failed: t('inflightFailed'),
    },
    formatPosition: (n, total) => t('inflightPositionValue', { n, total }),
    formatAttempt: (n, max) => (max ? t('inflightAttemptValue', { n, max }) : String(n)),
    announceFailed: (name, error) => (
      error ? t('inflightAnnounceFailedDetail', { name, error }) : t('inflightAnnounceFailed', { name })
    ),
    announceRetry: (name, attempt) => t('inflightAnnounceRetry', { name, attempt }),
    announceLimited: (seconds) => t('inflightAnnounceLimited', { seconds }),
    announceResumed: t('inflightAnnounceResumed'),
    announcePressure: (n, line) => t('inflightAnnouncePressure', { n, line }),
  }), [t])

  const liveBadge = !useMockFallback && Boolean(live)
  const mockBadge = useMockFallback && mockDemoEnabled && !live
  const stickyBindings = sticky?.bindings ?? 0

  return (
    <Card
      data-testid="inflight-live-card"
      data-expanded={expanded ? 'true' : 'false'}
      data-gsap-reveal
      variant={bare ? 'transparent' : undefined}
      className={`group overflow-hidden p-0 ${bare ? 'bg-transparent shadow-none' : ''}`}
    >
      {bare ? null : (
        <div className="flex items-start justify-between gap-3 px-4 py-3">
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
                <span className="mono inline-flex items-center gap-1 rounded-[3px] px-1.5 py-0.5 text-caption text-muted">
                  <span className="status-dot" data-state={liveBadge ? 'ok' : ''} />
                  {liveBadge ? t('inflightLiveBadge') : mockBadge ? t('inflightMockBadge') : t('inflightIdleBadge')}
                </span>
                {expanded && stickyBindings > 0 ? (
                  <span className="mono text-caption text-muted">{t('inflightStickyBindings', { n: stickyBindings })}</span>
                ) : null}
              </div>
              <p className="mt-0.5 text-caption text-muted">{t('inflightHint')}</p>
            </div>
          </button>
          <div className="flex shrink-0 flex-col items-end gap-2">
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
            {expanded && !controlled ? (
              <div className="flex items-center gap-1.5" onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()}>
                <CompactSwitch
                  size="sm"
                  isSelected={mockDemoEnabled}
                  ariaLabel={t('inflightMockToggleAria')}
                  label={t('inflightMockToggle')}
                  onChange={setMockDemoPref}
                />
              </div>
            ) : null}
          </div>
        </div>
      )}

      {expanded ? (
        <div id="inflight-live-body">
          <AgentQueue
            embedded={!bare}
            jobs={model.jobs}
            workers={model.workers}
            now={clockMs}
            rateLimits={model.rateLimits}
            backpressure={snap.backpressure}
            totals={snap.totals}
            title={t('inflightTitle')}
            labels={labels}
          />
        </div>
      ) : null}
    </Card>
  )
}
