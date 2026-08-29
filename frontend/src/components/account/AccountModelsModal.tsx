import { useEffect, useState } from 'react'
import { Button, Chip, Modal, Skeleton } from '@heroui/react'
import { ArrowClockwise, Cube, WarningCircle, X } from '@phosphor-icons/react'
import { fetchModels, refreshModels } from '@/api/overview'
import type { ModelInfo } from '@/api/types'
import { ProviderMark } from '@/components/ProviderMark'
import type { AccountRow } from '@/lib/account'
import { accountProviderLabel } from '@/lib/provider'

const ACCOUNT_BUTTON_CLASS = 'account-button'

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
                <p className="mt-1 text-xs font-normal leading-5 text-[var(--app-faint)]">{t('accountModelsHint')}</p>
                {account ? (
                  <div className="mt-2 flex flex-wrap items-center gap-2 text-[11px] text-[var(--app-muted)]">
                    <ProviderMark provider={account.provider} size={14} />
                    <span>{provider}</span>
                    <span className="text-[var(--app-line-strong)]">·</span>
                    <span className="mono">{account.id}</span>
                  </div>
                ) : null}
              </div>
              <Modal.CloseTrigger aria-label={t('close')} className="grid size-8 shrink-0 place-items-center rounded-lg text-[var(--app-muted)] transition-colors hover:bg-[var(--app-surface-muted)] hover:text-[var(--app-ink)]">
                <X size={16} />
              </Modal.CloseTrigger>
            </Modal.Header>
            <Modal.Body className="px-5 pb-2">
              {error ? (
                <div className="mb-3 flex gap-2 rounded-lg border border-[color-mix(in_srgb,var(--app-danger)_24%,transparent)] bg-[color-mix(in_srgb,var(--app-danger)_7%,transparent)] px-3 py-2.5 text-xs leading-5 text-[var(--app-danger)]">
                  <WarningCircle size={14} className="mt-0.5 shrink-0" />
                  <span>{t('accountModelsError', { msg: error })}</span>
                </div>
              ) : null}

              {loading ? (
                <div className="space-y-2 py-1">
                  <Skeleton className="h-14 rounded-lg" />
                  <Skeleton className="h-14 rounded-lg" />
                  <Skeleton className="h-14 rounded-lg" />
                </div>
              ) : models.length ? (
                <ul className="divide-y divide-[var(--app-line)] overflow-hidden rounded-lg border border-[var(--app-line)]">
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
                          <div className="mono mt-0.5 truncate text-[11px] text-[var(--app-faint)]">{model.id}</div>
                          {routed ? <div className="mt-0.5 text-[11px] text-[var(--app-faint)]">{t('routedTo', { model: routed })}</div> : null}
                        </div>
                        <div className="flex shrink-0 items-center gap-1.5 pt-0.5 text-[11px] text-[var(--app-muted)]">
                          <ProviderMark provider={ownedBy} size={12} />
                          <span>{ownedBy}</span>
                        </div>
                      </li>
                    )
                  })}
                </ul>
              ) : (
                <div className="grid min-h-48 place-items-center rounded-lg border border-dashed border-[var(--app-line-strong)] text-center">
                  <div className="max-w-xs px-6">
                    <Cube size={22} className="mx-auto text-[var(--app-faint)]" />
                    <div className="mt-3 text-sm font-medium">{t('accountModelsEmpty')}</div>
                    <div className="mt-1 text-xs leading-5 text-[var(--app-faint)]">{t('noModelsYet')}</div>
                  </div>
                </div>
              )}
            </Modal.Body>
            <Modal.Footer className="justify-between px-5 pb-5">
              <span className="mono text-xs text-[var(--app-faint)]">
                {loading ? t('refreshing') : t('shownTotal', { shown: models.length, total: models.length })}
              </span>
              <div className="flex items-center gap-2">
                <Button className={ACCOUNT_BUTTON_CLASS} size="sm" variant="ghost" onPress={onClose}>{t('close')}</Button>
                <Button className={ACCOUNT_BUTTON_CLASS} size="sm" variant="secondary" isPending={refreshing} onPress={() => void load(true)}>
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
