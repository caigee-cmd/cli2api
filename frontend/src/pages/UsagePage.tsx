import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Card, Tab, Table, Tabs, type TableSortDirection } from '@heroui/react'
import { ChartBar, Cube, Stack, UsersThree } from '@phosphor-icons/react'
import { fetchRequestStats, type RequestStats, type RequestStatsDim, type RequestStatsNamed } from '@/api/logs'
import { fetchAccounts } from '@/api/overview'
import type { Overview } from '@/api/types'
import { StatsWindowControls } from '@/components/overview/StatsWindowControls'
import { ExpiryDistribution } from '@/components/overview/ExpiryDistribution'
import { STATS_WINDOW_LABEL, statsQuery, type StatsWindow } from '@/lib/statsWindow'
import { ProviderMark } from '@/components/ProviderMark'
import { PageAlert } from '@/components/ui/PageAlert'
import { SkeletonBlock } from '@/components/ui/PageSkeletons'
import type { DateRangeValue } from '@/components/ui/TimeRangePicker'
import { useI18n } from '@/hooks/useI18n'
import { formatCompact, formatLatency, formatPercent } from '@/lib/format'
import { logsHref } from '@/lib/logsLink'
import { accountProviderFamilyLabel } from '@/lib/provider'
import { UsageCostPanel } from '@/components/usage/UsageCostPanel'

/** Rows per dimension; the server caps limit at 500. */
const USAGE_LIMIT = 500

type PageTab = 'stats' | 'cost'

type UsageSortKey = 'label' | 'count' | 'success' | 'p95' | 'tokens' | 'credits' | 'cache_write'

type UsageRow = {
  key: string
  label: string
  count: number
  success: number
  p95: number | null
  tokens: number
  prompt: number
  completion: number
  credits: number
  cache_write: number
  cache_read: number
  /** Logs filter for a drill-down, when the key is a real value. */
  filter?: Record<string, string>
}

const DIM_FILTER: Record<RequestStatsDim, string> = { model: 'model', account: 'account', provider: 'provider' }

function toUsageRow(item: RequestStatsNamed, dim: RequestStatsDim, label: string): UsageRow {
  const prompt = item.prompt_tokens ?? 0
  const completion = item.completion_tokens ?? 0
  const placeholder = item.key === '(unknown)' || item.key === '(unassigned)' || !item.key
  return {
    key: item.key,
    label,
    count: item.count,
    success: item.count ? item.ok / item.count : 0,
    p95: item.latency_p95_ms ?? null,
    tokens: prompt + completion,
    prompt,
    completion,
    credits: item.credits ?? 0,
    cache_write: item.cache_write_tokens ?? 0,
    cache_read: item.cache_read_tokens ?? 0,
    filter: placeholder ? undefined : { [DIM_FILTER[dim]]: item.key },
  }
}

/** Stable sort; missing p95 always sorts last. */
function sortUsageRows(rows: UsageRow[], key: UsageSortKey, direction: TableSortDirection) {
  const sign = direction === 'ascending' ? 1 : -1
  return [...rows].sort((a, b) => {
    if (key === 'label') return sign * a.label.localeCompare(b.label)
    const av = a[key]
    const bv = b[key]
    if (av == null && bv == null) return 0
    if (av == null) return 1
    if (bv == null) return -1
    return sign * ((av as number) - (bv as number)) || a.label.localeCompare(b.label)
  })
}

function formatCredits(value: number) {
  if (!value) return '0'
  if (value >= 1000) return formatCompact(value)
  return String(Math.round(value * 100) / 100)
}

export function UsagePage() {
  const { t } = useI18n()
  const navigate = useNavigate()
  const [pageTab, setPageTab] = useState<PageTab>('stats')
  const [dim, setDim] = useState<RequestStatsDim>('model')
  const [statsWindow, setStatsWindow] = useState<StatsWindow>('today')
  const [customRange, setCustomRange] = useState<DateRangeValue | null>(null)
  const [stats, setStats] = useState<RequestStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [accounts, setAccounts] = useState<NonNullable<Overview['accounts']>>([])
  const [sortKey, setSortKey] = useState<UsageSortKey>('count')
  const [sortDirection, setSortDirection] = useState<TableSortDirection>('descending')
  const requestRef = useRef(0)

  useEffect(() => {
    let cancelled = false
    void fetchAccounts(false).then((result) => { if (!cancelled) setAccounts(result.data || []) }).catch(() => {})
    return () => { cancelled = true }
  }, [])

  const customFrom = customRange?.start?.toString() ?? ''
  const customTo = customRange?.end?.toString() ?? ''
  useEffect(() => {
    const query = statsQuery(statsWindow, customRange)
    if (!query) {
      setLoading(false)
      return
    }
    const version = ++requestRef.current
    void fetchRequestStats({ ...query, dim: [dim], limit: USAGE_LIMIT })
      .then((data) => {
        if (version !== requestRef.current) return
        setStats(data)
        setError('')
      })
      .catch((err) => {
        if (version !== requestRef.current) return
        setStats(null)
        setError(err instanceof Error ? err.message : String(err))
      })
      .finally(() => { if (version === requestRef.current) setLoading(false) })
    // customRange is tracked through its string form.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dim, statsWindow, customFrom, customTo])

  const rows = useMemo(() => {
    const names = new Map(accounts.map((account) => [account.id, account.name || account.id]))
    const source = dim === 'model' ? stats?.models : dim === 'account' ? stats?.accounts : stats?.providers
    const mapped = (source ?? []).map((item) => {
      const label = item.key === '(unknown)' ? t('statsUnknown')
        : item.key === '(unassigned)' ? t('statsUnassigned')
          : dim === 'account' ? names.get(item.key) || item.key
            : dim === 'provider' ? accountProviderFamilyLabel(item.key, t)
              : item.key
      return toUsageRow(item, dim, label)
    })
    return sortUsageRows(mapped, sortKey, sortDirection)
  }, [stats, dim, accounts, sortKey, sortDirection, t])

  const totals = stats?.totals
  const tokens = stats?.tokens
  const columns: Array<{ id: UsageSortKey; label: string; numeric?: boolean }> = [
    { id: 'label', label: t(dim === 'model' ? 'logsColModel' : dim === 'account' ? 'logsColAccount' : 'logsColProvider') },
    { id: 'count', label: t('usageColRequests'), numeric: true },
    { id: 'success', label: t('usageColSuccess'), numeric: true },
    { id: 'p95', label: t('statsLatencyP95'), numeric: true },
    { id: 'tokens', label: t('usageColTokens'), numeric: true },
    { id: 'cache_write', label: t('usageColCacheWrite'), numeric: true },
    { id: 'credits', label: t('usageColCredits'), numeric: true },
  ]

  const statsBody = (
    <>
      <section className="flex flex-wrap items-center justify-end gap-3 pb-1">
        <StatsWindowControls
          value={statsWindow}
          onChange={setStatsWindow}
          custom={customRange}
          onCustomChange={setCustomRange}
          onBusy={() => setLoading(true)}
          t={t}
        />
      </section>

      {error ? <PageAlert title={t('failedStats', { msg: error })} /> : null}

      <section className="grid gap-3 sm:grid-cols-2 sm:gap-4 xl:grid-cols-5">
        {[
          [t('usageColRequests'), totals ? formatCompact(totals.requests) : '—', t('statsWindowHint', { window: t(STATS_WINDOW_LABEL[statsWindow]) })],
          [t('usageColSuccess'), totals ? formatPercent(totals.success_rate) : '—', totals ? `${totals.ok} / ${totals.requests}` : ''],
          [t('usageColTokens'), tokens ? formatCompact(tokens.total) : '—', tokens ? `${formatCompact(tokens.prompt)} / ${formatCompact(tokens.completion)}` : ''],
          [t('usageColCacheWrite'), tokens ? formatCompact(tokens.cache_write ?? 0) : '—', tokens ? `${t('chartCacheRead')} ${formatCompact(tokens.cache_read)}` : ''],
          [t('usageColCredits'), totals ? formatCredits(totals.credits ?? 0) : '—', ''],
        ].map(([label, value, detail]) => (
          <Card key={label} className="gap-0 p-4">
            <div className="text-body text-muted">{label}</div>
            <div className="tabular mt-1.5 text-section font-semibold">{loading ? <SkeletonBlock className="h-5 w-16" /> : value}</div>
            <div className="mono mt-1 truncate text-caption text-muted">{loading ? <SkeletonBlock className="h-3 w-24" /> : detail || '\u00a0'}</div>
          </Card>
        ))}
      </section>

      <Card className="overflow-hidden p-0">
        <div className="flex flex-col gap-3 border-b border-separator px-5 py-3 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
          <Tabs selectedKey={dim} onSelectionChange={(key) => { setLoading(true); setDim(String(key) as RequestStatsDim) }}>
            <Tabs.ListContainer className="w-fit">
              <Tabs.List aria-label={t('usageTitle')}>
                <Tab id="model" className="gap-1.5 whitespace-nowrap"><Tabs.Indicator className="transition-none" /><Cube size={14} />{t('usageByModel')}</Tab>
                <Tab id="account" className="gap-1.5 whitespace-nowrap"><Tabs.Indicator className="transition-none" /><UsersThree size={14} />{t('usageByAccount')}</Tab>
                <Tab id="provider" className="gap-1.5 whitespace-nowrap"><Tabs.Indicator className="transition-none" /><Stack size={14} />{t('usageByProvider')}</Tab>
              </Tabs.List>
            </Tabs.ListContainer>
          </Tabs>
          <span className="mono text-caption text-muted">{loading ? '' : t('usageRowCount', { count: rows.length })}</span>
        </div>
        {loading ? (
          <>
            <div className="divide-y divide-separator md:hidden" aria-busy="true">
              {Array.from({ length: 4 }, (_, index) => (
                <div key={index} className="space-y-3 px-5 py-4">
                  <SkeletonBlock className="h-4 w-40" />
                  <div className="grid grid-cols-2 gap-x-4 gap-y-3">
                    {Array.from({ length: 4 }, (_, cell) => (
                      <div key={cell} className="space-y-1.5">
                        <SkeletonBlock className="h-3 w-12" />
                        <SkeletonBlock className="h-4 w-16" />
                      </div>
                    ))}
                  </div>
                </div>
              ))}
            </div>
            <div className="hidden divide-y divide-separator md:block" aria-busy="true">
              {Array.from({ length: 6 }, (_, index) => (
                <div key={index} className="flex items-center gap-6 px-5 py-3.5">
                  <SkeletonBlock className="h-4 w-40" />
                  <div className="flex-1" />
                  {Array.from({ length: 6 }, (_, cell) => <SkeletonBlock key={cell} className="h-4 w-14" />)}
                </div>
              ))}
            </div>
          </>
        ) : rows.length === 0 ? (
          <div className="flex items-center gap-2 px-5 py-10 text-body text-muted"><ChartBar size={16} />{t('usageEmpty')}</div>
        ) : (
          <>
            <div className="divide-y divide-separator md:hidden">
              {rows.map((row) => {
                const clickable = Boolean(row.filter && stats)
                const metrics = [
                  { label: t('usageColRequests'), value: String(row.count) },
                  { label: t('usageColSuccess'), value: formatPercent(row.success), tone: row.success < 0.9 ? 'text-warning' : undefined },
                  { label: t('statsLatencyP95'), value: formatLatency(row.p95) },
                  { label: t('usageColTokens'), value: formatCompact(row.tokens), sub: `${formatCompact(row.prompt)} / ${formatCompact(row.completion)}` },
                  { label: t('usageColCacheWrite'), value: formatCompact(row.cache_write) },
                  { label: t('usageColCredits'), value: formatCredits(row.credits) },
                ]
                return (
                  <article
                    key={row.key}
                    className={`space-y-3 px-5 py-4 ${clickable ? 'cursor-pointer' : ''}`}
                    onClick={clickable ? () => navigate(logsHref(row.filter ?? {}, stats!.window.from, stats!.window.to)) : undefined}
                    onKeyDown={clickable ? (event) => {
                      if (event.key === 'Enter' || event.key === ' ') {
                        event.preventDefault()
                        navigate(logsHref(row.filter ?? {}, stats!.window.from, stats!.window.to))
                      }
                    } : undefined}
                    role={clickable ? 'button' : undefined}
                    tabIndex={clickable ? 0 : undefined}
                  >
                    <div className="flex min-w-0 items-center gap-2">
                      {dim === 'provider' && row.filter ? <ProviderMark provider={row.key} size={14} /> : null}
                      <span className="truncate text-body font-medium">{row.label}</span>
                    </div>
                    {dim === 'account' && row.label !== row.key ? <div className="mono truncate text-caption text-muted">{row.key}</div> : null}
                    <div className="grid grid-cols-2 gap-x-4 gap-y-3">
                      {metrics.map((metric) => (
                        <div key={metric.label} className="min-w-0 space-y-1">
                          <div className="text-caption text-muted">{metric.label}</div>
                          <div className={`mono text-body ${metric.tone ?? 'text-foreground'}`}>{metric.value}</div>
                          {'sub' in metric && metric.sub ? <div className="mono text-caption text-muted">{metric.sub}</div> : null}
                        </div>
                      ))}
                    </div>
                  </article>
                )
              })}
            </div>
            <Table className="usage-table hidden md:block">
              <Table.ScrollContainer className="min-w-0 overflow-x-auto">
                <Table.Content
                  aria-label={t('usageTitle')}
                  className="min-w-[48rem]"
                  sortDescriptor={{ column: sortKey, direction: sortDirection }}
                  onSortChange={(next) => {
                    setSortKey(String(next.column) as UsageSortKey)
                    setSortDirection(next.direction)
                  }}
                >
                  <Table.Header>
                    {columns.map((column) => (
                      <Table.Column key={column.id} id={column.id} allowsSorting isRowHeader={column.id === 'label'} className={column.numeric ? 'text-right' : undefined}>
                        {({ sortDirection: direction }) => (
                          <Table.SortableColumnHeader sortDirection={direction} className={column.numeric ? 'justify-end' : undefined}>
                            {column.label}
                          </Table.SortableColumnHeader>
                        )}
                      </Table.Column>
                    ))}
                  </Table.Header>
                  <Table.Body>
                    {rows.map((row) => (
                      <Table.Row
                        key={row.key}
                        id={row.key}
                        className={row.filter && stats ? 'cursor-pointer' : undefined}
                        onAction={row.filter && stats ? () => navigate(logsHref(row.filter ?? {}, stats.window.from, stats.window.to)) : undefined}
                      >
                        <Table.Cell>
                          <div className="flex min-w-0 items-center gap-2">
                            {dim === 'provider' && row.filter ? <ProviderMark provider={row.key} size={14} /> : null}
                            <span className="truncate text-body font-medium">{row.label}</span>
                            {dim === 'account' && row.label !== row.key ? <span className="mono truncate text-caption text-muted">{row.key}</span> : null}
                          </div>
                        </Table.Cell>
                        <Table.Cell className="mono text-right text-body">{row.count}</Table.Cell>
                        <Table.Cell className="mono text-right text-body">
                          <span className={row.success < 0.9 ? 'text-warning' : undefined}>{formatPercent(row.success)}</span>
                        </Table.Cell>
                        <Table.Cell className="mono text-right text-body">{formatLatency(row.p95)}</Table.Cell>
                        <Table.Cell className="text-right">
                          <div className="mono text-body">{formatCompact(row.tokens)}</div>
                          <div className="mono text-caption text-muted">{formatCompact(row.prompt)} / {formatCompact(row.completion)}</div>
                        </Table.Cell>
                        <Table.Cell className="mono text-right text-body">{formatCompact(row.cache_write)}</Table.Cell>
                        <Table.Cell className="mono text-right text-body">{formatCredits(row.credits)}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table.Content>
              </Table.ScrollContainer>
            </Table>
          </>
        )}
      </Card>

      <ExpiryDistribution accounts={accounts} t={t} />
    </>
  )

  return (
    <div className="space-y-6 sm:space-y-6">
      <section className="pb-1">
        <h2 className="text-title font-semibold">{t('usageTitle')}</h2>
        <p className="mt-1 max-w-2xl text-body leading-6 text-muted">
          {pageTab === 'cost' ? t('usageCostLead') : t('usageLead')}
        </p>
      </section>

      <Tabs selectedKey={pageTab} onSelectionChange={(key) => setPageTab(String(key) as PageTab)}>
        <Tabs.ListContainer className="w-fit">
          <Tabs.List aria-label={t('usageTitle')}>
            <Tab id="stats" className="gap-1.5 whitespace-nowrap">
              <Tabs.Indicator className="transition-none" />
              {t('usageStatsTab')}
            </Tab>
            <Tab id="cost" className="gap-1.5 whitespace-nowrap">
              <Tabs.Indicator className="transition-none" />
              {t('usageCostTab')}
            </Tab>
          </Tabs.List>
        </Tabs.ListContainer>

        <Tabs.Panel id="stats" className="space-y-6 pt-5">
          {statsBody}
        </Tabs.Panel>
        <Tabs.Panel id="cost" className="pt-5">
          <UsageCostPanel />
        </Tabs.Panel>
      </Tabs>
    </div>
  )
}
