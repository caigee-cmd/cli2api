import { useEffect, useState } from 'react'
import { Button, Chip, Modal } from '@heroui/react'
import { ArrowClockwise, Cube, X } from '@phosphor-icons/react'
import { fetchModels, refreshModels } from '@/api/overview'
import type { ModelInfo } from '@/api/types'
import { ProviderMark } from '@/components/ProviderMark'
import { EmptyPanel } from '@/components/ui/EmptyPanel'
import { SkeletonBlock } from '@/components/ui/PageSkeletons'
import { PageAlert } from '@/components/ui/PageAlert'
import type { AccountRow } from '@/lib/account'
import { accountProviderLabel } from '@/lib/provider'

type Translate = (key: string, vars?: Record<string, string | number>) => string

type Props = {
  account: AccountRow | null
  t: Translate
  onClose: () => void
}

function routedModelName(model: ModelInfo) {
  const routeName = model.route_display_name || ''
  return routeName && routeName !== (model.display_name || model.id) ? routeName : ''
}

export function AccountModelsModal({ account, t, onClose }: Props) {
  const [models, setModels] = useState<ModelInfo[]>([])
  const [loading, setLoading] = useState(Boolean(account?.id))
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState('')
  const accountId = account?.id || ''
  const title = t('accountModelsTitle', { name: account?.name || account?.id || '' })
  const provider = account ? accountProviderLabel(account.provider, account.region, t) : ''

  async function load(refresh = false) {
    if (!accountId) return
    if (refresh) setRefreshing(true)
    else {
      setLoading(true)
      setModels([])
    }
    setError('')
    try {
      const data = await (refresh ? refreshModels(accountId) : fetchModels(accountId))
      setModels(data.data || [])
    } catch (err) {
      setModels([])
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
      setRefreshing(false)
    }
  }

  useEffect(() => {
    if (!accountId) return
    let cancelled = false
    void fetchModels(accountId)
      .then((data) => {
        if (cancelled) return
        setModels(data.data || [])
        setError('')
        setLoading(false)
      })
      .catch((err) => {
        if (cancelled) return
        setModels([])
        setError(err instanceof Error ? err.message : String(err))
        setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [accountId])

  return (
    <Modal.Root isOpen={Boolean(account)} onOpenChange={(next: boolean) => { if (!next) onClose() }}>
      <Modal.Backdrop variant="blur">
        <Modal.Container size="lg" scroll="inside">
          <Modal.Dialog>
            <Modal.Header className="items-start justify-between gap-4 px-5 pt-5">
              <div className="min-w-0">
                <Modal.Heading className="text-lg font-semibold tracking-[-0.01em]">{title}</Modal.Heading>
                <p className="mt-1 text-xs font-normal leading-5 text-muted">{t('accountModelsHint')}</p>
                {account ? (
                  <div className="mt-2 flex flex-wrap items-center gap-2 text-[11px] text-muted">
                    <ProviderMark provider={account.provider} size={14} />
                    <span>{provider}</span>
                    <span className="text-border">·</span>
                    <span className="mono">{account.id}</span>
                  </div>
                ) : null}
              </div>
              <Modal.CloseTrigger aria-label={t('close')} className="grid size-8 shrink-0 place-items-center rounded-lg text-muted transition-colors hover:bg-surface-secondary hover:text-foreground">
                <X size={16} />
              </Modal.CloseTrigger>
            </Modal.Header>
            <Modal.Body className="px-5 pb-2">
              {error ? (
                <div className="mb-3">
                  <PageAlert title={t('accountModelsError', { msg: error })} />
                </div>
              ) : null}

              {loading || refreshing ? (
                <div className="space-y-2 py-1">
                  <SkeletonBlock className="h-14 w-full" />
                  <SkeletonBlock className="h-14 w-full" />
                  <SkeletonBlock className="h-14 w-full" />
                </div>
              ) : models.length ? (
                <ul className="divide-y divide-separator overflow-hidden rounded-lg border border-separator">
                  {models.map((model) => {
                    const ownedBy = model.provider || model.owned_by || account?.provider || 'qoder'
                    const routed = routedModelName(model)
                    return (
                      <li key={model.id} className="flex items-start gap-3 px-3 py-2.5">
                        <span className="status-dot mt-1.5" data-state={model.stale ? undefined : 'ok'} />
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-2">
                            <span className="truncate text-sm font-medium">{model.display_name || model.id}</span>
                            {model.stale ? <Chip size="sm" variant="soft" color="warning">{t('fallback')}</Chip> : null}
                          </div>
                          <div className="mono mt-0.5 truncate text-[11px] text-muted">{model.id}</div>
                          {routed ? <div className="mt-0.5 text-[11px] text-muted">{t('routedTo', { model: routed })}</div> : null}
                        </div>
                        <div className="flex shrink-0 items-center gap-1.5 pt-0.5 text-[11px] text-muted">
                          <ProviderMark provider={ownedBy} size={12} />
                          <span>{ownedBy}</span>
                        </div>
                      </li>
                    )
                  })}
                </ul>
              ) : (
                <EmptyPanel
                  className="min-h-48 rounded-3xl border border-dashed border-border"
                  icon={<Cube size={22} />}
                  title={t('accountModelsEmpty')}
                  hint={t('noModelsYet')}
                />
              )}
            </Modal.Body>
            <Modal.Footer className="justify-between px-5 pb-5">
              <span className="mono text-xs text-muted">
                {loading ? t('refreshing') : t('shownTotal', { shown: models.length, total: models.length })}
              </span>
              <div className="flex items-center gap-2">
                <Button size="sm" variant="ghost" onPress={onClose}>{t('close')}</Button>
                <Button size="sm" variant="secondary" isPending={refreshing} onPress={() => void load(true)}>
                  <ArrowClockwise size={14} />
                  {refreshing ? t('refreshing') : t('refresh')}
                </Button>
              </div>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal.Root>
  )
}
