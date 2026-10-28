import { lazy, Suspense, useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import gsap from 'gsap'
import { Link, useNavigate } from 'react-router-dom'
import { Button, Card, Chip, Tooltip } from '@heroui/react'
import {
  ArrowUpRight,
  ChartLineUp,
  Pause,
  Play,
  TrendDown,
  TrendUp,
} from '@phosphor-icons/react'
import { fetchRequestStats, type RequestStats } from '@/api/logs'
import { fetchSystemResources, type SystemResources } from '@/api/system'
import type { Overview } from '@/api/types'
import { CountUp } from '@/components/overview/CountUp'
import { TrafficChartEmpty } from '@/components/overview/TrafficChartEmpty'

const TrafficChart = lazy(() => import('@/components/overview/TrafficChart').then((mod) => ({ default: mod.TrafficChart })))
import { StatsWindowControls } from '@/components/overview/StatsWindowControls'
import { STATS_WINDOW_LABEL, statsQuery, type StatsWindow } from '@/lib/statsWindow'
import type { DateRangeValue } from '@/components/ui/TimeRangePicker'
import { PageAlert } from '@/components/ui/PageAlert'
import { OverviewPageSkeleton, RankListSkeleton, SkeletonBlock, TrafficChartSkeleton } from '@/components/ui/PageSkeletons'
import { useI18n } from '@/hooks/useI18n'
import { logsHref } from '@/lib/logsLink'
import { useOverview } from '@/hooks/useOverview'
import { fetchAccounts, fetchModels } from '@/api/overview'
import { formatBytes, formatCompact, formatLatency, formatPercent } from '@/lib/format'
import { accountProviderFamilyLabel, accountProviderLabel } from '@/lib/provider'
import { ProviderMark } from '@/components/ProviderMark'
import { LiveStrip } from '@/components/overview/LiveStrip'
import { useLive } from '@/hooks/useLive'

type AccountRow = NonNullable<Overview['accounts']>[number]

const STATS_REFRESH_MS = 30_000

const EMPTY_STATS: RequestStats = {
  window: { from: '', to: '', hours: 24 },
  totals: { requests: 0, ok: 0, incomplete: 0, error: 0, canceled: 0, streaming: 0, success_rate: 0 },
  latency: {},
  tokens: { prompt: 0, completion: 0, cache_read: 0, total: 0 },
  status: [],
  errors: [],
  models: [],
  accounts: [],
  providers: [],
  series: [],
}

function familyLabel(provider: string | undefined, t: (key: string) => string) {
  if (!provider || provider === '(unknown)') return t('statsUnknown')
  return accountProviderFamilyLabel(provider, t)
}

function accountLabel(accounts: AccountRow[], id: string, unassigned: string) {
  if (!id || id === '(unassigned)') return unassigned
  return accounts.find((account) => account.id === id)?.name || id
}

// Error kinds the logs page can filter on (its error_kind select).
const LOGS_ERROR_KINDS = new Set(['quota', 'rate_limit', 'auth', 'not_ready', 'unavailable', 'invalid_request', 'model_not_available'])

function errorLabel(kind: string, t: (key: string) => string) {
  const keys: Record<string, string> = {
    quota: 'logsKindQuota',
    rate_limit: 'logsKindRateLimit',
    auth: 'logsKindAuth',
    not_ready: 'logsKindNotReady',
    unavailable: 'logsKindUnavailable',
    invalid_request: 'logsKindInvalidRequest',
    model_not_available: 'logsKindModelNotAvailable',
  }
  return keys[kind] ? t(keys[kind]) : kind
}

const STATUS_ORDER = ['ok', 'incomplete', 'error', 'canceled'] as const
const STATUS_TONE: Record<(typeof STATUS_ORDER)[number], string> = {
  ok: 'bg-success',
  incomplete: 'bg-warning',
  error: 'bg-danger',
  canceled: 'bg-muted',
}
const STATUS_LABEL: Record<(typeof STATUS_ORDER)[number], string> = {
  ok: 'logsFilterOk',
  incomplete: 'logsFilterIncomplete',
  error: 'logsFilterError',
  canceled: 'logsFilterCanceled',
}

function ratio(part: number, whole: number) {
  return whole > 0 ? part / whole : null
}

/** Remaining time until an RFC 3339 instant, as "1h 37m" / "4m 12s" / "12s". */
function formatRemaining(until: string | null | undefined, now: number) {
  if (!until) return null
  const end = Date.parse(until)
  if (!Number.isFinite(end)) return null
  const left = Math.max(0, Math.round((end - now) / 1000))
  if (left <= 0) return null
  const h = Math.floor(left / 3600)
  const m = Math.floor((left % 3600) / 60)
  const sec = left % 60
  if (h) return `${h}h ${m}m`
  if (m) return `${m}m ${String(sec).padStart(2, '0')}s`
  return `${sec}s`
}

/** Latest future cooldown deadline of an account (runtime down_until or stored cooldown_until). */
function coolingUntil(account: AccountRow, now: number) {
  const candidates = [account.down_until, account.cooldown_until]
    .filter((value): value is string => Boolean(value))
    .filter((value) => Date.parse(value) > now)
  if (!candidates.length) return null
  return candidates.reduce((a, b) => (Date.parse(a) >= Date.parse(b) ? a : b))
}

function quotaTone(percentage?: number, exceeded?: boolean) {
  if (exceeded) return 'danger'
  if ((percentage ?? 0) >= 80) return 'warning'
  return 'ok'
}

export function OverviewPage() {
  const { lang, t } = useI18n()
  const { overview, loading, error } = useOverview()
  const [statsWindow, setStatsWindow] = useState<StatsWindow>('today')
  const [customRange, setCustomRange] = useState<DateRangeValue | null>(null)
  const [paused, setPaused] = useState(false)
  const navigate = useNavigate()
  const [stats, setStats] = useState<RequestStats | null>(null)
  const [statsError, setStatsError] = useState('')
  const [statsLoading, setStatsLoading] = useState(true)
  const [accounts, setAccounts] = useState<NonNullable<Overview['accounts']>>([])
  const [accountsLoading, setAccountsLoading] = useState(true)
  const [modelCount, setModelCount] = useState(0)
  const [resources, setResources] = useState<SystemResources | null>(null)
  const [now, setNow] = useState(() => Date.now())
  const { live, samples: liveSamples } = useLive()
  const liveById = new Map((live?.accounts ?? []).map((item) => [item.id, item]))

  const proxyOk = Boolean(overview?.proxy?.ok)
  const workerOk = Boolean(overview?.worker?.ok)
  const readyAccounts = overview?.worker?.ready_count ?? 0
  const hotAccounts = overview?.worker?.hot_count ?? 0
  const coolingAccounts = overview?.worker?.cooling_count ?? 0
  const inFlight = live?.in_flight ?? overview?.worker?.in_flight ?? 0
  const traffic = stats ?? EMPTY_STATS
  const affinity = overview?.routing?.session_affinity
  const affinityLookups = (affinity?.hits ?? 0) + (affinity?.misses ?? 0)
  const affinityHitRate = ratio(affinity?.hits ?? 0, affinityLookups)
  const coolingRows = accounts
    .map((account) => ({ account, until: coolingUntil(account, now) }))
    .filter((row): row is { account: AccountRow; until: string } => row.until != null)
  const hasCooling = coolingRows.length > 0

  // 1s clock only while something is counting down.
  useEffect(() => {
    if (!hasCooling) return
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [hasCooling])

  useEffect(() => {
    let cancelled = false
    void Promise.allSettled([fetchAccounts(false), fetchModels()])
      .then(([accountsResult, modelsResult]) => {
        if (cancelled) return
        if (accountsResult.status === 'fulfilled') setAccounts(accountsResult.value.data || [])
        if (modelsResult.status === 'fulfilled') setModelCount((modelsResult.value.data || []).length)
      })
      .finally(() => { if (!cancelled) setAccountsLoading(false) })
    return () => { cancelled = true }
  }, [])

  const customFrom = customRange?.start?.toString() ?? ''
  const customTo = customRange?.end?.toString() ?? ''
  const statsRequestRef = useRef(0)
  const loadStats = useCallback((quiet: boolean) => {
    const query = statsQuery(statsWindow, customRange)
    if (!query) {
      setStatsLoading(false)
      return
    }
    const version = ++statsRequestRef.current
    void fetchRequestStats(query)
      .then((data) => {
        if (version !== statsRequestRef.current) return
        setStats(data)
        setStatsError('')
      })
      .catch((err) => {
        if (version !== statsRequestRef.current || quiet) return
        setStats(null)
        setStatsError(err instanceof Error ? err.message : String(err))
      })
      .finally(() => {
        if (version === statsRequestRef.current) setStatsLoading(false)
      })
    // customRange is tracked through its string form.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [statsWindow, customFrom, customTo])

  useEffect(() => {
    loadStats(false)
    return () => { statsRequestRef.current++ }
  }, [loadStats])

  // Background refresh every 30s; skipped while paused or the tab is hidden,
  // and caught up as soon as the tab becomes visible again. Quiet so a failed
  // refresh keeps the last good traffic panel instead of blanking it.
  useEffect(() => {
    if (paused) return
    let last = Date.now()
    const tick = () => {
      if (document.hidden) return
      last = Date.now()
      loadStats(true)
    }
    const timer = setInterval(tick, STATS_REFRESH_MS)
    const onVisible = () => {
      if (!document.hidden && Date.now() - last >= STATS_REFRESH_MS) tick()
    }
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      clearInterval(timer)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [paused, loadStats])

  // Resource usage polls on its own 5s cadence; independent of the stats window.
  useEffect(() => {
    let cancelled = false
    const tick = () => {
      void fetchSystemResources()
        .then((data) => { if (!cancelled) setResources(data) })
        .catch(() => { /* keep last good sample */ })
    }
    tick()
    const timer = setInterval(tick, 5000)
    return () => {
      cancelled = true
      clearInterval(timer)
    }
  }, [])

  // Display-only trend: second half of the selected window vs the first half,
  // derived from the series the page already loads (no extra API call).
  const half = Math.floor(traffic.series.length / 2)
  const sum = (points: typeof traffic.series, key: 'requests' | 'ok') => points.reduce((total, point) => total + point[key], 0)
  const firstHalf = traffic.series.slice(0, half)
  const secondHalf = traffic.series.slice(half)
  const change = (before: number, after: number) => (before > 0 ? (after - before) / before : null)
  const requestTrend = change(sum(firstHalf, 'requests'), sum(secondHalf, 'requests'))
  const firstRate = sum(firstHalf, 'requests') ? sum(firstHalf, 'ok') / sum(firstHalf, 'requests') : null
  const secondRate = sum(secondHalf, 'requests') ? sum(secondHalf, 'ok') / sum(secondHalf, 'requests') : null
  const successTrend = firstRate != null && secondRate != null ? secondRate - firstRate : null
  const metrics = [
    { label: t('metricRequests'), value: traffic.totals.requests as number | null, kind: 'compact' as const, detail: t('statsWindowHint', { window: t(STATS_WINDOW_LABEL[statsWindow]) }), ok: traffic.totals.requests > 0, trend: requestTrend },
    { label: t('metricSuccess'), value: traffic.totals.success_rate, kind: 'percent' as const, detail: `${traffic.totals.ok} ${t('logsFilterOk')} · ${traffic.totals.incomplete} ${t('logsFilterIncomplete')} · ${traffic.totals.error} ${t('logsFilterError')}`, ok: traffic.totals.requests === 0 || traffic.totals.success_rate >= 0.9, trend: successTrend },
    { label: t('metricLatency'), value: traffic.latency.p95_ms ?? traffic.latency.avg_ms, kind: 'ms' as const, detail: `p50 ${formatLatency(traffic.latency.p50_ms)} · avg ${formatLatency(traffic.latency.avg_ms)}`, ok: traffic.latency.p95_ms == null || traffic.latency.p95_ms < 8000, trend: null as number | null },
    { label: t('metricTokens'), value: traffic.tokens.total, kind: 'compact' as const, detail: `${formatCompact(traffic.tokens.prompt)} / ${formatCompact(traffic.tokens.completion)}`, ok: true, trend: null as number | null },
  ]

  const cacheHitRate = ratio(traffic.tokens.cache_read, traffic.tokens.prompt)
  const streamingShare = ratio(traffic.totals.streaming, traffic.totals.requests)
  const secondaryMetrics = [
    { label: t('metricCacheHit'), value: cacheHitRate, kind: 'percent' as const, detail: t('metricCacheHitDetail', { read: formatCompact(traffic.tokens.cache_read), prompt: formatCompact(traffic.tokens.prompt) }) },
    { label: t('metricStreaming'), value: streamingShare, kind: 'percent' as const, detail: t('metricStreamingDetail', { count: traffic.totals.streaming, total: traffic.totals.requests }) },
    { label: t('metricCanceled'), value: traffic.totals.canceled as number | null, kind: 'int' as const, detail: t('metricCanceledDetail', { rate: formatPercent(ratio(traffic.totals.canceled, traffic.totals.requests) ?? 0) }) },
  ]
  const statusCounts = STATUS_ORDER.map((key) => ({
    key,
    count: traffic.status.find((item) => item.key === key)?.count ?? traffic.totals[key] ?? 0,
  }))

  const drill = (filters: Record<string, string>) => logsHref(filters, traffic.window.from, traffic.window.to)

  if (loading && !overview) return <OverviewPageSkeleton />

  return (
    <div className="space-y-5 sm:space-y-4">
      <section className="grid gap-3 pb-1 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end">
        <div>
          <h2 data-gsap-reveal className="text-title font-semibold">{t('homeDisplay')}</h2>
          <p className="mt-1 max-w-2xl text-body leading-6 text-muted">{t('homeLead')}</p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <StatsWindowControls
            value={statsWindow}
            onChange={setStatsWindow}
            custom={customRange}
            onCustomChange={setCustomRange}
            onBusy={() => setStatsLoading(true)}
            t={t}
          />
          <Tooltip delay={300}>
            <Button
              isIconOnly
              size="sm"
              variant="ghost"
              aria-label={paused ? t('statsRefreshResume') : t('statsRefreshPause')}
              aria-pressed={paused}
              onPress={() => setPaused((value) => !value)}
            >
              {paused ? <Play size={14} /> : <Pause size={14} />}
            </Button>
            <Tooltip.Content>{paused ? t('statsRefreshPaused') : t('statsRefreshAuto')}</Tooltip.Content>
          </Tooltip>
          <div className="text-left sm:text-right">
            <div className="mono text-caption text-muted">{overview?.time || '—'}</div>
            <div className="mt-1 inline-flex items-center gap-2 text-body font-medium">
              <span className="status-dot" data-state={proxyOk && workerOk ? 'ok' : 'danger'} />
              {proxyOk && workerOk ? t('running') : t('degraded')}
            </div>
          </div>
        </div>
      </section>

      {error ? <PageAlert title={t('failedOverview', { msg: error })} /> : null}

      <LiveStrip live={live} samples={liveSamples} t={t} />

      <section data-gsap-reveal className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {metrics.map((metric) => (
          <Card key={metric.label} className="gap-0 p-3.5">
            <div className="flex items-center justify-between gap-3">
              <div className="text-body text-muted">{metric.label}</div>
              <span className="status-dot" data-state={metric.ok ? 'ok' : 'danger'} />
            </div>
            <div className="mt-1.5 flex flex-wrap items-center gap-2">
              <div className="tabular text-metric font-medium leading-none">
                {statsLoading ? <SkeletonBlock className="h-7 w-24" /> : metric.value == null ? '—' : <CountUp value={metric.value} kind={metric.kind} />}
              </div>
              {!statsLoading && metric.trend != null ? <TrendChip value={metric.trend} hint={t('trendHalfHint')} /> : null}
            </div>
            <div className="mono mt-2 truncate text-caption text-muted">{statsLoading ? <SkeletonBlock className="h-3 w-28" /> : metric.detail}</div>
          </Card>
        ))}
      </section>

      <section className="grid gap-3 sm:grid-cols-3">
        {secondaryMetrics.map((metric) => (
          <Card key={metric.label} className="gap-0 p-3.5">
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:gap-3">
              <div className="min-w-0 flex-1">
                <div className="text-body text-muted">{metric.label}</div>
                <div className="mono mt-0.5 truncate text-caption text-muted">{statsLoading ? <SkeletonBlock className="h-3 w-28" /> : metric.detail}</div>
              </div>
              <div className="tabular text-section font-semibold sm:shrink-0">
                {statsLoading ? <SkeletonBlock className="h-5 w-12" /> : metric.value == null ? '—' : <CountUp value={metric.value} kind={metric.kind} />}
              </div>
            </div>
          </Card>
        ))}
      </section>

      <section className="grid gap-3">
        <Card data-gsap-reveal className="overflow-hidden p-0">
          <div className="flex items-center justify-between gap-3 border-b border-separator px-4 py-3">
            <div>
              <h3 className="font-semibold">{t('statsTraffic')}</h3>
              <p className="mt-0.5 text-caption text-muted">{t('statsTrafficHint')}</p>
            </div>
            <Chip size="sm" variant="soft" color={traffic.totals.error ? 'warning' : 'success'}>
              {formatPercent(traffic.totals.success_rate)}
            </Chip>
          </div>
          <div className="space-y-4 p-4">
            {statsError ? (
              <div className="text-body text-danger">{t('failedStats', { msg: statsError })}</div>
            ) : statsLoading ? (
              <TrafficChartSkeleton />
            ) : traffic.series.some((point) => point.requests > 0) ? (
              <Suspense fallback={<TrafficChartSkeleton />}>
                <TrafficChart
                  series={traffic.series}
                  lang={lang}
                  emptyLabel={t('statsEmptyTraffic')}
                  okLabel={t('logsFilterOk')}
                  errorLabel={t('logsFilterError')}
                  selectHint={t('statsDragHint')}
                  labels={{
                    requests: t('chartMetricRequests'),
                    tokens: t('chartMetricTokens'),
                    latency: t('chartMetricLatency'),
                    input: t('chartInput'),
                    output: t('chartOutput'),
                    cacheRead: t('chartCacheRead'),
                    p50: t('statsLatencyP50'),
                    p95: t('statsLatencyP95'),
                    total: t('chartTotal'),
                  }}
                  onSelectRange={(from, to) => navigate(logsHref({}, from, to))}
                />
              </Suspense>
            ) : (
              <TrafficChartEmpty emptyLabel={t('statsEmptyTraffic')} />
            )}
            <div className="grid grid-cols-2 gap-x-4 gap-y-4 pt-3 sm:grid-cols-4 sm:gap-3">
              {[
                [t('statsLatencyP50'), formatLatency(traffic.latency.p50_ms)],
                [t('statsLatencyP95'), formatLatency(traffic.latency.p95_ms)],
                [t('statsLatencyAvg'), formatLatency(traffic.latency.avg_ms)],
                [t('statsTTFB'), formatLatency(traffic.latency.ttfb_avg_ms)],
              ].map(([label, value]) => (
                <div key={String(label)}>
                  <div className="text-caption text-muted">{label}</div>
                  <div className="mono mt-1 text-body font-medium">{statsLoading ? <SkeletonBlock className="h-4 w-16" /> : value}</div>
                </div>
              ))}
            </div>
          </div>
        </Card>
      </section>

      <section className="grid gap-3 xl:grid-cols-[minmax(0,1.15fr)_minmax(0,.85fr)]">
        <Card data-gsap-reveal className="overflow-hidden p-0">
          <div className="flex items-center justify-between gap-3 border-b border-separator px-4 py-3">
            <div>
              <h3 className="font-semibold">{t('statsPool')}</h3>
              <p className="mt-0.5 text-caption text-muted">{t('statsPoolHint')}</p>
            </div>
            <Link to="/accounts" className="inline-flex items-center gap-1 text-caption font-medium text-muted hover:text-foreground">
              {t('navAccounts')}
              <ArrowUpRight size={12} />
            </Link>
          </div>
          <div className="grid grid-cols-2 gap-y-1 border-b border-separator sm:grid-cols-4 sm:gap-y-0">
            {[
              [t('ready'), readyAccounts, 'ok'],
              [t('hot'), hotAccounts, hotAccounts ? 'ok' : ''],
              [t('inFlight'), inFlight, inFlight ? 'ok' : ''],
              [t('statsAffinityHit'), affinityHitRate == null ? '—' : formatPercent(affinityHitRate), '', affinity ? t('statsAffinityHint', { hits: affinity.hits ?? 0, misses: affinity.misses ?? 0, bindings: affinity.bindings ?? 0 }) : ''],
            ].map(([label, value, state, hint]) => (
              <div key={String(label)} className="px-4 py-3 sm:py-2.5" title={hint ? String(hint) : undefined}>
                <div className="text-caption text-muted">{label}</div>
                <div className="mt-1.5 flex items-center gap-2 sm:mt-1">
                  <span className="mono text-section font-semibold">{value}</span>
                  {state ? <span className="status-dot" data-state={state} /> : null}
                </div>
              </div>
            ))}
          </div>
          {!accountsLoading && coolingRows.length ? (
            <div className="border-b border-separator px-4 py-2.5">
              <div className="text-caption font-medium text-muted">{t('statsCoolingDetails')}</div>
              <div className="mt-2 space-y-1.5">
                {coolingRows.map(({ account, until }) => (
                  <div key={account.id} className="flex flex-col gap-1.5 text-caption sm:flex-row sm:items-center sm:gap-2">
                    <div className="flex min-w-0 items-center gap-2">
                      <span className="status-dot shrink-0" data-state="warn" />
                      <span className="min-w-0 flex-1 truncate font-medium">{account.name || account.id}</span>
                    </div>
                    <div className="flex items-center justify-between gap-3 pl-4 sm:contents sm:pl-0">
                      <span className="mono shrink-0 text-muted">{account.last_error_kind ? errorLabel(account.last_error_kind, t) : t('statsUnknown')}</span>
                      <span className="mono shrink-0 tabular sm:w-16 sm:text-right">{formatRemaining(until, now) ?? '—'}</span>
                      <span className="mono shrink-0 text-muted sm:w-12 sm:text-right" title={t('statsBackoffLevel')}>
                        {account.restart_backoff_level ? `L${account.restart_backoff_level}` : '—'}
                      </span>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          ) : null}
          <div className="divide-y divide-separator">
            {accountsLoading ? (
              <RankListSkeleton />
            ) : accounts.length === 0 ? (
              <div className="px-4 py-6 text-body text-muted">{t('noAccounts')}</div>
            ) : accounts.slice(0, 6).map((account) => {
              const quota = account.quota
              const tone = quotaTone(quota?.percentage, quota?.exceeded)
              const liveRow = liveById.get(account.id)
              const rowInFlight = liveRow?.in_flight ?? account.in_flight ?? account.inFlight ?? 0
              const rowMax = liveRow?.max_inflight || account.max_inflight || 4
              const rowSaturated = rowInFlight >= rowMax
              return (
                <div key={account.id} className="flex flex-col gap-2.5 px-4 py-3 sm:flex-row sm:items-center sm:gap-3 sm:py-2.5">
                  <div className="flex min-w-0 flex-1 items-start gap-3 sm:items-center">
                    <span className="status-dot mt-1.5 shrink-0 sm:mt-0" data-state={account.ready ? 'ok' : 'danger'} />
                    <div className="min-w-0 flex-1">
                      <div className="flex min-w-0 items-center gap-2">
                        <ProviderMark provider={account.provider} size={14} />
                        <div className="truncate text-body font-medium">{account.name || account.id}</div>
                      </div>
                      <div className="mono mt-0.5 text-caption text-muted">
                        {accountProviderLabel(account.provider, account.region, t)}
                        {' · '}
                        {account.hot ? t('hot') : account.ready ? t('ready') : t('degraded')}
                      </div>
                    </div>
                  </div>
                  <div className="flex flex-wrap items-center gap-x-4 gap-y-2 pl-6 sm:shrink-0 sm:pl-0">
                    <div className="flex items-center gap-1.5" title={rowSaturated ? t('liveSaturated') : t('inFlight')} data-testid="account-inflight">
                      <span className="text-caption text-muted sm:hidden">{t('inFlight')}</span>
                      <span className={`mono text-caption tabular ${rowSaturated ? 'text-warning' : 'text-muted'}`}>{rowInFlight}/{rowMax}</span>
                      <span className="inline-block h-[2px] w-10 overflow-hidden rounded-[1px] bg-separator">
                        <span className={`block h-full ${rowSaturated ? 'bg-warning' : 'bg-foreground/50'}`} style={{ width: `${Math.min(100, (rowInFlight / rowMax) * 100)}%` }} />
                      </span>
                    </div>
                    {quota ? (
                      <div className="flex items-center gap-1.5" title={t('quota')}>
                        <span className="text-caption text-muted sm:hidden">{t('quota')}</span>
                        <span className="mono text-caption tabular text-muted">{Math.round(quota.percentage ?? 0)}%</span>
                        <span className="inline-block h-[2px] w-10 overflow-hidden rounded-[1px] bg-separator">
                          <span
                            className={`block h-full ${tone === 'danger' ? 'bg-danger' : tone === 'warning' ? 'bg-warning' : 'bg-success'}`}
                            style={{ width: `${Math.min(100, Math.max(0, quota.percentage ?? 0))}%` }}
                          />
                        </span>
                      </div>
                    ) : null}
                  </div>
                </div>
              )
            })}
          </div>
          <div className="flex items-center justify-between border-t border-separator px-4 py-2.5 text-caption text-muted">
            <span>{t('metricModels')} {modelCount}</span>
            <span className="flex min-w-0 flex-wrap items-center justify-end gap-x-2 gap-y-1">
              {['qoder', 'workbuddy', 'trae'].map((provider) => {
                const count = accounts.filter((account) => String(account.provider || 'qoder').toLowerCase() === provider).length
                if (!count) return null
                return (
                  <span key={provider} className="inline-flex items-center gap-1">
                    <ProviderMark provider={provider} size={11} />
                    {count}
                  </span>
                )
              })}
              <span>{coolingAccounts ? `${coolingAccounts} ${t('statsCooling')}` : `${accounts.length} ${t('accountCount')}`}</span>
            </span>
          </div>
        </Card>

        <ResourcesCard resources={resources} t={t} />
      </section>

      <section className="grid gap-3 xl:grid-cols-2 2xl:grid-cols-3">
        <Card data-gsap-reveal className="overflow-hidden p-0">
          <div className="flex items-center justify-between gap-3 border-b border-separator px-4 py-3">
            <div>
              <h3 className="font-semibold">{t('statsTopProviders')}</h3>
              <p className="mt-0.5 text-caption text-muted">{t('statsTopProvidersHint')}</p>
            </div>
          </div>
          {statsLoading ? (
            <RankListSkeleton />
          ) : (
            <RankList
              empty={t('statsNoProviders')}
              items={(traffic.providers || []).map((item) => ({
                key: item.key,
                label: familyLabel(item.key, t),
                count: item.count,
                meta: `${item.ok}/${item.count}`,
                mark: item.key === '(unknown)' ? undefined : item.key,
                href: item.key === '(unknown)' ? undefined : drill({ provider: item.key }),
              }))}
            />
          )}
        </Card>
        <Card data-gsap-reveal className="overflow-hidden p-0">
          <div className="flex items-center justify-between gap-3 border-b border-separator px-4 py-3">
            <div>
              <h3 className="font-semibold">{t('statsTopModels')}</h3>
              <p className="mt-0.5 text-caption text-muted">{t('providerCatalog')}</p>
            </div>
          </div>
          {statsLoading ? (
            <RankListSkeleton rows={4} />
          ) : (
            <RankList
              empty={t('statsNoModels')}
              items={traffic.models.map((item) => ({
                key: item.key,
                label: item.key === '(unknown)' ? t('statsUnknown') : item.key,
                count: item.count,
                href: item.key === '(unknown)' ? undefined : drill({ model: item.key }),
                meta: item.latency_avg_ms != null ? formatLatency(item.latency_avg_ms) : `${item.ok}/${item.count}`,
              }))}
            />
          )}
        </Card>

        <Card data-gsap-reveal className="overflow-hidden p-0">
          <div className="flex items-center justify-between gap-3 border-b border-separator px-4 py-3">
            <div>
              <h3 className="font-semibold">{t('statsStatusMix')}</h3>
              <p className="mt-0.5 text-caption text-muted">{t('statsStatusMixHint')}</p>
            </div>
          </div>
          {statsLoading ? (
            <RankListSkeleton rows={4} />
          ) : (
            <>
              <StatusBar items={statusCounts} t={t} />
              <div className="border-t border-separator px-4 pt-2.5 text-caption font-medium text-muted">{t('statsErrorMix')}</div>
              <RankList
                empty={t('statsNoErrors')}
                danger
                items={traffic.errors.map((item) => ({
                  key: item.key,
                  label: errorLabel(item.key, t),
                  count: item.count,
                  href: LOGS_ERROR_KINDS.has(item.key) ? drill({ error_kind: item.key }) : undefined,
                }))}
              />
              <div className="border-t border-separator px-4 pt-2.5 text-caption font-medium text-muted">{t('statsTopAccounts')}</div>
              <RankList
                empty={t('statsNoAccounts')}
                items={traffic.accounts.slice(0, 5).map((item) => ({
                  key: item.key,
                  label: accountLabel(accounts, item.key, t('statsUnassigned')),
                  count: item.count,
                  href: !item.key || item.key === '(unassigned)' ? undefined : drill({ account: item.key }),
                  meta: `${item.ok}/${item.count}`,
                }))}
              />
            </>
          )}
        </Card>
      </section>
    </div>
  )
}

function resourcesTone(rssBytes: number, cpuPercent: number) {
  if (rssBytes >= 2 * 1024 * 1024 * 1024 || cpuPercent >= 200) return 'danger'
  if (rssBytes >= 1024 * 1024 * 1024 || cpuPercent >= 100) return 'warning'
  return 'ok'
}

function ResourcesCard({ resources, t }: { resources: SystemResources | null; t: (key: string, vars?: Record<string, string | number>) => string }) {
  const workers = [...(resources?.workers ?? [])].sort((a, b) => b.rss_bytes - a.rss_bytes)
  const server = resources?.server
  return (
    <Card data-gsap-reveal className="overflow-hidden p-0">
      <div className="flex items-center justify-between gap-3 border-b border-separator px-4 py-3">
        <div>
          <h3 className="font-semibold">{t('resTitle')}</h3>
          <p className="mt-0.5 text-caption text-muted">{t('resHint')}</p>
        </div>
        <Chip size="sm" variant="soft" color={resources ? 'success' : 'default'}>
          {resources ? formatBytes(resources.total_rss_bytes) : '—'}
        </Chip>
      </div>
      <div className="divide-y divide-separator">
        {!resources ? (
          <RankListSkeleton rows={3} />
        ) : (
          <>
            <div className="px-4 py-2.5">
              <div className="flex items-baseline justify-between gap-3">
                <div className="flex min-w-0 items-center gap-2">
                  <span className="status-dot shrink-0" data-state={resourcesTone(server?.rss_bytes ?? 0, server?.cpu_percent ?? 0)} />
                  <div className="truncate text-body font-medium">{t('resServer')}</div>
                  <span className="mono text-caption text-muted">pid {server?.pid}</span>
                </div>
                <div className="mono shrink-0 text-caption text-muted">
                  {formatBytes(server?.rss_bytes)} · {(server?.cpu_percent ?? 0).toFixed(1)}%
                </div>
              </div>
            </div>
            {workers.length === 0 ? (
              <div className="px-4 py-3 text-body text-muted">{t('resEmpty')}</div>
            ) : workers.map((worker) => (
              <div key={worker.pid} className="px-4 py-2.5">
                <div className="flex items-baseline justify-between gap-3">
                  <div className="flex min-w-0 items-center gap-2">
                    <span className="status-dot shrink-0" data-state={resourcesTone(worker.rss_bytes, worker.cpu_percent)} />
                    <div className="truncate text-body font-medium">{worker.label || worker.account_id}</div>
                    <span className="mono text-caption text-muted">pid {worker.pid}</span>
                  </div>
                  <div className="mono shrink-0 text-caption text-muted">
                    {formatBytes(worker.rss_bytes)} · {worker.cpu_percent.toFixed(1)}%
                  </div>
                </div>
              </div>
            ))}
          </>
        )}
      </div>
      <div className="flex items-center justify-between border-t border-separator px-4 py-2.5 text-caption text-muted">
        <span className="mono">{server?.goroutines ?? 0} {t('resGoroutines')}</span>
        <span className="mono">
          {resources?.sampled_at ? new Date(resources.sampled_at).toLocaleTimeString() : '—'}
        </span>
      </div>
    </Card>
  )
}

function RankList({
  items,
  empty,
  danger,
}: {
  items: Array<{ key: string; label: string; count: number; meta?: string; mark?: string; href?: string }>
  empty: string
  danger?: boolean
}) {
  const rootRef = useRef<HTMLDivElement>(null)
  const peak = Math.max(1, ...items.map((item) => item.count))
  const signature = items.map((item) => `${item.key}:${item.count}`).join('|')

  useLayoutEffect(() => {
    const root = rootRef.current
    if (!root) return
    const context = gsap.context(() => {
      const media = gsap.matchMedia()
      media.add('(prefers-reduced-motion: reduce)', () => {
        gsap.set('[data-rank-fill]', { clearProps: 'transform' })
      })
      media.add('(prefers-reduced-motion: no-preference)', () => {
        const fills = gsap.utils.toArray<HTMLElement>('[data-rank-fill]')
        if (!fills.length) return
        gsap.fromTo(fills, { scaleX: 0 }, {
          scaleX: 1,
          duration: 0.38,
          ease: 'power2.out',
          stagger: 0.05,
          transformOrigin: '0% 50%',
        })
      })
    }, root)
    return () => context.revert()
  }, [signature])

  if (!items.length) {
    return <div className="px-4 py-6 text-body text-muted">{empty}</div>
  }
  return (
    <div ref={rootRef} className="divide-y divide-separator">
      {items.map((item) => {
        const body = (
          <>
            <div className="flex items-baseline justify-between gap-3">
              <div className="flex min-w-0 items-center gap-2">
                {item.mark ? <ProviderMark provider={item.mark} size={14} /> : null}
                <div className="truncate text-body font-medium">{item.label}</div>
              </div>
              <div className="mono shrink-0 text-caption text-muted">
                {item.meta ? `${item.count} · ${item.meta}` : item.count}
              </div>
            </div>
            <div className="mt-2 h-1 overflow-hidden rounded-[2px] bg-separator">
              <div
                data-rank-fill
                className={`h-full origin-left rounded-[2px] ${danger ? 'bg-danger' : 'bg-success'}`}
                style={{ width: `${Math.max(6, (item.count / peak) * 100)}%` }}
              />
            </div>
          </>
        )
        return item.href ? (
          <Link key={item.key} to={item.href} className="rank-row block px-4 py-2.5" title={item.label}>
            {body}
          </Link>
        ) : (
          <div key={item.key} className="px-4 py-2.5">{body}</div>
        )
      })}
    </div>
  )
}

function TrendChip({ value, hint }: { value: number; hint: string }) {
  const tone = Math.abs(value) < 0.005 ? 'flat' : value > 0 ? 'up' : 'down'
  const Icon = tone === 'down' ? TrendDown : tone === 'up' ? TrendUp : ChartLineUp
  return (
    <span className="trend-chip" data-tone={tone} title={hint}>
      <Icon size={12} weight="bold" />
      {value > 0 ? '+' : ''}{(value * 100).toFixed(1)}%
    </span>
  )
}

function StatusBar({
  items,
  t,
}: {
  items: Array<{ key: (typeof STATUS_ORDER)[number]; count: number }>
  t: (key: string) => string
}) {
  const total = items.reduce((sum, item) => sum + item.count, 0)
  if (!total) return <div className="px-4 py-3 text-body text-muted">{t('statsEmptyTraffic')}</div>
  return (
    <div className="px-4 py-3">
      <div className="flex h-2 gap-px overflow-hidden rounded-[3px] bg-separator" role="img" aria-label={items.map((item) => `${t(STATUS_LABEL[item.key])} ${item.count}`).join(', ')}>
        {items.filter((item) => item.count > 0).map((item) => (
          <div key={item.key} className={STATUS_TONE[item.key]} style={{ width: `${(item.count / total) * 100}%`, minWidth: 2 }} />
        ))}
      </div>
      <div className="mt-3 grid grid-cols-2 gap-x-4 gap-y-1.5">
        {items.map((item) => (
          <div key={item.key} className="flex items-center gap-2 text-caption">
            <span className={`inline-block size-2 shrink-0 rounded-[2px] ${STATUS_TONE[item.key]}`} />
            <span className="min-w-0 flex-1 truncate text-muted">{t(STATUS_LABEL[item.key])}</span>
            <span className="mono tabular">{item.count}</span>
            <span className="mono w-10 text-right tabular text-muted">{formatPercent(item.count / total)}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
