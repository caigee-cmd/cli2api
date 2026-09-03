import { useEffect, useState } from 'react'
import { Button, Chip, Modal } from '@heroui/react'
import { ArrowClockwise, X } from '@phosphor-icons/react'
import { fetchCheckinRecords } from '@/api/overview'
import type { CheckinRecord } from '@/api/types'
import { EmptyPanel } from '@/components/ui/EmptyPanel'
import { PageAlert } from '@/components/ui/PageAlert'
import { SkeletonBlock } from '@/components/ui/PageSkeletons'
import type { AccountRow } from '@/lib/account'

type Translate = (key: string, vars?: Record<string, string | number>) => string

type Props = {
  account: AccountRow | null
  t: Translate
  onClose: () => void
}

function recordStatus(record: CheckinRecord, t: Translate) {
  if (record.status === 'success') return { label: t('checkinRecordSuccess'), color: 'success' as const }
  if (record.status === 'already') return { label: t('checkinRecordAlready'), color: 'warning' as const }
  return { label: t('checkinRecordFailed'), color: 'danger' as const }
}

function formatRecordTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

export function AccountCheckinRecordsModal({ account, t, onClose }: Props) {
  const [records, setRecords] = useState<CheckinRecord[]>([])
  const [loading, setLoading] = useState(Boolean(account?.id))
  const [error, setError] = useState('')
  const accountId = account?.id || ''
  const title = t('checkinRecordsTitle', { name: account?.name || account?.id || '' })

  async function load() {
    if (!accountId) return
    setLoading(true)
    setError('')
    try {
      const response = await fetchCheckinRecords(accountId)
      setRecords(response.data || [])
    } catch (err) {
      setRecords([])
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    if (!accountId) return
    void load()
  }, [accountId])

  return (
    <Modal.Root isOpen={Boolean(account)} onOpenChange={(next: boolean) => { if (!next) onClose() }}>
      <Modal.Backdrop variant="blur">
        <Modal.Container size="lg" scroll="inside">
          <Modal.Dialog>
            <Modal.Header className="items-start justify-between gap-4 px-5 pt-5">
              <div className="min-w-0">
                <Modal.Heading className="text-lg font-semibold tracking-[-0.01em]">{title}</Modal.Heading>
                <p className="mt-1 text-xs leading-5 text-muted">{t('checkinRecordsHint')}</p>
              </div>
              <Modal.CloseTrigger aria-label={t('close')} className="grid size-8 shrink-0 place-items-center rounded-lg text-muted transition-colors hover:bg-surface-secondary hover:text-foreground">
                <X size={16} />
              </Modal.CloseTrigger>
            </Modal.Header>
            <Modal.Body className="px-5 pb-2">
              {error ? <div className="mb-3"><PageAlert title={error} /></div> : null}
              {loading ? (
                <div className="space-y-2 py-1">
                  <SkeletonBlock className="h-16 w-full" />
                  <SkeletonBlock className="h-16 w-full" />
                  <SkeletonBlock className="h-16 w-full" />
                </div>
              ) : records.length ? (
                <div className="divide-y divide-separator overflow-hidden rounded-2xl border border-separator">
                  {records.map((record) => {
                    const status = recordStatus(record, t)
                    return (
                      <div key={record.id} className="flex items-start gap-3 px-3 py-3">
                        <span className="status-dot mt-1.5 shrink-0" data-state={record.status === 'error' ? 'warn' : 'ok'} />
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-2">
                            <Chip size="sm" variant="soft" color={status.color}>{status.label}</Chip>
                            <span className="mono text-[11px] text-muted">{formatRecordTime(record.created_at)}</span>
                          </div>
                          <p className="mt-1 break-words text-xs leading-5 text-foreground/75">{record.message || '—'}</p>
                        </div>
                      </div>
                    )
                  })}
                </div>
              ) : (
                <EmptyPanel
                  className="min-h-44 rounded-3xl border border-dashed border-border"
                  title={t('checkinRecordsEmpty')}
                  hint={t('checkinRecordsHint')}
                />
              )}
            </Modal.Body>
            <Modal.Footer className="justify-between px-5 pb-5">
              <span className="mono text-xs text-muted">{t('checkinRecordsCount', { count: records.length })}</span>
              <div className="flex items-center gap-2">
                <Button size="sm" variant="ghost" onPress={onClose}>{t('close')}</Button>
                <Button size="sm" variant="secondary" isPending={loading} onPress={() => void load()}>
                  <ArrowClockwise size={14} />{t('refresh')}
                </Button>
              </div>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal.Root>
  )
}
