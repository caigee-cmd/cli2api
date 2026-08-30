import { useMemo, useState } from 'react'
import { Button, Card, Chip, Input, Table } from '@heroui/react'
import { Cube, ArrowClockwise, ArrowCounterClockwise, FloppyDisk, MagnifyingGlass, Info } from '@phosphor-icons/react'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import { refreshModels, updateModelContext, updateProviderReasoning, updateTraeMaxMode } from '@/api/overview'
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

type ModelInfo = NonNullable<Overview['models']>[number]

function modelSettingsKey(model: ModelInfo) {
  return model.settings_key || model.id
}

function modelProvider(model: ModelInfo) {
  // Account family wins over upstream owned_by (which may be a model vendor).
  return String(model.provider || model.owned_by || 'qoder').trim().toLowerCase()
}

function modelRowKey(model: ModelInfo) {
  return `${modelProvider(model)}:${model.settings_key || model.id}:${model.native_model || model.mapped_key || ''}`
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

export function ProvidersPage() {
  const { t } = useI18n()
  const { overview, loading, setOverview } = useOverview()
  const [filter, setFilter] = useState('')
  const [providerFilter, setProviderFilter] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState<PageSize>(50)
  const [busy, setBusy] = useState(false)
  const [savingKey, setSavingKey] = useState('')
  const [message, setMessage] = useState('')
  const [messageError, setMessageError] = useState(false)
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const [detailModel, setDetailModel] = useState<ModelInfo | null>(null)
  const models = useMemo(() => overview?.models || [], [overview?.models])
  const providers = useMemo(() => {
    const ids = new Set<string>()
    for (const model of models) ids.add(modelProvider(model))
    return [...ids].sort()
  }, [models])

  const filtered = useMemo(() => {
    const query = filter.trim().toLowerCase()
    return models.filter((model) => {
      const provider = modelProvider(model)
      if (providerFilter && provider !== providerFilter) return false
      if (!query) return true
      return `${model.display_name || ''} ${model.id} ${model.mapped_key || ''} ${model.provider || ''} ${model.owned_by || ''}`.toLowerCase().includes(query)
    })
  }, [filter, models, providerFilter])

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

  if (loading && !overview) return <ProvidersPageSkeleton />

  function updateModelInOverview(model: ModelInfo, result: Awaited<ReturnType<typeof updateModelContext>>) {
    const key = modelSettingsKey(model)
    const nextModels = models.map((item) => modelSettingsKey(item) === key
      ? {
          ...item,
          settings_key: result.model,
          context_length: result.context_length,
          default_context_length: result.default_context_length,
          context_custom: result.context_custom,
        }
      : item)
    setOverview({ ...(overview || {}), models: nextModels })
    setDrafts((current) => ({ ...current, [key]: String(result.context_length) }))
  }

  function updateTraeInOverview(model: ModelInfo, maxMode: boolean) {
    const key = modelSettingsKey(model)
    const nextModels = models.map((item) => {
      if (modelSettingsKey(item) !== key || modelProvider(item) !== 'trae') return item
      const dev = item.catalog_context_length || item.default_context_length || item.context_length
      const max = item.catalog_context_length_max
      const effort = item.reasoning_effort || item.reasoning_default || ''
      return {
        ...item,
        max_mode: maxMode,
        context_custom: maxMode || Boolean(effort && effort !== item.reasoning_default),
        context_length: maxMode && max ? max : dev,
      }
    })
    setOverview({ ...(overview || {}), models: nextModels })
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
    setOverview({ ...(overview || {}), models: nextModels })
  }

  async function onRefresh() {
    setBusy(true)
    setMessage('')
    setMessageError(false)
    try {
      const data = await refreshModels()
      setOverview({ ...(overview || {}), models: data.data || [] })
    } catch (error) {
      setMessageError(true)
      setMessage(error instanceof Error ? error.message : String(error))
    } finally {
      setBusy(false)
    }
  }

  async function onSave(model: ModelInfo) {
    const key = modelSettingsKey(model)
    const value = Number(drafts[key] ?? model.context_length ?? model.default_context_length)
    if (!Number.isInteger(value) || value < 1024 || value > 4_000_000) {
      setMessageError(true)
      setMessage(t('contextInvalid'))
      return
    }
    setSavingKey(key)
    setMessage('')
    setMessageError(false)
    try {
      const result = await updateModelContext(key, value)
      updateModelInOverview(model, result)
      setMessage(t('contextSaved', { model: model.id }))
    } catch (error) {
      setMessageError(true)
      setMessage(error instanceof Error ? error.message : String(error))
    } finally {
      setSavingKey('')
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
    setSavingKey(key)
    setMessage('')
    setMessageError(false)
    try {
      await updateTraeMaxMode(key, maxMode)
      updateTraeInOverview(model, maxMode)
      setMessage(maxMode ? t('contextMaxOn', { model: model.id }) : t('contextMaxOff', { model: model.id }))
    } catch (error) {
      setMessageError(true)
      setMessage(error instanceof Error ? error.message : String(error))
    } finally {
      setSavingKey('')
    }
  }

  async function onReset(model: ModelInfo) {
    const key = modelSettingsKey(model)
    setSavingKey(key)
    setMessage('')
    setMessageError(false)
    try {
      const result = await updateModelContext(key, 0)
      updateModelInOverview(model, result)
      setMessage(t('contextReset', { model: model.id }))
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
        <div data-gsap-reveal className="flex flex-col gap-2 sm:flex-row sm:items-center">
          <SearchBar className="sm:w-72" value={filter} onChange={setFilter} placeholder={t('filterPh')} ariaLabel={t('filter')} />
          <FilterSelect
            ariaLabel={t('providerCol')}
            value={providerFilter}
            onChange={setProviderFilter}
            options={[
              { id: '', label: t('providerFilterAll') },
              ...providers.map((provider) => ({ id: provider, label: provider })),
            ]}
          />
          <Button size="sm" variant="secondary" isPending={busy} onPress={() => void onRefresh()}>
            <ArrowClockwise size={14} />
            {busy ? t('refreshing') : t('refresh')}
          </Button>
        </div>
      </section>

      {message ? <PageAlert status={messageError ? 'danger' : 'success'} title={message} /> : null}

      <Card data-gsap-reveal className="overflow-hidden p-0">
        <div className="flex items-center justify-between gap-4 border-b border-separator px-5 py-4">
          <div className="flex items-center gap-3">
            <div className="grid size-9 place-items-center rounded-lg bg-surface-secondary text-muted"><Cube size={16} /></div>
            <div>
              <div className="text-sm font-semibold">{t('providerCatalog')}</div>
              <div className="mono mt-0.5 text-[10px] text-muted">{t('contextConfigHint')}</div>
            </div>
          </div>
          <Chip size="sm" variant="soft">{filtered.length}</Chip>
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
          <Table>
            <Table.ScrollContainer>
              <Table.Content aria-label={t('availableModels')}>
                <Table.Header>
                  <Table.Column isRowHeader>{t('modelCol')}</Table.Column>
                  <Table.Column>{t('requestIdCol')}</Table.Column>
                  <Table.Column>{t('providerCol')}</Table.Column>
                  <Table.Column>{t('qoderKeyCol')}</Table.Column>
                  <Table.Column>{t('contextWindowCol')}</Table.Column>
                  <Table.Column>{t('stateCol')}</Table.Column>
                  <Table.Column>{t('actions')}</Table.Column>
                </Table.Header>
                <Table.Body>
                  {paged.map((model) => {
                    const key = modelSettingsKey(model)
                    const saving = savingKey === key
                    const provider = modelProvider(model)
                    return (
                      <Table.Row key={modelRowKey(model)}>
                        <Table.Cell>
                          <div className="flex items-center gap-3 py-1">
                            <span className="status-dot" data-state={model.stale ? undefined : 'ok'} />
                            <div>
                              <div className="font-medium">{model.display_name || model.id}</div>
                              {routedModelName(model) ? <div className="mt-0.5 text-[10px] text-muted">{t('routedTo', { model: routedModelName(model) })}</div> : null}
                            </div>
                          </div>
                        </Table.Cell>
                        <Table.Cell><span className="mono text-xs font-medium">{model.id}</span></Table.Cell>
                        <Table.Cell>
                          <div className="flex items-center gap-2">
                            <ProviderMark provider={provider} size={14} />
                            <span className="text-xs font-medium">{provider}</span>
                          </div>
                        </Table.Cell>
                        <Table.Cell><span className="mono text-xs text-muted">{model.mapped_key || model.native_model || model.id}</span></Table.Cell>
                        <Table.Cell>
                          {provider === 'qoder' ? (
                            <div className="flex min-w-52 items-center gap-2">
                              <Input
                                className="w-40"
                                type="number"
                                min={1024}
                                max={4000000}
                                step={1024}
                                value={drafts[key] ?? String(model.context_length || model.default_context_length || '')}
                                onChange={(event) => setDrafts((current) => ({ ...current, [key]: event.target.value }))}
                                aria-label={`${model.id} ${t('contextWindowCol')}`}
                              />
                              <span className="mono text-[10px] text-muted">tokens</span>
                            </div>
                          ) : (
                            <div className="flex min-w-64 flex-wrap items-center gap-3">
                              <span className="mono text-xs text-muted">{formatTokens(model.catalog_context_length || model.context_length)}</span>
                              {provider === 'trae' && model.supports_max_mode ? (
                                <div className="flex items-center gap-2">
                                  <CompactSwitch
                                    isSelected={Boolean(model.max_mode)}
                                    isDisabled={saving}
                                    ariaLabel={`${model.id} ${t('maxMode')}`}
                                    onChange={(selected) => void onToggleTraeMax(model, selected)}
                                  />
                                  <span className="text-[11px] text-muted">{t('maxMode')}{model.catalog_context_length_max ? ` ${formatTokens(model.catalog_context_length_max)}` : ''}</span>
                                </div>
                              ) : null}
                              {(model.reasoning_options || []).length > 1 ? (
                                <FilterSelect
                                  className="min-w-28"
                                  ariaLabel={`${model.id} ${t('reasoningLevels')}`}
                                  value={model.reasoning_effort || model.reasoning_default || model.reasoning_options?.[0] || ''}
                                  onChange={(next) => { if (next) void onReasoningChange(model, next) }}
                                  options={(model.reasoning_options || []).map((level) => ({ id: level, label: reasoningLabel(t, level) }))}
                                />
                              ) : (model.reasoning_options || []).length === 1 ? (
                                <span className="text-[11px] text-muted">{reasoningLabel(t, model.reasoning_options![0])}</span>
                              ) : model.reasoning_type ? (
                                <span className="text-[11px] text-muted">{t('reasoningFixed', { type: model.reasoning_type })}</span>
                              ) : provider === 'trae' && !model.supports_max_mode ? (
                                <span className="text-[11px] text-muted">{t('catalogWindow')}</span>
                              ) : null}
                            </div>
                          )}
                        </Table.Cell>
                        <Table.Cell>
                          <Chip size="sm" variant="soft" color={model.context_custom ? 'warning' : model.stale ? 'warning' : 'success'}>
                            {model.context_custom ? t('custom') : t('defaultValue')}
                          </Chip>
                        </Table.Cell>
                        <Table.Cell>
                          <div className="flex items-center gap-2">
                            {provider === 'qoder' ? (
                              <>
                                <Button size="sm" variant="secondary" isPending={saving} onPress={() => void onSave(model)}>
                                  <FloppyDisk size={14} />{t('save')}
                                </Button>
                                <Button size="sm" variant="ghost" isDisabled={saving || !model.context_custom} onPress={() => void onReset(model)} aria-label={t('resetDefault')}>
                                  <ArrowCounterClockwise size={14} />
                                </Button>
                              </>
                            ) : (
                              <Button size="sm" variant="ghost" onPress={() => setDetailModel(model)}>
                                <Info size={14} />{t('modelDetails')}
                              </Button>
                            )}
                          </div>
                        </Table.Cell>
                      </Table.Row>
                    )
                  })}
                </Table.Body>
              </Table.Content>
            </Table.ScrollContainer>
          </Table>
        )}
      </Card>

      <ModelDetailsModal model={detailModel} t={t} onClose={() => setDetailModel(null)} />
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
