import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Navigate, useLocation, useNavigate, useParams } from 'react-router-dom'
import { Button, Card, Input, Label, NumberField, Separator, TextArea } from '@heroui/react'
import { ArrowLeft, Plus, TrashSimple } from '@phosphor-icons/react'
import {
  createAggregation,
  previewAggregation,
  updateAggregation,
  fetchAggregations,
  AGGREGATION_STRATEGIES,
  type AggregationMember,
  type AggregationPreviewAttempt,
  type AggregationRecord,
} from '@/api/modelPools'
import { fetchAccounts, fetchModels, fetchProviders, type ProviderDescriptor } from '@/api/overview'
import type { ModelInfo } from '@/api/types'
import { ProviderMark } from '@/components/ProviderMark'
import { AccountChoices } from '@/components/model-pool/AccountChoices'
import { RouteChain, type FallbackAttempt } from '@/components/model-pool/RouteChain'
import { DispatchCanvas } from '@/components/model-pool/DispatchCanvas'
import {
  STRATEGY_ID,
  addPath,
  canonicalDocument,
  createStep,
  documentFromRecord,
  emptyDocument,
  findStep,
  insertStep,
  moveStep,
  previewPathStatus,
  selectedAccountIds,
  removePath,
  removeStep,
  setOnlyFallback,
  updatePath,
  updateStep,
  validatePipeline,
  type PipelineDocument,
  type PipelineIssue,
  type PipelineStep,
} from '@/components/model-pool/pipeline'
import { modelOptions, modelTitle, normalizeProvider, normalizeRegion, providerOptions, regionOptions, type AccountRow } from '@/components/model-pool/options'
import { CompactSwitch } from '@/components/ui/CompactSwitch'
import { FilterSelect, type FilterSelectOption } from '@/components/ui/FilterSelect'
import { PageAlert } from '@/components/ui/PageAlert'
import { SkeletonBlock } from '@/components/ui/PageSkeletons'
import { useCrossProviderPool } from '@/hooks/useCrossProviderPool'
import { useI18n } from '@/hooks/useI18n'
import { toast } from '@/lib/toast'
import { accountProviderLabel } from '@/lib/provider'

const REASONING_LEVELS = ['none', 'low', 'medium', 'high', 'xhigh', 'max'] as const
const ID_RE = /^[a-z0-9/-]+$/

const ISSUE_KEY: Record<PipelineIssue, string> = {
  empty: 'modelPoolMembersRequired',
  incomplete: 'modelPoolMemberIncomplete',
  group: 'modelPoolGroupRequired',
  self: 'modelPoolSelfGroup',
  classifier: 'modelPoolClassifierIncomplete',
}

export function ModelPoolEditorPage() {
  const { t } = useI18n()
  const navigate = useNavigate()
  const location = useLocation()
  const splat = useParams()['*'] ?? ''
  const isNew = location.pathname === '/model-pool/new'
  const routeId = isNew ? '' : splat
  const { enabled, loading: poolLoading } = useCrossProviderPool()

  const loadedKey = useRef<string | null>(null)
  const [aggregations, setAggregations] = useState<AggregationRecord[]>([])
  const [descriptors, setDescriptors] = useState<ProviderDescriptor[]>([])
  const [models, setModels] = useState<ModelInfo[]>([])
  const [accounts, setAccounts] = useState<AccountRow[]>([])
  const [loading, setLoading] = useState(true)
  const [ready, setReady] = useState(false)
  const [missing, setMissing] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const [name, setName] = useState('')
  const [modelId, setModelId] = useState('')
  const [enabledFlag, setEnabledFlag] = useState(true)
  const [strategy, setStrategy] = useState('priority')
  const [allowFallbacks, setAllowFallbacks] = useState(true)
  const [maxAttempts, setMaxAttempts] = useState(3)
  const [contextLength, setContextLength] = useState(0)
  const [maxOutput, setMaxOutput] = useState(0)
  const [reasoningOptions, setReasoningOptions] = useState<string[]>([])
  const [reasoningEffort, setReasoningEffort] = useState('')
  const [doc, setDoc] = useState<PipelineDocument>(emptyDocument)
  const [selected, setSelected] = useState<string | null>(STRATEGY_ID)
  const [keywordDraft, setKeywordDraft] = useState<Record<string, string>>({})

  const [message, setMessage] = useState('')
  const [previewBusy, setPreviewBusy] = useState(false)
  const [previewError, setPreviewError] = useState('')
  const [preview, setPreview] = useState<{ path: string; attempts: AggregationPreviewAttempt[] } | null>(null)

  const commitDoc = useCallback((recipe: (current: PipelineDocument) => PipelineDocument) => {
    setDoc(recipe)
  }, [])

  const hydrate = useCallback((record: AggregationRecord | null) => {
    const next = documentFromRecord(record?.document, record?.members || [])
    setDoc(next)
    setName(record?.name || '')
    setModelId(record?.id || '')
    setEnabledFlag(record?.enabled ?? true)
    setStrategy(record?.strategy || 'priority')
    setAllowFallbacks(record?.allow_fallbacks ?? true)
    setMaxAttempts(record?.max_attempts || 3)
    setContextLength(record?.context_length ?? 0)
    setMaxOutput(record?.max_output_tokens ?? 0)
    setReasoningOptions(record?.reasoning_options ?? [])
    setReasoningEffort(record?.reasoning_effort ?? '')
    setSelected(STRATEGY_ID)
    setKeywordDraft({})
    setPreview(null)
    setPreviewError('')
    setMissing(false)
    setError('')
  }, [])

  useEffect(() => {
    if (!enabled || poolLoading) return
    const key = isNew ? 'new' : routeId
    if (loadedKey.current === key) return
    let cancelled = false
    setLoading(true)
    void (async () => {
      try {
        const [aggResult, providerResult, modelResult, accountResult] = await Promise.all([
          fetchAggregations(),
          fetchProviders(),
          fetchModels(undefined, false, 'regional'),
          fetchAccounts(),
        ])
        if (cancelled) return
        const list = aggResult.data || []
        setAggregations(list)
        setDescriptors(providerResult.data || [])
        setModels(modelResult.data || [])
        setAccounts(accountResult.data || [])
        if (isNew) {
          hydrate(null)
        } else {
          const found = list.find((item) => item.id === routeId)
          if (!found) setMissing(true)
          else hydrate(found)
        }
        if (!cancelled) loadedKey.current = key
        setReady(true)
      } catch (err) {
        if (!cancelled) {
          setReady(false)
          setError(err instanceof Error ? err.message : String(err))
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => { cancelled = true }
  }, [enabled, poolLoading, isNew, routeId, hydrate])

  const selfId = isNew ? modelId.trim().toLowerCase() : routeId
  const providerSelectOptions = useMemo(() => providerOptions(descriptors, accounts), [descriptors, accounts])
  const strategyOptions = useMemo<FilterSelectOption[]>(
    () => AGGREGATION_STRATEGIES.map((item) => ({ id: item, label: t(`modelPoolStrategy_${item}`) })),
    [t],
  )
  const groupOptions = useMemo<FilterSelectOption[]>(() => {
    const pools = aggregations
      .filter((item) => item.id !== selfId && item.enabled)
      .map((item) => ({ id: item.id, label: `${item.name} (${item.id})` }))
    return [{ id: '', label: pools.length ? t('modelPoolNestedPoolPick') : t('modelPoolNestedPoolEmpty') }, ...pools]
  }, [aggregations, selfId, t])

  const skipped = useMemo(() => (preview?.path ? previewPathStatus(doc, preview.path) : {}), [doc, preview])
  const selectedStep = selected ? findStep(doc.steps, selected) : null

  useEffect(() => {
    if (!selected || selected === STRATEGY_ID) return
    if (!findStep(doc.steps, selected)) setSelected(STRATEGY_ID)
  }, [doc, selected])

  function addSeat(parentPathId: string | null, kind: PipelineStep['kind'], index = Number.MAX_SAFE_INTEGER) {
    const step = createStep(kind, { match: t('modelPoolPathMatch'), fallback: t('modelPoolPathDefault') })
    commitDoc((current) => insertStep(current, parentPathId, index, step))
    setSelected(step.id)
  }

  function moveSeat(parentPathId: string | null, from: number, to: number) {
    commitDoc((current) => moveStep(current, parentPathId, from, to))
  }

  function toggleReasoningLevel(level: string) {
    const next = reasoningOptions.includes(level)
      ? reasoningOptions.filter((item) => item !== level)
      : [...reasoningOptions, level]
    setReasoningOptions(next)
    if (reasoningEffort && !next.includes(reasoningEffort)) setReasoningEffort('')
  }

  function formError() {
    if (isNew) {
      const trimmed = modelId.trim().toLowerCase()
      if (!trimmed || trimmed.length > 128 || !ID_RE.test(trimmed)) return t('modelPoolIdInvalid')
    }
    if (!name.trim()) return t('modelPoolNameRequired')
    const issue = validatePipeline(doc, selfId)
    if (issue) return t(ISSUE_KEY[issue])
    if (!Number.isInteger(maxAttempts) || maxAttempts < 1 || maxAttempts > 64) return t('modelPoolAttemptsInvalid')
    if (!Number.isInteger(contextLength) || contextLength < 0 || (contextLength > 0 && contextLength < 1024)) return t('modelPoolCapabilitiesInvalid')
    if (!Number.isInteger(maxOutput) || maxOutput < 0 || (maxOutput > 0 && maxOutput < 1024) || (contextLength > 0 && maxOutput > contextLength)) {
      return t('modelPoolCapabilitiesInvalid')
    }
    if (reasoningEffort && !reasoningOptions.includes(reasoningEffort)) return t('modelPoolReasoningInvalid')
    return ''
  }

  function payload() {
    return {
      name: name.trim(),
      enabled: enabledFlag,
      strategy,
      allow_fallbacks: allowFallbacks,
      max_attempts: maxAttempts,
      context_length: contextLength,
      max_output_tokens: maxOutput,
      reasoning_options: reasoningOptions,
      reasoning_effort: reasoningEffort,
      document: canonicalDocument(doc),
      members: [] as AggregationMember[],
    }
  }

  async function save() {
    const invalid = formError()
    if (invalid) {
      setError(invalid)
      return
    }
    setBusy(true)
    setError('')
    try {
      const body = payload()
      if (isNew) {
        const saved = await createAggregation({ ...body, id: modelId.trim().toLowerCase() })
        toast({ type: 'success', title: t('modelPoolSaved') })
        navigate(`/model-pool/${saved.id}`, { replace: true })
      } else {
        const saved = await updateAggregation(routeId, body)
        hydrate(saved)
        toast({ type: 'success', title: t('modelPoolSaved') })
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function runPreview() {
    const invalid = validatePipeline(doc, selfId)
    if (invalid) {
      setPreview(null)
      setPreviewError(t(ISSUE_KEY[invalid]))
      return
    }
    setPreviewBusy(true)
    setPreviewError('')
    try {
      const result = await previewAggregation({
        id: selfId || 'preview',
        strategy,
        document: canonicalDocument(doc),
        message: message.trim(),
      })
      setPreview(result)
    } catch (err) {
      setPreview(null)
      setPreviewError(err instanceof Error ? err.message : String(err))
    } finally {
      setPreviewBusy(false)
    }
  }

  const attempts = useMemo<FallbackAttempt[]>(() => (preview?.attempts || []).map((attempt, index) => ({
    id: attempt.id || `${attempt.provider}-${attempt.model}-${index}`,
    model: attempt.model,
    provider: attempt.provider,
    region: attempt.region,
    status: attempt.status === 'skipped' ? 'skipped' : 'waiting',
    reason: attempt.reason === 'no ready account' ? t('modelPoolNoReady') : attempt.reason,
    logo: <ProviderMark provider={attempt.provider} size={14} />,
  })), [preview, t])

  if (poolLoading) return <SkeletonBlock className="h-64 w-full rounded-xl" />
  if (!enabled) return <Navigate to="/" replace />

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <Button variant="ghost" onPress={() => navigate('/model-pool')}>
          <ArrowLeft size={16} />{t('modelPoolBack')}
        </Button>
        <div className="flex items-center gap-3">
          <CompactSwitch isSelected={enabledFlag} ariaLabel={t('modelPoolEnabled')} label={t('modelPoolEnabled')} onChange={setEnabledFlag} isDisabled={busy} />
          <Button onPress={() => void save()} isPending={busy}>{t('save')}</Button>
        </div>
      </div>
      <div className="grid gap-4 sm:grid-cols-2 sm:max-w-3xl">
        <Field label={t('modelPoolName')} hint={t('modelPoolNameHint')}>
          <Input
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder={t('modelPoolNamePh')}
            aria-label={t('modelPoolName')}
            disabled={busy}
          />
        </Field>
        {isNew ? (
          <Field label={t('modelPoolId')} hint={t('modelPoolIdHint')}>
            <Input
              className="mono"
              value={modelId}
              onChange={(event) => setModelId(event.target.value)}
              placeholder="my-claude"
              aria-label={t('modelPoolId')}
              disabled={busy}
              autoCapitalize="none"
              autoCorrect="off"
              spellCheck={false}
            />
          </Field>
        ) : (
          <Field label={t('modelPoolId')} hint={t('modelPoolIdLocked')}>
            <p className="mono truncate pt-2 text-body text-foreground">{routeId}</p>
          </Field>
        )}
      </div>
      <p className="max-w-3xl text-caption leading-5 text-muted">{t('modelPoolScope')}</p>

      {error ? <PageAlert status="danger" title={error} /> : null}
      {missing ? <PageAlert status="danger" title={t('modelPoolMissing')} /> : null}

      {loading ? (
        <SkeletonBlock className="h-[640px] w-full rounded-xl" />
      ) : missing || !ready ? null : (
        <div className="grid items-start gap-6 xl:grid-cols-[minmax(0,1fr)_22rem]">
          <div className="min-w-0 xl:col-start-1 xl:row-start-1">
            <DispatchCanvas
              steps={doc.steps}
              selected={selected}
              strategyTitle={t(`modelPoolStrategy_${strategy}`)}
              strategyLine={t('modelPoolStrategyNote')}
              strategyChips={[
                { text: allowFallbacks ? t('modelPoolFailoverOn', { count: maxAttempts }) : t('modelPoolFailoverOff'), tone: 'plain' },
                { text: t('modelPoolSeatCount', { count: doc.steps.length }), tone: 'plain' },
              ]}
              requestTitle={name.trim() || (isNew ? (modelId.trim() || t('modelPoolEntryDetail')) : routeId)}
              requestToken={requestToken(name, isNew ? modelId : routeId)}
              skipped={skipped}
              busy={busy}
              labels={{
                request: t('modelPoolEntryTitle'),
                requestLine: t('modelPoolEntryNote'),
                strategyKind: t('modelPoolStrategyNode'),
                model: t('modelPoolStepProvider'),
                modelLine: t('modelPoolStepProviderDetail'),
                group: t('modelPoolStepGroup'),
                groupLine: t('modelPoolStepGroupDetail'),
                intent: t('modelPoolStepIntent'),
                intentLine: t('modelPoolStepIntentDetail'),
                add: t('modelPoolAddSeat'),
                remove: t('modelPoolRemoveSeat'),
                hint: t('modelPoolEntryHint'),
                palette: t('modelPoolCanvasPalette'),
                incomplete: t('modelPoolCanvasIncomplete'),
                skipped: t('modelPoolCanvasSkip'),
                fit: t('modelPoolCanvasFit'),
                zoomIn: t('modelPoolCanvasZoomIn'),
                zoomOut: t('modelPoolCanvasZoomOut'),
                drag: t('modelPoolDragSeat'),
              }}
              describe={(step) => seatCopy(step, strategy === 'fill-first', t, models, aggregations, accounts)}
              onSelectStrategy={() => setSelected(STRATEGY_ID)}
              onSelect={setSelected}
              onInsert={addSeat}
              onMove={moveSeat}
              onRemove={(id) => {
                commitDoc((current) => removeStep(current, id))
                setSelected(STRATEGY_ID)
              }}
            />
          </div>

          <Card className="p-4 xl:sticky xl:top-4 xl:col-start-2 xl:row-span-2 xl:max-h-[calc(100dvh-8rem)] xl:overflow-y-auto">
            <div className="space-y-4">
              {selected === STRATEGY_ID || selected == null ? (
                <div className="space-y-4">
                  <h2 className="text-body font-semibold">{t('modelPoolStrategyNode')}</h2>
                  <Field label={t('modelPoolStrategy')} hint={t('modelPoolStrategyHint')}>
                    <FilterSelect value={strategy} onChange={setStrategy} ariaLabel={t('modelPoolStrategy')} options={strategyOptions} className="w-full" />
                  </Field>
                  <Field label={t('modelPoolFailover')}>
                    <div className="flex flex-wrap items-center gap-3">
                      <CompactSwitch isSelected={allowFallbacks} ariaLabel={t('modelPoolFailover')} onChange={setAllowFallbacks} isDisabled={busy} />
                      <NumberField value={maxAttempts} onChange={(value) => setMaxAttempts(value ?? 3)} minValue={1} maxValue={64} isDisabled={busy || !allowFallbacks}>
                        <NumberField.Group>
                          <NumberField.DecrementButton />
                          <NumberField.Input aria-label={t('modelPoolMaxAttempts')} />
                          <NumberField.IncrementButton />
                        </NumberField.Group>
                      </NumberField>
                    </div>
                    <p className="text-caption leading-5 text-muted">{t('modelPoolMaxAttemptsHint')}</p>
                  </Field>
                </div>
              ) : null}

              {selectedStep?.kind === 'member' ? (
                <MemberFields
                  step={selectedStep}
                  t={t}
                  busy={busy}
                  showWeight={strategy === 'fill-first'}
                  providers={providerSelectOptions}
                  regions={regionOptions(descriptors, accounts, selectedStep.provider || '')}
                  models={modelOptions(models, selectedStep.provider || '', selectedStep.region || '', t)}
                  accounts={accounts}
                  onChange={(patch) => commitDoc((current) => updateStep(current, selectedStep.id, patch))}
                />
              ) : null}

              {selectedStep?.kind === 'group' ? (
                <div className="space-y-4">
                  <h2 className="text-body font-semibold">{t('modelPoolStepGroup')}</h2>
                  <Field label={t('modelPoolNestedPool')} hint={t('modelPoolStepGroupDetail')}>
                    <FilterSelect
                      value={selectedStep.aggregation_id || ''}
                      onChange={(value) => commitDoc((current) => updateStep(current, selectedStep.id, { aggregation_id: value }))}
                      ariaLabel={t('modelPoolNestedPool')}
                      options={groupOptions}
                      className="w-full"
                    />
                  </Field>
                  {strategy === 'fill-first' ? (
                    <WeightField t={t} busy={busy} value={selectedStep.weight || 100} onChange={(weight) => commitDoc((current) => updateStep(current, selectedStep.id, { weight }))} />
                  ) : null}
                </div>
              ) : null}

              {selectedStep?.kind === 'branch' ? (
                <div className="space-y-4">
                  <div className="flex items-center justify-between gap-2">
                    <h2 className="text-body font-semibold">{t('modelPoolStepIntent')}</h2>
                    <Button size="sm" variant="ghost" onPress={() => commitDoc((current) => addPath(current, selectedStep.id, t('modelPoolPathMatch')))}>
                      <Plus size={14} />{t('modelPoolAddPath')}
                    </Button>
                  </div>
                  <p className="text-caption leading-5 text-muted">{t('modelPoolStepIntentDetail')}</p>
                  <Field label={t('modelPoolClassifier')} hint={t('modelPoolClassifierHint')}>
                    <div className="space-y-2">
                      <FilterSelect
                        value={selectedStep.classifier?.provider || ''}
                        onChange={(value) => commitDoc((current) => updateStep(current, selectedStep.id, {
                          classifier: value ? { provider: value, region: '', model: '' } : undefined,
                        }))}
                        ariaLabel={t('modelPoolProvider')}
                        options={[{ id: '', label: t('modelPoolClassifierOff') }, ...providerSelectOptions]}
                        className="w-full"
                      />
                      <FilterSelect
                        value={selectedStep.classifier?.region || ''}
                        onChange={(value) => commitDoc((current) => updateStep(current, selectedStep.id, {
                          classifier: { provider: selectedStep.classifier?.provider || '', region: value, model: '' },
                        }))}
                        ariaLabel={t('modelPoolRegion')}
                        options={[{ id: '', label: t('modelPoolAnyRegion') }, ...regionOptions(descriptors, accounts, selectedStep.classifier?.provider || '')]}
                        className="w-full"
                      />
                      <FilterSelect
                        value={selectedStep.classifier?.model || ''}
                        onChange={(value) => commitDoc((current) => updateStep(current, selectedStep.id, {
                          classifier: { provider: selectedStep.classifier?.provider || '', region: selectedStep.classifier?.region || '', model: value },
                        }))}
                        ariaLabel={t('modelPoolModel')}
                        options={modelOptions(models, selectedStep.classifier?.provider || '', selectedStep.classifier?.region || '', t)}
                        className="w-full"
                      />
                    </div>
                  </Field>
                  {(selectedStep.paths ?? []).map((path) => (
                    <div key={path.id} className="space-y-3 rounded-xl bg-surface-secondary p-3">
                      <div className="flex items-center justify-between gap-2">
                        <Label className="text-caption text-muted">{t('modelPoolPathLabel')}</Label>
                        <Button
                          isIconOnly
                          size="sm"
                          variant="ghost"
                          aria-label={t('modelPoolRemovePath')}
                          isDisabled={(selectedStep.paths?.length ?? 0) <= 2}
                          onPress={() => commitDoc((current) => removePath(current, path.id))}
                        >
                          <TrashSimple size={14} />
                        </Button>
                      </div>
                      <Input
                        value={path.label}
                        aria-label={t('modelPoolPathLabel')}
                        onChange={(event) => commitDoc((current) => updatePath(current, path.id, { label: event.target.value }))}
                      />
                      <Field label={t('modelPoolKeywords')} hint={t('modelPoolKeywordsHint')}>
                        <Input
                          value={keywordDraft[path.id] ?? (path.keywords || []).join(', ')}
                          aria-label={t('modelPoolKeywords')}
                          onChange={(event) => {
                            const text = event.target.value
                            setKeywordDraft((current) => ({ ...current, [path.id]: text }))
                            const keywords = text.split(',').map((item) => item.trim()).filter(Boolean)
                            commitDoc((current) => updatePath(current, path.id, { keywords }))
                          }}
                        />
                      </Field>
                      <Field label={t('modelPoolIntentLabel')} hint={t('modelPoolIntentHint')}>
                        <Input
                          value={path.intent || ''}
                          aria-label={t('modelPoolIntentLabel')}
                          onChange={(event) => commitDoc((current) => updatePath(current, path.id, { intent: event.target.value }))}
                        />
                      </Field>
                      <CompactSwitch
                        isSelected={Boolean(path.fallback)}
                        ariaLabel={t('modelPoolPathFallback')}
                        label={t('modelPoolPathFallback')}
                        onChange={(selectedFlag) => {
                          if (selectedFlag) commitDoc((current) => setOnlyFallback(current, path.id))
                        }}
                      />
                    </div>
                  ))}
                </div>
              ) : null}

              <Separator />
              <CapabilityFields
                t={t}
                busy={busy}
                contextLength={contextLength}
                maxOutput={maxOutput}
                reasoningOptions={reasoningOptions}
                reasoningEffort={reasoningEffort}
                onContext={setContextLength}
                onOutput={setMaxOutput}
                onToggle={toggleReasoningLevel}
                onEffort={setReasoningEffort}
              />
            </div>
          </Card>

          <section className="min-w-0 space-y-3 xl:col-start-1">
            <div>
              <h2 className="text-body font-semibold">{t('modelPoolPreview')}</h2>
              <p className="mt-1 text-caption leading-5 text-muted">{t('modelPoolPreviewHint')}</p>
            </div>
            <TextArea
              className="min-h-24"
              value={message}
              onChange={(event) => setMessage(event.target.value)}
              placeholder={t('modelPoolPreviewPh')}
              aria-label={t('modelPoolPreview')}
            />
            <div className="flex items-center justify-between gap-3">
              <span className="text-caption text-muted">{preview?.path ? `${t('modelPoolPreviewPath')} · ${preview.path}` : ''}</span>
              <Button variant="secondary" onPress={() => void runPreview()} isPending={previewBusy}>{t('modelPoolPreviewRun')}</Button>
            </div>
            {previewError ? <PageAlert status="danger" title={previewError} /> : null}
            {preview ? (
              <RouteChain
                attempts={attempts}
                title={message.trim() || t('modelPoolPreview')}
                route={selfId || 'preview'}
                request={{ text: message.trim() }}
              />
            ) : null}
          </section>
        </div>
      )}
    </div>
  )
}

function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <div className="space-y-1.5">
      <Label className="text-caption text-muted">{label}</Label>
      {children}
      {hint ? <p className="text-caption leading-5 text-muted">{hint}</p> : null}
    </div>
  )
}

function WeightField({ t, busy, value, onChange }: { t: (key: string, vars?: Record<string, string | number>) => string; busy: boolean; value: number; onChange: (weight: number) => void }) {
  return (
    <Field label={t('modelPoolWeight')} hint={t('modelPoolWeightHint')}>
      <NumberField value={value} onChange={(next) => onChange(next ?? 100)} minValue={1} maxValue={1000000} isDisabled={busy}>
        <NumberField.Group>
          <NumberField.DecrementButton />
          <NumberField.Input aria-label={t('modelPoolWeight')} />
          <NumberField.IncrementButton />
        </NumberField.Group>
      </NumberField>
    </Field>
  )
}

function MemberFields({
  step,
  t,
  busy,
  showWeight,
  providers,
  regions,
  models,
  accounts,
  onChange,
}: {
  step: PipelineStep
  t: (key: string, vars?: Record<string, string | number>) => string
  busy: boolean
  showWeight: boolean
  providers: FilterSelectOption[]
  regions: FilterSelectOption[]
  models: FilterSelectOption[]
  accounts: AccountRow[]
  onChange: (patch: Partial<PipelineStep>) => void
}) {
  const chosen = selectedAccountIds(step)
  const providerAccounts = accountsForProvider(accounts, step.provider || '', step.region || '')
  const modelChoices = models.filter((item) => item.id)
  function setAccounts(ids: string[]) {
    onChange({ account_ids: ids, account_id: ids.length === 1 ? ids[0] : undefined })
  }
  return (
    <div className="space-y-4">
      <h2 className="text-body font-semibold">{t('modelPoolStepProvider')}</h2>
      <Field label={t('modelPoolProvider')}>
        <FilterSelect
          value={step.provider || ''}
          onChange={(value) => onChange({ provider: value, region: '', model: '', account_id: undefined, account_ids: [] })}
          ariaLabel={t('modelPoolProvider')}
          options={providers}
          className="w-full"
        />
      </Field>
      {regions.length > 1 ? (
        <Field label={t('modelPoolRegion')}>
          <FilterSelect
            value={step.region || ''}
            onChange={(value) => {
              const allowed = new Set(accountsForProvider(accounts, step.provider || '', value).map((account) => account.id))
              const ids = chosen.filter((id) => allowed.has(id))
              onChange({ region: value, model: '', account_ids: ids, account_id: ids.length === 1 ? ids[0] : undefined })
            }}
            ariaLabel={t('modelPoolRegion')}
            options={[{ id: '', label: t('modelPoolAnyRegion') }, ...regions]}
            className="w-full"
          />
        </Field>
      ) : null}
      {step.provider ? (
        <Field label={t('modelPoolAccounts')} hint={t('modelPoolAccountsHint')}>
          <AccountChoices accounts={providerAccounts} selected={chosen} busy={busy} t={t} onChange={setAccounts} />
        </Field>
      ) : null}
      <Field label={t('modelPoolModel')}>
        <FilterSelect
          value={step.model || ''}
          onChange={(value) => onChange({ model: value })}
          ariaLabel={t('modelPoolModel')}
          options={modelChoices.length ? modelChoices : [{ id: '', label: t('modelPoolNoModels') }]}
          className="w-full"
          isDisabled={!step.provider || modelChoices.length === 0}
        />
      </Field>
      {showWeight ? <WeightField t={t} busy={busy} value={step.weight || 100} onChange={(weight) => onChange({ weight })} /> : null}
    </div>
  )
}

function CapabilityFields({
  t,
  busy,
  contextLength,
  maxOutput,
  reasoningOptions,
  reasoningEffort,
  onContext,
  onOutput,
  onToggle,
  onEffort,
}: {
  t: (key: string, vars?: Record<string, string | number>) => string
  busy: boolean
  contextLength: number
  maxOutput: number
  reasoningOptions: string[]
  reasoningEffort: string
  onContext: (value: number) => void
  onOutput: (value: number) => void
  onToggle: (level: string) => void
  onEffort: (value: string) => void
}) {
  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-body font-semibold">{t('modelPoolCapabilities')}</h2>
        <p className="mt-1 text-caption leading-5 text-muted">{t('modelPoolCapabilitiesHint')}</p>
      </div>
      <Field label={t('modelPoolContextLength')} hint={t('modelPoolContextLengthHint')}>
        <NumberField value={contextLength} onChange={(value) => onContext(value ?? 0)} minValue={0} maxValue={4000000} isDisabled={busy}>
          <NumberField.Group>
            <NumberField.DecrementButton />
            <NumberField.Input aria-label={t('modelPoolContextLength')} />
            <NumberField.IncrementButton />
          </NumberField.Group>
        </NumberField>
      </Field>
      <Field label={t('modelPoolMaxOutput')} hint={t('modelPoolMaxOutputHint')}>
        <NumberField value={maxOutput} onChange={(value) => onOutput(value ?? 0)} minValue={0} maxValue={4000000} isDisabled={busy}>
          <NumberField.Group>
            <NumberField.DecrementButton />
            <NumberField.Input aria-label={t('modelPoolMaxOutput')} />
            <NumberField.IncrementButton />
          </NumberField.Group>
        </NumberField>
      </Field>
      <Field label={t('modelPoolReasoningLevels')} hint={t('modelPoolReasoningLevelsHint')}>
        <div className="flex flex-wrap gap-2">
          {REASONING_LEVELS.map((level) => {
            const selected = reasoningOptions.includes(level)
            return (
              <Button key={level} size="sm" variant={selected ? 'secondary' : 'ghost'} aria-pressed={selected} isDisabled={busy} onPress={() => onToggle(level)}>
                <span className="mono">{level}</span>
              </Button>
            )
          })}
        </div>
      </Field>
      <Field label={t('modelPoolReasoningDefault')} hint={t('modelPoolReasoningDefaultHint')}>
        <FilterSelect
          value={reasoningEffort}
          onChange={onEffort}
          ariaLabel={t('modelPoolReasoningDefault')}
          options={[{ id: '', label: t('modelPoolReasoningAuto') }, ...reasoningOptions.map((level) => ({ id: level, label: level }))]}
          className="w-full"
          isDisabled={busy || reasoningOptions.length === 0}
        />
      </Field>
    </div>
  )
}

function requestToken(name: string, id: string) {
  const token = id.trim()
  if (!token || token === name.trim()) return undefined
  return token
}

function seatCopy(
  step: PipelineStep,
  showWeight: boolean,
  t: (key: string, vars?: Record<string, string | number>) => string,
  models: ModelInfo[],
  aggregations: AggregationRecord[],
  accounts: AccountRow[],
) {
  const weight = showWeight ? t('modelPoolWeightNote', { weight: step.weight || 100 }) : ''
  if (step.kind === 'group') {
    const pool = aggregations.find((item) => item.id === step.aggregation_id)
    const ready = Boolean(step.aggregation_id)
    return {
      title: pool?.name || step.aggregation_id || t('modelPoolStepGroup'),
      detail: ready ? t('modelPoolStepGroupNote') : t('modelPoolStepGroupDetail'),
      note: ready ? '' : t('modelPoolStepGroupNote'),
      facts: ready ? [t('modelPoolSeatCount', { count: pool?.members.length ?? 0 }), weight].filter(Boolean) : [],
      chips: ready ? [{ text: step.aggregation_id || '', tone: 'plain' as const }] : [],
    }
  }
  if (step.kind === 'branch') {
    const paths = (step.paths ?? []).map((path) => path.label).filter(Boolean)
    return {
      title: t('modelPoolStepIntent'),
      detail: t('modelPoolStepIntentNote'),
      note: '',
      facts: [t('modelPoolPathCount', { count: paths.length })],
      chips: [
        ...(step.classifier?.model ? [{ text: modelTitle(models, step.classifier.model), tone: 'model' as const }] : []),
        ...paths.map((text) => ({ text, tone: 'plain' as const })),
      ],
    }
  }
  const ids = selectedAccountIds(step)
  const pool = accountsForProvider(accounts, step.provider || '', step.region || '')
  const names = ids.map((id) => accounts.find((account) => account.id === id)?.name || id)
  if (!step.provider) {
    return {
      title: t('modelPoolStepProvider'),
      detail: t('modelPoolStepProviderDetail'),
      note: t('modelPoolStepProviderDetail'),
      facts: [],
      chips: [],
    }
  }
  return {
    title: accountProviderLabel(step.provider, step.region || '', t),
    detail: '',
    note: '',
    facts: [
      ids.length === 0 ? t('modelPoolSeatOrderAll') : t('modelPoolSeatOrderPicked'),
      ids.length === 0 ? '' : t('modelPoolAccountsCount', { count: ids.length }),
      weight,
    ].filter(Boolean),
    chips: [
      ...(step.model ? [{ text: modelTitle(models, step.model), tone: 'model' as const }] : []),
      ...(names.length === 0
        ? [{ text: pool.length ? t('modelPoolAccountsAllCount', { count: pool.length }) : t('modelPoolAccountsAll'), tone: 'account' as const }]
        : names.map((text) => ({ text, tone: 'account' as const }))),
    ],
  }
}

function accountsForProvider(accounts: AccountRow[], provider: string, region: string) {
  if (!provider) return []
  const wanted = normalizeProvider(provider)
  const wantedRegion = region ? normalizeRegion(region) : ''
  return accounts
    .filter((account) => account.enabled !== false && normalizeProvider(account.provider) === wanted)
    .filter((account) => !wantedRegion || normalizeRegion(account.region) === wantedRegion)
    .sort((a, b) => (a.priority ?? 0) - (b.priority ?? 0) || (a.name || a.id).localeCompare(b.name || b.id))
}

