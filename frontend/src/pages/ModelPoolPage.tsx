import { useCallback, useEffect, useMemo, useState } from 'react'
import { Navigate } from 'react-router-dom'
import {
  Alert,
  Button,
  Card,
  Chip,
  Description,
  Form,
  Input,
  Label,
  Modal,
  NumberField,
  Table,
} from '@heroui/react'
import { ArrowDown, ArrowUp, Plus, TrashSimple, X } from '@phosphor-icons/react'
import {
  createAggregation,
  deleteAggregation,
  fetchAggregations,
  updateAggregation,
  AGGREGATION_STRATEGIES,
  type AggregationInput,
  type AggregationMember,
  type AggregationRecord,
} from '@/api/modelPools'
import { fetchAccounts, fetchModels, fetchProviders, type ProviderDescriptor } from '@/api/overview'
import type { ModelInfo, Overview } from '@/api/types'
import { ProviderMark } from '@/components/ProviderMark'
import { CompactSwitch } from '@/components/ui/CompactSwitch'
import { ConfirmDialog } from '@/components/ui/ConfirmDialog'
import { EmptyPanel } from '@/components/ui/EmptyPanel'
import { FilterSelect, type FilterSelectOption } from '@/components/ui/FilterSelect'
import { FormRow } from '@/components/ui/FormRow'
import { PageAlert } from '@/components/ui/PageAlert'
import { SkeletonBlock } from '@/components/ui/PageSkeletons'
import { useCrossProviderPool } from '@/hooks/useCrossProviderPool'
import { useI18n } from '@/hooks/useI18n'
import { toast } from '@/lib/toast'
import { accountProviderLabel } from '@/lib/provider'

type TFunc = (key: string, vars?: Record<string, string | number>) => string
type AccountRow = NonNullable<Overview['accounts']>[number]

const REASONING_LEVELS = ['none', 'low', 'medium', 'high', 'xhigh', 'max'] as const

type MemberDraft = AggregationMember & { key: string }

function newMemberDraft(): MemberDraft {
  return { key: `${Date.now()}-${Math.random()}`, provider: '', region: '', model: '', account_id: '', priority: 0, weight: 100 }
}

function normalizeProvider(value?: string) {
  return String(value || 'qoder').trim().toLowerCase()
}

function normalizeRegion(value?: string) {
  const region = String(value || '').trim().toLowerCase()
  return region === '' || region === 'gl' ? 'global' : region
}

function providerOptions(descriptors: ProviderDescriptor[], accounts: AccountRow[]): FilterSelectOption[] {
  const connected = new Set(accounts.map((account) => normalizeProvider(account.provider)))
  return descriptors
    .filter((d) => connected.has(normalizeProvider(d.id)))
    .map((d) => ({ id: d.id, label: d.label }))
}

function regionOptions(descriptors: ProviderDescriptor[], accounts: AccountRow[], provider: string): FilterSelectOption[] {
  const descriptor = descriptors.find((d) => d.id === provider)
  if (!descriptor) return []
  const connected = new Set(
    accounts
      .filter((account) => normalizeProvider(account.provider) === normalizeProvider(provider))
      .map((account) => normalizeRegion(account.region)),
  )
  return (descriptor.regions || [])
    .filter((r) => connected.has(normalizeRegion(r.id)))
    .map((r) => ({ id: r.id, label: r.label }))
}

// Models come from the console catalog, already filtered by the connected
// accounts' provider policy. Region narrowing keeps a provider's CN policy from
// leaking a Global-only model into this member.
function modelOptions(models: ModelInfo[], provider: string, region: string, t: TFunc): FilterSelectOption[] {
  const seen = new Set<string>()
  const out: FilterSelectOption[] = []
  const wantedProvider = String(provider || '').trim().toLowerCase()
  const wantedRegion = String(region || '').trim() ? normalizeRegion(region) : ''
  for (const model of models) {
    const taggedProvider = String(model.provider || model.owned_by || '').trim().toLowerCase()
    if (taggedProvider && taggedProvider !== wantedProvider) continue
    if (wantedRegion) {
      const modelRegions = [model.region, ...(model.regions || [])]
        .map((value) => normalizeRegion(value))
        .filter(Boolean)
      if (modelRegions.length && !modelRegions.includes(wantedRegion)) continue
    }
    const id = model.id
    if (!id || seen.has(id)) continue
    seen.add(id)
    const label = model.display_name || model.route_display_name || id
    out.push({ id, label: `${label} (${id})` })
  }
  if (!out.length) out.push({ id: '', label: t('modelPoolNoModels') })
  return out
}

function strategyLabel(value: string, t: TFunc): string {
  return t(`modelPoolStrategy_${value}`)
}

export function ModelPoolPage() {
  const { t } = useI18n()
  const { enabled, loading: poolLoading } = useCrossProviderPool()
  const [aggregations, setAggregations] = useState<AggregationRecord[]>([])
  const [descriptors, setDescriptors] = useState<ProviderDescriptor[]>([])
  const [models, setModels] = useState<ModelInfo[]>([])
  const [accounts, setAccounts] = useState<AccountRow[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [createOpen, setCreateOpen] = useState(false)
  const [editTarget, setEditTarget] = useState<AggregationRecord | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<AggregationRecord | null>(null)
  const [deleteBusy, setDeleteBusy] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [aggResult, providerResult, modelResult, accountResult] = await Promise.all([
        fetchAggregations(),
        fetchProviders(),
        fetchModels(undefined, false, 'regional'),
        fetchAccounts(),
      ])
      setAggregations(aggResult.data || [])
      setDescriptors(providerResult.data || [])
      setModels(modelResult.data || [])
      setAccounts(accountResult.data || [])
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (!enabled || poolLoading) return
    void load()
  }, [enabled, poolLoading, load])

  async function toggleEnabled(agg: AggregationRecord, next: boolean) {
    const previous = agg.enabled
    setAggregations((current) => current.map((item) => item.id === agg.id ? { ...item, enabled: next } : item))
    try {
      const updated = await updateAggregation(agg.id, { enabled: next })
      setAggregations((current) => current.map((item) => item.id === agg.id ? updated : item))
      toast({ type: 'success', title: t('modelPoolSaved') })
    } catch (err) {
      setAggregations((current) => current.map((item) => item.id === agg.id ? { ...item, enabled: previous } : item))
      toast({ type: 'error', title: err instanceof Error ? err.message : String(err) })
    }
  }

  async function confirmDelete() {
    if (!deleteTarget) return
    setDeleteBusy(true)
    try {
      await deleteAggregation(deleteTarget.id)
      setAggregations((current) => current.filter((item) => item.id !== deleteTarget.id))
      toast({ type: 'success', title: t('modelPoolDeleted') })
      setDeleteTarget(null)
    } catch (err) {
      toast({ type: 'error', title: err instanceof Error ? err.message : String(err) })
    } finally {
      setDeleteBusy(false)
    }
  }

  if (poolLoading) return <SkeletonBlock className="h-64 w-full rounded-xl" />
  if (!enabled) return <Navigate to="/" replace />

  return (
    <div className="space-y-6">
      <section className="flex flex-wrap items-end justify-between gap-4 pb-4">
        <div>
          <h1 className="text-title font-semibold tracking-tight">{t('modelPoolTitle')}</h1>
          <p className="mt-1 max-w-2xl text-caption text-muted">{t('modelPoolLead')}</p>
        </div>
        <Button onPress={() => setCreateOpen(true)}>
          <Plus size={16} />{t('modelPoolCreate')}
        </Button>
      </section>

      {error ? <PageAlert status="danger" title={error} /> : null}

      <Card className="overflow-hidden">
        {loading ? (
          <div className="space-y-3 p-5"><SkeletonBlock className="h-10 w-full" /><SkeletonBlock className="h-10 w-full" /><SkeletonBlock className="h-10 w-full" /></div>
        ) : aggregations.length === 0 ? (
          <EmptyPanel title={t('modelPoolEmpty')} hint={t('modelPoolEmptyHint')} icon={<ArrowUp size={28} />} />
        ) : (
          <Table>
            <Table.ScrollContainer>
              <Table.Content aria-label={t('modelPoolTitle')}>
                <Table.Header>
                  <Table.Column>{t('modelPoolColName')}</Table.Column>
                  <Table.Column>{t('modelPoolColStrategy')}</Table.Column>
                  <Table.Column>{t('modelPoolColMembers')}</Table.Column>
                  <Table.Column>{t('modelPoolColFailover')}</Table.Column>
                  <Table.Column>{t('modelPoolColEnabled')}</Table.Column>
                  <Table.Column>{t('modelPoolColActions')}</Table.Column>
                </Table.Header>
                <Table.Body>
                  {aggregations.map((agg) => (
                    <Table.Row key={agg.id}>
                      <Table.Cell>
                        <div className="min-w-0">
                          <div className="font-medium">{agg.name}</div>
                          <div className="mono text-caption text-muted">{agg.id}</div>
                        </div>
                      </Table.Cell>
                      <Table.Cell><Chip size="sm" variant="soft">{strategyLabel(agg.strategy, t)}</Chip></Table.Cell>
                      <Table.Cell><span className="mono text-caption">{agg.members.length}</span></Table.Cell>
                      <Table.Cell>
                        <span className="text-caption text-muted">
                          {agg.allow_fallbacks ? t('modelPoolFailoverOn', { count: agg.max_attempts }) : t('modelPoolFailoverOff')}
                        </span>
                      </Table.Cell>
                      <Table.Cell>
                        <CompactSwitch isSelected={agg.enabled} ariaLabel={`${t('modelPoolColEnabled')} ${agg.id}`} onChange={(v) => void toggleEnabled(agg, v)} />
                      </Table.Cell>
                      <Table.Cell>
                        <div className="flex items-center gap-1">
                          <Button size="sm" variant="ghost" onPress={() => setEditTarget(agg)}>{t('edit')}</Button>
                          <Button isIconOnly size="sm" variant="ghost" aria-label={t('delete')} onPress={() => setDeleteTarget(agg)}><TrashSimple size={16} /></Button>
                        </div>
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table.Content>
            </Table.ScrollContainer>
          </Table>
        )}
      </Card>

      {(createOpen || editTarget) && (
        <AggregationEditorModal
          isOpen
          initial={editTarget}
          descriptors={descriptors}
          models={models}
          accounts={accounts}
          t={t}
          onClose={() => { setCreateOpen(false); setEditTarget(null) }}
          onSave={async (input) => {
            if (editTarget) await updateAggregation(editTarget.id, input)
            else await createAggregation(input)
            toast({ type: 'success', title: t('modelPoolSaved') })
            setCreateOpen(false)
            setEditTarget(null)
            await load()
          }}
        />
      )}

      <ConfirmDialog
        isOpen={Boolean(deleteTarget)}
        title={t('modelPoolDeleteTitle')}
        description={deleteTarget ? t('modelPoolDeleteDesc', { name: deleteTarget.name }) : ''}
        confirmLabel={t('delete')}
        cancelLabel={t('cancel')}
        closeLabel={t('close')}
        isPending={deleteBusy}
        onClose={() => setDeleteTarget(null)}
        onConfirm={() => void confirmDelete()}
      />
    </div>
  )
}

function AggregationEditorModal({
  isOpen,
  initial,
  descriptors,
  models,
  accounts,
  t,
  onClose,
  onSave,
}: {
  isOpen: boolean
  initial: AggregationRecord | null
  descriptors: ProviderDescriptor[]
  models: ModelInfo[]
  accounts: AccountRow[]
  t: TFunc
  onClose: () => void
  onSave: (input: AggregationInput) => Promise<void>
}) {
  const [id, setId] = useState('')
  const [name, setName] = useState('')
  const [strategy, setStrategy] = useState<string>('priority')
  const [allowFallbacks, setAllowFallbacks] = useState(true)
  const [maxAttempts, setMaxAttempts] = useState(3)
  const [contextLength, setContextLength] = useState(0)
  const [maxOutput, setMaxOutput] = useState(0)
  const [reasoningOptions, setReasoningOptions] = useState<string[]>([])
  const [reasoningEffort, setReasoningEffort] = useState('')
  const [members, setMembers] = useState<MemberDraft[]>([])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!isOpen) return
    setId(initial?.id || '')
    setName(initial?.name || '')
    setStrategy(initial?.strategy || 'priority')
    setAllowFallbacks(initial?.allow_fallbacks ?? true)
    setMaxAttempts(initial?.max_attempts ?? 3)
    setContextLength(initial?.context_length ?? 0)
    setMaxOutput(initial?.max_output_tokens ?? 0)
    setReasoningOptions(initial?.reasoning_options ?? [])
    setReasoningEffort(initial?.reasoning_effort ?? '')
    setMembers((initial?.members || []).map((m) => ({ ...m, key: `${m.provider}-${m.model}-${Math.random()}` })))
    setError('')
  }, [initial, isOpen])

  const strategyOptions = useMemo<FilterSelectOption[]>(
    () => AGGREGATION_STRATEGIES.map((s) => ({ id: s, label: strategyLabel(s, t) })),
    [t],
  )
  const providerSelectOptions = useMemo(() => providerOptions(descriptors, accounts), [descriptors, accounts])

  function updateMember(key: string, patch: Partial<MemberDraft>) {
    setMembers((current) => current.map((m) => m.key === key ? { ...m, ...patch } : m))
  }

  function moveMember(index: number, delta: number) {
    setMembers((current) => {
      const next = [...current]
      const target = index + delta
      if (target < 0 || target >= next.length) return current
      ;[next[index], next[target]] = [next[target], next[index]]
      // Recompute priority from order so the backend order matches the UI.
      return next.map((m, i) => ({ ...m, priority: i }))
    })
  }

  function toggleReasoningLevel(level: string) {
    const next = reasoningOptions.includes(level)
      ? reasoningOptions.filter((item) => item !== level)
      : [...reasoningOptions, level]
    setReasoningOptions(next)
    if (reasoningEffort && !next.includes(reasoningEffort)) setReasoningEffort('')
  }

  async function submit(event?: { preventDefault(): void }) {
    event?.preventDefault()
    if (!initial) {
      const trimmed = id.trim().toLowerCase()
      if (!trimmed) { setError(t('modelPoolIdRequired')); return }
      if (!/^[a-z0-9.\-\/]+$/.test(trimmed)) { setError(t('modelPoolIdInvalid')); return }
    }
    if (!members.length) { setError(t('modelPoolMembersRequired')); return }
    if (members.some((m) => !m.provider || !m.model)) { setError(t('modelPoolMemberIncomplete')); return }
    if (!Number.isInteger(maxAttempts) || maxAttempts < 1 || maxAttempts > 64) {
      setError(t('modelPoolAttemptsInvalid')); return
    }
    if (!Number.isInteger(contextLength) || contextLength < 0 || (contextLength > 0 && contextLength < 1024)) {
      setError(t('modelPoolCapabilitiesInvalid')); return
    }
    if (!Number.isInteger(maxOutput) || maxOutput < 0 || (maxOutput > 0 && maxOutput < 1024) || (contextLength > 0 && maxOutput > contextLength)) {
      setError(t('modelPoolCapabilitiesInvalid')); return
    }
    if (reasoningEffort && !reasoningOptions.includes(reasoningEffort)) {
      setError(t('modelPoolReasoningInvalid')); return
    }
    setBusy(true)
    setError('')
    try {
      const payload: AggregationInput = {
        name: name.trim() || (initial?.id || id.trim().toLowerCase()),
        strategy,
        allow_fallbacks: allowFallbacks,
        max_attempts: maxAttempts,
        context_length: contextLength,
        max_output_tokens: maxOutput,
        reasoning_options: reasoningOptions,
        reasoning_effort: reasoningEffort,
        members: members.map((m, i) => ({
          provider: m.provider,
          region: m.region || '',
          model: m.model,
          // Accounts are selected by the provider pool at request time. The member
          // is provider-wide, never pinned to one account from this editor.
          account_id: '',
          priority: i,
          weight: m.weight || 100,
        })),
      }
      if (!initial) payload.id = id.trim().toLowerCase()
      await onSave(payload)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal.Root isOpen={isOpen} onOpenChange={(open: boolean) => { if (!open && !busy) onClose() }}>
      <Modal.Backdrop variant="blur">
        <Modal.Container placement="center" size="lg" scroll="inside">
          <Modal.Dialog className="sm:min-w-[40rem]">
            <Form className="flex min-h-0 flex-1 flex-col" onSubmit={submit}>
              <Modal.Header className="items-start justify-between gap-4 px-6 pt-6">
                <div>
                  <Modal.Heading className="text-section font-semibold">{initial ? t('modelPoolEdit') : t('modelPoolCreate')}</Modal.Heading>
                  <p className="mt-1.5 text-body leading-6 text-muted">{t('modelPoolEditorHint')}</p>
                </div>
                <Modal.CloseTrigger isDisabled={busy} aria-label={t('close')} className="grid size-9 place-items-center rounded-lg text-muted hover:bg-surface-secondary"><X size={18} /></Modal.CloseTrigger>
              </Modal.Header>
              <Modal.Body className="space-y-5 px-6 pb-2 pt-1">
                {!initial ? (
                  <FormRow label={t('modelPoolId')} hint={t('modelPoolIdHint')}>
                    <Input value={id} onChange={(e) => setId(e.target.value)} placeholder="my-claude" aria-label={t('modelPoolId')} disabled={busy} />
                  </FormRow>
                ) : null}
                <FormRow label={t('modelPoolName')} hint={t('modelPoolNameHint')}>
                  <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={t('modelPoolNamePh')} aria-label={t('modelPoolName')} disabled={busy} />
                </FormRow>
                <FormRow label={t('modelPoolStrategy')} hint={t('modelPoolStrategyHint')}>
                  <FilterSelect value={strategy} onChange={setStrategy} ariaLabel={t('modelPoolStrategy')} options={strategyOptions} className="w-full" />
                </FormRow>
                <FormRow label={t('modelPoolFailover')}>
                  <div className="flex flex-wrap items-center gap-4">
                    <CompactSwitch isSelected={allowFallbacks} ariaLabel={t('modelPoolFailover')} label={t('modelPoolFailover')} onChange={setAllowFallbacks} isDisabled={busy} />
                    <NumberField value={maxAttempts} onChange={(v) => setMaxAttempts(v ?? 3)} minValue={1} maxValue={64} isDisabled={busy || !allowFallbacks}>
                      <NumberField.Group>
                        <NumberField.DecrementButton />
                        <NumberField.Input aria-label={t('modelPoolMaxAttempts')} />
                        <NumberField.IncrementButton />
                      </NumberField.Group>
                    </NumberField>
                    <Description className="text-caption text-muted">{t('modelPoolMaxAttemptsHint')}</Description>
                  </div>
                </FormRow>

                <div className="space-y-3">
                  <div>
                    <Label className="text-body font-medium text-muted">{t('modelPoolCapabilities')}</Label>
                    <Description className="text-caption leading-5 text-muted">{t('modelPoolCapabilitiesHint')}</Description>
                  </div>
                  <div className="grid gap-3 sm:grid-cols-2">
                    <FormRow label={t('modelPoolContextLength')} hint={t('modelPoolContextLengthHint')} align="start">
                      <NumberField value={contextLength} onChange={(v) => setContextLength(v ?? 0)} minValue={0} maxValue={4000000} isDisabled={busy}>
                        <NumberField.Group>
                          <NumberField.DecrementButton />
                          <NumberField.Input aria-label={t('modelPoolContextLength')} />
                          <NumberField.IncrementButton />
                        </NumberField.Group>
                      </NumberField>
                    </FormRow>
                    <FormRow label={t('modelPoolMaxOutput')} hint={t('modelPoolMaxOutputHint')} align="start">
                      <NumberField value={maxOutput} onChange={(v) => setMaxOutput(v ?? 0)} minValue={0} maxValue={4000000} isDisabled={busy}>
                        <NumberField.Group>
                          <NumberField.DecrementButton />
                          <NumberField.Input aria-label={t('modelPoolMaxOutput')} />
                          <NumberField.IncrementButton />
                        </NumberField.Group>
                      </NumberField>
                    </FormRow>
                  </div>
                  <FormRow label={t('modelPoolReasoningLevels')} hint={t('modelPoolReasoningLevelsHint')} align="start">
                    <div className="flex flex-wrap gap-2">
                      {REASONING_LEVELS.map((level) => {
                        const selected = reasoningOptions.includes(level)
                        return (
                          <Button key={level} size="sm" variant={selected ? 'secondary' : 'ghost'} aria-pressed={selected} isDisabled={busy} onPress={() => toggleReasoningLevel(level)}>
                            <span className="mono">{level}</span>
                          </Button>
                        )
                      })}
                    </div>
                  </FormRow>
                  <FormRow label={t('modelPoolReasoningDefault')} hint={t('modelPoolReasoningDefaultHint')} align="start">
                    <FilterSelect
                      value={reasoningEffort}
                      onChange={setReasoningEffort}
                      ariaLabel={t('modelPoolReasoningDefault')}
                      options={[{ id: '', label: t('modelPoolReasoningAuto') }, ...reasoningOptions.map((level) => ({ id: level, label: level }))]}
                      className="w-full"
                      isDisabled={busy || reasoningOptions.length === 0}
                    />
                  </FormRow>
                </div>

                <div className="space-y-3">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <Label className="text-body font-medium text-muted">{t('modelPoolMembers')}</Label>
                      <Description className="text-caption leading-5 text-muted">{t('modelPoolMembersHint')}</Description>
                    </div>
                    <Button size="sm" variant="ghost" onPress={() => setMembers((current) => [...current, newMemberDraft()])} isDisabled={busy}><Plus size={14} />{t('modelPoolAddMember')}</Button>
                  </div>
                  {members.length === 0 ? (
                    <p className="rounded-lg bg-surface-secondary px-3 py-2.5 text-caption text-muted">{t('modelPoolMembersEmpty')}</p>
                  ) : (
                    <div className="space-y-2">
                      {members.map((member, index) => {
                        const regions = regionOptions(descriptors, accounts, member.provider)
                        const modelChoices = modelOptions(models, member.provider, member.region || '', t)
                        return (
                          <div key={member.key} className="rounded-xl bg-surface-secondary p-3">
                            <div className="mb-2 flex items-center justify-between gap-2">
                              <span className="text-caption font-medium text-muted">#{index + 1}</span>
                              <div className="flex items-center gap-1">
                                <Button isIconOnly size="sm" variant="ghost" aria-label={t('modelPoolMoveUp')} onPress={() => moveMember(index, -1)} isDisabled={busy || index === 0}><ArrowUp size={15} /></Button>
                                <Button isIconOnly size="sm" variant="ghost" aria-label={t('modelPoolMoveDown')} onPress={() => moveMember(index, 1)} isDisabled={busy || index === members.length - 1}><ArrowDown size={15} /></Button>
                                <Button isIconOnly size="sm" variant="ghost" aria-label={t('delete')} onPress={() => setMembers((current) => current.filter((m) => m.key !== member.key))} isDisabled={busy}><TrashSimple size={15} /></Button>
                              </div>
                            </div>
                            <div className="grid gap-2 sm:grid-cols-2">
                              <div className="space-y-1">
                                <Label className="text-caption text-muted">{t('modelPoolProvider')}</Label>
                                <FilterSelect value={member.provider} onChange={(v) => updateMember(member.key, { provider: v, region: '', model: '', account_id: '' })} ariaLabel={t('modelPoolProvider')} options={providerSelectOptions} className="w-full" />
                              </div>
                              <div className="space-y-1">
                                <Label className="text-caption text-muted">{t('modelPoolRegion')}</Label>
                                <FilterSelect value={member.region || ''} onChange={(v) => updateMember(member.key, { region: v, model: '', account_id: '' })} ariaLabel={t('modelPoolRegion')} options={[{ id: '', label: t('modelPoolAnyRegion') }, ...regions]} className="w-full" />
                              </div>
                              <div className="space-y-1 sm:col-span-2">
                                <Label className="text-caption text-muted">{t('modelPoolModel')}</Label>
                                <FilterSelect value={member.model} onChange={(v) => updateMember(member.key, { model: v })} ariaLabel={t('modelPoolModel')} options={modelChoices} className="w-full" />
                              </div>
                              <div className="space-y-1 sm:col-span-2">
                                <Label className="text-caption text-muted">{t('modelPoolWeight')}</Label>
                                <NumberField value={member.weight} onChange={(v) => updateMember(member.key, { weight: v ?? 100 })} minValue={1} maxValue={1000000} isDisabled={busy}>
                                  <NumberField.Group>
                                    <NumberField.DecrementButton />
                                    <NumberField.Input aria-label={t('modelPoolWeight')} />
                                    <NumberField.IncrementButton />
                                  </NumberField.Group>
                                </NumberField>
                              </div>
                            </div>
                            {member.provider ? (
                              <div className="mt-2 flex items-center gap-2 text-caption text-muted">
                                <ProviderMark provider={member.provider} size={14} />
                                <span>{accountProviderLabel(member.provider, member.region || '', t)}</span>
                              </div>
                            ) : null}
                          </div>
                        )
                      })}
                    </div>
                  )}
                </div>
              </Modal.Body>
              {error ? (
                <div className="px-6 pb-2">
                  <Alert status="danger"><Alert.Indicator /><Alert.Content><Alert.Title>{error}</Alert.Title></Alert.Content></Alert>
                </div>
              ) : null}
              <Modal.Footer className="justify-end">
                <Button variant="ghost" isDisabled={busy} onPress={onClose}>{t('cancel')}</Button>
                <Button type="submit" isPending={busy}>{initial ? t('save') : t('create')}</Button>
              </Modal.Footer>
            </Form>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal.Root>
  )
}
