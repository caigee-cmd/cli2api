import { useEffect, useMemo, useState } from 'react'
import { Button, Card, Chip, Table, Tooltip } from '@heroui/react'
import { Cube, ArrowClockwise, MagnifyingGlass, Info } from '@phosphor-icons/react'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import { fetchModelsCached, fetchProviders, refreshModels, updateProviderMaxMode, updateProviderReasoning, type ProviderDescriptor } from '@/api/overview'
import type { Overview } from '@/api/types'
import { ProviderMark } from '@/components/ProviderMark'
import { ModelDetailsModal, formatTokens } from '@/components/ModelDetailsModal'
import { CompactSwitch } from '@/components/ui/CompactSwitch'
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

function modelSettingsKey(model: ModelInfo) {
  return model.settings_key || model.id
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

function reasoningLabel(t: (key: string, vars?: Record<string, string | number>) => string, level: string) {
  const key = `reasoningLevel_${level}`
  const label = t(key)
  return label === key ? level : label
}

type Translate = (key: string, vars?: Record<string, string | number>) => string

function HintLabel({ label, hint }: { label: string; hint: string }) {
  return (
    <Tooltip>
      <Tooltip.Trigger>
        <span className="cursor-help text-[11px] text-muted">{label}</span>
      </Tooltip.Trigger>
      <Tooltip.Content>
        <p className="max-w-xs text-xs leading-5">{hint}</p>
      </Tooltip.Content>
    </Tooltip>
  )
}

function ModelContextControls({
  model,
  saving,
  t,
  onToggleTraeMax,
  onReasoningChange,
}: {
  model: ModelInfo
  saving: boolean
  t: Translate
  onToggleTraeMax: (model: ModelInfo, selected: boolean) => void
  onReasoningChange: (model: ModelInfo, next: string) => void
}) {
  const provider = modelProvider(model)
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-3">
      <span className="mono text-xs text-muted">
        {formatTokens(model.catalog_context_length || model.context_length)}
        {model.catalog_context_length_max && model.catalog_context_length_max !== (model.catalog_context_length || model.context_length)
          ? ` → ${formatTokens(model.catalog_context_length_max)}`
          : ''}
      </span>
      {provider === 'workbuddy' && model.catalog_context_length_max && model.catalog_context_length_max !== (model.catalog_context_length || model.context_length) ? (
        <HintLabel label={t('catalogWindow')} hint={t('workbuddyContextHint')} />
      ) : null}
      {(provider === 'trae' || provider === 'qoder') && model.supports_max_mode ? (
        <div className="flex items-center gap-2">
          <CompactSwitch
            isSelected={Boolean(model.max_mode)}
            isDisabled={saving}
            ariaLabel={`${model.id} ${t('maxMode')}`}
            onChange={(selected) => onToggleTraeMax(model, selected)}
          />
          <HintLabel
            label={`${t('maxMode')}${model.catalog_context_length_max ? ` ${formatTokens(model.catalog_context_length_max)}` : ''}`}
            hint={provider === 'qoder' ? t('qoderMaxModeHint') : t('maxModeHint')}
          />
        </div>
      ) : null}
      {(model.reasoning_options || []).length > 1 ? (
        <div className="flex min-w-0 items-center gap-2">
          <HintLabel label={t('reasoningLevels')} hint={t('reasoningHint')} />
          <FilterSelect
            className="min-w-28"
            ariaLabel={`${model.id} ${t('reasoningLevels')}`}
            value={model.reasoning_effort || model.reasoning_default || model.reasoning_options?.[0] || ''}
            onChange={(next) => { if (next) onReasoningChange(model, next) }}
            options={(model.reasoning_options || []).map((level) => ({ id: level, label: reasoningLabel(t, level) }))}
          />
        </div>
      ) : (model.reasoning_options || []).length === 1 ? (
        <HintLabel label={`${t('reasoningLevels')} ${reasoningLabel(t, model.reasoning_options![0])}`} hint={t('reasoningHint')} />
      ) : model.reasoning_type ? (
        <span className="text-[11px] text-muted">{t('reasoningFixed', { type: model.reasoning_type })}</span>
      ) : provider === 'trae' && !model.supports_max_mode ? (
        <HintLabel label={t('catalogWindow')} hint={t('catalogWindowHint')} />
      ) : null}
    </div>
  )
}

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
  const [savingKey, setSavingKey] = useState('')
  const [message, setMessage] = useState('')
  const [messageError, setMessageError] = useState(false)
  const [detailModel, setDetailModel] = useState<ModelInfo | null>(null)
  const [models, setModels] = useState<ModelInfo[]>([])
  const [modelsLoading, setModelsLoading] = useState(true)
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


  function updateTraeInOverview(model: ModelInfo, maxMode: boolean) {
    const key = modelSettingsKey(model)
    const provider = modelProvider(model)
    const nextModels = models.map((item) => {
      if (modelSettingsKey(item) !== key || modelProvider(item) !== provider) return item
      const effort = item.reasoning_effort || item.reasoning_default || ''
      const itemDev = item.catalog_context_length || item.default_context_length || item.context_length || 0
      const itemMax = item.catalog_context_length_max || 0
      return {
        ...item,
        max_mode: maxMode,
        // Only the toggle and its window change here. The prompt/output ceilings
        // stay at their catalog defaults; the views pick the Max tier from
        // *_max at render time, so turning max mode off restores the default.
        context_length: maxMode && itemMax ? itemMax : itemDev,
        default_context_length: itemDev,
        context_custom: provider === 'qoder' ? maxMode : maxMode || Boolean(effort && effort !== item.reasoning_default),
      }
    })
    setModels(nextModels)
  }

  function updateReasoningInOverview(model: ModelInfo, effort: string) {
    const key = modelSettingsKey(model)
    const provider = modelProvider(model)
    const nextModels = models.map((item) => {
      if (modelSettingsKey(item) !== key || modelProvider(item) !== provider) return item
      return {
        ...item,
        reasoning_effort: effort,
        context_custom: Boolean(item.max_mode) || Boolean(effort && effort !== item.reasoning_default),
      }
    })
    setModels(nextModels)
  }

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


  async function onReasoningChange(model: ModelInfo, effort: string) {
    const provider = modelProvider(model)
    if (provider !== 'trae' && provider !== 'workbuddy') return
    const key = modelSettingsKey(model)
    setSavingKey(key)
    setMessage('')
    setMessageError(false)
    try {
      await updateProviderReasoning(provider, key, effort)
      updateReasoningInOverview(model, effort)
      setMessage(t('reasoningSaved', { model: model.id, level: reasoningLabel(t, effort) }))
    } catch (error) {
      setMessageError(true)
      setMessage(error instanceof Error ? error.message : String(error))
    } finally {
      setSavingKey('')
    }
  }

  async function onToggleTraeMax(model: ModelInfo, maxMode: boolean) {
    if (!model.supports_max_mode) {
      setMessageError(true)
      setMessage(t('contextMaxUnavailable'))
      return
    }
    const key = modelSettingsKey(model)
    const provider = modelProvider(model)
    setSavingKey(key)
    setMessage('')
    setMessageError(false)
    try {
      await updateProviderMaxMode(provider, key, maxMode, model.catalog_context_length_max)
      updateTraeInOverview(model, maxMode)
      setMessage(maxMode ? t('contextMaxOn', { model: model.id }) : t('contextMaxOff', { model: model.id }))
    } catch (error) {
      setMessageError(true)
      setMessage(error instanceof Error ? error.message : String(error))
    } finally {
      setSavingKey('')
    }
  }


  return (
    <div className="space-y-6">
      <section className="grid gap-5 border-b border-separator pb-6 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end">
        <div data-gsap-reveal>
          <h2 className="text-2xl font-semibold tracking-[-0.035em]">{t('availableModels')}</h2>
          <p className="mt-1 text-sm text-muted">
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
            <Button size="sm" variant="secondary" isPending={busy} onPress={() => void onRefresh()}>
              <ArrowClockwise size={14} />
              {busy ? t('refreshing') : t('refresh')}
            </Button>
          </div>
        </div>
      </section>

      {message ? <PageAlert status={messageError ? 'danger' : 'success'} title={message} /> : null}

      <Card data-gsap-reveal className="overflow-hidden p-0">
        <div className="flex items-start justify-between gap-4 border-b border-separator px-4 py-4 sm:items-center sm:px-5">
          <div className="flex min-w-0 items-center gap-3">
            <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-surface-secondary text-muted"><Cube size={16} /></div>
            <div className="min-w-0">
              <div className="text-sm font-semibold">{t('providerCatalog')}</div>
              <div className="mt-0.5 text-xs leading-5 text-muted">{t('contextConfigHint')}</div>
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
                const key = modelSettingsKey(model)
                const saving = savingKey === key
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
                            {credits ? <span className="mono text-[11px] text-muted">{credits}</span> : null}
                          </div>
                          <div className="mono mt-0.5 truncate text-[11px] text-muted">{model.id}</div>
                          {routedModelName(model) ? <div className="mt-0.5 text-[10px] text-muted">{t('routedTo', { model: routedModelName(model) })}</div> : null}
                        </div>
                      </div>
                      <Chip size="sm" variant="soft" color={model.context_custom ? 'warning' : model.stale ? 'warning' : 'success'}>
                        {model.context_custom ? t('custom') : t('defaultValue')}
                      </Chip>
                    </div>
                    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
                      <span className="inline-flex items-center gap-1.5">
                        <ProviderMark provider={provider} size={14} />
                        <span className="font-medium">{providerLabel}</span>
                      </span>
                      <span className="mono break-all text-muted">{model.mapped_key || model.native_model || model.id}</span>
                    </div>
                    <ModelContextControls
                      model={model}
                      saving={saving}
                      t={t}
                      onToggleTraeMax={(item, selected) => void onToggleTraeMax(item, selected)}
                      onReasoningChange={(item, next) => void onReasoningChange(item, next)}
                    />
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
                    <Table.Column>{t('modelDefaultsCol')}</Table.Column>
                    <Table.Column>{t('stateCol')}</Table.Column>
                    <Table.Column>{t('actions')}</Table.Column>
                  </Table.Header>
                  <Table.Body>
                    {paged.map((model) => {
                      const key = modelSettingsKey(model)
                      const saving = savingKey === key
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
                                  {credits ? <span className="mono text-[11px] text-muted">{credits}</span> : null}
                                </div>
                                {routedModelName(model) ? <div className="mt-0.5 text-[10px] text-muted">{t('routedTo', { model: routedModelName(model) })}</div> : null}
                              </div>
                            </div>
                          </Table.Cell>
                          <Table.Cell><span className="mono text-xs font-medium">{model.id}</span></Table.Cell>
                          <Table.Cell>
                            <div className="flex items-center gap-2">
                              <ProviderMark provider={provider} size={14} />
                              <span className="text-xs font-medium">{providerLabel}</span>
                            </div>
                          </Table.Cell>
                          <Table.Cell><span className="mono text-xs text-muted">{model.mapped_key || model.native_model || model.id}</span></Table.Cell>
                          <Table.Cell>
                            <ModelContextControls
                              model={model}
                              saving={saving}
                              t={t}
                              onToggleTraeMax={(item, selected) => void onToggleTraeMax(item, selected)}
                              onReasoningChange={(item, next) => void onReasoningChange(item, next)}
                            />
                          </Table.Cell>
                          <Table.Cell>
                            <Chip size="sm" variant="soft" color={model.context_custom ? 'warning' : model.stale ? 'warning' : 'success'}>
                              {model.context_custom ? t('custom') : t('defaultValue')}
                            </Chip>
                          </Table.Cell>
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
        model={detailModel ? models.find((m) => modelSettingsKey(m) === modelSettingsKey(detailModel)) || detailModel : null}
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
