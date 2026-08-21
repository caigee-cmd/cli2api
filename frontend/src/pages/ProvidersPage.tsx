import { useMemo, useState } from 'react'
import { Button, Card, Chip, Input, Table } from '@heroui/react'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import { refreshModels } from '@/api/overview'

export function ProvidersPage() {
  const { t } = useI18n()
  const { overview, setOverview } = useOverview()
  const [filter, setFilter] = useState('')
  const [busy, setBusy] = useState(false)
  const models = overview?.models || []

  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase()
    if (!q) return models
    return models.filter((m) => `${m.id} ${m.mapped_key || ''}`.toLowerCase().includes(q))
  }, [filter, models])

  async function onRefresh() {
    setBusy(true)
    try {
      const data = await refreshModels()
      setOverview({ ...(overview || {}), models: data.data || [] })
    } catch (err) {
      alert(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <p className="mb-1 text-xs font-semibold uppercase tracking-[0.08em] text-emerald-400">{t('catalog')}</p>
          <h2 className="text-2xl font-semibold tracking-tight sm:text-3xl">{t('availableModels')}</h2>
          <p className="mt-1 text-sm text-zinc-400">
            {models.length
              ? t('shownTotal', { shown: filtered.length, total: models.length })
              : t('noModelsYet')}
          </p>
        </div>
        <div className="flex w-full flex-col gap-2 sm:w-auto sm:flex-row sm:items-center">
          <Input
            className="sm:w-64"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder={t('filterPh')}
            aria-label={t('filter')}
          />
          <Button variant="secondary" isPending={busy} onPress={() => void onRefresh()}>
            {busy ? t('refreshing') : t('refresh')}
          </Button>
        </div>
      </div>

      <Card className="overflow-hidden border border-white/10 bg-white/[0.02] p-0 shadow-none">
        {filtered.length === 0 ? (
          <div className="p-8 text-sm text-zinc-400">
            {models.length ? t('noModelsMatch') : t('noProviders')}
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
                  {filtered.map((m) => (
                    <Table.Row key={m.id}>
                      <Table.Cell>
                        <span className="font-medium">{m.id}</span>
                      </Table.Cell>
                      <Table.Cell>
                        <span className="mono text-xs text-zinc-400">{m.mapped_key || m.id}</span>
                      </Table.Cell>
                      <Table.Cell>
                        <Chip size="sm" variant="soft" color={m.stale ? 'warning' : 'success'}>
                          {m.stale ? t('fallback') : t('ready')}
                        </Chip>
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
