import { useEffect, useId, useMemo, useState, type ReactNode } from 'react'
import { Button, Card, Chip, Table, Tooltip } from '@heroui/react'
import { ArrowClockwise, CaretRight, MagnifyingGlass, Info, Lightning, GearSix } from '@phosphor-icons/react'
import { useCollapse } from '@/hooks/useCollapse'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import { checkModels, fetchAccounts, fetchModelsCached, fetchProviders, reloadModels, refreshModels, updateProviderMaxMode, type ModelProbeResponse, type ModelProbeResult, type ProviderDescriptor } from '@/api/overview'
import { fetchProviderPolicies, type ProviderPolicy } from '@/api/providerSettings'
import type { Overview } from '@/api/types'
import { ProviderMark } from '@/components/ProviderMark'
import { ModelDetailsModal, formatTokens } from '@/components/ModelDetailsModal'
import { ProviderSettingsModal } from '@/components/provider/ProviderSettingsModal'
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
type AccountInfo = NonNullable<Overview['accounts']>[number]

type ProviderRegionOption = {
  value: string
  provider: string
  region: string
  checkin?: boolean
  checkinTimezone?: string
}

function providerRegionOptions(descriptors: ProviderDescriptor[]): ProviderRegionOption[] {
  const options: ProviderRegionOption[] = []
  for (const descriptor of descriptors) {
    for (const region of descriptor.regions || []) {
      if (!region?.id) continue
      options.push({
        value: `${descriptor.id}:${region.id}`,
        provider: descriptor.id,
        region: region.id,
        checkin: Boolean(region.checkin),
        checkinTimezone: region.checkin?.timezone === 'Local' ? 'Asia/Shanghai' : region.checkin?.timezone,
      })
    }
  }
  return options
}

function connectedProviderValues(accounts: AccountInfo[], options: ProviderRegionOption[]) {
  const values = new Set<string>()
  for (const account of accounts) {
    const provider = String(account.provider || 'qoder').trim().toLowerCase()
    const region = String(account.region || '').trim().toLowerCase()
    const option = options.find((item) => item.provider === provider && item.region === region) ||
      options.find((item) => item.provider === provider)
    if (option) values.add(option.value)
  }
  return Array.from(values)
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

type Translate = (key: string, vars?: Record<string, string | number>) => string

type ModelGroup = {
  key: string
  provider: string
  region: string
  label: string
  models: ModelInfo[]
}

function groupModelsByProvider(models: ModelInfo[], t: Translate): ModelGroup[] {
  const groups = new Map<string, ModelGroup>()
  for (const model of models) {
    const provider = modelProvider(model)
    // "aggregation" is the model-pool's synthetic logical-model label, not a
    // upstream provider. Never render it as a provider or a settings card.
    if (provider === 'aggregation') continue
    const region = modelRegion(model)
    const key = `${provider}:${region || 'any'}`
    let group = groups.get(key)
    if (!group) {
      group = {
        key,
        provider,
        region,
        label: accountProviderLabel(provider, region || undefined, t),
        models: [],
      }
      groups.set(key, group)
    }
    group.models.push(model)
  }
  return Array.from(groups.values())
}

function defaultDropSystemPrompt(provider: string) {
  const value = provider.toLowerCase()
  return !['trae', 'kimi', 'codearts', 'minimaxcode'].includes(value)
}

function settingsRegionFor(group: ModelGroup, options: ProviderRegionOption[]) {
  if (group.region && group.region !== 'any') return group.region
  return options.find((option) => option.provider === group.provider)?.region || ''
}

function orderProviderGroups(groups: ModelGroup[], options: ProviderRegionOption[]) {
  const order = new Map(options.map((option, index) => [`${option.provider}:${option.region || 'any'}`, index]))
  return [...groups].sort((left, right) => {
    const leftIndex = order.get(left.key)
    const rightIndex = order.get(right.key)
    if (leftIndex != null && rightIndex != null) return leftIndex - rightIndex
    if (leftIndex != null) return -1
    if (rightIndex != null) return 1
    return 0
  })
}

function providerPolicyFor(policies: ProviderPolicy[], provider: string, region: string): ProviderPolicy {
  const existing = policies.find((policy) => policy.provider === provider && (policy.region || 'any') === region)
  if (existing) return existing
  return {
    provider,
    region,
    credit_floor: null,
    percent_floor: null,
    proxy_url: '',
    drop_system_prompt: defaultDropSystemPrompt(provider),
    model_aliases: {},
    enabled_models: [],
    models_restricted: false,
    checkin_time: '09:00',
    checkin_auto: true,
  }
}

function ModelContextControls({
  model,
  saving,
  t,
  variant = 'table',
  onToggleTraeMax,
}: {
  model: ModelInfo
  saving: boolean
  t: Translate
  /** table: stacked inside a stable-width cell; card: mobile label-above */
  variant?: 'table' | 'card'
  onToggleTraeMax: (model: ModelInfo, selected: boolean) => void
}) {
  const provider = modelProvider(model)
  const isCard = variant === 'card'
  const defaultWindow = model.catalog_context_length || model.context_length
  const maxWindow = model.catalog_context_length_max
  const hasMaxWindow = Boolean(maxWindow && maxWindow !== defaultWindow)
  const reasoningOptions = model.reasoning_options || []
  const canToggleMax = (provider === 'trae' || provider === 'qoder') && model.supports_max_mode
  const reasoningText = reasoningOptions.length
    ? reasoningOptions.join(' / ')
    : model.reasoning_type
      ? t('reasoningFixed', { type: model.reasoning_type })
      : '—'
  return (
    <div className={isCard ? 'flex min-w-0 flex-col gap-2' : 'flex min-w-0 flex-col gap-1'}>
      <div className="flex min-w-0 items-center gap-2">
        <span className="mono text-caption font-medium">
          {formatTokens(defaultWindow)}{hasMaxWindow ? ` → ${formatTokens(maxWindow)}` : ''}
        </span>
        {canToggleMax ? (
          <CompactSwitch
            isSelected={Boolean(model.max_mode)}
            isDisabled={saving}
            ariaLabel={`${model.id} ${t('maxMode')}`}
            onChange={(selected) => onToggleTraeMax(model, selected)}
          />
        ) : null}
      </div>
      <div className="min-w-0 truncate text-caption text-muted" title={reasoningText}>
        {reasoningText}
      </div>
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

function ProviderGroupIdentity({ group, t }: { group: ModelGroup; t: Translate }) {
  return (
    <div className="flex min-w-0 items-center gap-3">
      <div className="grid size-8 shrink-0 place-items-center rounded-lg bg-surface text-foreground">
        <ProviderMark provider={group.provider} size={16} />
      </div>
      <div className="min-w-0">
        <div className="truncate text-body font-semibold">{group.label}</div>
        <div className="text-caption text-muted">{t('usageModelCount', { count: group.models.length })}</div>
      </div>
    </div>
  )
}

function ProviderGroupCard({
  group,
  t,
  collapsible,
  collapsed,
  onToggle,
  onSettings,
  settingsDisabled,
  savingKey,
  modelCheckChip,
  onToggleTraeMax,
  onDetails,
}: {
  group: ModelGroup
  t: Translate
  collapsible: boolean
  collapsed: boolean
  onToggle: () => void
  onSettings: () => void
  settingsDisabled: boolean
  savingKey: string
  modelCheckChip: (model: ModelInfo) => ReactNode
  onToggleTraeMax: (model: ModelInfo, selected: boolean) => void
  onDetails: (model: ModelInfo) => void
}) {
  const bodyId = useId()
  const open = collapsible ? !collapsed : true
  const { mounted, outerRef, innerRef } = useCollapse(open)

  return (
    <Card className="overflow-hidden p-0">
      <div className={`flex min-w-0 items-center gap-3 bg-surface-secondary px-4 py-3 ${mounted ? 'border-b border-separator' : ''}`}>
        {collapsible ? (
          <button
            type="button"
            className="flex min-w-0 flex-1 items-center justify-between gap-3 rounded-lg text-left outline-none focus-visible:ring-2 focus-visible:ring-focus"
            aria-expanded={!collapsed}
            aria-controls={bodyId}
            aria-label={`${collapsed ? t('providerExpand') : t('providerCollapse')} ${group.label}`}
            onClick={onToggle}
          >
            <ProviderGroupIdentity group={group} t={t} />
            <CaretRight
              size={16}
              className={`shrink-0 text-muted transition-transform ${collapsed ? '' : 'rotate-90'}`}
              aria-hidden
            />
          </button>
        ) : (
          <div className="flex min-w-0 flex-1 items-center">
            <ProviderGroupIdentity group={group} t={t} />
          </div>
        )}
        <Button className="shrink-0" size="sm" variant="ghost" isDisabled={settingsDisabled} onPress={onSettings}>
          <GearSix size={14} />
          {t('providerSettingsOpen')}
        </Button>
      </div>
      {mounted ? (
        <div id={bodyId} ref={outerRef} className="min-w-0">
          <div ref={innerRef}>
            {group.models.length === 0 ? (
              <div className="px-4 py-8 text-center text-caption text-muted">{t('noModelsYet')}</div>
            ) : (
              <>
                <div className="divide-y divide-separator lg:hidden">
                  {group.models.map((model) => {
                    const key = modelSettingsKey(model)
                    const saving = savingKey === key
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
                        <div className="mono break-all text-caption text-muted">{model.mapped_key || model.native_model || model.id}</div>
                        <ModelContextControls
                          model={model}
                          saving={saving}
                          t={t}
                          variant="card"
                          onToggleTraeMax={onToggleTraeMax}
                        />
                        <ModelActions
                          model={model}
                          t={t}
                          onDetails={onDetails}
                        />
                      </article>
                    )
                  })}
                </div>
                <Table className="hidden min-w-0 lg:block">
                  <Table.ScrollContainer className="min-w-0 overflow-x-auto">
                    <Table.Content aria-label={`${group.label} ${t('availableModels')}`} className="min-w-[58rem]">
                      <Table.Header>
                        <Table.Column isRowHeader>{t('modelCol')}</Table.Column>
                        <Table.Column>{t('requestIdCol')}</Table.Column>
                        <Table.Column>{t('qoderKeyCol')}</Table.Column>
                        <Table.Column>{t('modelDefaultsCol')}</Table.Column>
                        <Table.Column>{t('stateCol')}</Table.Column>
                        <Table.Column>{t('modelDetailsCol')}</Table.Column>
                      </Table.Header>
                      <Table.Body>
                        {group.models.map((model) => {
                          const key = modelSettingsKey(model)
                          const saving = savingKey === key
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
                              <Table.Cell><span className="mono text-caption text-muted">{model.mapped_key || model.native_model || model.id}</span></Table.Cell>
                              <Table.Cell>
                                <ModelContextControls
                                  model={model}
                                  saving={saving}
                                  t={t}
                                  variant="table"
                                  onToggleTraeMax={onToggleTraeMax}
                                />
                              </Table.Cell>
                              <Table.Cell>{modelCheckChip(model)}</Table.Cell>
                              <Table.Cell>
                                <ModelActions
                                  model={model}
                                  t={t}
                                  onDetails={onDetails}
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
          </div>
        </div>
      ) : null}
    </Card>
  )
}

export function ProvidersPage() {
  const { t } = useI18n()
  const { overview, loading } = useOverview()
  const [filter, setFilter] = useState('')
  const [providerFilter, setProviderFilter] = useState('')
  const [providerOptions, setProviderOptions] = useState<ProviderRegionOption[]>([])
  const [providersLoading, setProvidersLoading] = useState(true)
  const [accountList, setAccountList] = useState<AccountInfo[] | null>(null)
  const [accountsLoading, setAccountsLoading] = useState(true)
  const [providerPolicies, setProviderPolicies] = useState<ProviderPolicy[]>([])
  const [policiesLoading, setPoliciesLoading] = useState(true)
  const [policyError, setPolicyError] = useState(false)
  const [settingsTarget, setSettingsTarget] = useState<{ provider: string; region: string; label: string } | null>(null)
  const [collapsedGroups, setCollapsedGroups] = useState<Set<string>>(new Set())
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState<PageSize>(50)
  const [busy, setBusy] = useState(false)
  const [savingKey, setSavingKey] = useState('')
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
      .finally(() => { if (!cancelled) setProvidersLoading(false) })
    void fetchAccounts(false)
      .then((result) => { if (!cancelled) setAccountList(result.data || []) })
      .catch(() => { if (!cancelled) setAccountList(null) })
      .finally(() => { if (!cancelled) setAccountsLoading(false) })
    void fetchProviderPolicies()
      .then((result) => {
        if (!cancelled) setProviderPolicies(result.data || [])
      })
      .catch((error) => {
        if (cancelled) return
        setPolicyError(true)
        setMessageError(true)
        setMessage(error instanceof Error ? error.message : String(error))
      })
      .finally(() => { if (!cancelled) setPoliciesLoading(false) })
    return () => { cancelled = true }
  }, [])

  const connectedValues = useMemo(
    () => accountList ? connectedProviderValues(accountList, providerOptions) : null,
    [accountList, providerOptions],
  )
  const modelProviderValues = useMemo(() => {
    const values = new Set<string>()
    for (const model of models) {
      const provider = modelProvider(model)
      const region = modelRegion(model)
      const option = providerOptions.find((item) => item.provider === provider && item.region === region) ||
        providerOptions.find((item) => item.provider === provider)
      if (option) values.add(option.value)
    }
    return Array.from(values)
  }, [models, providerOptions])
  const visibleProviderOptions = useMemo(() => {
    const values = connectedValues && connectedValues.length ? connectedValues : modelProviderValues
    if (!values.length) return []
    return providerOptions.filter((option) => values.includes(option.value))
  }, [connectedValues, modelProviderValues, providerOptions])
  const effectiveProviderFilter =
    providerFilter === '' || visibleProviderOptions.some((option) => option.value === providerFilter)
      ? providerFilter
      : visibleProviderOptions[0]?.value || ''
  const allProvidersView = effectiveProviderFilter === ''

  const filtered = useMemo(() => {
    const query = filter.trim().toLowerCase()
    const [filterProvider, filterRegion] = effectiveProviderFilter.includes(':')
      ? effectiveProviderFilter.split(':', 2)
      : [effectiveProviderFilter, '']
    return models.filter((model) => {
      const provider = modelProvider(model)
      const region = modelRegion(model)
      if (filterProvider && provider !== filterProvider) return false
      if (filterRegion && region !== filterRegion) return false
      if (!query) return true
      const providerLabel = accountProviderLabel(provider, region || undefined, t)
      return `${model.display_name || ''} ${model.id} ${model.mapped_key || ''} ${provider} ${providerLabel} ${model.owned_by || ''} ${region} ${model.credits || ''} ${model.free ? 'free' : ''}`.toLowerCase().includes(query)
    })
  }, [filter, models, effectiveProviderFilter, t])

  const filterKey = [filter, effectiveProviderFilter, pageSize].join('\0')
  const [appliedFilterKey, setAppliedFilterKey] = useState(filterKey)
  if (appliedFilterKey !== filterKey) {
    setAppliedFilterKey(filterKey)
    setPage(1)
  }
  const groupedFiltered = useMemo(() => {
    const groups = groupModelsByProvider(filtered, t)
    if (filter.trim()) return orderProviderGroups(groups, visibleProviderOptions)
    const seen = new Set(groups.map((group) => group.key))
    const addOptionGroup = (option: ProviderRegionOption) => {
      const key = `${option.provider}:${option.region || 'any'}`
      if (seen.has(key)) return
      seen.add(key)
      groups.push({
        key,
        provider: option.provider,
        region: option.region,
        label: accountProviderLabel(option.provider, option.region, t),
        models: [],
      })
    }
    if (effectiveProviderFilter) {
      const option = visibleProviderOptions.find((item) => item.value === effectiveProviderFilter)
      if (option) addOptionGroup(option)
    } else {
      // The all-providers view still shows a card for every connected provider,
      // including one whose models are temporarily empty or restricted away.
      for (const option of visibleProviderOptions) addOptionGroup(option)
    }
    for (const policy of providerPolicies) {
      // Fallback for a restrictive policy that hides every model and has no
      // matching descriptor option: keep its settings entry reachable.
      if (!policy.models_restricted || !policy.provider || !policy.region || policy.region === 'any') continue
      if (effectiveProviderFilter) {
        const [selectedProvider, selectedRegion] = effectiveProviderFilter.includes(':') ? effectiveProviderFilter.split(':', 2) : [effectiveProviderFilter, '']
        if (selectedProvider && selectedProvider !== policy.provider) continue
        if (selectedRegion && selectedRegion !== policy.region) continue
      }
      const key = `${policy.provider}:${policy.region}`
      if (seen.has(key)) continue
      seen.add(key)
      groups.push({
        key,
        provider: policy.provider,
        region: policy.region,
        label: accountProviderLabel(policy.provider, policy.region, t),
        models: [],
      })
    }
    return orderProviderGroups(groups, visibleProviderOptions)
  }, [filtered, filter, effectiveProviderFilter, visibleProviderOptions, providerPolicies, t])
  const pageGroups = useMemo(() => {
    const pages: ModelGroup[][] = []
    let current: ModelGroup[] = []
    let count = 0
    for (const group of groupedFiltered) {
      if (current.length > 0 && count + group.models.length > pageSize) {
        pages.push(current)
        current = []
        count = 0
      }
      current.push(group)
      count += group.models.length
    }
    if (current.length > 0) pages.push(current)
    return pages.length > 0 ? pages : [[]]
  }, [groupedFiltered, pageSize])
  const pageCount = Math.max(1, pageGroups.length)
  const currentPage = Math.min(appliedFilterKey !== filterKey ? 1 : Math.max(1, page), pageCount)
  if (page !== currentPage) {
    setPage(currentPage)
  }
  const groupedPaged = pageGroups[currentPage - 1] ?? []
  const shownBefore = pageGroups
    .slice(0, currentPage - 1)
    .reduce((sum, group) => sum + group.reduce((groupSum, item) => groupSum + item.models.length, 0), 0)
  const pageModelCount = groupedPaged.reduce((sum, group) => sum + group.models.length, 0)
  const shownFrom = filtered.length === 0 ? 0 : shownBefore + 1
  const shownTo = filtered.length === 0 ? 0 : shownBefore + pageModelCount
  const shownLabel = filtered.length
    ? t('logsShownTotal', { shown: `${shownFrom}–${shownTo}`, total: filtered.length })
    : t('shownTotal', { shown: 0, total: models.length })
  const settingsOption = settingsTarget
    ? providerOptions.find((option) => option.provider === settingsTarget.provider && option.region === settingsTarget.region)
    : undefined
  const settingsPolicy = settingsTarget
    ? providerPolicyFor(providerPolicies, settingsTarget.provider, settingsTarget.region)
    : null

  if ((loading && !overview) || modelsLoading || accountsLoading || providersLoading) return <ProvidersPageSkeleton />


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

  function toggleProviderGroup(key: string) {
    setCollapsedGroups((current) => {
      const next = new Set(current)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
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

  function openProviderSettings(group: ModelGroup) {
    if (policiesLoading || policyError) {
      setMessageError(true)
      setMessage(policyError ? t('providerSettingsLoadFailed') : t('refreshing'))
      return
    }
    const region = settingsRegionFor(group, providerOptions)
    if (!region && providerOptions.some((option) => option.provider === group.provider)) {
      setMessageError(true)
      setMessage(t('providerSettingsRegionMissing'))
      return
    }
    setSettingsTarget({ provider: group.provider, region, label: group.label })
  }

  function onProviderSaved(policy: ProviderPolicy) {
    setProviderPolicies((current) => [
      ...current.filter((item) => !(item.provider === policy.provider && (item.region || 'any') === (policy.region || 'any'))),
      policy,
    ])
    setMessageError(false)
    setMessage(t('providerSettingsSaved'))
    // Saving a policy only changes which cached catalog entries are visible; it
    // does not need an upstream catalog refresh. Re-filter the cached catalog in
    // the background so the modal can close immediately.
    void reloadModels(undefined, 'regional')
      .then((data) => setModels(data.data || []))
      .catch((error) => {
        setMessageError(true)
        setMessage(t('providerSettingsSavedRefreshFailed', { message: error instanceof Error ? error.message : String(error) }))
      })
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
            {visibleProviderOptions.length ? (
              <FilterSelect
                ariaLabel={t('providerCol')}
                value={effectiveProviderFilter}
                onChange={setProviderFilter}
                options={[
                  { id: '', label: t('providerFilterAll') },
                  ...visibleProviderOptions.map((option) => ({
                    id: option.value,
                    label: accountProviderLabel(option.provider, option.region, t),
                  })),
                ]}
              />
            ) : null}
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

      {busy || loading ? (
        <Card data-gsap-reveal className="overflow-hidden p-0">
          <ProvidersTableSkeleton />
        </Card>
      ) : groupedFiltered.length === 0 ? (
        <Card data-gsap-reveal className="overflow-hidden p-0">
          <EmptyPanel
            icon={<MagnifyingGlass size={22} />}
            title={models.length ? t('noModelsMatch') : t('noProviders')}
            hint={models.length ? t('noModelsMatch') : t('noModelsYet')}
          />
        </Card>
      ) : (
        <div data-gsap-reveal className="space-y-4">
          {groupedPaged.map((group) => (
            <ProviderGroupCard
              key={group.key}
              group={group}
              t={t}
              collapsible={allProvidersView}
              collapsed={allProvidersView && collapsedGroups.has(group.key)}
              onToggle={() => toggleProviderGroup(group.key)}
              onSettings={() => openProviderSettings(group)}
              settingsDisabled={policiesLoading || policyError}
              savingKey={savingKey}
              modelCheckChip={modelCheckChip}
              onToggleTraeMax={(item, selected) => void onToggleTraeMax(item, selected)}
              onDetails={setDetailModel}
            />
          ))}
        </div>
      )}

      <ModelDetailsModal
        model={detailModel ? models.find((m) => modelRowKey(m) === modelRowKey(detailModel)) || detailModel : null}
        t={t}
        onClose={() => setDetailModel(null)}
      />
      {settingsTarget && settingsPolicy ? (
        <ProviderSettingsModal
          key={`${settingsTarget.provider}:${settingsTarget.region}`}
          provider={settingsTarget.provider}
          region={settingsTarget.region}
          label={settingsTarget.label}
          policy={settingsPolicy}
          checkinSupported={Boolean(settingsOption?.checkin)}
          checkinTimezone={settingsOption?.checkinTimezone}
          t={t}
          onClose={() => setSettingsTarget(null)}
          onSaved={(policy) => onProviderSaved(policy)}
        />
      ) : null}
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
