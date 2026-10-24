import { useMemo, useState } from 'react'
import { Card, Table } from '@heroui/react'
import type { RequestUsage, RequestUsageCreditRow, RequestUsageRow } from '@/api/logs'
import type { ModelInfo } from '@/api/types'
import { FilterToggle } from '@/components/ui/FilterToggle'
import { SkeletonBlock } from '@/components/ui/PageSkeletons'

// UsagePanels ports the reference panel's 「用量明细」 and 「积分扣除历史」 onto the
// Overview page. Both read the same /api/logs/usage payload (already scoped to the
// selected window) so no page-private fetching is required. Money/credit numbers
// are shown verbatim from upstream usage.credit — we never fabricate a value for
// requests where the upstream did not report credits.

type Translate = (key: string, vars?: Record<string, string | number>) => string
type AccountOption = { id: string; name?: string }

type Dim = 'account' | 'model' | 'realm'
type SortBy = 'total' | 'requests' | 'errors' | 'latency'
type CreditDim = 'account' | 'model'

function trimFixed(value: number, digits: number) {
  const rounded = value.toFixed(digits)
  return rounded.includes('.') ? rounded.replace(/0+$/, '').replace(/\.$/, '') : rounded
}

function fmtTokens(value: number | null | undefined) {
  const n = Number(value || 0)
  if (!Number.isFinite(n)) return '—'
  if (n >= 1e9) return `${trimFixed(n / 1e9, 2)}B`
  if (n >= 1e6) return `${trimFixed(n / 1e6, 2)}M`
  if (n >= 1e3) return `${trimFixed(n / 1e3, 1)}k`
  return String(Math.round(n))
}

function fmtCredit(value: number | null | undefined) {
  const n = Number(value || 0)
  if (!Number.isFinite(n)) return '—'
  return trimFixed(n, 2)
}

function fmtCreditRatio(value: number | null | undefined, samples: number, tokens: number) {
  if (!samples || !tokens) return '—'
  const n = Number(value || 0)
  if (!Number.isFinite(n)) return '—'
  return `${trimFixed(n, 4)} / 1M`
}

function fmtLatencyMs(value: number | null | undefined) {
  const n = Number(value || 0)
  if (!n) return '—'
  if (n >= 1000) return `${trimFixed(n / 1000, 2)}s`
  return `${Math.round(n)}ms`
}

function fmtModelRate(rate: number | null | undefined) {
  if (rate === null || rate === undefined) return '—'
  const n = Number(rate)
  if (!Number.isFinite(n) || n < 0) return '—'
  return `x${trimFixed(n, 2)}`
}

// cli2api does not store the per-model credit multiplier on request logs. When
// the ledger row lacks a rate we look it up from the regional catalog instead
// of inventing one. Catalog credits look like "x0.11 credits" (cn) or "x0.03"
// (global), so strip the optional suffix + leading "x".
function parseCreditsText(text: string | null | undefined) {
  const raw = (text || '').trim().toLowerCase().replace(/\s*credits?$/, '').replace(/^x/, '')
  if (raw === '') return null
  const n = Number(raw)
  return Number.isFinite(n) ? n : null
}

type CatalogRate = { rate: number | null; cn: boolean }

function catalogRate(models: ModelInfo[], key: string): number | null {
  if (!models.length || !key) return null
  const slash = key.indexOf('/')
  const prefix = slash >= 0 ? key.slice(0, slash).toLowerCase() : ''
  const bare = slash >= 0 ? key.slice(slash + 1) : key
  // A model can appear once per realm: deepseek-v4.1-flash is x0.00 (free) on
  // global but x0.11 on cn. The merged catalog marks the cn entry with a
  // trailing "credits" suffix, so use the row's realm prefix to pick the
  // matching entry instead of guessing.
  const matches = models.filter(
    (model) => model.id === key || model.id === bare || model.native_model === bare,
  )
  if (!matches.length) return null
  const parsed: CatalogRate[] = matches.map((model) => ({
    rate: parseCreditsText(model.credits),
    cn: /credits?\s*$/i.test((model.credits || '').trim()),
  }))
  let pool = parsed
  const wantCn = prefix.includes('cn') ? true : /(^|[-_])(gl|global|intl)([-_]|$)/.test(prefix) ? false : null
  if (wantCn !== null) {
    const scoped = parsed.filter((entry) => entry.cn === wantCn)
    if (scoped.length) pool = scoped
  }
  const rates = pool.map((entry) => entry.rate).filter((value): value is number => value !== null)
  if (!rates.length) return null
  const positive = rates.filter((value) => value > 0)
  return positive.length ? Math.max(...positive) : rates[0]
}

function cacheRate(hit: number, miss: number) {
  const h = Number(hit || 0)
  const m = Number(miss || 0)
  const total = h + m
  if (!total) return null
  return (h / total) * 100
}

function CacheCell({ hit, miss }: { hit: number; miss: number }) {
  const pct = cacheRate(hit, miss)
  if (pct == null) return <div className="mono text-right text-muted">—</div>
  const tone = pct >= 90 ? 'text-success' : pct >= 80 ? 'text-warning' : 'text-danger'
  return <div className={`mono text-right ${tone}`} title={`${fmtTokens(hit)} / ${fmtTokens(hit + miss)}`}>{trimFixed(pct, 1)}%</div>
}

function num(value: string) {
  const n = Number(value || 0)
  return Number.isFinite(n) ? n : 0
}

function sortRows(rows: RequestUsageRow[], sort: SortBy) {
  const out = rows.slice()
  const val = (row: RequestUsageRow) =>
    sort === 'requests' ? num(String(row.requests))
      : sort === 'errors' ? num(String(row.errors))
        : sort === 'latency' ? num(String(row.avg_latency_ms ?? 0))
          : num(String(row.total_tokens))
  out.sort((a, b) => val(b) - val(a))
  return out
}

function accountName(accounts: AccountOption[], id: string) {
  return accounts.find((account) => account.id === id)?.name || id.slice(0, 8)
}

export function UsageDetail({
  usage,
  accounts,
  loading,
  t,
}: {
  usage: RequestUsage | null
  accounts: AccountOption[]
  loading: boolean
  t: Translate
}) {
  const [dim, setDim] = useState<Dim>('account')
  const [sort, setSort] = useState<SortBy>('total')

  const rows = useMemo(() => {
    if (!usage) return []
    const source = dim === 'model' ? usage.by_model : dim === 'realm' ? usage.by_realm : usage.by_account
    return sortRows(source || [], sort)
  }, [usage, dim, sort])

  const withPerf = dim === 'account'
  const colCount = withPerf ? 8 : 7

  return (
    <Card data-gsap-reveal data-skeleton-swap="stats" className="overflow-hidden p-0">
      <div className="flex flex-col gap-3 border-b border-separator px-5 py-4 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
        <div className="min-w-0">
          <h3 className="font-semibold">{t('usageDetailTitle')}</h3>
          <p className="mt-0.5 text-xs text-muted">{t('usageDetailHint')}</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <FilterToggle
            value={dim}
            onChange={(next) => setDim(next as Dim)}
            ariaLabel={t('usageDetailTitle')}
            options={[
              { id: 'account', label: t('usageDimAccount') },
              { id: 'model', label: t('usageDimModel') },
              { id: 'realm', label: t('usageDimRealm') },
            ]}
          />
          <FilterToggle
            value={sort}
            onChange={(next) => setSort(next as SortBy)}
            ariaLabel={t('usageDetailHint')}
            options={[
              { id: 'total', label: t('usageSortTotal') },
              { id: 'requests', label: t('usageSortRequests') },
              { id: 'errors', label: t('usageSortErrors') },
              { id: 'latency', label: t('usageSortLatency') },
            ]}
          />
        </div>
      </div>

      {/* Mobile: stacked metric cards so label+value are not crammed into columns. */}
      <div className="divide-y divide-separator md:hidden">
        {loading ? (
          Array.from({ length: 4 }).map((_, index) => (
            <div key={`skeleton-${index}`} className="space-y-3 px-5 py-4">
              <SkeletonBlock className="h-4 w-32" />
              <div className="grid grid-cols-2 gap-x-4 gap-y-3">
                {Array.from({ length: 4 }).map((__, cell) => (
                  <div key={cell} className="space-y-1.5">
                    <SkeletonBlock className="h-3 w-12" />
                    <SkeletonBlock className="h-4 w-16" />
                  </div>
                ))}
              </div>
            </div>
          ))
        ) : rows.length === 0 ? (
          <div className="px-5 py-8 text-body text-muted">{t('usageEmpty')}</div>
        ) : (
          rows.map((row) => {
            const label =
              dim === 'account' ? accountName(accounts, row.key)
                : dim === 'realm' ? (row.key || '—')
                  : (row.key || '—')
            const sub = dim === 'account' ? row.realm : ''
            const metrics: Array<{ label: string; value: string; tone?: string }> = [
              { label: t('usageColRequests'), value: fmtTokens(row.requests) },
              { label: t('usageColErrors'), value: row.errors ? fmtTokens(row.errors) : '—', tone: row.errors ? 'text-warning' : 'text-muted' },
              { label: t('usageColPrompt'), value: fmtTokens(row.prompt_tokens), tone: 'text-muted' },
              { label: t('usageColCacheHit'), value: cacheRate(row.cache_hit_tokens, row.cache_miss_tokens) == null ? '—' : `${trimFixed(cacheRate(row.cache_hit_tokens, row.cache_miss_tokens) as number, 1)}%` },
              { label: t('usageColCompletion'), value: fmtTokens(row.completion_tokens), tone: 'text-muted' },
              { label: t('usageColTotal'), value: fmtTokens(row.total_tokens) },
            ]
            if (withPerf) metrics.push({ label: t('usageColAvgLatency'), value: fmtLatencyMs(row.avg_latency_ms), tone: 'text-muted' })
            return (
              <article key={row.key} className="space-y-3 px-5 py-4">
                <div>
                  <div className="text-body font-medium text-foreground">{label}</div>
                  {sub ? <div className="mono mt-0.5 text-caption text-muted">{sub}</div> : null}
                </div>
                <div className="grid grid-cols-2 gap-x-4 gap-y-3">
                  {metrics.map((metric) => (
                    <div key={metric.label} className="min-w-0 space-y-1">
                      <div className="text-caption text-muted">{metric.label}</div>
                      <div className={`mono text-body ${metric.tone ?? 'text-foreground'}`}>{metric.value}</div>
                    </div>
                  ))}
                </div>
              </article>
            )
          })
        )}
      </div>

      <Table aria-label={t('usageDetailTitle')} className="hidden md:block">
        <Table.ScrollContainer className="min-w-0 overflow-x-auto">
          <Table.Content className="min-w-[44rem]">
            <Table.Header>
              <Table.Column isRowHeader>
                {dim === 'model' ? t('usageColModel') : dim === 'realm' ? t('usageColRealm') : t('usageColAccount')}
              </Table.Column>
              <Table.Column><div className="text-right">{t('usageColRequests')}</div></Table.Column>
              <Table.Column><div className="text-right">{t('usageColErrors')}</div></Table.Column>
              <Table.Column><div className="text-right">{t('usageColPrompt')}</div></Table.Column>
              <Table.Column><div className="text-right">{t('usageColCacheHit')}</div></Table.Column>
              <Table.Column><div className="text-right">{t('usageColCompletion')}</div></Table.Column>
              <Table.Column><div className="text-right">{t('usageColTotal')}</div></Table.Column>
              {withPerf ? (
                <>
                  <Table.Column><div className="text-right">{t('usageColAvgLatency')}</div></Table.Column>
                </>
              ) : null}
            </Table.Header>
            <Table.Body>
              {loading ? (
                Array.from({ length: 4 }).map((_, index) => (
                  <Table.Row key={`skeleton-${index}`}>
                    <Table.Cell><SkeletonBlock className="h-4 w-32" /></Table.Cell>
                    {Array.from({ length: colCount - 1 }).map((__, cell) => (
                      <Table.Cell key={cell}><SkeletonBlock className="ml-auto h-4 w-12" /></Table.Cell>
                    ))}
                  </Table.Row>
                ))
              ) : rows.length === 0 ? (
                <Table.Row>
                  <Table.Cell><span className="text-body text-muted">{t('usageEmpty')}</span></Table.Cell>
                  {Array.from({ length: colCount - 1 }).map((__, cell) => (
                    <Table.Cell key={cell}>{null}</Table.Cell>
                  ))}
                </Table.Row>
              ) : (
                rows.map((row) => {
                  const label =
                    dim === 'account' ? accountName(accounts, row.key)
                      : dim === 'realm' ? (row.key || '—')
                        : (row.key || '—')
                  const sub = dim === 'account' ? row.realm : ''
                  return (
                    <Table.Row key={row.key}>
                      <Table.Cell>
                        <div className="text-foreground">{label}</div>
                        {sub ? <div className="mono text-caption text-muted">{sub}</div> : null}
                      </Table.Cell>
                      <Table.Cell><div className="mono text-right text-foreground">{fmtTokens(row.requests)}</div></Table.Cell>
                      <Table.Cell>
                        <div className={`mono text-right ${row.errors ? 'text-warning' : 'text-muted'}`}>
                          {row.errors ? fmtTokens(row.errors) : '—'}
                        </div>
                      </Table.Cell>
                      <Table.Cell><div className="mono text-right text-muted">{fmtTokens(row.prompt_tokens)}</div></Table.Cell>
                      <Table.Cell><CacheCell hit={row.cache_hit_tokens} miss={row.cache_miss_tokens} /></Table.Cell>
                      <Table.Cell><div className="mono text-right text-muted">{fmtTokens(row.completion_tokens)}</div></Table.Cell>
                      <Table.Cell><div className="mono text-right text-foreground">{fmtTokens(row.total_tokens)}</div></Table.Cell>
                      {withPerf ? (
                        <>
                          <Table.Cell><div className="mono text-right text-muted">{fmtLatencyMs(row.avg_latency_ms)}</div></Table.Cell>
                        </>
                      ) : null}
                    </Table.Row>
                  )
                })
              )}
            </Table.Body>
          </Table.Content>
        </Table.ScrollContainer>
      </Table>
      <p className="px-5 py-3 text-caption text-muted">{t('usageDimNote', { n: rows.length })}</p>
    </Card>
  )
}

function Kpi({ label, value, sub, tone }: { label: string; value: string; sub: string; tone?: string }) {
  return (
    <div className="rounded-lg bg-surface-tertiary px-3 py-2.5">
      <div className="text-caption text-muted">{label}</div>
      <div className={`mono mt-1 text-body font-semibold ${tone ?? 'text-foreground'}`}>{value}</div>
      <div className="mt-0.5 truncate text-caption text-muted" title={sub}>{sub}</div>
    </div>
  )
}

export function CreditHistory({
  usage,
  accounts,
  models,
  loading,
  t,
}: {
  usage: RequestUsage | null
  accounts: AccountOption[]
  models: ModelInfo[]
  loading: boolean
  t: Translate
}) {
  const [dim, setDim] = useState<CreditDim>('account')

  const totals = usage?.totals
  const rows: RequestUsageCreditRow[] = useMemo(() => {
    if (!usage) return []
    return (dim === 'model' ? usage.credit_by_model : usage.credit_by_account) || []
  }, [usage, dim])

  const withRate = dim === 'model'
  const colCount = withRate ? 7 : 6

  return (
    <Card data-gsap-reveal data-skeleton-swap="stats" className="overflow-hidden p-0">
      <div className="flex flex-col gap-3 border-b border-separator px-5 py-4 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
        <div className="min-w-0">
          <h3 className="font-semibold">{t('usageCreditTitle')}</h3>
          <p className="mt-0.5 text-xs text-muted">{t('usageCreditHint')}</p>
        </div>
        <FilterToggle
          value={dim}
          onChange={(next) => setDim(next as CreditDim)}
          ariaLabel={t('usageCreditTitle')}
          options={[
            { id: 'account', label: t('usageDimAccount') },
            { id: 'model', label: t('usageDimModel') },
          ]}
        />
      </div>

      <div className="grid grid-cols-2 gap-3 border-b border-separator px-5 py-4 sm:grid-cols-3 sm:gap-2.5 xl:grid-cols-5">
        {loading || !totals ? (
          Array.from({ length: 5 }).map((_, index) => (
            <div key={index} className="rounded-lg bg-surface-tertiary px-3 py-2.5">
              <SkeletonBlock className="h-3 w-16" />
              <SkeletonBlock className="mt-2 h-5 w-20" />
            </div>
          ))
        ) : (
          <>
            <Kpi label={t('usageCreditKpiCredits')} value={fmtCredit(totals.credits)} sub={t('usageCreditKpiCreditsSub')} />
            <Kpi label={t('usageCreditKpiTokens')} value={fmtTokens(totals.credit_tokens)} sub={t('usageCreditKpiTokensSub')} />
            <Kpi
              label={t('usageCreditKpiAvg')}
              value={fmtCreditRatio(totals.credits_per_1m_tokens, totals.credit_samples, totals.credit_tokens)}
              sub={t('usageCreditKpiAvgSub')}
              tone="text-success"
            />
            <Kpi label={t('usageCreditKpiSamples')} value={String(totals.credit_samples || 0)} sub={t('usageCreditKpiSamplesSub')} />
            <Kpi
              label={t('usageCreditKpiCache')}
              value={cacheRate(totals.cache_hit_tokens, totals.cache_miss_tokens) == null ? '—' : `${trimFixed(cacheRate(totals.cache_hit_tokens, totals.cache_miss_tokens) as number, 1)}%`}
              sub={t('usageCreditKpiCacheSub')}
            />
          </>
        )}
      </div>

      <div className="divide-y divide-separator md:hidden">
        {loading ? (
          Array.from({ length: 4 }).map((_, index) => (
            <div key={`skeleton-${index}`} className="space-y-3 px-5 py-4">
              <SkeletonBlock className="h-4 w-32" />
              <div className="grid grid-cols-2 gap-x-4 gap-y-3">
                {Array.from({ length: 4 }).map((__, cell) => (
                  <div key={cell} className="space-y-1.5">
                    <SkeletonBlock className="h-3 w-12" />
                    <SkeletonBlock className="h-4 w-16" />
                  </div>
                ))}
              </div>
            </div>
          ))
        ) : rows.length === 0 ? (
          <div className="px-5 py-8 text-body text-muted">{t('usageCreditEmpty')}</div>
        ) : (
          rows.map((row) => {
            const label = withRate ? (row.key || '—') : accountName(accounts, row.key)
            const sub = !withRate && row.realm ? row.realm : ''
            const metrics: Array<{ label: string; value: string; tone?: string }> = []
            if (withRate) metrics.push({ label: t('usageCreditColRate'), value: fmtModelRate(row.rate ?? catalogRate(models, row.key)), tone: 'text-muted' })
            metrics.push(
              { label: t('usageColRequests'), value: fmtTokens(row.requests) },
              { label: t('usageCreditColCredits'), value: fmtCredit(row.credits) },
              { label: t('usageCreditColSampleTokens'), value: fmtTokens(row.credit_tokens), tone: 'text-muted' },
              { label: t('usageCreditColPer1M'), value: fmtCreditRatio(row.credits_per_1m_tokens, row.credit_samples, row.credit_tokens), tone: 'text-muted' },
              { label: t('usageColCacheHit'), value: cacheRate(row.cache_hit_tokens, row.cache_miss_tokens) == null ? '—' : `${trimFixed(cacheRate(row.cache_hit_tokens, row.cache_miss_tokens) as number, 1)}%` },
            )
            return (
              <article key={row.key} className="space-y-3 px-5 py-4">
                <div>
                  <div className="text-body font-medium text-foreground">{label}</div>
                  {sub ? <div className="mono mt-0.5 text-caption text-muted">{sub}</div> : null}
                </div>
                <div className="grid grid-cols-2 gap-x-4 gap-y-3">
                  {metrics.map((metric) => (
                    <div key={metric.label} className="min-w-0 space-y-1">
                      <div className="text-caption text-muted">{metric.label}</div>
                      <div className={`mono text-body ${metric.tone ?? 'text-foreground'}`}>{metric.value}</div>
                    </div>
                  ))}
                </div>
              </article>
            )
          })
        )}
      </div>

      <Table aria-label={t('usageCreditTitle')} className="hidden md:block">
        <Table.ScrollContainer className="min-w-0 overflow-x-auto">
          <Table.Content className="min-w-[40rem]">
            <Table.Header>
              <Table.Column isRowHeader>{withRate ? t('usageColModel') : t('usageColAccount')}</Table.Column>
              {withRate ? <Table.Column><div className="text-right">{t('usageCreditColRate')}</div></Table.Column> : null}
              <Table.Column><div className="text-right">{t('usageColRequests')}</div></Table.Column>
              <Table.Column><div className="text-right">{t('usageCreditColCredits')}</div></Table.Column>
              <Table.Column><div className="text-right">{t('usageCreditColSampleTokens')}</div></Table.Column>
              <Table.Column><div className="text-right">{t('usageCreditColPer1M')}</div></Table.Column>
              <Table.Column><div className="text-right">{t('usageColCacheHit')}</div></Table.Column>
            </Table.Header>
            <Table.Body>
              {loading ? (
                Array.from({ length: 4 }).map((_, index) => (
                  <Table.Row key={`skeleton-${index}`}>
                    <Table.Cell><SkeletonBlock className="h-4 w-32" /></Table.Cell>
                    {Array.from({ length: colCount - 1 }).map((__, cell) => (
                      <Table.Cell key={cell}><SkeletonBlock className="ml-auto h-4 w-12" /></Table.Cell>
                    ))}
                  </Table.Row>
                ))
              ) : rows.length === 0 ? (
                <Table.Row>
                  <Table.Cell><span className="text-body text-muted">{t('usageCreditEmpty')}</span></Table.Cell>
                  {Array.from({ length: colCount - 1 }).map((__, cell) => (
                    <Table.Cell key={cell}>{null}</Table.Cell>
                  ))}
                </Table.Row>
              ) : (
                rows.map((row) => {
                  const label = withRate ? (row.key || '—') : accountName(accounts, row.key)
                  const sub = !withRate && row.realm ? row.realm : ''
                  return (
                    <Table.Row key={row.key}>
                      <Table.Cell>
                        <div className="text-foreground">{label}</div>
                        {sub ? <div className="mono text-caption text-muted">{sub}</div> : null}
                      </Table.Cell>
                      {withRate ? <Table.Cell><div className="mono text-right text-muted">{fmtModelRate(row.rate ?? catalogRate(models, row.key))}</div></Table.Cell> : null}
                      <Table.Cell><div className="mono text-right text-foreground">{fmtTokens(row.requests)}</div></Table.Cell>
                      <Table.Cell><div className="mono text-right text-foreground">{fmtCredit(row.credits)}</div></Table.Cell>
                      <Table.Cell><div className="mono text-right text-muted">{fmtTokens(row.credit_tokens)}</div></Table.Cell>
                      <Table.Cell><div className="mono text-right text-muted">{fmtCreditRatio(row.credits_per_1m_tokens, row.credit_samples, row.credit_tokens)}</div></Table.Cell>
                      <Table.Cell><CacheCell hit={row.cache_hit_tokens} miss={row.cache_miss_tokens} /></Table.Cell>
                    </Table.Row>
                  )
                })
              )}
            </Table.Body>
          </Table.Content>
        </Table.ScrollContainer>
      </Table>
      <p className="px-5 py-3 text-caption text-muted">
        {t('usageCreditNote', { accounts: usage?.credit_by_account?.length ?? 0, models: usage?.credit_by_model?.length ?? 0 })}
      </p>
    </Card>
  )
}
