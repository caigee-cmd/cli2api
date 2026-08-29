import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import gsap from 'gsap'
import { Link } from 'react-router-dom'
import { Button, ButtonGroup, Card, Chip } from '@heroui/react'
import {
  ArrowSquareOut,
  ArrowUpRight,
  Copy,
  Cube,
  Pulse,
  WarningCircle,
} from '@phosphor-icons/react'
import { fetchRequestStats, type RequestStats } from '@/api/logs'
import type { Overview } from '@/api/types'
import { CountUp } from '@/components/overview/CountUp'
import { TrafficChart } from '@/components/overview/TrafficChart'
import { OverviewPageSkeleton } from '@/components/ui/PageSkeletons'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import { formatCompact, formatLatency, formatPercent } from '@/lib/format'
import { accountProviderFamilyLabel, accountProviderLabel } from '@/lib/provider'
import { absUrl } from '@/lib/url'
import { ProviderMark } from '@/components/ProviderMark'

type AccountRow = NonNullable<Overview['accounts']>[number]
type StatsWindow = 1 | 24 | 168

const EMPTY_STATS: RequestStats = {
  window: { from: '', to: '', hours: 24 },
  totals: { requests: 0, ok: 0, error: 0, canceled: 0, streaming: 0, success_rate: 0 },
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

function quotaTone(percentage?: number, exceeded?: boolean) {
  if (exceeded || (percentage ?? 0) >= 100) return 'danger'
  if ((percentage ?? 0) >= 80) return 'warning'
  return 'ok'
}

export function OverviewPage() {
  const { lang, t } = useI18n()
  const { overview, loading, error } = useOverview()
  const [hours, setHours] = useState<StatsWindow>(24)
  const [stats, setStats] = useState<RequestStats | null>(null)
  const [statsError, setStatsError] = useState('')
  const [copiedEndpoint, setCopiedEndpoint] = useState('')
  const [statsLoading, setStatsLoading] = useState(true)

  const proxyOk = Boolean(overview?.proxy?.ok)
  const workerOk = Boolean(overview?.worker?.ok)
  const accounts = overview?.accounts || []
  const readyAccounts = accounts.filter((account) => account.ready).length
  const hotAccounts = accounts.filter((account) => account.hot).length
  const coolingAccounts = accounts.filter((account) => account.down_until || account.cooldown_until).length
  const inFlight = accounts.reduce((total, account) => total + (account.in_flight ?? account.inFlight ?? 0), 0)
  const modelCount = overview?.models?.length ?? 0
  const lastError = accounts.map((account) => account.last_error || account.lastError).find(Boolean) || '—'
  const traffic = stats ?? EMPTY_STATS
  const base = absUrl(overview?.access?.openai_base_url || '/v1')

  useEffect(() => {
    let cancelled = false
    void fetchRequestStats({ hours })
      .then((data) => {
        if (cancelled) return
        setStats(data)
        setStatsError('')
      })
      .catch((err) => {
        if (cancelled) return
        setStats(null)
        setStatsError(err instanceof Error ? err.message : String(err))
      })
      .finally(() => {
        if (!cancelled) setStatsLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [hours])

  const endpoints = useMemo(() => [
    { name: t('endpointOpenAI'), url: base, method: 'BASE' },
    { name: t('endpointChat'), url: absUrl(overview?.access?.chat_completions || `${base}/chat/completions`), method: 'POST' },
    { name: t('endpointModels'), url: absUrl(overview?.access?.models || `${base}/models`), method: 'GET' },
    { name: t('endpointHealth'), url: absUrl(overview?.access?.health || '/health'), method: 'GET' },
  ], [base, overview, t])

  const metrics = [
    { label: t('metricRequests'), value: traffic.totals.requests as number | null, kind: 'compact' as const, detail: t('statsWindowHint', { window: t(hours === 1 ? 'statsWindow1h' : hours === 168 ? 'statsWindow7d' : 'statsWindow24h') }), ok: traffic.totals.requests > 0 },
    { label: t('metricSuccess'), value: traffic.totals.success_rate, kind: 'percent' as const, detail: `${traffic.totals.ok} ${t('logsFilterOk')} · ${traffic.totals.error} ${t('logsFilterError')}`, ok: traffic.totals.requests === 0 || traffic.totals.success_rate >= 0.9 },
    { label: t('metricLatency'), value: traffic.latency.p95_ms ?? traffic.latency.avg_ms, kind: 'ms' as const, detail: `p50 ${formatLatency(traffic.latency.p50_ms)} · avg ${formatLatency(traffic.latency.avg_ms)}`, ok: traffic.latency.p95_ms == null || traffic.latency.p95_ms < 8000 },
    { label: t('metricTokens'), value: traffic.tokens.total, kind: 'compact' as const, detail: `${formatCompact(traffic.tokens.prompt)} / ${formatCompact(traffic.tokens.completion)}`, ok: true },
  ]

  if (loading) return <OverviewPageSkeleton />

  return (
    <div className="space-y-6">
      <section className="grid gap-4 border-b border-[var(--app-line)] pb-4 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end">
        <div>
          <h2 data-gsap-reveal className="text-2xl font-semibold tracking-[-0.035em]">{t('homeDisplay')}</h2>
          <p className="mt-1 max-w-2xl text-sm leading-6 text-[var(--app-muted)]">{t('homeLead')}</p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <ButtonGroup className="toolbar-group">
            {([1, 24, 168] as const).map((value) => (
              <Button
                key={value}
                size="sm"
                variant={hours === value ? 'secondary' : 'ghost'}
                onPress={() => {
                  if (hours === value) return
                  setStatsLoading(true)
                  setHours(value)
                }}
              >
                {t(value === 1 ? 'statsWindow1h' : value === 168 ? 'statsWindow7d' : 'statsWindow24h')}
              </Button>
            ))}
          </ButtonGroup>
          <div className="text-left sm:text-right">
            <div className="mono text-[11px] text-[var(--app-faint)]">{overview?.time || '—'}</div>
            <div className="mt-1 inline-flex items-center gap-2 text-sm font-medium">
              <span className="status-dot" data-state={proxyOk && workerOk ? 'ok' : 'danger'} />
              {proxyOk && workerOk ? t('running') : t('degraded')}
            </div>
          </div>
        </div>
      </section>

      {error ? (
        <div className="flex items-start gap-2 rounded-lg border border-[color-mix(in_srgb,var(--app-danger)_28%,var(--app-line))] bg-[color-mix(in_srgb,var(--app-danger)_7%,var(--app-surface))] px-4 py-3 text-sm text-[var(--app-danger)]">
          <WarningCircle className="mt-0.5 shrink-0" size={17} />
          {t('failedOverview', { msg: error })}
        </div>
      ) : null}

      <section data-gsap-reveal className="grid overflow-hidden rounded-lg border border-[var(--app-line)] bg-[var(--app-surface)] sm:grid-cols-2 xl:grid-cols-4">
        {metrics.map((metric, index) => (
          <div
            key={metric.label}
            className={`min-h-32 p-5 ${index ? 'border-t border-[var(--app-line)] sm:border-l sm:border-t-0' : ''} ${index === 2 ? 'sm:border-l-0 sm:border-t xl:border-l xl:border-t-0' : ''}`}
          >
            <div className="text-xs font-medium text-[var(--app-muted)]">{metric.label}</div>
            <div className="mt-6 flex items-end justify-between gap-3">
              <div>
                <div className="mono text-2xl font-semibold tracking-[-0.035em]">
                  {statsLoading && !stats ? '—' : metric.value == null ? '—' : <CountUp value={metric.value} kind={metric.kind} />}
                </div>
                <div className="mono mt-1 text-[11px] text-[var(--app-faint)]">{metric.detail}</div>
              </div>
              <span className="status-dot" data-state={metric.ok ? 'ok' : 'danger'} />
            </div>
          </div>
        ))}
      </section>

      <section className="grid gap-5 xl:grid-cols-[minmax(0,1.35fr)_minmax(320px,.75fr)]">
        <Card data-gsap-reveal className="app-panel-flat overflow-hidden rounded-lg p-0">
          <div className="flex items-center justify-between gap-3 border-b border-[var(--app-line)] px-5 py-4">
            <div>
              <h3 className="font-semibold tracking-[-0.015em]">{t('statsTraffic')}</h3>
              <p className="mt-0.5 text-xs text-[var(--app-faint)]">{t('statsTrafficHint')}</p>
            </div>
            <Chip size="sm" variant="soft" color={traffic.totals.error ? 'warning' : 'success'}>
              {formatPercent(traffic.totals.success_rate)}
            </Chip>
          </div>
          <div className="space-y-5 p-5">
            {statsError ? (
              <div className="text-sm text-[var(--app-danger)]">{t('failedStats', { msg: statsError })}</div>
            ) : (
              <TrafficChart
                series={traffic.series}
                lang={lang}
                emptyLabel={t('statsEmptyTraffic')}
                okLabel={t('logsFilterOk')}
                errorLabel={t('logsFilterError')}
              />
            )}
            <div className="grid grid-cols-2 gap-3 border-t border-[var(--app-line)] pt-4 sm:grid-cols-4">
              {[
                [t('statsLatencyP50'), formatLatency(traffic.latency.p50_ms)],
                [t('statsLatencyP95'), formatLatency(traffic.latency.p95_ms)],
                [t('statsLatencyAvg'), formatLatency(traffic.latency.avg_ms)],
                [t('statsTTFB'), formatLatency(traffic.latency.ttfb_avg_ms)],
              ].map(([label, value]) => (
                <div key={String(label)}>
                  <div className="text-[11px] text-[var(--app-faint)]">{label}</div>
                  <div className="mono mt-1 text-sm font-medium">{value}</div>
                </div>
              ))}
            </div>
          </div>
        </Card>

        <Card data-gsap-reveal className="app-panel-flat overflow-hidden rounded-lg p-0">
          <div className="flex items-center justify-between gap-3 border-b border-[var(--app-line)] px-5 py-4">
            <div>
              <h3 className="font-semibold tracking-[-0.015em]">{t('statsPool')}</h3>
              <p className="mt-0.5 text-xs text-[var(--app-faint)]">{t('statsPoolHint')}</p>
            </div>
            <Link to="/accounts" className="inline-flex items-center gap-1 text-xs font-medium text-[var(--app-muted)] hover:text-[var(--app-ink)]">
              {t('navAccounts')}
              <ArrowUpRight size={12} />
            </Link>
          </div>
          <div className="grid grid-cols-3 divide-x divide-[var(--app-line)] border-b border-[var(--app-line)]">
            {[
              [t('ready'), readyAccounts, 'ok'],
              [t('hot'), hotAccounts, hotAccounts ? 'ok' : ''],
              [t('inFlight'), inFlight, inFlight ? 'ok' : ''],
            ].map(([label, value, state]) => (
              <div key={String(label)} className="px-4 py-3">
                <div className="text-[11px] text-[var(--app-faint)]">{label}</div>
                <div className="mt-1 flex items-center gap-2">
                  <span className="mono text-lg font-semibold">{value}</span>
                  {state ? <span className="status-dot" data-state={state} /> : null}
                </div>
              </div>
            ))}
          </div>
          <div className="divide-y divide-[var(--app-line)]">
            {accounts.length === 0 ? (
              <div className="px-5 py-8 text-sm text-[var(--app-faint)]">{t('noAccounts')}</div>
            ) : accounts.slice(0, 6).map((account) => {
              const quota = account.quota
              const tone = quotaTone(quota?.percentage, quota?.exceeded)
              return (
                <div key={account.id} className="flex items-center gap-3 px-5 py-3">
                  <span className="status-dot shrink-0" data-state={account.ready ? 'ok' : 'danger'} />
                  <div className="min-w-0 flex-1">
                    <div className="flex min-w-0 items-center gap-2">
                      <ProviderMark provider={account.provider} size={14} />
                      <div className="truncate text-sm font-medium">{account.name || account.id}</div>
                    </div>
                    <div className="mono mt-0.5 text-[10px] text-[var(--app-faint)]">
                      {accountProviderLabel(account.provider, account.region, t)}
                      {' · '}
                      {account.hot ? t('hot') : account.ready ? t('ready') : t('degraded')}
                      {quota ? ` · ${Math.round(quota.percentage ?? 0)}%` : ''}
                    </div>
                  </div>
                  {quota ? (
                    <div className="w-16 shrink-0">
                      <div className="h-1 overflow-hidden rounded-[2px] bg-[var(--app-line)]">
                        <div
                          className={`h-full ${tone === 'danger' ? 'bg-[var(--app-danger)]' : tone === 'warning' ? 'bg-[var(--warning)]' : 'bg-[var(--app-ok)]'}`}
                          style={{ width: `${Math.min(100, Math.max(0, quota.percentage ?? 0))}%` }}
                        />
                      </div>
                    </div>
                  ) : (
                    <span className="mono text-[11px] text-[var(--app-faint)]">{account.in_flight ?? account.inFlight ?? 0}</span>
                  )}
                </div>
              )
            })}
          </div>
          <div className="flex items-center justify-between border-t border-[var(--app-line)] px-5 py-3 text-[11px] text-[var(--app-faint)]">
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
      </section>

      <section className="grid gap-5 xl:grid-cols-2 2xl:grid-cols-[minmax(0,1.05fr)_minmax(0,.85fr)_minmax(0,.85fr)_minmax(280px,.75fr)]">
        <Card data-gsap-reveal className="app-panel-flat overflow-hidden rounded-lg p-0">
          <div className="flex items-center justify-between gap-3 border-b border-[var(--app-line)] px-5 py-4">
            <div>
              <h3 className="font-semibold tracking-[-0.015em]">{t('statsTopProviders')}</h3>
              <p className="mt-0.5 text-xs text-[var(--app-faint)]">{t('statsTopProvidersHint')}</p>
            </div>
            <Pulse size={15} className="text-[var(--app-faint)]" />
          </div>
          <RankList
            empty={t('statsNoProviders')}
            items={(traffic.providers || []).map((item) => ({
              key: item.key,
              label: familyLabel(item.key, t),
              count: item.count,
              meta: `${item.ok}/${item.count}`,
              mark: item.key === '(unknown)' ? undefined : item.key,
            }))}
          />
        </Card>

        <Card data-gsap-reveal className="app-panel-flat overflow-hidden rounded-lg p-0">
          <div className="flex items-center justify-between gap-3 border-b border-[var(--app-line)] px-5 py-4">
            <div>
              <h3 className="font-semibold tracking-[-0.015em]">{t('statsTopModels')}</h3>
              <p className="mt-0.5 text-xs text-[var(--app-faint)]">{t('providerCatalog')}</p>
            </div>
            <Cube size={15} className="text-[var(--app-faint)]" />
          </div>
          <RankList
            empty={t('statsNoModels')}
            items={traffic.models.map((item) => ({
              key: item.key,
              label: item.key === '(unknown)' ? t('statsUnknown') : item.key,
              count: item.count,
              meta: item.latency_avg_ms != null ? formatLatency(item.latency_avg_ms) : `${item.ok}/${item.count}`,
            }))}
          />
        </Card>

        <Card data-gsap-reveal className="app-panel-flat overflow-hidden rounded-lg p-0">
          <div className="flex items-center justify-between gap-3 border-b border-[var(--app-line)] px-5 py-4">
            <div>
              <h3 className="font-semibold tracking-[-0.015em]">{traffic.errors.length ? t('statsErrorMix') : t('statsTopAccounts')}</h3>
              <p className="mt-0.5 text-xs text-[var(--app-faint)]">{traffic.errors.length ? t('statsErrorMixHint') : t('statsPoolHint')}</p>
            </div>
            <Pulse size={15} className="text-[var(--app-faint)]" />
          </div>
          {traffic.errors.length ? (
            <RankList
              empty={t('statsNoErrors')}
              danger
              items={traffic.errors.map((item) => ({
                key: item.key,
                label: errorLabel(item.key, t),
                count: item.count,
              }))}
            />
          ) : (
            <RankList
              empty={t('statsNoAccounts')}
              items={traffic.accounts.map((item) => ({
                key: item.key,
                label: accountLabel(accounts, item.key, t('statsUnassigned')),
                count: item.count,
                meta: `${item.ok}/${item.count}`,
              }))}
            />
          )}
        </Card>

        <Card data-gsap-reveal className="app-panel-flat overflow-hidden rounded-lg p-0">
          <div className="border-b border-[var(--app-line)] px-5 py-4">
            <h3 className="font-semibold tracking-[-0.015em]">{t('endpoints')}</h3>
            <p className="mt-0.5 text-xs text-[var(--app-faint)]">{t('routesHint')}</p>
          </div>
          <div className="divide-y divide-[var(--app-line)]">
            {endpoints.map((item) => (
              <div key={item.name} className="group grid gap-2 px-5 py-3.5 sm:grid-cols-[56px_minmax(0,1fr)_auto] sm:items-center">
                <span className="mono text-[10px] font-semibold text-[var(--app-muted)]">{item.method}</span>
                <div className="min-w-0">
                  <div className="text-sm font-medium">{item.name}</div>
                  <code className="mono mt-1 block truncate text-[11px] text-[var(--app-faint)]">{item.url}</code>
                </div>
                <div className="flex gap-1 opacity-100 sm:opacity-0 sm:group-hover:opacity-100">
                  <Button isIconOnly size="sm" variant="ghost" aria-label={t('copy')} onPress={() => { void navigator.clipboard.writeText(item.url); setCopiedEndpoint(item.name); window.setTimeout(() => setCopiedEndpoint(''), 1100) }}>{copiedEndpoint === item.name ? <span className="mono text-[9px] text-[var(--app-ok)]">OK</span> : <Copy size={14} />}</Button>
                  <Button isIconOnly size="sm" variant="ghost" aria-label={t('open')} onPress={() => window.open(item.url, '_blank', 'noopener,noreferrer')}><ArrowSquareOut size={14} /></Button>
                </div>
              </div>
            ))}
          </div>
          <div className="flex items-center justify-between border-t border-[var(--app-line)] px-5 py-3 text-xs text-[var(--app-faint)]">
            <span className="truncate">{lastError === '—' ? 'Authorization: Bearer' : lastError}</span>
            <Link to="/logs" className="inline-flex items-center gap-1 hover:text-[var(--app-ink)]">{t('navLogs')}<ArrowUpRight size={12} /></Link>
          </div>
        </Card>
      </section>
    </div>
  )
}

function RankList({
  items,
  empty,
  danger,
}: {
  items: Array<{ key: string; label: string; count: number; meta?: string; mark?: string }>
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
    return <div className="px-5 py-8 text-sm text-[var(--app-faint)]">{empty}</div>
  }
  return (
    <div ref={rootRef} className="divide-y divide-[var(--app-line)]">
      {items.map((item) => (
        <div key={item.key} className="px-5 py-3">
          <div className="flex items-baseline justify-between gap-3">
            <div className="flex min-w-0 items-center gap-2">
              {item.mark ? <ProviderMark provider={item.mark} size={14} /> : null}
              <div className="truncate text-sm font-medium">{item.label}</div>
            </div>
            <div className="mono shrink-0 text-[11px] text-[var(--app-faint)]">
              {item.meta ? `${item.count} · ${item.meta}` : item.count}
            </div>
          </div>
          <div className="mt-2 h-1 overflow-hidden rounded-[2px] bg-[var(--app-line)]">
            <div
              data-rank-fill
              className={`h-full origin-left rounded-[2px] ${danger ? 'bg-[var(--app-danger)]' : 'bg-[var(--app-ok)]'}`}
              style={{ width: `${Math.max(6, (item.count / peak) * 100)}%` }}
            />
          </div>
        </div>
      ))}
    </div>
  )
}
