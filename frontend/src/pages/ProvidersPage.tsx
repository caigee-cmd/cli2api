import { useMemo, useState } from 'react'
import { Button, Card, Chip, Input, Table } from '@heroui/react'
import { Cube, ArrowClockwise, ArrowCounterClockwise, FloppyDisk, MagnifyingGlass } from '@phosphor-icons/react'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import { refreshModels, updateModelContext } from '@/api/overview'
import type { Overview } from '@/api/types'
import { ProvidersPageSkeleton } from '@/components/ui/PageSkeletons'

type ModelInfo = NonNullable<Overview['models']>[number]

function modelSettingsKey(model: ModelInfo) {
  return model.settings_key || model.id
}

function routedModelName(model: ModelInfo) {
  const routeName = model.route_display_name || ''
  return routeName && routeName !== (model.display_name || model.id) ? routeName : ''
}

export function ProvidersPage() {
  const { t } = useI18n()
  const { overview, loading, setOverview } = useOverview()
  const [filter, setFilter] = useState('')
  const [busy, setBusy] = useState(false)
  const [savingKey, setSavingKey] = useState('')
  const [message, setMessage] = useState('')
  const [messageError, setMessageError] = useState(false)
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const models = useMemo(() => overview?.models || [], [overview?.models])

  const filtered = useMemo(() => {
    const query = filter.trim().toLowerCase()
    if (!query) return models
    return models.filter((model) => `${model.display_name || ''} ${model.id} ${model.mapped_key || ''}`.toLowerCase().includes(query))
  }, [filter, models])

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
      <section className="grid gap-5 border-b border-[var(--app-line)] pb-6 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end">
        <div>
          <p className="mb-2 text-xs font-semibold tracking-[0.14em] text-[var(--accent)] uppercase">{t('catalog')}</p>
          <h2 className="text-3xl font-semibold tracking-[-0.04em] sm:text-4xl">{t('availableModels')}</h2>
          <p className="mt-2 text-sm text-[var(--app-muted)]">
            {models.length ? t('shownTotal', { shown: filtered.length, total: models.length }) : t('noModelsYet')}
          </p>
        </div>
        <div className="flex flex-col gap-2 sm:flex-row">
          <Input className="sm:w-72" value={filter} onChange={(event) => setFilter(event.target.value)} placeholder={t('filterPh')} aria-label={t('filter')} />
          <Button variant="secondary" isPending={busy} onPress={() => void onRefresh()}>
            <ArrowClockwise size={15} />
            {busy ? t('refreshing') : t('refresh')}
          </Button>
        </div>
      </section>

      {message ? <div className={messageError
        ? 'rounded-lg border border-[color:color-mix(in_srgb,var(--app-danger)_28%,transparent)] bg-[color:color-mix(in_srgb,var(--app-danger)_8%,transparent)] px-4 py-3 text-sm text-[var(--app-danger)]'
        : 'rounded-lg border border-[color:color-mix(in_srgb,var(--accent)_24%,transparent)] bg-[color:color-mix(in_srgb,var(--accent)_7%,transparent)] px-4 py-3 text-sm text-[var(--app-muted)]'}>{message}</div> : null}

      <Card className="app-panel-flat overflow-hidden rounded-lg p-0 shadow-none">
        <div className="flex items-center justify-between gap-4 border-b border-[var(--app-line)] px-5 py-4">
          <div className="flex items-center gap-3">
            <div className="grid size-9 place-items-center rounded-lg bg-[var(--app-surface-muted)] text-[var(--app-muted)]"><Cube size={16} /></div>
            <div>
              <div className="text-sm font-semibold">{t('providerCatalog')}</div>
              <div className="mono mt-0.5 text-[10px] text-[var(--app-faint)]">{t('contextConfigHint')}</div>
            </div>
          </div>
          <Chip size="sm" variant="soft">{filtered.length}</Chip>
        </div>

        {filtered.length === 0 ? (
          <div className="grid min-h-72 place-items-center px-6 py-12 text-center">
            <div>
              <MagnifyingGlass size={22} className="mx-auto text-[var(--app-faint)]" />
              <div className="mt-4 text-sm font-medium">{models.length ? t('noModelsMatch') : t('noProviders')}</div>
              <div className="mt-1 text-xs text-[var(--app-faint)]">{models.length ? filter : t('noModelsYet')}</div>
            </div>
          </div>
        ) : (
          <Table>
            <Table.ScrollContainer>
              <Table.Content aria-label={t('availableModels')}>
                <Table.Header>
                  <Table.Column isRowHeader>{t('modelCol')}</Table.Column>
                  <Table.Column>{t('requestIdCol')}</Table.Column>
                  <Table.Column>{t('qoderKeyCol')}</Table.Column>
                  <Table.Column>{t('contextWindowCol')}</Table.Column>
                  <Table.Column>{t('stateCol')}</Table.Column>
                  <Table.Column>{t('actions')}</Table.Column>
                </Table.Header>
                <Table.Body>
                  {filtered.map((model) => {
                    const key = modelSettingsKey(model)
                    const saving = savingKey === key
                    return (
                      <Table.Row key={model.id}>
                        <Table.Cell>
                          <div className="flex items-center gap-3 py-1">
                            <span className="status-dot" data-state={model.stale ? undefined : 'ok'} />
                            <div>
                              <div className="font-medium">{model.display_name || model.id}</div>
                              {routedModelName(model) ? <div className="mt-0.5 text-[10px] text-[var(--app-faint)]">{t('routedTo', { model: routedModelName(model) })}</div> : null}
                            </div>
                          </div>
                        </Table.Cell>
                        <Table.Cell><span className="mono text-xs font-medium">{model.id}</span></Table.Cell>
                        <Table.Cell><span className="mono text-xs text-[var(--app-muted)]">{model.mapped_key || model.id}</span></Table.Cell>
                        <Table.Cell>
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
                            <span className="mono text-[10px] text-[var(--app-faint)]">tokens</span>
                          </div>
                        </Table.Cell>
                        <Table.Cell>
                          <Chip size="sm" variant="soft" color={model.context_custom ? 'accent' : model.stale ? 'warning' : 'success'}>
                            {model.context_custom ? t('custom') : t('defaultValue')}
                          </Chip>
                        </Table.Cell>
                        <Table.Cell>
                          <div className="flex items-center gap-2">
                            <Button size="sm" variant="secondary" isPending={saving} onPress={() => void onSave(model)}>
                              <FloppyDisk size={14} />{t('save')}
                            </Button>
                            <Button size="sm" variant="ghost" isDisabled={saving || !model.context_custom} onPress={() => void onReset(model)} aria-label={t('resetDefault')}>
                              <ArrowCounterClockwise size={14} />
                            </Button>
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
    </div>
  )
}
