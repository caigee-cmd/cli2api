import { lazy, Suspense, useCallback, useEffect, useState } from 'react'
import { Accordion, Button, Card, Chip, Table } from '@heroui/react'
import { ArrowClockwise } from '@phosphor-icons/react'
import { fetchUsageCost, fetchUsagePricing, type CostBreakdown, type PricingCatalog } from '@/api/usage'
import { EmptyPanel } from '@/components/ui/EmptyPanel'
import { FilterToggle } from '@/components/ui/FilterToggle'
import { PageAlert } from '@/components/ui/PageAlert'
import { SkeletonBlock } from '@/components/ui/PageSkeletons'
import type { CostChartMetric } from '@/components/usage/CostChart'
import { useI18n } from '@/hooks/useI18n'
import { formatCNY, formatCompact, formatCredits, formatUnitPrice } from '@/lib/format'

const CostChart = lazy(() => import('@/components/usage/CostChart').then((mod) => ({ default: mod.CostChart })))

type UsageWindow = 'today' | '24' | '168' | 'all'

/** Local midnight as an ISO instant — the start of the "today" calendar range. */
function todayStart() {
  const start = new Date()
  start.setHours(0, 0, 0, 0)
  return start.toISOString()
}

const EMPTY_BREAKDOWN: CostBreakdown = {
  window: { from: '', to: '', hours: 24 },
  currency: 'CNY',
  models: [],
  totals: {
    id: 'total', label: '', vendor: '', currency: 'CNY',
    requests: 0, cache_hit: 0, cache_miss: 0, output: 0, total_tokens: 0,
    cost: 0, cost_off_peak: 0, cost_peak: 0, peak_requests: 0,
    credits: 0, priced: false,
  },
  series: [],
}

function pct(part: number, whole: number) {
  if (!whole) return '0%'
  const value = (part / whole) * 100
  return `${value >= 10 ? value.toFixed(0) : value.toFixed(1)}%`
}

export function UsageCostPanel() {
  const { t, lang } = useI18n()
  const [window, setWindow] = useState<UsageWindow>('today')
  const [chartMetric, setChartMetric] = useState<CostChartMetric>('credits')
  const [breakdown, setBreakdown] = useState<CostBreakdown | null>(null)
  const [pricing, setPricing] = useState<PricingCatalog | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    void fetchUsagePricing()
      .then((data) => { if (!cancelled) setPricing(data) })
      .catch(() => { /* pricing table is optional decoration */ })
    return () => { cancelled = true }
  }, [])

  const load = useCallback(async () => {
    setLoading(true)
    try {
      // "today" is a calendar range (local midnight -> now), matching the
      // Stats tab and the overview page; the API defaults `to` to now.
      const query = window === 'all'
        ? {}
        : window === 'today'
          ? { from: todayStart() }
          : { from: new Date(Date.now() - Number(window) * 3600 * 1000).toISOString(), to: new Date().toISOString() }
      const data = await fetchUsageCost(query)
      setBreakdown(data)
      setError('')
    } catch (err) {
      setBreakdown(null)
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [window])

  useEffect(() => { void load() }, [load])

  const data = breakdown ?? EMPTY_BREAKDOWN
  const totals = data.totals
  const hasData = totals.requests > 0 && data.models.length > 0
  const pricedModels = data.models.filter((model) => model.priced)
  const hasPricedCost = totals.cost > 0

  // Primary KPIs: quota + tokens for every model. DeepSeek CNY stays out of the hero.
  const metrics = [
    { label: t('usageCredits'), value: formatCredits(totals.credits), detail: t('usageCreditsHint') },
    { label: t('usageTotalTokens'), value: formatCompact(totals.total_tokens), detail: `${formatCompact(totals.cache_hit)} / ${formatCompact(totals.cache_miss)} / ${formatCompact(totals.output)}  ${t('usageTokenSplitNote')}` },
    { label: t('usageRequests'), value: String(totals.requests), detail: t('usageModelCount', { count: data.models.length }) },
    { label: t('usageCacheHitRate'), value: pct(totals.cache_hit, totals.cache_hit + totals.cache_miss), detail: formatCompact(totals.cache_hit) },
  ]

  const chartTotalChip = chartMetric === 'credits'
    ? formatCredits(totals.credits)
    : chartMetric === 'tokens'
      ? formatCompact(totals.total_tokens)
      : formatCNY(totals.cost)

  return (
    <div className="space-y-6">
      <section className="flex flex-wrap items-center justify-end gap-3 pb-2">
        <FilterToggle
          value={window}
          onChange={(next) => setWindow(next as UsageWindow)}
          ariaLabel={t('usageWindow')}
          options={[
            { id: 'today', label: t('statsWindowToday') },
            { id: '24', label: t('usageWindow24h') },
            { id: '168', label: t('usageWindow7d') },
            { id: 'all', label: t('usageWindowTotal') },
          ]}
        />
        <Button
          size="sm"
          variant="secondary"
          onPress={() => void load()}
          isDisabled={loading}
        >
          <ArrowClockwise size={14} />{t('refresh')}
        </Button>
      </section>

      {error ? <PageAlert title={t('failedStats', { msg: error })} /> : null}

      <section className="grid gap-3 sm:grid-cols-2 sm:gap-4 xl:grid-cols-4">
        {metrics.map((metric) => (
          <Card key={metric.label} className="gap-0 p-4">
            <div className="text-body text-muted">{metric.label}</div>
            <div className="tabular mt-1.5 text-section font-semibold">
              {loading ? <SkeletonBlock className="h-5 w-16" /> : metric.value}
            </div>
            <div className="mono mt-1 truncate text-caption text-muted">
              {loading ? <SkeletonBlock className="h-3 w-24" /> : (metric.detail || ' ')}
            </div>
          </Card>
        ))}
      </section>

      <Card className="overflow-hidden p-0">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-separator px-5 py-4">
          <div>
            <h3 className="font-semibold tracking-[-0.015em]">{t('usageChartTitle')}</h3>
            <p className="mt-0.5 text-xs text-muted">{t('usageChartHint')}</p>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <FilterToggle
              value={chartMetric}
              onChange={(next) => setChartMetric(next as CostChartMetric)}
              ariaLabel={t('usageChartMetric')}
              options={[
                { id: 'credits', label: t('usageColCredits') },
                { id: 'tokens', label: t('usageColTokens') },
              ]}
            />
            <Chip size="sm" variant="soft" color="default">{chartTotalChip}</Chip>
          </div>
        </div>
        <div className="p-5">
          {loading ? (
            <div className="grid h-72 place-items-center"><SkeletonBlock className="h-56 w-full" /></div>
          ) : hasData ? (
            <Suspense fallback={<div className="grid h-72 place-items-center"><SkeletonBlock className="h-56 w-full" /></div>}>
              <CostChart
                series={data.series}
                models={data.models}
                lang={lang}
                emptyLabel={t('usageChartEmpty')}
                totalLabel={t('usageTotals')}
                metric={chartMetric}
              />
            </Suspense>
          ) : (
            <EmptyPanel title={t('usageEmpty')} hint={t('usageRetentionHint')} />
          )}
        </div>
      </Card>

      <Card className="overflow-hidden p-0">
        <div className="flex items-center justify-between gap-3 border-b border-separator px-5 py-4">
          <div>
            <h3 className="font-semibold tracking-[-0.015em]">{t('usageBreakdownTitle')}</h3>
            <p className="mt-0.5 text-xs text-muted">{t('usageBreakdownHint')}</p>
          </div>
        </div>
        {loading ? (
          <div className="space-y-3 p-5">
            {[0, 1, 2].map((row) => <SkeletonBlock key={row} className="h-9 w-full" />)}
          </div>
        ) : !hasData ? (
          <EmptyPanel title={t('usageEmpty')} hint={t('usageRetentionHint')} />
        ) : (
          <Table>
            <Table.ScrollContainer>
              <Table.Content aria-label={t('usageBreakdownTitle')}>
                <Table.Header>
                  <Table.Column isRowHeader>{t('usageColModel')}</Table.Column>
                  <Table.Column>{t('usageColRequests')}</Table.Column>
                  <Table.Column>{t('usageColInputHit')}</Table.Column>
                  <Table.Column>{t('usageColInputMiss')}</Table.Column>
                  <Table.Column>{t('usageColOutput')}</Table.Column>
                  <Table.Column>{t('usageColTokens')}</Table.Column>
                  <Table.Column>{t('usageColCredits')}</Table.Column>
                </Table.Header>
                <Table.Body>
                  {data.models.map((model) => (
                    <Table.Row key={model.id}>
                      <Table.Cell>
                        <div className="text-sm font-medium">{model.label}</div>
                        <div className="mono mt-0.5 text-[10px] text-muted">{model.id}</div>
                      </Table.Cell>
                      <Table.Cell><span className="mono text-xs">{model.requests}</span></Table.Cell>
                      <Table.Cell><span className="mono text-xs">{formatCompact(model.cache_hit)}</span></Table.Cell>
                      <Table.Cell><span className="mono text-xs">{formatCompact(model.cache_miss)}</span></Table.Cell>
                      <Table.Cell><span className="mono text-xs">{formatCompact(model.output)}</span></Table.Cell>
                      <Table.Cell><span className="mono text-xs">{formatCompact(model.total_tokens)}</span></Table.Cell>
                      <Table.Cell><span className="mono text-xs font-semibold">{formatCredits(model.credits)}</span></Table.Cell>
                    </Table.Row>
                  ))}
                  <Table.Row>
                    <Table.Cell><span className="text-sm font-semibold">{t('usageTotals')}</span></Table.Cell>
                    <Table.Cell><span className="mono text-xs font-semibold">{totals.requests}</span></Table.Cell>
                    <Table.Cell><span className="mono text-xs font-semibold">{formatCompact(totals.cache_hit)}</span></Table.Cell>
                    <Table.Cell><span className="mono text-xs font-semibold">{formatCompact(totals.cache_miss)}</span></Table.Cell>
                    <Table.Cell><span className="mono text-xs font-semibold">{formatCompact(totals.output)}</span></Table.Cell>
                    <Table.Cell><span className="mono text-xs font-semibold">{formatCompact(totals.total_tokens)}</span></Table.Cell>
                    <Table.Cell><span className="mono text-xs font-semibold">{formatCredits(totals.credits)}</span></Table.Cell>
                  </Table.Row>
                </Table.Body>
              </Table.Content>
            </Table.ScrollContainer>
          </Table>
        )}
      </Card>

      <Accordion className="rounded-xl bg-surface-secondary" hideSeparator defaultExpandedKeys={[]}>
        <Accordion.Item id="deepseek-estimate">
          <Accordion.Heading>
            <Accordion.Trigger className="items-center gap-3 px-5 py-4 text-left">
              <span className="min-w-0 flex-1">
                <span className="block font-semibold tracking-[-0.015em]">{t('usageEstimateTitle')}</span>
                <span className="mt-0.5 block text-xs text-muted">{t('usageEstimateHint')}</span>
              </span>
              {hasPricedCost ? (
                <Chip size="sm" variant="soft" color="default">{formatCNY(totals.cost)}</Chip>
              ) : null}
              <Accordion.Indicator />
            </Accordion.Trigger>
          </Accordion.Heading>
          <Accordion.Panel>
            <Accordion.Body className="space-y-5 px-5 pb-5">
              <p className="text-xs leading-6 text-muted">{t('usageRenderNote')}</p>

              {hasPricedCost ? (
                <div>
                  <div className="mb-3 flex items-center justify-between gap-3">
                    <h4 className="text-sm font-medium">{t('usageEstimateChartTitle')}</h4>
                    <Chip size="sm" variant="soft" color="default">{formatCNY(totals.cost)}</Chip>
                  </div>
                  <Suspense fallback={<SkeletonBlock className="h-56 w-full" />}>
                    <CostChart
                      series={data.series}
                      models={pricedModels.length ? pricedModels : data.models}
                      lang={lang}
                      emptyLabel={t('usageEstimateChartEmpty')}
                      totalLabel={t('usageTotals')}
                      metric="cost"
                    />
                  </Suspense>
                </div>
              ) : null}

              {pricedModels.length ? (
                <Table>
                  <Table.ScrollContainer>
                    <Table.Content aria-label={t('usageEstimateTitle')}>
                      <Table.Header>
                        <Table.Column isRowHeader>{t('usageColModel')}</Table.Column>
                        <Table.Column>{t('usageColSplit')}</Table.Column>
                        <Table.Column>{t('usageColCost')}</Table.Column>
                      </Table.Header>
                      <Table.Body>
                        {pricedModels.map((model) => (
                          <Table.Row key={model.id}>
                            <Table.Cell>
                              <div className="text-sm font-medium">{model.label}</div>
                              <div className="mono mt-0.5 text-[10px] text-muted">{model.id}</div>
                            </Table.Cell>
                            <Table.Cell>
                              <span className="mono text-xs text-muted">{formatCNY(model.cost_peak)} / {formatCNY(model.cost_off_peak)}</span>
                            </Table.Cell>
                            <Table.Cell>
                              <span className="mono text-sm font-semibold">{formatCNY(model.cost)}</span>
                            </Table.Cell>
                          </Table.Row>
                        ))}
                        <Table.Row>
                          <Table.Cell><span className="text-sm font-semibold">{t('usageTotals')}</span></Table.Cell>
                          <Table.Cell>
                            <span className="mono text-xs text-muted">{formatCNY(totals.cost_peak)} / {formatCNY(totals.cost_off_peak)}</span>
                          </Table.Cell>
                          <Table.Cell>
                            <span className="mono text-sm font-semibold">{formatCNY(totals.cost)}</span>
                          </Table.Cell>
                        </Table.Row>
                      </Table.Body>
                    </Table.Content>
                  </Table.ScrollContainer>
                </Table>
              ) : (
                <p className="text-xs text-muted">{t('usageEstimateChartEmpty')}</p>
              )}

              <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
                <div>
                  <h4 className="mb-2 text-sm font-medium">{t('usagePriceTitle')}</h4>
                  <p className="mb-3 text-xs text-muted">{t('usagePeakNote')}</p>
                  <div className="divide-y divide-separator rounded-lg bg-background">
                    {(pricing?.models ?? []).map((model) => (
                      <div key={model.id} className="px-4 py-3">
                        <div className="flex items-center justify-between gap-3">
                          <div className="text-sm font-medium">{model.label}</div>
                          <span className="mono text-[10px] text-muted">{model.currency}</span>
                        </div>
                        <div className="mono mt-1 text-[10px] text-muted">{model.id}</div>
                        <div className="mt-2 grid grid-cols-2 gap-2 text-[11px]">
                          <div className="rounded-lg bg-surface-secondary px-2.5 py-2">
                            <div className="text-[10px] text-muted">{t('usagePriceBandOff')}</div>
                            <div className="mono mt-0.5">{formatUnitPrice(model.off_peak.cache_hit)} · {formatUnitPrice(model.off_peak.cache_miss)} · {formatUnitPrice(model.off_peak.output)}</div>
                            <div className="mt-1 text-[9px] text-muted">{t('usagePriceHit')} · {t('usagePriceMiss')} · {t('usagePriceOutput')}</div>
                          </div>
                          <div className="rounded-lg bg-surface-secondary px-2.5 py-2">
                            <div className="text-[10px] text-muted">{t('usagePriceBandPeak')}</div>
                            <div className="mono mt-0.5">{formatUnitPrice(model.peak.cache_hit)} · {formatUnitPrice(model.peak.cache_miss)} · {formatUnitPrice(model.peak.output)}</div>
                            <div className="mt-1 text-[9px] text-muted">{t('usagePriceHit')} · {t('usagePriceMiss')} · {t('usagePriceOutput')}</div>
                          </div>
                        </div>
                      </div>
                    ))}
                    {!pricing?.models?.length ? (
                      <div className="px-4 py-6 text-sm text-muted">{t('usageEmpty')}</div>
                    ) : null}
                  </div>
                </div>

                <div className="space-y-3 text-xs leading-6 text-muted">
                  <h4 className="text-sm font-medium text-foreground">{t('usageNoteTitle')}</h4>
                  <p>{t('usageRetentionHint')}</p>
                  <p>{t('usagePeakNote')}</p>
                  {data.unpriced?.length ? <p>{t('usageUnpricedHint', { models: data.unpriced.join(', ') })}</p> : null}
                  {pricing?.note ? <p className="mono text-[10px]">{pricing.note}</p> : null}
                </div>
              </div>
            </Accordion.Body>
          </Accordion.Panel>
        </Accordion.Item>
      </Accordion>
    </div>
  )
}
