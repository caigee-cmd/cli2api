import { useCallback, useEffect, useMemo, useState } from 'react'
import { getLocalTimeZone, type DateValue } from '@internationalized/date'
import {
  Button,
  ButtonGroup,
  Card,
  Chip,
  DateField,
  DateRangePicker,
  Input,
  Label,
  Modal,
  RangeCalendar,
  Tab,
  Table,
  Tabs,
  TimeField,
  type TimeValue,
} from '@heroui/react'
import {
  ArrowClockwise,
  MagnifyingGlass,
  Scroll,
  TerminalWindow,
  TrashSimple,
  WarningCircle,
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
import { ListPager, type PageSize } from '@/components/ui/ListPager'
import { LogsRequestListSkeleton, LogsRuntimeListSkeleton } from '@/components/ui/PageSkeletons'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import { accountProviderLabel } from '@/lib/provider'

type PageTab = 'requests' | 'runtime'
type RequestFilter = 'all' | 'ok' | 'error' | 'canceled'
type RuntimeFilter = 'all' | 'info' | 'warn' | 'error'
type StreamFilter = 'all' | 'stream' | 'sync'
type TimeRange = 'all' | '1h' | '24h' | '7d' | 'custom'
type ErrorKindFilter = 'all' | 'quota' | 'rate_limit' | 'auth' | 'not_ready' | 'unavailable' | 'invalid_request' | 'model_not_available'

const FILTER_SELECT_CLASS = 'h-8 min-w-36 rounded-lg border border-[var(--app-line-strong)] bg-[var(--app-surface-solid)] px-2.5 text-xs text-[var(--app-ink)]'

type DateRangeValue = { start: DateValue; end: DateValue }

function statusColor(status?: string): 'success' | 'warning' | 'danger' | 'default' {
  if (status === 'ok') return 'success'
  if (status === 'streaming' || status === 'started') return 'warning'
  if (status === 'error' || status === 'canceled') return 'danger'
  return 'default'
}

function levelDot(level?: string) {
  if (level === 'error') return 'danger'
  if (level === 'warn') return undefined
  return 'ok'
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

function TokenSplit({ log, inLabel, outLabel }: { log: RequestLog; inLabel: string; outLabel: string }) {
  const prompt = log.prompt_tokens
  const completion = log.completion_tokens
  if (prompt == null && completion == null) {
    return <span className="mono text-xs text-[var(--app-faint)]">—</span>
  }
  return (
    <div className="leading-4">
      <div className="mono text-xs">{prompt ?? 0} / {completion ?? 0}</div>
      <div className="mt-0.5 text-[10px] text-[var(--app-faint)]">{inLabel} / {outLabel}</div>
    </div>
  )
}

function formatLatency(ms?: number | null) {
  if (ms == null) return '—'
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

function dateValueToISO(value: DateValue | null | undefined, endOfMinute = false) {
  if (!value) return undefined
  const date = value.toDate(getLocalTimeZone())
  if (!Number.isFinite(date.getTime())) return undefined
  if (endOfMinute) date.setSeconds(59, 999)
  return date.toISOString()
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
  const { overview } = useOverview()
  const accounts = overview?.accounts
  const models = overview?.models
  const [tab, setTab] = useState<PageTab>('requests')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [requestFilter, setRequestFilter] = useState<RequestFilter>('all')
  const [runtimeFilter, setRuntimeFilter] = useState<RuntimeFilter>('all')
  const [requestQuery, setRequestQuery] = useState('')
  const [runtimeQuery, setRuntimeQuery] = useState('')
  const [accountFilter, setAccountFilter] = useState('')
  const [runtimeAccount, setRuntimeAccount] = useState('')
  const [modelFilter, setModelFilter] = useState('')
  const [streamFilter, setStreamFilter] = useState<StreamFilter>('all')
  const [errorKind, setErrorKind] = useState<ErrorKindFilter>('all')
  const [timeRange, setTimeRange] = useState<TimeRange>('all')
  const [customRange, setCustomRange] = useState<DateRangeValue | null>(null)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState<PageSize>(50)
  const [runtimePage, setRuntimePage] = useState(1)
  const [runtimePageSize, setRuntimePageSize] = useState<PageSize>(50)
  const [requests, setRequests] = useState<RequestLog[]>([])
  const [total, setTotal] = useState(0)
  const [runtime, setRuntime] = useState<RuntimeLogEntry[]>([])
  const [runtimeTotal, setRuntimeTotal] = useState(0)
  const [selected, setSelected] = useState<RequestLog | null>(null)
  const [detailOpen, setDetailOpen] = useState(false)
  const [clearOpen, setClearOpen] = useState(false)
  const [busy, setBusy] = useState(false)

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

  const hasRequestFilters = Boolean(
    requestQuery.trim()
    || requestFilter !== 'all'
    || accountFilter
    || modelFilter
    || streamFilter !== 'all'
    || errorKind !== 'all'
    || timeRange !== 'all',
  )

  const requestFilterKey = [
    requestFilter,
    requestQuery,
    accountFilter,
    modelFilter,
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
      const result = await fetchRequestLogs({
        status: requestFilter === 'all' ? undefined : requestFilter,
        q: requestQuery.trim() || undefined,
        account: accountFilter || undefined,
        model: modelFilter || undefined,
        stream: streamFilter === 'all' ? undefined : streamFilter === 'stream',
        error_kind: errorKind === 'all' ? undefined : errorKind,
        from: timeRange === 'custom' ? dateValueToISO(customRange?.start) : rangeFromPreset(timeRange).from,
        to: timeRange === 'custom' ? dateValueToISO(customRange?.end, true) : rangeFromPreset(timeRange).to,
        limit: pageSize,
        offset: (currentPage - 1) * pageSize,
      })
      setRequests(result.items || [])
      setTotal(result.total || 0)
      setError('')
    } catch (err) {
      if (!quiet) setError(err instanceof Error ? err.message : String(err))
    } finally {
      if (!quiet) setLoading(false)
    }
  }, [requestFilter, requestQuery, accountFilter, modelFilter, streamFilter, errorKind, timeRange, customRange, currentPage, pageSize])

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
      if (!quiet) setLoading(false)
    }
  }, [runtimeFilter, runtimeQuery, runtimeAccount, currentRuntimePage, runtimePageSize])

  const load = useCallback(async (quiet = false) => {
    if (tab === 'requests') await loadRequests(quiet)
    else await loadRuntime(quiet)
  }, [tab, loadRequests, loadRuntime])

  useEffect(() => {
    setLoading(true)
    const delay = tab === 'requests' && requestQuery.trim() ? 280 : tab === 'runtime' && runtimeQuery.trim() ? 280 : 0
    const timer = window.setTimeout(() => void load(false), delay)
    return () => window.clearTimeout(timer)
  }, [load, tab, requestQuery, runtimeQuery])

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
    setRequestQuery('')
    setRequestFilter('all')
    setAccountFilter('')
    setModelFilter('')
    setStreamFilter('all')
    setErrorKind('all')
    setTimeRange('all')
    setCustomRange(null)
    setPage(1)
  }

  async function openDetail(id: string) {
    setBusy(true)
    try {
      const detail = await fetchRequestLog(id)
      setSelected(detail)
      setDetailOpen(true)
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function onClear() {
    setBusy(true)
    try {
      await clearRequestLogs()
      setClearOpen(false)
      await loadRequests(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-6">
      <section className="flex flex-wrap items-end justify-between gap-4 border-b border-[var(--app-line)] pb-4">
        <div>
          <h2 data-gsap-reveal className="text-2xl font-semibold tracking-[-0.035em]">{t('navLogs')}</h2>
          <p className="mt-1 max-w-2xl text-sm leading-6 text-[var(--app-muted)]">{t('logsLead')}</p>
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

      {error ? (
        <div className="flex items-start gap-2 rounded-lg border border-[color-mix(in_srgb,var(--app-danger)_28%,var(--app-line))] bg-[color-mix(in_srgb,var(--app-danger)_7%,var(--app-surface))] px-4 py-3 text-sm text-[var(--app-danger)]">
          <WarningCircle className="mt-0.5 shrink-0" size={17} />
          {t('failedLogs', { msg: error })}
        </div>
      ) : null}

      <Tabs.Root selectedKey={tab} onSelectionChange={(key) => setTab(String(key) as PageTab)}>
        <Tabs.List className="grid max-w-md grid-cols-2 gap-1 rounded-lg bg-[var(--app-surface-muted)] p-1">
          <Tab id="requests" className="flex items-center justify-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium text-[var(--app-faint)] data-[hovered=true]:text-[var(--app-fg)] data-[selected=true]:bg-[var(--app-surface)] data-[selected=true]:text-[var(--app-ink)] data-[selected=true]:shadow-sm">
            <Scroll size={13} />{t('logsRequests')}
          </Tab>
          <Tab id="runtime" className="flex items-center justify-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium text-[var(--app-faint)] data-[hovered=true]:text-[var(--app-fg)] data-[selected=true]:bg-[var(--app-surface)] data-[selected=true]:text-[var(--app-ink)] data-[selected=true]:shadow-sm">
            <TerminalWindow size={13} />{t('logsRuntime')}
          </Tab>
        </Tabs.List>

        <Tabs.Panel id="requests" className="space-y-4 pt-5">
          <div className="flex flex-col gap-3">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div className="flex flex-wrap items-center gap-2">
                <Input
                  className="sm:w-72"
                  value={requestQuery}
                  onChange={(event) => setRequestQuery(event.target.value)}
                  placeholder={t('logsSearchRequests')}
                  aria-label={t('logsSearchRequests')}
                />
                <ButtonGroup className="toolbar-group">
                  {([
                    ['all', t('logsFilterAll')],
                    ['ok', t('logsFilterOk')],
                    ['error', t('logsFilterError')],
                    ['canceled', t('logsFilterCanceled')],
                  ] as Array<[RequestFilter, string]>).map(([value, label]) => (
                    <Button
                      key={value}
                      size="sm"
                      variant={requestFilter === value ? 'secondary' : 'ghost'}
                      onPress={() => setRequestFilter(value)}
                    >
                      {label}
                    </Button>
                  ))}
                </ButtonGroup>
              </div>
              <div className="flex items-center gap-2">
                {hasRequestFilters ? (
                  <Button size="sm" variant="ghost" onPress={clearRequestFilters}>{t('clearFilters')}</Button>
                ) : null}
                <div className="mono text-[11px] text-[var(--app-faint)]">{shownLabel}</div>
              </div>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <select
                className={FILTER_SELECT_CLASS}
                value={accountFilter}
                onChange={(event) => setAccountFilter(event.target.value)}
                aria-label={t('logsColAccount')}
              >
                <option value="">{t('logsFilterAccountAll')}</option>
                {(accounts || []).map((account) => (
                  <option key={account.id} value={account.id}>{account.name || account.id}</option>
                ))}
              </select>
              <select
                className={FILTER_SELECT_CLASS}
                value={modelFilter}
                onChange={(event) => setModelFilter(event.target.value)}
                aria-label={t('logsColModel')}
              >
                <option value="">{t('logsFilterModelAll')}</option>
                {(models || []).map((model) => (
                  <option key={model.id} value={model.id}>{model.display_name || model.id}</option>
                ))}
              </select>
              <ButtonGroup className="toolbar-group">
                {([
                  ['all', t('logsFilterStreamAll')],
                  ['stream', t('logsStreamYes')],
                  ['sync', t('logsStreamNo')],
                ] as Array<[StreamFilter, string]>).map(([value, label]) => (
                  <Button
                    key={value}
                    size="sm"
                    variant={streamFilter === value ? 'secondary' : 'ghost'}
                    onPress={() => setStreamFilter(value)}
                  >
                    {label}
                  </Button>
                ))}
              </ButtonGroup>
              <select
                className={FILTER_SELECT_CLASS}
                value={errorKind}
                onChange={(event) => setErrorKind(event.target.value as ErrorKindFilter)}
                aria-label={t('errorKind')}
              >
                <option value="all">{t('logsFilterKindAll')}</option>
                <option value="quota">{t('logsKindQuota')}</option>
                <option value="rate_limit">{t('logsKindRateLimit')}</option>
                <option value="auth">{t('logsKindAuth')}</option>
                <option value="not_ready">{t('logsKindNotReady')}</option>
                <option value="unavailable">{t('logsKindUnavailable')}</option>
                <option value="invalid_request">{t('logsKindInvalidRequest')}</option>
                <option value="model_not_available">{t('logsKindModelNotAvailable')}</option>
              </select>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <span className="text-xs font-medium text-[var(--app-muted)]">{t('logsTimeRange')}</span>
              <ButtonGroup className="toolbar-group">
                {([
                  ['all', t('logsTimeAll')],
                  ['1h', t('logsTime1h')],
                  ['24h', t('logsTime24h')],
                  ['7d', t('logsTime7d')],
                  ['custom', t('logsTimeCustom')],
                ] as Array<[TimeRange, string]>).map(([value, label]) => (
                  <Button
                    key={value}
                    size="sm"
                    variant={timeRange === value ? 'secondary' : 'ghost'}
                    onPress={() => setTimeRange(value)}
                  >
                    {label}
                  </Button>
                ))}
              </ButtonGroup>
              {timeRange === 'custom' ? (
                <DateRangePicker
                  className="w-[min(100%,42rem)]"
                  granularity="minute"
                  hourCycle={24}
                  shouldForceLeadingZeros
                  value={customRange}
                  onChange={(next) => setCustomRange(next)}
                  aria-label={t('logsTimeRange')}
                >
                  {({ state }) => (
                    <>
                      <DateField.Group className="h-8 min-h-8 w-full min-w-0 text-xs" variant="secondary">
                        <DateField.Input className="min-w-0" slot="start" aria-label={t('logsTimeFrom')}>
                          {(segment) => <DateField.Segment segment={segment} />}
                        </DateField.Input>
                        <DateRangePicker.RangeSeparator />
                        <DateField.Input className="min-w-0" slot="end" aria-label={t('logsTimeTo')}>
                          {(segment) => <DateField.Segment segment={segment} />}
                        </DateField.Input>
                        <DateField.Suffix>
                          <DateRangePicker.Trigger>
                            <DateRangePicker.TriggerIndicator />
                          </DateRangePicker.Trigger>
                        </DateField.Suffix>
                      </DateField.Group>
                      <DateRangePicker.Popover className="flex w-[20.5rem] max-w-[calc(100vw-2rem)] flex-col gap-3 p-3">
                        <RangeCalendar aria-label={t('logsTimeRange')}>
                          <RangeCalendar.Header>
                            <RangeCalendar.YearPickerTrigger>
                              <RangeCalendar.YearPickerTriggerHeading />
                              <RangeCalendar.YearPickerTriggerIndicator />
                            </RangeCalendar.YearPickerTrigger>
                            <RangeCalendar.NavButton slot="previous" />
                            <RangeCalendar.NavButton slot="next" />
                          </RangeCalendar.Header>
                          <RangeCalendar.Grid>
                            <RangeCalendar.GridHeader>
                              {(day) => (
                                <RangeCalendar.HeaderCell>{day}</RangeCalendar.HeaderCell>
                              )}
                            </RangeCalendar.GridHeader>
                            <RangeCalendar.GridBody>
                              {(date) => <RangeCalendar.Cell date={date} />}
                            </RangeCalendar.GridBody>
                          </RangeCalendar.Grid>
                          <RangeCalendar.YearPickerGrid>
                            <RangeCalendar.YearPickerGridBody>
                              {({ year }) => <RangeCalendar.YearPickerCell year={year} />}
                            </RangeCalendar.YearPickerGridBody>
                          </RangeCalendar.YearPickerGrid>
                        </RangeCalendar>
                        <div className="flex flex-col gap-3">
                          <div className="flex items-center justify-between gap-3">
                            <Label className="text-xs">{t('logsTimeFrom')}</Label>
                            <TimeField
                              aria-label={t('logsTimeFrom')}
                              granularity="minute"
                              hourCycle={24}
                              value={state.timeRange?.start ?? null}
                              onChange={(value) =>
                                state.setTimeRange({
                                  start: value as TimeValue,
                                  end: state.timeRange?.end as TimeValue,
                                })
                              }
                            >
                              <TimeField.Group className="h-8 min-h-8 w-[7.5rem] text-xs" variant="secondary">
                                <TimeField.Input>
                                  {(segment) => <TimeField.Segment segment={segment} />}
                                </TimeField.Input>
                              </TimeField.Group>
                            </TimeField>
                          </div>
                          <div className="flex items-center justify-between gap-3">
                            <Label className="text-xs">{t('logsTimeTo')}</Label>
                            <TimeField
                              aria-label={t('logsTimeTo')}
                              granularity="minute"
                              hourCycle={24}
                              value={state.timeRange?.end ?? null}
                              onChange={(value) =>
                                state.setTimeRange({
                                  start: state.timeRange?.start as TimeValue,
                                  end: value as TimeValue,
                                })
                              }
                            >
                              <TimeField.Group className="h-8 min-h-8 w-[7.5rem] text-xs" variant="secondary">
                                <TimeField.Input>
                                  {(segment) => <TimeField.Segment segment={segment} />}
                                </TimeField.Input>
                              </TimeField.Group>
                            </TimeField>
                          </div>
                        </div>
                      </DateRangePicker.Popover>
                    </>
                  )}
                </DateRangePicker>
              ) : null}
            </div>
          </div>

          <Card data-gsap-reveal className="app-panel-flat overflow-hidden rounded-lg p-0 shadow-none" aria-busy={loading}>
            {loading ? (
              <LogsRequestListSkeleton />
            ) : requests.length === 0 ? (
              <div className="grid min-h-72 place-items-center px-6 py-12 text-center">
                <div>
                  <MagnifyingGlass size={22} className="mx-auto text-[var(--app-faint)]" />
                  <div className="mt-4 text-sm font-medium">{hasRequestFilters ? t('logsNoMatch') : t('logsEmptyRequests')}</div>
                  {hasRequestFilters ? (
                    <Button className="mt-4" size="sm" variant="ghost" onPress={clearRequestFilters}>{t('clearFilters')}</Button>
                  ) : null}
                </div>
              </div>
            ) : (
              <Table>
                <Table.ScrollContainer>
                  <Table.Content aria-label={t('logsRequests')}>
                    <Table.Header>
                      <Table.Column isRowHeader>{t('logsColTime')}</Table.Column>
                      <Table.Column>{t('logsColModel')}</Table.Column>
                      <Table.Column>{t('logsColProvider')}</Table.Column>
                      <Table.Column>{t('logsColAccount')}</Table.Column>
                      <Table.Column>{t('logsColStatus')}</Table.Column>
                      <Table.Column>{t('logsColStream')}</Table.Column>
                      <Table.Column>{t('logsColLatency')}</Table.Column>
                      <Table.Column>{t('logsColTTFT')}</Table.Column>
                      <Table.Column>{t('logsColTokens')}</Table.Column>
                    </Table.Header>
                    <Table.Body>
                      {requests.map((item) => (
                        <Table.Row key={item.id} className="cursor-pointer" onClick={() => void openDetail(item.id)}>
                          <Table.Cell>
                            <div className="py-1">
                              <div className="mono text-xs">{formatTime(item.created_at, lang)}</div>
                              <div className="mono mt-0.5 text-[10px] text-[var(--app-faint)]">{item.id}</div>
                            </div>
                          </Table.Cell>
                          <Table.Cell>
                            <div className="text-sm font-medium">{item.requested_model || '—'}</div>
                            {item.mapped_model && item.mapped_model !== item.requested_model ? (
                              <div className="mono mt-0.5 text-[10px] text-[var(--app-faint)]">{item.mapped_model}</div>
                            ) : null}
                          </Table.Cell>
                          <Table.Cell>
                            <span className="text-xs">{providerLabel(item)}</span>
                          </Table.Cell>
                          <Table.Cell>
                            <span className="text-xs">{item.account_id ? (accountNameById.get(item.account_id) || item.account_id) : '—'}</span>
                            {item.account_id && accountNameById.get(item.account_id) && accountNameById.get(item.account_id) !== item.account_id ? (
                              <div className="mono mt-0.5 text-[10px] text-[var(--app-faint)]">{item.account_id}</div>
                            ) : null}
                          </Table.Cell>
                          <Table.Cell>
                            <Chip size="sm" variant="soft" color={statusColor(item.status)}>{item.status}</Chip>
                            {item.error_kind ? <div className="mono mt-1 text-[10px] text-[var(--app-faint)]">{item.error_kind}</div> : null}
                          </Table.Cell>
                          <Table.Cell><span className="text-xs text-[var(--app-muted)]">{item.stream ? t('logsStreamYes') : t('logsStreamNo')}</span></Table.Cell>
                          <Table.Cell><span className="mono text-xs">{formatLatency(item.latency_ms)}</span></Table.Cell>
                          <Table.Cell><span className="mono text-xs">{formatLatency(item.ttfb_ms)}</span></Table.Cell>
                          <Table.Cell><TokenSplit log={item} inLabel={t('logsTokensIn')} outLabel={t('logsTokensOut')} /></Table.Cell>
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

        <Tabs.Panel id="runtime" className="space-y-4 pt-5">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex flex-wrap items-center gap-2">
              <Input
                className="sm:w-72"
                value={runtimeQuery}
                onChange={(event) => setRuntimeQuery(event.target.value)}
                placeholder={t('logsSearchRuntime')}
                aria-label={t('logsSearchRuntime')}
              />
              <select
                className={FILTER_SELECT_CLASS}
                value={runtimeAccount}
                onChange={(event) => setRuntimeAccount(event.target.value)}
                aria-label={t('logsColAccount')}
              >
                <option value="">{t('logsFilterAccountAll')}</option>
                {(accounts || []).map((account) => (
                  <option key={account.id} value={account.id}>{account.name || account.id}</option>
                ))}
              </select>
              <ButtonGroup className="toolbar-group">
                {([
                  ['all', t('logsLevelAll')],
                  ['info', t('logsLevelInfo')],
                  ['warn', t('logsLevelWarn')],
                  ['error', t('logsLevelError')],
                ] as Array<[RuntimeFilter, string]>).map(([value, label]) => (
                  <Button
                    key={value}
                    size="sm"
                    variant={runtimeFilter === value ? 'secondary' : 'ghost'}
                    onPress={() => setRuntimeFilter(value)}
                  >
                    {label}
                  </Button>
                ))}
              </ButtonGroup>
            </div>
            <div className="flex items-center gap-2 text-xs text-[var(--app-faint)]">
              {currentRuntimePage === 1 ? (
                <>
                  <span className="status-dot" data-state="ok" />
                  {t('logsAutoRefresh')}
                </>
              ) : null}
              <span className="mono">{shownLabel}</span>
            </div>
          </div>

          <Card data-gsap-reveal className="app-panel-flat overflow-hidden rounded-lg p-0 shadow-none" aria-busy={loading}>
            {loading ? (
              <LogsRuntimeListSkeleton />
            ) : runtime.length === 0 ? (
              <div className="grid min-h-72 place-items-center px-6 py-12 text-center">
                <div>
                  <TerminalWindow size={22} className="mx-auto text-[var(--app-faint)]" />
                  <div className="mt-4 text-sm font-medium">{t('logsEmptyRuntime')}</div>
                </div>
              </div>
            ) : (
              <div className="divide-y divide-[var(--app-line)]">
                {runtime.map((entry) => (
                  <div key={entry.id} className="grid gap-2 px-5 py-3 sm:grid-cols-[150px_72px_minmax(0,1fr)] sm:items-start">
                    <div className="mono text-[11px] text-[var(--app-faint)]">{formatTime(entry.time, lang)}</div>
                    <div className="flex items-center gap-2 text-xs">
                      <span className="status-dot" data-state={levelDot(entry.level)} />
                      <span className="font-medium uppercase tracking-[0.04em] text-[var(--app-muted)]">{entry.level}</span>
                    </div>
                    <div className="min-w-0">
                      {entry.account_id ? <div className="mono mb-1 text-[10px] text-[var(--app-faint)]">{entry.account_id}</div> : null}
                      <div className="mono break-all text-xs leading-5 text-[var(--app-ink)]">{entry.message}</div>
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
      </Tabs.Root>

      <Modal isOpen={detailOpen} onOpenChange={setDetailOpen}>
        <Modal.Backdrop>
          <Modal.Container>
            <Modal.Dialog className="max-w-2xl bg-[var(--app-surface)] text-[var(--app-ink)]">
              <div className="flex items-start justify-between gap-3 border-b border-[var(--app-line)] px-5 py-4">
                <div>
                  <h3 className="font-semibold">{t('logsDetailTitle')}</h3>
                  <p className="mono mt-1 text-[11px] text-[var(--app-faint)]">{selected?.id}</p>
                </div>
                <Button isIconOnly size="sm" variant="ghost" onPress={() => setDetailOpen(false)} aria-label={t('close')}>
                  <X size={14} />
                </Button>
              </div>
              <div className="space-y-4 px-5 py-4">
                <dl className="grid gap-3 sm:grid-cols-2">
                  {[
                    [t('logsColStatus'), selected?.status || '—'],
                    [t('logsColModel'), selected?.requested_model || '—'],
                    [t('logsColProvider'), selected ? providerLabel(selected) : '—'],
                    [t('logsColAccount'), selected?.account_id ? (accountNameById.get(selected.account_id) || selected.account_id) : '—'],
                    [t('logsColLatency'), formatLatency(selected?.latency_ms)],
                    [t('logsColTTFT'), formatLatency(selected?.ttfb_ms)],
                    [t('logsColStream'), selected?.stream ? t('logsStreamYes') : t('logsStreamNo')],
                  ].map(([label, value]) => (
                    <div key={String(label)}>
                      <dt className="text-[11px] text-[var(--app-faint)]">{label}</dt>
                      <dd className="mt-1 break-all text-sm font-medium">{value}</dd>
                    </div>
                  ))}
                  <div>
                    <dt className="text-[11px] text-[var(--app-faint)]">{t('logsColTokens')}</dt>
                    <dd className="mt-1">
                      {selected ? (
                        <TokenSplit log={selected} inLabel={t('logsTokensIn')} outLabel={t('logsTokensOut')} />
                      ) : '—'}
                    </dd>
                  </div>
                </dl>
                {selected?.error_message ? (
                  <div className="rounded-lg border border-[var(--app-line)] bg-[var(--app-surface-muted)] px-3 py-2 text-xs leading-5 text-[var(--app-muted)]">
                    {selected.error_kind ? <span className="mono mr-2 text-[var(--app-faint)]">{selected.error_kind}</span> : null}
                    {selected.error_message}
                  </div>
                ) : null}
                <div>
                  <div className="text-xs font-medium text-[var(--app-muted)]">{t('logsAttempts')}</div>
                  {selected?.attempts?.length ? (
                    <div className="mt-2 divide-y divide-[var(--app-line)] rounded-lg border border-[var(--app-line)]">
                      {selected.attempts.map((attempt) => (
                        <div key={attempt.id} className="grid gap-1 px-3 py-2.5 text-xs sm:grid-cols-[48px_minmax(0,1fr)_auto]">
                          <div className="mono text-[var(--app-faint)]">#{attempt.attempt_index}</div>
                          <div>
                            <div className="font-medium">{attempt.account_id || '—'}</div>
                            <div className="mt-0.5 text-[var(--app-faint)]">{attempt.error_message || attempt.status}</div>
                          </div>
                          <Chip size="sm" variant="soft" color={statusColor(attempt.status === 'failover' ? 'canceled' : attempt.status)}>
                            {attempt.status}
                          </Chip>
                        </div>
                      ))}
                    </div>
                  ) : (
                    <p className="mt-2 text-xs text-[var(--app-faint)]">{t('logsNoAttempts')}</p>
                  )}
                </div>
              </div>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal>

      <Modal isOpen={clearOpen} onOpenChange={setClearOpen}>
        <Modal.Backdrop>
          <Modal.Container>
            <Modal.Dialog className="max-w-md bg-[var(--app-surface)] text-[var(--app-ink)]">
              <div className="px-5 py-4">
                <h3 className="font-semibold">{t('logsClear')}</h3>
                <p className="mt-2 text-sm leading-6 text-[var(--app-muted)]">{t('logsClearConfirm')}</p>
                <div className="mt-5 flex justify-end gap-2">
                  <Button size="sm" variant="ghost" onPress={() => setClearOpen(false)}>{t('close')}</Button>
                  <Button size="sm" variant="danger" isPending={busy} onPress={() => void onClear()}>
                    <TrashSimple size={14} />{t('logsClear')}
                  </Button>
                </div>
              </div>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal>
    </div>
  )
}
