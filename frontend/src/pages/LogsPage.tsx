import { toast } from '@/lib/toast'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  Button,
  Card,
  Chip,
  Modal,
  Tab,
  Table,
  Tabs,
} from '@heroui/react'
import {
  ArrowClockwise,
  MagnifyingGlass,
  Scroll,
  TerminalWindow,
  TrashSimple,
  X,
} from '@phosphor-icons/react'
import {
  clearRequestLogs,
  fetchRequestLog,
  fetchRequestLogs,
  fetchRuntimeLogs,
  type RequestLog,
  type RuntimeLogEntry,
} from '@/api/logs'
import { fetchAccounts, fetchModels } from '@/api/overview'
import type { Overview } from '@/api/types'
import { ConfirmDialog } from '@/components/ui/ConfirmDialog'
import { EmptyPanel } from '@/components/ui/EmptyPanel'
import { FilterSearchSelect } from '@/components/ui/FilterSearchSelect'
import { FilterSelect } from '@/components/ui/FilterSelect'
import { FilterToggle } from '@/components/ui/FilterToggle'
import { ListPager, type PageSize } from '@/components/ui/ListPager'
import { PageAlert } from '@/components/ui/PageAlert'
import { LogsPageSkeleton, LogsRequestListSkeleton, LogsRuntimeListSkeleton, SkeletonBlock } from '@/components/ui/PageSkeletons'
import { LatencyStats } from '@/components/ui/LatencyStats'
import { SearchBar } from '@/components/ui/SearchBar'
import { dateValueToISO, isoToDateValue, TimeRangePicker, type DateRangeValue } from '@/components/ui/TimeRangePicker'
import { useI18n } from '@/hooks/useI18n'
import { useApiKeyOptions } from '@/hooks/useApiKeyOptions'
import { accountProviderFamilyLabel, accountProviderLabel } from '@/lib/provider'

type PageTab = 'requests' | 'runtime'
type RequestFilter = 'all' | 'ok' | 'incomplete' | 'error' | 'canceled'
type RuntimeFilter = 'all' | 'info' | 'warn' | 'error'
type StreamFilter = 'all' | 'stream' | 'sync'
type TimeRange = 'all' | '1h' | '24h' | '7d' | 'custom'
type ErrorKindFilter = 'all' | 'quota' | 'rate_limit' | 'auth' | 'not_ready' | 'unavailable' | 'invalid_request' | 'model_not_available'

function statusColor(status?: string): 'success' | 'warning' | 'danger' | 'default' {
  if (status === 'ok') return 'success'
  if (status === 'streaming' || status === 'started' || status === 'incomplete') return 'warning'
  if (status === 'error' || status === 'canceled') return 'danger'
  return 'default'
}

function levelDot(level?: string) {
  if (level === 'error') return 'danger'
  if (level === 'warn') return undefined
  return 'ok'
}

function runtimeLevelColor(level?: string): 'success' | 'warning' | 'danger' | 'default' {
  if (level === 'error') return 'danger'
  if (level === 'warn') return 'warning'
  if (level === 'info') return 'success'
  return 'default'
}

function formatTime(value?: string | null, lang: 'en' | 'zh' = 'zh') {
  if (!value) return '—'
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return value
  return new Intl.DateTimeFormat(lang === 'zh' ? 'zh-CN' : 'en-US', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  }).format(date)
}

function reasoningLabel(log: Pick<RequestLog, 'requested_reasoning' | 'resolved_reasoning'>) {
  const requested = log.requested_reasoning?.trim() || ''
  const resolved = log.resolved_reasoning?.trim() || ''
  if (!requested && !resolved) return ''
  if (requested && resolved && requested !== resolved) return `${requested} → ${resolved}`
  return resolved || requested
}

function TokenSplit({
  log,
  inLabel,
  outLabel,
  pointsLabel,
}: {
  log: RequestLog
  inLabel: string
  outLabel: string
  pointsLabel?: (value: string) => string
}) {
  const prompt = log.prompt_tokens
  const completion = log.completion_tokens
  const credit = log.credits ?? log.usage_detail?.credit
  const creditText = credit != null && Number.isFinite(credit) ? formatCredit(credit) : null
  if (prompt == null && completion == null && creditText == null) {
    return <span className="mono text-xs text-muted">—</span>
  }
  return (
    <div className="leading-4">
      {prompt != null || completion != null ? (
        <>
          <div className="mono text-xs">{prompt ?? 0} / {completion ?? 0}</div>
          <div className="mt-0.5 text-caption text-muted">{inLabel} / {outLabel}</div>
        </>
      ) : null}
      {creditText != null ? (
        <div className={`mono text-caption text-success ${prompt != null || completion != null ? 'mt-0.5' : ''}`}>
          {pointsLabel ? pointsLabel(creditText) : creditText}
        </div>
      ) : null}
    </div>
  )
}

function RequestDetailSkeleton() {
  return (
    <div className="space-y-5" aria-busy="true" aria-live="polite">
      <div className="grid gap-4 sm:grid-cols-2">
        {Array.from({ length: 8 }, (_, index) => (
          <div key={index} className="space-y-2">
            <SkeletonBlock className="h-3 w-20" />
            <SkeletonBlock className="h-5 w-36 max-w-full" />
          </div>
        ))}
      </div>
      <div className="space-y-3 rounded-lg bg-surface-secondary px-3 py-3">
        <SkeletonBlock className="h-3 w-24" />
        <SkeletonBlock className="h-4 w-full" />
        <SkeletonBlock className="h-4 w-4/5" />
      </div>
      <div className="space-y-3">
        <SkeletonBlock className="h-3 w-20" />
        <SkeletonBlock className="h-16 w-full" />
      </div>
    </div>
  )
}

function formatCredit(value?: number | null) {
  if (value == null || !Number.isFinite(value)) return '—'
  return String(Math.round(value * 10000) / 10000)
}

const REQUEST_FILTERS: RequestFilter[] = ['all', 'ok', 'incomplete', 'error', 'canceled']
const ERROR_KINDS: ErrorKindFilter[] = ['all', 'quota', 'rate_limit', 'auth', 'not_ready', 'unavailable', 'invalid_request', 'model_not_available']
const TIME_RANGES: TimeRange[] = ['all', '1h', '24h', '7d', 'custom']

type UrlFilters = {
  requestFilter: RequestFilter
  requestId: string
  accountFilter: string
  apiKeyFilter: string
  modelFilter: string
  providerFilter: string
  streamFilter: StreamFilter
  errorKind: ErrorKindFilter
  timeRange: TimeRange
  customRange: DateRangeValue | null
}

/** Request-log filters from the URL (?status=&account=&api_key=&model=&error_kind=&stream=&id=&range=&from=&to=). */
function parseLogsUrl(params: URLSearchParams): UrlFilters {
  const pick = <T extends string>(value: string | null, allowed: T[], fallback: T) =>
    (allowed as string[]).includes(value ?? '') ? (value as T) : fallback
  const start = isoToDateValue(params.get('from'))
  const end = isoToDateValue(params.get('to'))
  const custom = start && end ? { start, end } : null
  const stream = params.get('stream')
  return {
    requestFilter: pick(params.get('status'), REQUEST_FILTERS, 'all'),
    requestId: params.get('id') ?? '',
    accountFilter: params.get('account') ?? '',
    apiKeyFilter: params.get('api_key') ?? '',
    modelFilter: params.get('model') ?? '',
    providerFilter: params.get('provider') ?? '',
    streamFilter: stream === 'stream' || stream === 'true' ? 'stream' : stream === 'sync' || stream === 'false' ? 'sync' : 'all',
    errorKind: pick(params.get('error_kind'), ERROR_KINDS, 'all'),
    timeRange: custom ? 'custom' : pick(params.get('range'), TIME_RANGES, '1h'),
    customRange: custom,
  }
}

function buildLogsUrl(filters: UrlFilters) {
  const params = new URLSearchParams()
  if (filters.requestFilter !== 'all') params.set('status', filters.requestFilter)
  if (filters.requestId) params.set('id', filters.requestId)
  if (filters.accountFilter) params.set('account', filters.accountFilter)
  if (filters.apiKeyFilter) params.set('api_key', filters.apiKeyFilter)
  if (filters.modelFilter) params.set('model', filters.modelFilter)
  if (filters.providerFilter) params.set('provider', filters.providerFilter)
  if (filters.streamFilter !== 'all') params.set('stream', filters.streamFilter)
  if (filters.errorKind !== 'all') params.set('error_kind', filters.errorKind)
  const from = filters.timeRange === 'custom' ? dateValueToISO(filters.customRange?.start) : undefined
  const to = filters.timeRange === 'custom' ? dateValueToISO(filters.customRange?.end, true) : undefined
  if (from && to) {
    params.set('from', from)
    params.set('to', to)
  } else if (filters.timeRange !== '1h') {
    params.set('range', filters.timeRange)
  }
  return params.toString()
}

function rangeFromPreset(preset: TimeRange) {
  if (preset === 'all' || preset === 'custom') return { from: undefined as string | undefined, to: undefined as string | undefined }
  const now = new Date()
  const from = new Date(now)
  if (preset === '1h') from.setHours(from.getHours() - 1)
  if (preset === '24h') from.setHours(from.getHours() - 24)
  if (preset === '7d') from.setDate(from.getDate() - 7)
  return { from: from.toISOString(), to: now.toISOString() }
}

export function LogsPage() {
  const { t, lang } = useI18n()
  const [searchParams, setSearchParams] = useSearchParams()
  const [initialUrl] = useState(() => parseLogsUrl(new URLSearchParams(window.location.search)))
  const [accounts, setAccounts] = useState<NonNullable<Overview['accounts']>>([])
  const [models, setModels] = useState<NonNullable<Overview['models']>>([])
  const [tab, setTab] = useState<PageTab>('requests')
  const [loading, setLoading] = useState(true)
  const [booted, setBooted] = useState(false)
  const [error, setError] = useState('')
  const [requestFilter, setRequestFilter] = useState<RequestFilter>(initialUrl.requestFilter)
  const [runtimeFilter, setRuntimeFilter] = useState<RuntimeFilter>('all')
  const [requestId, setRequestId] = useState(initialUrl.requestId)
  const [runtimeQuery, setRuntimeQuery] = useState('')
  const [accountFilter, setAccountFilter] = useState(initialUrl.accountFilter)
  const [apiKeyFilter, setApiKeyFilter] = useState(initialUrl.apiKeyFilter)
  const [runtimeAccount, setRuntimeAccount] = useState('')
  const [modelFilter, setModelFilter] = useState(initialUrl.modelFilter)
  const [providerFilter, setProviderFilter] = useState(initialUrl.providerFilter)
  const [streamFilter, setStreamFilter] = useState<StreamFilter>(initialUrl.streamFilter)
  const [errorKind, setErrorKind] = useState<ErrorKindFilter>(initialUrl.errorKind)
  const [timeRange, setTimeRange] = useState<TimeRange>(initialUrl.timeRange)
  const [customRange, setCustomRange] = useState<DateRangeValue | null>(initialUrl.customRange)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState<PageSize>(50)
  const [runtimePage, setRuntimePage] = useState(1)
  const [runtimePageSize, setRuntimePageSize] = useState<PageSize>(50)
  const [requests, setRequests] = useState<RequestLog[]>([])
  const [total, setTotal] = useState(0)
  const [runtime, setRuntime] = useState<RuntimeLogEntry[]>([])
  const [runtimeTotal, setRuntimeTotal] = useState(0)
  const [selectedRuntime, setSelectedRuntime] = useState<RuntimeLogEntry | null>(null)
  const [runtimeDetailOpen, setRuntimeDetailOpen] = useState(false)
  const [selected, setSelected] = useState<RequestLog | null>(null)
  const [detailOpen, setDetailOpen] = useState(false)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState('')
  const detailRequestRef = useRef(0)
  const [clearOpen, setClearOpen] = useState(false)
  const [busy, setBusy] = useState(false)

  // URL <-> filter controls. External URL changes (back/forward, drill-down
  // links) are applied to the controls; control changes replace the URL.
  const searchString = searchParams.toString()
  const urlString = buildLogsUrl({ requestFilter, requestId, accountFilter, apiKeyFilter, modelFilter, providerFilter, streamFilter, errorKind, timeRange, customRange })
  const lastUrlRef = useRef(searchString)
  useEffect(() => {
    if (searchString === lastUrlRef.current) return
    lastUrlRef.current = searchString
    const next = parseLogsUrl(new URLSearchParams(searchString))
    setRequestFilter(next.requestFilter)
    setRequestId(next.requestId)
    setAccountFilter(next.accountFilter)
    setApiKeyFilter(next.apiKeyFilter)
    setModelFilter(next.modelFilter)
    setProviderFilter(next.providerFilter)
    setStreamFilter(next.streamFilter)
    setErrorKind(next.errorKind)
    setTimeRange(next.timeRange)
    setCustomRange(next.customRange)
  }, [searchString])
  useEffect(() => {
    if (urlString === lastUrlRef.current) return
    lastUrlRef.current = urlString
    setSearchParams(urlString, { replace: true })
  }, [urlString, setSearchParams])

  useEffect(() => {
    let cancelled = false
    void Promise.allSettled([fetchAccounts(false), fetchModels()]).then(([accountsResult, modelsResult]) => {
      if (cancelled) return
      if (accountsResult.status === 'fulfilled') setAccounts(accountsResult.value.data || [])
      if (modelsResult.status === 'fulfilled') setModels(modelsResult.value.data || [])
    })
    return () => { cancelled = true }
  }, [])

  const accountNameById = useMemo(() => {
    const names = new Map<string, string>()
    for (const account of accounts || []) names.set(account.id, account.name || account.id)
    return names
  }, [accounts])

  const accountProviderById = useMemo(() => {
    const providers = new Map<string, { provider?: string; region?: string }>()
    for (const account of accounts || []) {
      providers.set(account.id, { provider: account.provider, region: account.region })
    }
    return providers
  }, [accounts])

  function providerLabel(item: { account_id?: string; provider?: string }) {
    const account = item.account_id ? accountProviderById.get(item.account_id) : undefined
    const provider = item.provider || account?.provider
    if (!provider) return '—'
    return accountProviderLabel(provider, account?.region, t)
  }

  function routingLabel(routing?: string) {
    switch (routing) {
      case 'pin': return t('logsRouting_pin')
      case 'sticky': return t('logsRouting_sticky')
      case 'sticky_escape': return t('logsRouting_sticky_escape')
      default: return t('logsRouting_pool')
    }
  }

  const accountOptions = useMemo(
    () => (accounts || []).map((account) => ({ id: account.id, label: account.name || account.id })),
    [accounts],
  )
  const { nameById: apiKeyNameById, options: apiKeyOptions } = useApiKeyOptions()

  /** Name of the API key that issued the request; "—" for console traffic. */
  function apiKeyLabel(item: { api_key_id?: string }) {
    const id = item.api_key_id?.trim()
    if (!id) return '—'
    return apiKeyNameById.get(id) || id
  }

  const providerOptions = useMemo(() => {
    const ids = new Set<string>()
    for (const account of accounts || []) if (account.provider) ids.add(account.provider)
    if (providerFilter) ids.add(providerFilter)
    return [
      { id: '', label: t('logsFilterProviderAll') },
      ...[...ids].sort().map((id) => ({ id, label: accountProviderFamilyLabel(id, t) })),
    ]
  }, [accounts, providerFilter, t])
  const modelOptions = useMemo(() => {
    const seen = new Map<string, { id: string; label: string }>()
    for (const model of models || []) {
      if (!seen.has(model.id)) seen.set(model.id, { id: model.id, label: model.display_name || model.id })
    }
    return [...seen.values()]
  }, [models])

  const loadRequestIdOptions = useCallback(async (query: string) => {
    const result = await fetchRequestLogs({
      q: query || undefined,
      limit: 20,
      offset: 0,
    })
    return (result.items || []).map((item) => ({ id: item.id, label: item.id }))
  }, [])

  const hasRequestFilters = Boolean(
    requestId
    || requestFilter !== 'all'
    || accountFilter
    || apiKeyFilter
    || modelFilter
    || providerFilter
    || streamFilter !== 'all'
    || errorKind !== 'all'
    || timeRange !== '1h',
  )

  const requestFilterKey = [
    requestFilter,
    requestId,
    accountFilter,
    apiKeyFilter,
    modelFilter,
    providerFilter,
    streamFilter,
    errorKind,
    timeRange,
    customRange?.start?.toString() ?? '',
    customRange?.end?.toString() ?? '',
    pageSize,
  ].join('\0')
  const runtimeFilterKey = [runtimeFilter, runtimeQuery, runtimeAccount, runtimePageSize].join('\0')
  const [appliedFilterKey, setAppliedFilterKey] = useState(requestFilterKey)
  const [appliedRuntimeFilterKey, setAppliedRuntimeFilterKey] = useState(runtimeFilterKey)
  if (appliedFilterKey !== requestFilterKey) {
    setAppliedFilterKey(requestFilterKey)
    setPage(1)
  }
  if (appliedRuntimeFilterKey !== runtimeFilterKey) {
    setAppliedRuntimeFilterKey(runtimeFilterKey)
    setRuntimePage(1)
  }
  const pageCount = Math.max(1, Math.ceil(total / pageSize))
  const currentPage = Math.min(appliedFilterKey !== requestFilterKey ? 1 : Math.max(1, page), pageCount)
  if (page !== currentPage) {
    setPage(currentPage)
  }
  const runtimePageCount = Math.max(1, Math.ceil(runtimeTotal / runtimePageSize))
  const currentRuntimePage = Math.min(
    appliedRuntimeFilterKey !== runtimeFilterKey ? 1 : Math.max(1, runtimePage),
    runtimePageCount,
  )
  if (runtimePage !== currentRuntimePage) {
    setRuntimePage(currentRuntimePage)
  }

  const loadRequests = useCallback(async (quiet = false) => {
    if (!quiet) setLoading(true)
    try {
      const range = requestId
        ? { from: undefined as string | undefined, to: undefined as string | undefined }
        : timeRange === 'custom'
          ? { from: dateValueToISO(customRange?.start), to: dateValueToISO(customRange?.end, true) }
          : rangeFromPreset(timeRange)
      const result = await fetchRequestLogs({
        status: requestFilter === 'all' ? undefined : requestFilter,
        id: requestId || undefined,
        account: accountFilter || undefined,
        api_key: apiKeyFilter || undefined,
        model: modelFilter || undefined,
        provider: providerFilter || undefined,
        stream: streamFilter === 'all' ? undefined : streamFilter === 'stream',
        error_kind: errorKind === 'all' ? undefined : errorKind,
        from: range.from,
        to: range.to,
        limit: pageSize,
        offset: (currentPage - 1) * pageSize,
      })
      setRequests(result.items || [])
      setTotal(result.total || 0)
      setError('')
    } catch (err) {
      if (!quiet) setError(err instanceof Error ? err.message : String(err))
    } finally {
      if (!quiet) {
        setLoading(false)
        setBooted(true)
      }
    }
  }, [requestFilter, requestId, accountFilter, apiKeyFilter, modelFilter, providerFilter, streamFilter, errorKind, timeRange, customRange, currentPage, pageSize])

  const loadRuntime = useCallback(async (quiet = false) => {
    if (!quiet) setLoading(true)
    try {
      const result = await fetchRuntimeLogs({
        level: runtimeFilter === 'all' ? undefined : runtimeFilter,
        q: runtimeQuery.trim() || undefined,
        account: runtimeAccount || undefined,
        limit: runtimePageSize,
        offset: (currentRuntimePage - 1) * runtimePageSize,
      })
      setRuntime(result.items || [])
      setRuntimeTotal(result.total ?? 0)
      setError('')
    } catch (err) {
      if (!quiet) setError(err instanceof Error ? err.message : String(err))
    } finally {
      if (!quiet) {
        setLoading(false)
        setBooted(true)
      }
    }
  }, [runtimeFilter, runtimeQuery, runtimeAccount, currentRuntimePage, runtimePageSize])

  const load = useCallback(async (quiet = false) => {
    if (tab === 'requests') await loadRequests(quiet)
    else await loadRuntime(quiet)
  }, [tab, loadRequests, loadRuntime])

  useEffect(() => {
    setLoading(true)
    const delay = tab === 'runtime' && runtimeQuery.trim() ? 280 : 0
    const timer = window.setTimeout(() => void load(false), delay)
    return () => window.clearTimeout(timer)
  }, [load, tab, runtimeQuery])

  useEffect(() => {
    if (tab !== 'runtime' || currentRuntimePage !== 1) return
    const timer = window.setInterval(() => void loadRuntime(true), 3000)
    return () => window.clearInterval(timer)
  }, [tab, currentRuntimePage, loadRuntime])

  const shownFrom = total === 0 ? 0 : (currentPage - 1) * pageSize + 1
  const shownTo = Math.min(total, currentPage * pageSize)
  const runtimeShownFrom = runtimeTotal === 0 ? 0 : (currentRuntimePage - 1) * runtimePageSize + 1
  const runtimeShownTo = Math.min(runtimeTotal, currentRuntimePage * runtimePageSize)

  const shownLabel = useMemo(() => {
    if (tab !== 'requests') {
      return t('logsShownTotal', {
        shown: runtime.length ? `${runtimeShownFrom}–${runtimeShownTo}` : 0,
        total: runtimeTotal,
      })
    }
    return t('logsShownTotal', { shown: requests.length ? `${shownFrom}–${shownTo}` : 0, total })
  }, [tab, requests.length, runtime.length, shownFrom, shownTo, total, runtimeShownFrom, runtimeShownTo, runtimeTotal, t])

  function clearRequestFilters() {
    setRequestId('')
    setRequestFilter('all')
    setAccountFilter('')
    setApiKeyFilter('')
    setModelFilter('')
    setProviderFilter('')
    setStreamFilter('all')
    setErrorKind('all')
    setTimeRange('1h')
    setCustomRange(null)
    setPage(1)
  }

  async function openDetail(request: RequestLog) {
    const requestVersion = ++detailRequestRef.current
    setSelected(request)
    setDetailError('')
    setDetailLoading(true)
    setDetailOpen(true)
    try {
      const detail = await fetchRequestLog(request.id)
      if (detailRequestRef.current !== requestVersion) return
      setSelected(detail)
    } catch (err) {
      if (detailRequestRef.current !== requestVersion) return
      setDetailError(err instanceof Error ? err.message : String(err))
    } finally {
      if (detailRequestRef.current === requestVersion) setDetailLoading(false)
    }
  }

  function openRuntimeDetail(entry: RuntimeLogEntry) {
    setSelectedRuntime(entry)
    setRuntimeDetailOpen(true)
  }

  async function onClear() {
    setBusy(true)
    toast({ id: 'logs-clear', type: 'loading', title: t('toastClearingLogs') })
    try {
      await clearRequestLogs()
      setClearOpen(false)
      await loadRequests(false)
      toast({ id: 'logs-clear', type: 'success', title: t('toastLogsCleared') })
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      toast({ id: 'logs-clear', type: 'error', title: t('toastFailed'), description: err instanceof Error ? err.message : String(err) })
    } finally {
      setBusy(false)
    }
  }

  // Refetches (page, filter, refresh) keep the current rows on screen instead
  // of swapping back to the skeleton; the skeleton is only for an empty list.
  // No entrance/fade animation anywhere: skeleton -> content is an instant swap.
  const listSkeleton = loading && (tab === 'requests' ? requests.length === 0 : runtime.length === 0)
  if (loading && !booted) return <LogsPageSkeleton />

  return (
    <div className="space-y-6">
      <section className="flex flex-wrap items-end justify-between gap-4 pb-4">
        <div>
          <h2 className="text-title font-semibold">{t('navLogs')}</h2>
          <p className="mt-1 max-w-2xl text-body leading-6 text-muted">{t('logsLead')}</p>
        </div>
        <div className="flex items-center gap-2">
          {tab === 'requests' ? (
            <Button size="sm" variant="ghost" onPress={() => setClearOpen(true)} isDisabled={busy || total === 0}>
              <TrashSimple size={14} />{t('logsClear')}
            </Button>
          ) : null}
          <Button size="sm" variant="secondary" isPending={loading} onPress={() => void load(false)}>
            <ArrowClockwise size={15} />{t('refresh')}
          </Button>
        </div>
      </section>

      {error ? <PageAlert title={t('failedLogs', { msg: error })} /> : null}

      <Tabs selectedKey={tab} onSelectionChange={(key) => setTab(String(key) as PageTab)}>
        <Tabs.ListContainer className="w-fit">
          <Tabs.List>
            <Tab id="requests" className="gap-1.5">
              <Tabs.Indicator className="transition-none" />
              <Scroll size={13} />{t('logsRequests')}
            </Tab>
            <Tab id="runtime" className="gap-1.5">
              <Tabs.Indicator className="transition-none" />
              <TerminalWindow size={13} />{t('logsRuntime')}
            </Tab>
          </Tabs.List>
        </Tabs.ListContainer>

        <Tabs.Panel id="requests" data-tab-panel="requests" className="space-y-4 pt-5">
          <div className="data-toolbar flex flex-col gap-2">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <FilterToggle
                value={requestFilter}
                onChange={(next) => setRequestFilter(next as RequestFilter)}
                ariaLabel={t('logsFilterAll')}
                options={[
                  { id: 'all', label: t('logsFilterAll') },
                  { id: 'ok', label: t('logsFilterOk') },
                  { id: 'incomplete', label: t('logsFilterIncomplete') },
                  { id: 'error', label: t('logsFilterError') },
                  { id: 'canceled', label: t('logsFilterCanceled') },
                ]}
              />
              <div className="flex items-center gap-2">
                {hasRequestFilters ? (
                  <Button size="sm" variant="ghost" onPress={clearRequestFilters}>{t('clearFilters')}</Button>
                ) : null}
                <div className="mono text-caption text-muted">{shownLabel}</div>
              </div>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <FilterSearchSelect
                ariaLabel={t('logsColAccount')}
                value={accountFilter}
                onChange={setAccountFilter}
                options={accountOptions}
                allLabel={t('logsFilterAccountAll')}
                searchPlaceholder={t('logsSearchAccount')}
                emptyLabel={t('logsNoFilterOptions')}
              />
              <FilterSearchSelect
                ariaLabel={t('logsColApiKey')}
                value={apiKeyFilter}
                onChange={setApiKeyFilter}
                options={apiKeyOptions}
                allLabel={t('logsFilterApiKeyAll')}
                searchPlaceholder={t('logsSearchApiKey')}
                emptyLabel={t('logsNoFilterOptions')}
              />
              <FilterSearchSelect
                ariaLabel={t('logsColModel')}
                value={modelFilter}
                onChange={setModelFilter}
                options={modelOptions}
                allLabel={t('logsFilterModelAll')}
                searchPlaceholder={t('logsSearchModel')}
                emptyLabel={t('logsNoFilterOptions')}
              />
              <FilterSelect
                ariaLabel={t('logsColProvider')}
                value={providerFilter}
                onChange={setProviderFilter}
                options={providerOptions}
              />
              <FilterSearchSelect
                ariaLabel={t('logsSearchRequestId')}
                value={requestId}
                onChange={setRequestId}
                options={requestId ? [{ id: requestId, label: requestId }] : []}
                allLabel={t('logsFilterIdAll')}
                searchPlaceholder={t('logsSearchRequestId')}
                emptyLabel={t('logsNoFilterOptions')}
                loadOptions={loadRequestIdOptions}
                className="min-w-56"
              />
              <FilterToggle
                value={streamFilter}
                onChange={(next) => setStreamFilter(next as StreamFilter)}
                ariaLabel={t('logsFilterStreamAll')}
                options={[
                  { id: 'all', label: t('logsFilterStreamAll') },
                  { id: 'stream', label: t('logsStreamYes') },
                  { id: 'sync', label: t('logsStreamNo') },
                ]}
              />
              <FilterSelect
                ariaLabel={t('errorKind')}
                value={errorKind === 'all' ? '' : errorKind}
                onChange={(next) => setErrorKind((next || 'all') as ErrorKindFilter)}
                options={[
                  { id: '', label: t('logsFilterKindAll') },
                  { id: 'quota', label: t('logsKindQuota') },
                  { id: 'rate_limit', label: t('logsKindRateLimit') },
                  { id: 'auth', label: t('logsKindAuth') },
                  { id: 'not_ready', label: t('logsKindNotReady') },
                  { id: 'unavailable', label: t('logsKindUnavailable') },
                  { id: 'invalid_request', label: t('logsKindInvalidRequest') },
                  { id: 'model_not_available', label: t('logsKindModelNotAvailable') },
                ]}
              />
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <span className="text-xs font-medium text-muted">{t('logsTimeRange')}</span>
              <FilterToggle
                value={timeRange}
                onChange={(next) => setTimeRange(next as TimeRange)}
                ariaLabel={t('logsTimeRange')}
                options={[
                  { id: 'all', label: t('logsTimeAll') },
                  { id: '1h', label: t('logsTime1h') },
                  { id: '24h', label: t('logsTime24h') },
                  { id: '7d', label: t('logsTime7d') },
                  { id: 'custom', label: t('logsTimeCustom') },
                ]}
              />
              {timeRange === 'custom' ? (
                <TimeRangePicker
                  className="w-[min(100%,42rem)]"
                  value={customRange}
                  onChange={setCustomRange}
                  ariaLabel={t('logsTimeRange')}
                  fromLabel={t('logsTimeFrom')}
                  toLabel={t('logsTimeTo')}
                />
              ) : null}
            </div>
          </div>

          <Card className="overflow-hidden p-0" aria-busy={loading}>
            {listSkeleton ? (
              <LogsRequestListSkeleton />
            ) : requests.length === 0 ? (
              <EmptyPanel
                icon={<MagnifyingGlass size={22} />}
                title={hasRequestFilters ? t('logsNoMatch') : t('logsEmptyRequests')}
                action={hasRequestFilters ? <Button size="sm" variant="ghost" onPress={clearRequestFilters}>{t('clearFilters')}</Button> : null}
              />
            ) : (
              <Table>
                <Table.ScrollContainer>
                  <Table.Content aria-label={t('logsRequests')}>
                    <Table.Header>
                      <Table.Column isRowHeader>{t('logsColTime')}</Table.Column>
                      <Table.Column>{t('logsColModel')}</Table.Column>
                      <Table.Column>{t('logsColReasoning')}</Table.Column>
                      <Table.Column>{t('logsColProvider')}</Table.Column>
                      <Table.Column>{t('logsColAccount')}</Table.Column>
                      <Table.Column>{t('logsColApiKey')}</Table.Column>
                      <Table.Column>{t('logsColStatus')}</Table.Column>
                      <Table.Column>{t('logsColStream')}</Table.Column>
                      <Table.Column>{t('logsColLatency')}</Table.Column>
                      <Table.Column>{t('logsColTokens')}</Table.Column>
                    </Table.Header>
                    <Table.Body>
                      {requests.map((item) => (
                        <Table.Row key={item.id} className="cursor-pointer" onAction={() => void openDetail(item)}>
                          <Table.Cell>
                            <div className="py-1">
                              <div className="mono text-xs">{formatTime(item.created_at, lang)}</div>
                              <div className="mono mt-0.5 text-caption text-muted">{item.id}</div>
                            </div>
                          </Table.Cell>
                          <Table.Cell>
                            <div className="text-body font-medium">{item.requested_model || '—'}</div>
                            {item.mapped_model && item.mapped_model !== item.requested_model ? (
                              <div className="mono mt-0.5 text-caption text-muted">{item.mapped_model}</div>
                            ) : null}
                          </Table.Cell>
                          <Table.Cell>
                            <span className="mono text-xs">{reasoningLabel(item) || '—'}</span>
                          </Table.Cell>
                          <Table.Cell>
                            <span className="text-xs">{providerLabel(item)}</span>
                          </Table.Cell>
                          <Table.Cell>
                            <span className="text-xs">{item.account_id ? (accountNameById.get(item.account_id) || item.account_id) : '—'}</span>
                            {item.account_id && accountNameById.get(item.account_id) && accountNameById.get(item.account_id) !== item.account_id ? (
                              <div className="mono mt-0.5 text-caption text-muted">{item.account_id}</div>
                            ) : null}
                          </Table.Cell>
                          <Table.Cell>
                            <span className="text-xs">{apiKeyLabel(item)}</span>
                          </Table.Cell>
                          <Table.Cell>
                            <Chip size="sm" variant="soft" color={statusColor(item.status)}>{item.status}</Chip>
                            {item.error_kind ? <div className="mono mt-1 text-caption text-muted">{item.error_kind}</div> : null}
                          </Table.Cell>
                          <Table.Cell><span className="text-xs text-muted">{item.stream ? t('logsStreamYes') : t('logsStreamNo')}</span></Table.Cell>
                          <Table.Cell>
                            <LatencyStats
                              latencyMs={item.latency_ms}
                              ttfbMs={item.ttfb_ms}
                              completionTokens={item.completion_tokens}
                              stream={item.stream}
                              labels={{
                                ttft: t('logsColTTFT'),
                                total: t('logsLatencyTotal'),
                                tps: t('logsOutputTPS'),
                              }}
                            />
                          </Table.Cell>
                          <Table.Cell>
                            <TokenSplit
                              log={item}
                              inLabel={t('logsTokensIn')}
                              outLabel={t('logsTokensOut')}
                              pointsLabel={(value) => t('logsTokensPoints', { value })}
                            />
                          </Table.Cell>
                        </Table.Row>
                      ))}
                    </Table.Body>
                  </Table.Content>
                </Table.ScrollContainer>
              </Table>
            )}
          </Card>

          <ListPager
            total={total}
            page={currentPage}
            pageCount={pageCount}
            pageSize={pageSize}
            loading={loading}
            pageSizeLabel={t('logsPageSize')}
            pageLabel={t('logsPage', { page: currentPage, pages: pageCount })}
            prevLabel={t('logsPrevPage')}
            nextLabel={t('logsNextPage')}
            onPage={setPage}
            onPageSize={setPageSize}
          />
        </Tabs.Panel>

        <Tabs.Panel id="runtime" data-tab-panel="runtime" className="space-y-4 pt-5">
          <div className="data-toolbar flex flex-wrap items-center justify-between gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <SearchBar
                className="sm:w-72"
                value={runtimeQuery}
                onChange={setRuntimeQuery}
                placeholder={t('logsSearchRuntime')}
                ariaLabel={t('logsSearchRuntime')}
              />
              <FilterSearchSelect
                ariaLabel={t('logsColAccount')}
                value={runtimeAccount}
                onChange={setRuntimeAccount}
                options={accountOptions}
                allLabel={t('logsFilterAccountAll')}
                searchPlaceholder={t('logsSearchAccount')}
                emptyLabel={t('logsNoFilterOptions')}
              />
              <FilterToggle
                value={runtimeFilter}
                onChange={(next) => setRuntimeFilter(next as RuntimeFilter)}
                ariaLabel={t('logsLevelAll')}
                options={[
                  { id: 'all', label: t('logsLevelAll') },
                  { id: 'info', label: t('logsLevelInfo') },
                  { id: 'warn', label: t('logsLevelWarn') },
                  { id: 'error', label: t('logsLevelError') },
                ]}
              />
            </div>
            <div className="flex items-center gap-2 text-xs text-muted">
              {currentRuntimePage === 1 ? (
                <>
                  <span className="status-dot" data-state="ok" />
                  {t('logsAutoRefresh')}
                </>
              ) : null}
              <span className="mono">{shownLabel}</span>
            </div>
          </div>

          <Card className="overflow-hidden p-0" aria-busy={loading}>
            {listSkeleton ? (
              <LogsRuntimeListSkeleton />
            ) : runtime.length === 0 ? (
              <EmptyPanel icon={<TerminalWindow size={22} />} title={t('logsEmptyRuntime')} />
            ) : (
              <div className="divide-y divide-separator">
                {runtime.map((entry) => (
                  <div
                    key={entry.id}
                    className="grid cursor-pointer gap-2 px-5 py-3 transition-colors hover:bg-surface-secondary/55 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent sm:grid-cols-[150px_72px_minmax(0,1fr)] sm:items-start"
                    role="button"
                    tabIndex={0}
                    onClick={() => openRuntimeDetail(entry)}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter' || event.key === ' ') {
                        event.preventDefault()
                        openRuntimeDetail(entry)
                      }
                    }}
                  >
                    <div className="mono text-caption text-muted">{formatTime(entry.time, lang)}</div>
                    <div className="flex items-center gap-2 text-xs">
                      <span className="status-dot" data-state={levelDot(entry.level)} />
                      <span className="font-medium text-muted">{entry.level}</span>
                    </div>
                    <div className="min-w-0">
                      {entry.account_id ? <div className="mono mb-1 text-caption text-muted">{entry.account_id}</div> : null}
                      <div className="mono break-all text-xs leading-5 text-foreground">{entry.message}</div>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </Card>

          <ListPager
            total={runtimeTotal}
            page={currentRuntimePage}
            pageCount={runtimePageCount}
            pageSize={runtimePageSize}
            loading={loading}
            pageSizeLabel={t('logsPageSize')}
            pageLabel={t('logsPage', { page: currentRuntimePage, pages: runtimePageCount })}
            prevLabel={t('logsPrevPage')}
            nextLabel={t('logsNextPage')}
            onPage={setRuntimePage}
            onPageSize={setRuntimePageSize}
          />
        </Tabs.Panel>
      </Tabs>

      <Modal.Root isOpen={runtimeDetailOpen} onOpenChange={(open: boolean) => {
        setRuntimeDetailOpen(open)
        if (!open) setSelectedRuntime(null)
      }}>
        <Modal.Backdrop variant="blur">
          <Modal.Container size="lg" scroll="inside">
            <Modal.Dialog className="w-full max-w-4xl">
              <Modal.Header className="items-start justify-between gap-4 px-5 pt-5">
                <div className="min-w-0">
                  <Modal.Heading className="text-section font-semibold">{t('logsRuntimeDetailTitle')}</Modal.Heading>
                  <p className="mono mt-1 text-caption text-muted">{selectedRuntime ? `#${selectedRuntime.id}` : '—'}</p>
                </div>
                <Modal.CloseTrigger aria-label={t('close')} className="grid size-8 shrink-0 place-items-center rounded-lg text-muted transition-colors hover:bg-surface-secondary hover:text-foreground">
                  <X size={16} />
                </Modal.CloseTrigger>
              </Modal.Header>
              <Modal.Body className="space-y-5 px-5 pb-5">
                <dl className="grid gap-3 sm:grid-cols-2">
                  {[
                    [t('logsColTime'), selectedRuntime ? formatTime(selectedRuntime.time, lang) : '—'],
                    [t('logsRuntimeLevel'), selectedRuntime?.level || '—'],
                    [t('logsRuntimeSource'), selectedRuntime?.source || '—'],
                    [t('logsColAccount'), selectedRuntime?.account_id || '—'],
                  ].map(([label, value]) => (
                    <div key={String(label)}>
                      <dt className="text-caption text-muted">{label}</dt>
                      <dd className="mt-1 break-all text-body font-medium">
                        {label === t('logsRuntimeLevel') && selectedRuntime ? (
                          <Chip size="sm" variant="soft" color={runtimeLevelColor(selectedRuntime.level)}>
                            {selectedRuntime.level}
                          </Chip>
                        ) : value}
                      </dd>
                    </div>
                  ))}
                </dl>
                <section className="space-y-2">
                  <div className="text-xs font-medium text-muted">{t('logsRuntimeMessage')}</div>
                  <pre className="mono max-h-[min(38rem,60vh)] overflow-auto whitespace-pre-wrap break-words rounded-lg bg-surface-secondary px-4 py-3 text-xs leading-6 text-foreground">
                    {selectedRuntime?.message || '—'}
                  </pre>
                </section>
              </Modal.Body>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal.Root>

      <Modal.Root isOpen={detailOpen} onOpenChange={(open: boolean) => {
        setDetailOpen(open)
        if (!open) {
          detailRequestRef.current += 1
          setDetailLoading(false)
          setDetailError('')
          setSelected(null)
        }
      }}>
        <Modal.Backdrop variant="blur">
          <Modal.Container size="lg" scroll="inside">
            <Modal.Dialog className="w-full max-w-4xl">
              <Modal.Header className="items-start justify-between gap-4 px-5 pt-5">
                <div className="min-w-0">
                  <Modal.Heading className="text-section font-semibold">{t('logsDetailTitle')}</Modal.Heading>
                  <p className="mono mt-1 text-caption text-muted">{selected?.id}</p>
                </div>
                <Modal.CloseTrigger aria-label={t('close')} className="grid size-8 shrink-0 place-items-center rounded-lg text-muted transition-colors hover:bg-surface-secondary hover:text-foreground">
                  <X size={16} />
                </Modal.CloseTrigger>
              </Modal.Header>
              <Modal.Body className="space-y-4 px-5 pb-5">
                {detailLoading ? (
                  <RequestDetailSkeleton />
                ) : detailError ? (
                  <PageAlert title={t('requestFailed')} description={detailError} />
                ) : (
                  <>
                    <dl className="grid gap-3 sm:grid-cols-2">
                  {[
                    [t('logsColStatus'), selected?.status || '—'],
                    [t('logsColModel'), selected?.requested_model || '—'],
                    [t('logsColReasoning'), selected ? (reasoningLabel(selected) || '—') : '—'],
                    [t('logsColProvider'), selected ? providerLabel(selected) : '—'],
                    [t('logsColAccount'), selected?.account_id ? (accountNameById.get(selected.account_id) || selected.account_id) : '—'],
                    [t('logsColApiKey'), selected ? apiKeyLabel(selected) : '—'],
                    [t('logsColStream'), selected?.stream ? t('logsStreamYes') : t('logsStreamNo')],
                    [t('logsRouting'), selected ? routingLabel(selected.routing) : '—'],
                  ].map(([label, value]) => (
                    <div key={String(label)}>
                      <dt className="text-caption text-muted">{label}</dt>
                      <dd className="mt-1 break-all text-body font-medium">{value}</dd>
                    </div>
                  ))}
                  <div>
                    <dt className="text-caption text-muted">{t('logsColTokens')}</dt>
                    <dd className="mt-1">
                      {selected ? (
                        <TokenSplit
                          log={selected}
                          inLabel={t('logsTokensIn')}
                          outLabel={t('logsTokensOut')}
                          pointsLabel={(value) => t('logsTokensPoints', { value })}
                        />
                      ) : '—'}
                    </dd>
                  </div>
                  <div className="sm:col-span-2">
                    <LatencyStats
                      latencyMs={selected?.latency_ms}
                      ttfbMs={selected?.ttfb_ms}
                      completionTokens={selected?.completion_tokens}
                      stream={selected?.stream}
                      labels={{
                        heading: t('logsColLatency'),
                        ttft: t('logsColTTFT'),
                        total: t('logsLatencyTotal'),
                        tps: t('logsOutputTPS'),
                      }}
                    />
                  </div>
                </dl>
                {selected && selected.message_count ? (
                  <div className="rounded-lg bg-surface-secondary px-3 py-3 text-xs">
                    <div className="font-medium text-muted">{t('logsMessageShape')}</div>
                    <dl className="mt-2 grid gap-2 sm:grid-cols-2">
                      <div><dt className="text-caption text-muted">{t('logsMessageCount')}</dt><dd className="mono mt-0.5">{selected.message_count}</dd></div>
                      <div><dt className="text-caption text-muted">{t('logsEmptyMessageIndexes')}</dt><dd className="mono mt-0.5 break-all">{selected.empty_message_indexes?.length ? selected.empty_message_indexes.join(', ') : '—'}</dd></div>
                      <div className="sm:col-span-2"><dt className="text-caption text-muted">{t('logsMessageRoles')}</dt><dd className="mono mt-0.5 break-all">{selected.message_roles?.join(' → ') || '—'}</dd></div>
                    </dl>
                  </div>
                ) : null}
                {selected?.error_message ? (
                  <div className="rounded-lg bg-surface-secondary px-3 py-2 text-xs leading-5 text-muted">
                    {selected.error_kind ? <span className="mono mr-2 text-muted">{selected.error_kind}</span> : null}
                    {selected.error_message}
                  </div>
                ) : null}
                {selected?.stream_diagnostic ? (
                  <div className="rounded-lg bg-surface-secondary px-3 py-3 text-xs">
                    <div className="font-medium text-muted">{t('logsStreamDiagnostics')}</div>
                    <dl className="mt-2 grid gap-2 sm:grid-cols-2">
                      {[
                        [t('logsCancellationSource'), selected.stream_diagnostic.cancellation_source || '—'],
                        [t('logsUpstreamStatus'), selected.stream_diagnostic.upstream_status ?? '—'],
                        [t('logsSSEEvents'), selected.stream_diagnostic.sse_event_count],
                        [t('logsBytesRead'), selected.stream_diagnostic.bytes_read],
                        [t('logsLastEvent'), selected.stream_diagnostic.last_event || '—'],
                        [t('logsStreamComplete'), selected.stream_diagnostic.saw_done ? 'yes' : 'no'],
                      ].map(([label, value]) => (
                        <div key={String(label)}>
                          <dt className="text-caption text-muted">{label}</dt>
                          <dd className="mono mt-0.5 break-all">{value}</dd>
                        </div>
                      ))}
                    </dl>
                    {selected.stream_diagnostic.context_err || selected.stream_diagnostic.relay_error ? (
                      <div className="mt-2 space-y-1 break-all text-muted">
                        {selected.stream_diagnostic.context_err ? <div>context: {selected.stream_diagnostic.context_err}</div> : null}
                        {selected.stream_diagnostic.relay_error ? <div>relay: {selected.stream_diagnostic.relay_error}</div> : null}
                      </div>
                    ) : null}
                  </div>
                ) : null}
                {selected?.usage_detail ? (
                  <div className="rounded-lg bg-surface-secondary px-3 py-3 text-xs">
                    <div className="font-medium text-muted">{t('logsUsageDetail')}</div>
                    <dl className="mt-2 grid gap-2 sm:grid-cols-2">
                      {[
                        [t('logsCreditConsumed'), `${formatCredit(selected.usage_detail.credit)} ${selected.usage_detail.unit || 'credits'}`],
                        [t('logsColProvider'), selected.usage_detail.provider || '—'],
                      ].map(([label, value]) => (
                        <div key={String(label)}>
                          <dt className="text-caption text-muted">{label}</dt>
                          <dd className="mono mt-0.5 break-all">{value}</dd>
                        </div>
                      ))}
                    </dl>
                  </div>
                ) : null}
                <div>
                  <div className="text-xs font-medium text-muted">{t('logsAttempts')}</div>
                  {selected?.attempts?.length ? (
                    <div className="mt-2 divide-y divide-separator overflow-hidden rounded-lg bg-surface-secondary">
                      {selected.attempts.map((attempt) => (
                        <div key={attempt.id} className="grid gap-1 px-3 py-2.5 text-xs sm:grid-cols-[48px_minmax(0,1fr)_auto]">
                          <div className="mono text-muted">#{attempt.attempt_index}</div>
                          <div>
                            <div className="font-medium">{attempt.account_id || '—'}</div>
                            <div className="mt-0.5 text-muted">{attempt.error_message || attempt.status}</div>
                          </div>
                          <Chip size="sm" variant="soft" color={statusColor(attempt.status === 'failover' ? 'canceled' : attempt.status)}>
                            {attempt.status}
                          </Chip>
                        </div>
                      ))}
                    </div>
                  ) : (
                    <p className="mt-2 text-xs text-muted">{t('logsNoAttempts')}</p>
                  )}
                </div>
                  </>
                )}
              </Modal.Body>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal.Root>

      <ConfirmDialog
        isOpen={clearOpen}
        title={t('logsClear')}
        description={t('logsClearConfirm')}
        confirmLabel={t('logsClear')}
        cancelLabel={t('close')}
        closeLabel={t('close')}
        isPending={busy}
        onClose={() => setClearOpen(false)}
        onConfirm={() => void onClear()}
      />
    </div>
  )
}
