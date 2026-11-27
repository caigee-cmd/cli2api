import { useCallback, useEffect, useState } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'
import { Button, Card, Chip, Table } from '@heroui/react'
import { ArrowUp, Plus, TrashSimple } from '@phosphor-icons/react'
import {
  deleteAggregation,
  fetchAggregations,
  updateAggregation,
  type AggregationRecord,
} from '@/api/modelPools'
import { CompactSwitch } from '@/components/ui/CompactSwitch'
import { ConfirmDialog } from '@/components/ui/ConfirmDialog'
import { EmptyPanel } from '@/components/ui/EmptyPanel'
import { PageAlert } from '@/components/ui/PageAlert'
import { SkeletonBlock } from '@/components/ui/PageSkeletons'
import { useCrossProviderPool } from '@/hooks/useCrossProviderPool'
import { useI18n } from '@/hooks/useI18n'
import { toast } from '@/lib/toast'

type TFunc = (key: string, vars?: Record<string, string | number>) => string

function strategyLabel(value: string, t: TFunc): string {
  return t(`modelPoolStrategy_${value}`)
}

export function ModelPoolPage() {
  const { t } = useI18n()
  const navigate = useNavigate()
  const { enabled, loading: poolLoading } = useCrossProviderPool()
  const [aggregations, setAggregations] = useState<AggregationRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [deleteTarget, setDeleteTarget] = useState<AggregationRecord | null>(null)
  const [deleteBusy, setDeleteBusy] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const aggResult = await fetchAggregations()
      setAggregations(aggResult.data || [])
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
          <p className="mt-1 max-w-2xl text-caption leading-5 text-muted">{t('modelPoolScope')}</p>
        </div>
        <Button onPress={() => navigate('/model-pool/new')}>
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
                        <CompactSwitch isSelected={agg.enabled} ariaLabel={`${t('modelPoolColEnabled')} ${agg.id}`} onChange={(value) => void toggleEnabled(agg, value)} />
                      </Table.Cell>
                      <Table.Cell>
                        <div className="flex items-center gap-1">
                          <Button size="sm" variant="ghost" onPress={() => navigate(`/model-pool/${agg.id}`)}>{t('edit')}</Button>
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
