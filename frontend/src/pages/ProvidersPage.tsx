import { useEffect, useMemo, useState } from 'react'
import { Button, Card, Chip, Table, Tooltip } from '@heroui/react'
import { Cube, ArrowClockwise, MagnifyingGlass, Info, Lightning } from '@phosphor-icons/react'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import { checkModels, fetchModelsCached, fetchProviders, refreshModels, type ModelProbeResponse, type ModelProbeResult, type ProviderDescriptor } from '@/api/overview'
import type { Overview } from '@/api/types'
import { ProviderMark } from '@/components/ProviderMark'
import { ModelDetailsModal } from '@/components/ModelDetailsModal'
import { EmptyPanel } from '@/components/ui/EmptyPanel'
import { FilterSelect } from '@/components/ui/FilterSelect'
import { ListPager, type PageSize } from '@/components/ui/ListPager'
import { PageAlert } from '@/components/ui/PageAlert'
import { ProvidersPageSkeleton, ProvidersTableSkeleton } from '@/components/ui/PageSkeletons'
import { SearchBar } from '@/components/ui/SearchBar'
import { modelCreditsText, modelIsFree } from '@/lib/format'
import { accountProviderLabel } from '@/lib/provider'

type ModelInfo = NonNullable<Overview['models']>[number]

type ProviderRegionOption = {
  value: string
  provider: string
  region: string
}

function providerRegionOptions(descriptors: ProviderDescriptor[]): ProviderRegionOption[] {
  const options: ProviderRegionOption[] = []
  for (const descriptor of descriptors) {
    for (const region of descriptor.regions || []) {
      if (!region?.id) continue
      options.push({ value: `${descriptor.id}:${region.id}`, provider: descriptor.id, region: region.id })
    }
  }
  return options
}

function modelProvider(model: ModelInfo) {
  // Account family wins over upstream owned_by (which may be a model vendor).
  return String(model.provider || model.owned_by || 'qoder').trim().toLowerCase()
}

function modelRegion(model: ModelInfo) {
  const region = String(model.region || '').trim().toLowerCase()
  if (region) return region
  const regions = model.regions || []
  return String(regions[0] || '').trim().toLowerCase()
}

function modelRowKey(model: ModelInfo) {
  return `${modelProvider(model)}:${modelRegion(model) || 'any'}:${model.settings_key || model.id}:${model.native_model || model.mapped_key || ''}`
}

function routedModelName(model: ModelInfo) {
  const routeName = model.route_display_name || ''
  return routeName && routeName !== (model.display_name || model.id) ? routeName : ''
}

type Translate = (key: string, vars?: Record<string, string | number>) => string

function ModelActions({
  model,
  t,
  onDetails,
}: {
  model: ModelInfo
  t: Translate
  onDetails: (model: ModelInfo) => void
}) {
  return (
    <Button size="sm" variant="ghost" onPress={() => onDetails(model)}>
      <Info size={14} />{t('modelDetails')}
    </Button>
  )
}

export function ProvidersPage() {
  const { t } = useI18n()
  const { overview, loading } = useOverview()
  const [filter, setFilter] = useState('')
  const [providerFilter, setProviderFilter] = useState('')
  const [providerOptions, setProviderOptions] = useState<ProviderRegionOption[]>([])
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState<PageSize>(50)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const [messageError, setMessageError] = useState(false)
  const [detailModel, setDetailModel] = useState<ModelInfo | null>(null)
  const [models, setModels] = useState<ModelInfo[]>([])
  const [modelsLoading, setModelsLoading] = useState(true)
  const [checking, setChecking] = useState(false)
  const [probeResult, setProbeResult] = useState<ModelProbeResponse | null>(null)
  useEffect(() => {
    let cancelled = false
    void fetchModelsCached(undefined, 'regional')
      .then((data) => { if (!cancelled) setModels(data.data || []) })
      .catch(() => undefined)
      .finally(() => { if (!cancelled) setModelsLoading(false) })
    void fetchProviders()
      .then((result) => {
        if (!cancelled) setProviderOptions(providerRegionOptions(result.data || []))
      })
      .catch(() => undefined)
    return () => { cancelled = true }
  }, [])

  const filtered = useMemo(() => {
    const query = filter.trim().toLowerCase()
    const [filterProvider, filterRegion] = providerFilter.includes(':')
      ? providerFilter.split(':', 2)
      : [providerFilter, '']
    return models.filter((model) => {
      const provider = modelProvider(model)
      const region = modelRegion(model)
      if (filterProvider && provider !== filterProvider) return false
      if (filterRegion && region !== filterRegion) return false
      if (!query) return true
      const providerLabel = accountProviderLabel(provider, region || undefined, t)
      return `${model.display_name || ''} ${model.id} ${model.mapped_key || ''} ${provider} ${providerLabel} ${model.owned_by || ''} ${region} ${model.credits || ''} ${model.free ? 'free' : ''}`.toLowerCase().includes(query)
    })
  }, [filter, models, providerFilter, t])

  const filterKey = [filter, providerFilter, pageSize].join('\0')
  const [appliedFilterKey, setAppliedFilterKey] = useState(filterKey)
  if (appliedFilterKey !== filterKey) {
    setAppliedFilterKey(filterKey)
    setPage(1)
  }
  const pageCount = Math.max(1, Math.ceil(filtered.length / pageSize))
  const currentPage = Math.min(appliedFilterKey !== filterKey ? 1 : Math.max(1, page), pageCount)
  if (page !== currentPage) {
    setPage(currentPage)
  }
  const paged = useMemo(() => {
    const start = (currentPage - 1) * pageSize
    return filtered.slice(start, start + pageSize)
  }, [currentPage, filtered, pageSize])
  const shownFrom = filtered.length === 0 ? 0 : (currentPage - 1) * pageSize + 1
  const shownTo = Math.min(filtered.length, currentPage * pageSize)
  const shownLabel = filtered.length
    ? t('logsShownTotal', { shown: `${shownFrom}–${shownTo}`, total: filtered.length })
    : t('shownTotal', { shown: 0, total: models.length })

  if ((loading && !overview) || modelsLoading) return <ProvidersPageSkeleton />


  async function onRefresh() {
    setBusy(true)
    setMessage('')
    setMessageError(false)
    try {
      const data = await refreshModels(undefined, 'regional')
      setModels(data.data || [])
    } catch (error) {
      setMessageError(true)
      setMessage(error instanceof Error ? error.message : String(error))
    } finally {
      setBusy(false)
    }
  }


  function modelCheckResult(model: ModelInfo): ModelProbeResult | undefined {
    return probeResult?.results.find((result) => result.model === model.id)
  }

  function modelCheckChip(model: ModelInfo) {
    const checked = modelCheckResult(model)
    if (!checked) {
      return <Chip size="sm" variant="soft" color="default">{t('checkNotRun')}</Chip>
    }
    const accounts = checked.accounts || []
    const passed = accounts.length ? accounts.filter((account) => account.ok).length : (checked.ok ? 1 : 0)
    const total = accounts.length || checked.attempt_count || (checked.ok ? 1 : 0)
    return (
      <Chip size="sm" variant="soft" color={checked.ok ? 'success' : 'danger'}>
        {checked.ok ? t('checkResultOK') : t('checkResultFailed')}
        {total ? ` · ${t('checkPassedAccounts', { passed, total })}` : ''}
      </Chip>
    )
  }

  async function onCheckAllModels() {
    if (!models.length) {
      setMessage(t('checkModelsEmpty'))
      setMessageError(false)
      return
    }
    setChecking(true)
    setProbeResult(null)
    setMessage('')
    setMessageError(false)
    try {
      const result = await checkModels(models.map((model) => model.id))
      setProbeResult(result)
      setMessage(t('checkModelsSummary', {
        passed: result.summary.passed,
        failed: result.summary.failed,
        total: result.summary.total,
        elapsed: result.elapsed_ms,
      }))
    } catch (error) {
      setMessageError(true)
      setMessage(error instanceof Error ? error.message : String(error))
    } finally {
      setChecking(false)
    }
  }

  return (
    <div className="space-y-6">
      <section className="grid gap-5 pb-6 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end">
        <div data-gsap-reveal>
          <h2 className="text-title font-semibold">{t('availableModels')}</h2>
          <p className="mt-1 text-body text-muted">
            {models.length ? shownLabel : t('noModelsYet')}
          </p>
        </div>
        <div data-gsap-reveal className="flex min-w-0 flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center">
          <SearchBar className="w-full min-w-0 sm:w-72" value={filter} onChange={setFilter} placeholder={t('filterPh')} ariaLabel={t('filter')} />
          <div className="flex min-w-0 items-center gap-2">
            <FilterSelect
              ariaLabel={t('providerCol')}
              value={providerFilter}
              onChange={setProviderFilter}
              options={[
                { id: '', label: t('providerFilterAll') },
                ...providerOptions.map((option) => ({
                  id: option.value,
                  label: accountProviderLabel(option.provider, option.region, t),
                })),
              ]}
            />
            <Button size="sm" variant="secondary" isPending={busy || checking} onPress={() => void onRefresh()}>
              <ArrowClockwise size={14} />
              {busy ? t('refreshing') : t('refresh')}
            </Button>
            <Tooltip>
              <Tooltip.Trigger>
                <Button size="sm" variant="secondary" isPending={checking} isDisabled={busy || models.length === 0} onPress={() => void onCheckAllModels()}>
                  <Lightning size={14} />
                  {checking ? t('checkingModels') : t('checkAllModels')}
                </Button>
              </Tooltip.Trigger>
              <Tooltip.Content><p className="max-w-xs text-caption leading-5">{t('checkModelsHint')}</p></Tooltip.Content>
            </Tooltip>
          </div>
        </div>
      </section>

      {message ? <PageAlert status={messageError ? 'danger' : 'success'} title={message} /> : null}

      {probeResult ? (
        <Card data-gsap-reveal className="overflow-hidden p-0">
          <div className="flex items-start justify-between gap-4 border-b border-separator px-4 py-4 sm:items-center sm:px-5">
            <div className="min-w-0">
              <div className="text-body font-semibold">{t('checkAllModels')}</div>
              <div className="mt-0.5 text-caption leading-5 text-muted">
                {t('checkModelsSummary', {
                  passed: probeResult.summary.passed,
                  failed: probeResult.summary.failed,
                  total: probeResult.summary.total,
                  elapsed: probeResult.elapsed_ms,
                })}
              </div>
            </div>
            <Chip size="sm" variant="soft" color={probeResult.summary.failed ? 'warning' : 'success'} className="shrink-0">
              {probeResult.summary.passed}/{probeResult.summary.total}
            </Chip>
          </div>
          <Table className="hidden min-w-0 lg:block">
            <Table.ScrollContainer className="max-h-[24rem] min-w-0 overflow-auto">
              <Table.Content aria-label={t('checkAllModels')} className="min-w-[52rem]">
                <Table.Header>
                  <Table.Column isRowHeader>{t('modelCol')}</Table.Column>
                  <Table.Column>{t('stateCol')}</Table.Column>
                  <Table.Column>{t('account')}</Table.Column>
                  <Table.Column>{t('providerCol')}</Table.Column>
                  <Table.Column>{t('logsColLatency')}</Table.Column>
                  <Table.Column>{t('checkAccountResults')}</Table.Column>
                  <Table.Column>{t('response')}</Table.Column>
                </Table.Header>
                <Table.Body>
                  {probeResult.results.map((result) => (
                    <Table.Row key={result.model}>
                      <Table.Cell><span className="mono text-caption font-medium">{result.model}</span></Table.Cell>
                      <Table.Cell>
                        <Chip size="sm" variant="soft" color={result.ok ? 'success' : result.code === 'no_models' || result.message === 'model required' ? 'warning' : 'danger'}>
                          {result.ok
                            ? t('checkResultOK')
                            : result.code === 'no_models' || result.message === 'model required'
                              ? t('checkResultSkipped')
                              : t('checkResultFailed')}
                        </Chip>
                      </Table.Cell>
                      <Table.Cell><span className="mono text-caption text-muted">{result.account_id || '—'}</span></Table.Cell>
                      <Table.Cell><span className="text-caption">{result.provider || '—'}</span></Table.Cell>
                      <Table.Cell><span className="mono text-caption text-muted">{result.latency_ms}ms</span></Table.Cell>
                      <Table.Cell>
                        <div className="space-y-1">
                          {(result.accounts || []).length ? result.accounts!.map((account) => (
                            <div key={`${result.model}:${account.account_id}`} className="text-caption leading-4">
                              <span className="mono font-medium">{account.account_id}</span>
                              <span className={account.ok ? 'text-success' : 'text-danger'}>
                                {' '}{account.ok ? t('checkResultOK') : t('checkResultFailed')}
                              </span>
                              <span className="text-muted"> · {account.latency_ms}ms</span>
                              {account.message ? <span className="block break-all text-muted">{account.message}</span> : null}
                            </div>
                          )) : <span className="text-caption text-muted">—</span>}
                        </div>
                      </Table.Cell>
                      <Table.Cell><span className="break-all text-caption text-muted">{result.message || (result.ok ? t('checkResultOK') : '')}</span></Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table.Content>
            </Table.ScrollContainer>
          </Table>
          <div className="divide-y divide-separator lg:hidden">
            {probeResult.results.map((result) => (
              <div key={result.model} className="space-y-1 px-4 py-3">
                <div className="flex items-center justify-between gap-3">
                  <span className="mono min-w-0 truncate text-caption font-medium">{result.model}</span>
                  <Chip size="sm" variant="soft" color={result.ok ? 'success' : 'danger'}>
                    {result.ok ? t('checkResultOK') : t('checkResultFailed')}
                  </Chip>
                </div>
                <div className="text-caption text-muted">{result.account_id || result.provider || '—'} · {result.latency_ms}ms</div>
                {(result.accounts || []).length ? (
                  <div className="space-y-0.5">
                    {result.accounts!.map((account) => (
                      <div key={`${result.model}:${account.account_id}`} className="text-caption leading-4">
                        <span className="mono font-medium">{account.account_id}</span>
                        <span className={account.ok ? 'text-success' : 'text-danger'}>
                          {' '}{account.ok ? t('checkResultOK') : t('checkResultFailed')}
                        </span>
                        <span className="text-muted"> · {account.latency_ms}ms</span>
                        {account.message ? <span className="block break-all text-muted">{account.message}</span> : null}
                      </div>
                    ))}
                  </div>
                ) : null}
                {result.message ? <div className="break-all text-caption text-muted">{result.message}</div> : null}
              </div>
            ))}
          </div>
        </Card>
      ) : null}

      <Card data-gsap-reveal className="overflow-hidden p-0">
        <div className="flex items-start justify-between gap-4 border-b border-separator px-4 py-4 sm:items-center sm:px-5">
          <div className="flex min-w-0 items-center gap-3">
            <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-surface-secondary text-muted"><Cube size={16} /></div>
            <div className="min-w-0">
              <div className="text-body font-semibold">{t('providerCatalog')}</div>
              <div className="mt-0.5 text-caption leading-5 text-muted">{t('contextConfigHint')}</div>
            </div>
          </div>
          <Chip size="sm" variant="soft" className="shrink-0">{filtered.length}</Chip>
        </div>

        {busy || loading ? (
          <ProvidersTableSkeleton />
        ) : filtered.length === 0 ? (
          <EmptyPanel
            icon={<MagnifyingGlass size={22} />}
            title={models.length ? t('noModelsMatch') : t('noProviders')}
            hint={models.length ? (filter || providerFilter || t('noModelsMatch')) : t('noModelsYet')}
          />
        ) : (
          <>
            <div className="divide-y divide-separator lg:hidden">
              {paged.map((model) => {
                const provider = modelProvider(model)
                const region = modelRegion(model)
                const providerLabel = accountProviderLabel(provider, region || undefined, t)
                const credits = modelCreditsText(model)
                const free = modelIsFree(model)
                return (
                  <article key={modelRowKey(model)} className="space-y-3 px-4 py-4">
                    <div className="flex items-start justify-between gap-3">
                      <div className="flex min-w-0 items-start gap-3">
                        <span className="status-dot mt-1.5" data-state={model.stale ? undefined : 'ok'} />
                        <div className="min-w-0">
                          <div className="flex flex-wrap items-center gap-2">
                            <div className="truncate font-medium">{model.display_name || model.id}</div>
                            {free ? <Chip size="sm" variant="soft" color="success">{t('modelFree')}</Chip> : null}
                            {credits ? <span className="mono text-caption text-muted">{credits}</span> : null}
                          </div>
                          <div className="mono mt-0.5 truncate text-caption text-muted">{model.id}</div>
                          {routedModelName(model) ? <div className="mt-0.5 text-caption text-muted">{t('routedTo', { model: routedModelName(model) })}</div> : null}
                        </div>
                      </div>
                      {modelCheckChip(model)}
                    </div>
                    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-caption">
                      <span className="inline-flex items-center gap-1.5">
                        <ProviderMark provider={provider} size={14} />
                        <span className="font-medium">{providerLabel}</span>
                      </span>
                      <span className="mono break-all text-muted">{model.mapped_key || model.native_model || model.id}</span>
                    </div>
                    <ModelActions
                      model={model}
                      t={t}
                      onDetails={setDetailModel}
                    />
                  </article>
                )
              })}
            </div>
            <Table className="hidden min-w-0 lg:block">
              <Table.ScrollContainer className="min-w-0 overflow-x-auto">
                <Table.Content aria-label={t('availableModels')} className="min-w-[68rem]">
                  <Table.Header>
                    <Table.Column isRowHeader>{t('modelCol')}</Table.Column>
                    <Table.Column>{t('requestIdCol')}</Table.Column>
                    <Table.Column>{t('providerCol')}</Table.Column>
                    <Table.Column>{t('qoderKeyCol')}</Table.Column>
                    <Table.Column>{t('stateCol')}</Table.Column>
                    <Table.Column>{t('modelDetailsCol')}</Table.Column>
                  </Table.Header>
                  <Table.Body>
                    {paged.map((model) => {
                      const provider = modelProvider(model)
                      const region = modelRegion(model)
                      const providerLabel = accountProviderLabel(provider, region || undefined, t)
                      const credits = modelCreditsText(model)
                      const free = modelIsFree(model)
                      return (
                        <Table.Row key={modelRowKey(model)}>
                          <Table.Cell>
                            <div className="flex items-center gap-3 py-1">
                              <span className="status-dot" data-state={model.stale ? undefined : 'ok'} />
                              <div className="min-w-0">
                                <div className="flex flex-wrap items-center gap-2">
                                  <div className="font-medium">{model.display_name || model.id}</div>
                                  {free ? <Chip size="sm" variant="soft" color="success">{t('modelFree')}</Chip> : null}
                                  {credits ? <span className="mono text-caption text-muted">{credits}</span> : null}
                                </div>
                                {routedModelName(model) ? <div className="mt-0.5 text-caption text-muted">{t('routedTo', { model: routedModelName(model) })}</div> : null}
                              </div>
                            </div>
                          </Table.Cell>
                          <Table.Cell><span className="mono text-caption font-medium">{model.id}</span></Table.Cell>
                          <Table.Cell>
                            <div className="flex items-center gap-2">
                              <ProviderMark provider={provider} size={14} />
                              <span className="text-caption font-medium">{providerLabel}</span>
                            </div>
                          </Table.Cell>
                          <Table.Cell><span className="mono text-caption text-muted">{model.mapped_key || model.native_model || model.id}</span></Table.Cell>
                          <Table.Cell>{modelCheckChip(model)}</Table.Cell>
                          <Table.Cell>
                            <ModelActions
                              model={model}
                              t={t}
                              onDetails={setDetailModel}
                            />
                          </Table.Cell>
                        </Table.Row>
                      )
                    })}
                  </Table.Body>
                </Table.Content>
              </Table.ScrollContainer>
            </Table>
          </>
        )}
      </Card>

      <ModelDetailsModal
        model={detailModel ? models.find((m) => modelRowKey(m) === modelRowKey(detailModel)) || detailModel : null}
        t={t}
        onClose={() => setDetailModel(null)}
      />
      <ListPager
        total={filtered.length}
        page={currentPage}
        pageCount={pageCount}
        pageSize={pageSize}
        loading={busy}
        pageSizeLabel={t('logsPageSize')}
        pageLabel={t('logsPage', { page: currentPage, pages: pageCount })}
        prevLabel={t('logsPrevPage')}
        nextLabel={t('logsNextPage')}
        onPage={setPage}
        onPageSize={setPageSize}
      />
    </div>
  )
}
