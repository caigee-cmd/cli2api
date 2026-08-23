import { useMemo, useState } from 'react'
import { Button, Card, Chip, Input, Table } from '@heroui/react'
import { Box, RefreshCw, Search } from 'lucide-react'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import { refreshModels } from '@/api/overview'

export function ProvidersPage() {
  const { t } = useI18n()
  const { overview, setOverview } = useOverview()
  const [filter, setFilter] = useState('')
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const models = overview?.models || []

  const filtered = useMemo(() => {
    const query = filter.trim().toLowerCase()
    if (!query) return models
    return models.filter((model) => `${model.id} ${model.mapped_key || ''}`.toLowerCase().includes(query))
  }, [filter, models])

  async function onRefresh() {
    setBusy(true)
    setMessage('')
    try {
      const data = await refreshModels()
      setOverview({ ...(overview || {}), models: data.data || [] })
    } catch (error) {
      setMessage(error instanceof Error ? error.message : String(error))
    } finally {
      setBusy(false)
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
            <RefreshCw size={15} />
            {busy ? t('refreshing') : t('refresh')}
          </Button>
        </div>
      </section>

      {message ? <div className="rounded-lg border border-[color:color-mix(in_srgb,var(--app-danger)_28%,transparent)] bg-[color:color-mix(in_srgb,var(--app-danger)_8%,transparent)] px-4 py-3 text-sm text-[var(--app-danger)]">{message}</div> : null}

      <Card className="app-panel-flat overflow-hidden rounded-xl p-0 shadow-none">
        <div className="flex items-center justify-between gap-4 border-b border-[var(--app-line)] px-5 py-4">
          <div className="flex items-center gap-3">
            <div className="grid size-9 place-items-center rounded-lg bg-[var(--app-surface-muted)] text-[var(--app-muted)]"><Box size={16} /></div>
            <div>
              <div className="text-sm font-semibold">{t('providerCatalog')}</div>
              <div className="mono mt-0.5 text-[10px] text-[var(--app-faint)]">{t('providerCatalogMeta')}</div>
            </div>
          </div>
          <Chip size="sm" variant="soft">{filtered.length}</Chip>
        </div>

        {filtered.length === 0 ? (
          <div className="grid min-h-72 place-items-center px-6 py-12 text-center">
            <div>
              <Search size={22} className="mx-auto text-[var(--app-faint)]" />
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
                  <Table.Column>{t('mappedKeyCol')}</Table.Column>
                  <Table.Column>{t('stateCol')}</Table.Column>
                </Table.Header>
                <Table.Body>
                  {filtered.map((model) => (
                    <Table.Row key={model.id}>
                      <Table.Cell>
                        <div className="flex items-center gap-3 py-1">
                          <span className="status-dot" data-state={model.stale ? undefined : 'ok'} />
                          <span className="font-medium">{model.id}</span>
                        </div>
                      </Table.Cell>
                      <Table.Cell><span className="mono text-xs text-[var(--app-muted)]">{model.mapped_key || model.id}</span></Table.Cell>
                      <Table.Cell>
                        <Chip size="sm" variant="soft" color={model.stale ? 'warning' : 'success'}>{model.stale ? t('fallback') : t('ready')}</Chip>
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table.Content>
            </Table.ScrollContainer>
          </Table>
        )}
      </Card>
    </div>
  )
}
