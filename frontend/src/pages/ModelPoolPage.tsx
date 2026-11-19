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
import { fetchModels, fetchProviders, type ProviderDescriptor } from '@/api/overview'
import type { ModelInfo } from '@/api/types'
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

type MemberDraft = AggregationMember & { key: string }

function newMemberDraft(): MemberDraft {
  return { key: `${Date.now()}-${Math.random()}`, provider: '', region: '', model: '', priority: 0, weight: 100 }
}

function providerOptions(descriptors: ProviderDescriptor[]): FilterSelectOption[] {
  return descriptors.map((d) => ({ id: d.id, label: d.label }))
}

function regionOptions(descriptors: ProviderDescriptor[], provider: string): FilterSelectOption[] {
  const descriptor = descriptors.find((d) => d.id === provider)
  return (descriptor?.regions || []).map((r) => ({ id: r.id, label: r.label }))
}

// Models are catalog entries with an optional provider field. When a model has
// no provider tag it is treated as available on every provider (shared/bare
// catalog), so it can still be selected as an aggregation member.
function modelOptions(models: ModelInfo[], provider: string, t: TFunc): FilterSelectOption[] {
  const seen = new Set<string>()
  const out: FilterSelectOption[] = []
  for (const model of models) {
    if (model.provider && model.provider !== provider) continue
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
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [createOpen, setCreateOpen] = useState(false)
  const [editTarget, setEditTarget] = useState<AggregationRecord | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<AggregationRecord | null>(null)
  const [deleteBusy, setDeleteBusy] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [aggResult, providerResult, modelResult] = await Promise.all([
        fetchAggregations(),
        fetchProviders(),
        fetchModels(),
      ])
      setAggregations(aggResult.data || [])
      setDescriptors(providerResult.data || [])
      setModels(modelResult.data || [])
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
  t,
  onClose,
  onSave,
}: {
  isOpen: boolean
  initial: AggregationRecord | null
  descriptors: ProviderDescriptor[]
  models: ModelInfo[]
  t: TFunc
  onClose: () => void
  onSave: (input: AggregationInput) => Promise<void>
}) {
  const [id, setId] = useState('')
  const [name, setName] = useState('')
  const [strategy, setStrategy] = useState<string>('priority')
  const [allowFallbacks, setAllowFallbacks] = useState(true)
  const [maxAttempts, setMaxAttempts] = useState(3)
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
    setMembers((initial?.members || []).map((m) => ({ ...m, key: `${m.provider}-${m.model}-${Math.random()}` })))
    setError('')
  }, [initial, isOpen])

  const strategyOptions = useMemo<FilterSelectOption[]>(
    () => AGGREGATION_STRATEGIES.map((s) => ({ id: s, label: strategyLabel(s, t) })),
    [t],
  )
  const providerSelectOptions = useMemo(() => providerOptions(descriptors), [descriptors])

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

  async function submit(event?: { preventDefault(): void }) {
    event?.preventDefault()
    if (!initial) {
      const trimmed = id.trim().toLowerCase()
      if (!trimmed) { setError(t('modelPoolIdRequired')); return }
    }
    if (!name.trim()) { setError(t('modelPoolNameRequired')); return }
    if (!members.length) { setError(t('modelPoolMembersRequired')); return }
    if (members.some((m) => !m.provider || !m.model)) { setError(t('modelPoolMemberIncomplete')); return }
    if (!Number.isInteger(maxAttempts) || maxAttempts < 1 || maxAttempts > 64) {
      setError(t('modelPoolAttemptsInvalid')); return
    }
    setBusy(true)
    setError('')
    try {
      const payload: AggregationInput = {
        name: name.trim(),
        strategy,
        allow_fallbacks: allowFallbacks,
        max_attempts: maxAttempts,
        members: members.map((m, i) => ({
          provider: m.provider,
          region: m.region || '',
          model: m.model,
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
            <Form onSubmit={submit}>
              <Modal.Header className="items-start justify-between gap-4 px-6 pt-6">
                <div>
                  <Modal.Heading className="text-section font-semibold">{initial ? t('modelPoolEdit') : t('modelPoolCreate')}</Modal.Heading>
                  <p className="mt-1.5 text-body leading-6 text-muted">{t('modelPoolEditorHint')}</p>
                </div>
                <Modal.CloseTrigger isDisabled={busy} aria-label={t('close')} className="grid size-9 place-items-center rounded-lg text-muted hover:bg-surface-secondary"><X size={18} /></Modal.CloseTrigger>
              </Modal.Header>
              <Modal.Body className="space-y-5 px-6 pb-2 pt-1">
                {error ? (
                  <Alert status="danger"><Alert.Indicator /><Alert.Content><Alert.Title>{error}</Alert.Title></Alert.Content></Alert>
                ) : null}
                {!initial ? (
                  <FormRow label={t('modelPoolId')} hint={t('modelPoolIdHint')}>
                    <Input value={id} onChange={(e) => setId(e.target.value)} placeholder="my-claude" aria-label={t('modelPoolId')} disabled={busy} />
                  </FormRow>
                ) : null}
                <FormRow label={t('modelPoolName')}>
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
                        const regions = regionOptions(descriptors, member.provider)
                        const modelChoices = modelOptions(models, member.provider, t)
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
                                <FilterSelect value={member.provider} onChange={(v) => updateMember(member.key, { provider: v, region: '', model: '' })} ariaLabel={t('modelPoolProvider')} options={providerSelectOptions} className="w-full" />
                              </div>
                              <div className="space-y-1">
                                <Label className="text-caption text-muted">{t('modelPoolRegion')}</Label>
                                <FilterSelect value={member.region || ''} onChange={(v) => updateMember(member.key, { region: v })} ariaLabel={t('modelPoolRegion')} options={[{ id: '', label: t('modelPoolAnyRegion') }, ...regions]} className="w-full" />
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
