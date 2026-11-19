import { useEffect, useRef, useState } from 'react'
import { Button, Chip, Modal } from '@heroui/react'
import { ArrowClockwise, Cube, X } from '@phosphor-icons/react'
import { fetchModels, refreshModels } from '@/api/overview'
import type { ModelInfo } from '@/api/types'
import { ProviderMark } from '@/components/ProviderMark'
import { EmptyPanel } from '@/components/ui/EmptyPanel'
import { ListPager, type PageSize } from '@/components/ui/ListPager'
import { SkeletonBlock } from '@/components/ui/PageSkeletons'
import { PageAlert } from '@/components/ui/PageAlert'
import type { AccountRow } from '@/lib/account'
import { modelCreditsText, modelIsFree } from '@/lib/format'
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
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState<PageSize>(20)
  const [total, setTotal] = useState(0)
  const [pageCount, setPageCount] = useState(1)
  const requestID = useRef(0)
  const accountId = account?.id || ''
  const title = t('accountModelsTitle', { name: account?.name || account?.id || '' })
  const provider = account ? accountProviderLabel(account.provider, account.region, t) : ''

  async function load(requestedPage = 1, requestedPageSize = pageSize, refresh = false) {
    if (!accountId) return
    const currentRequest = ++requestID.current
    if (refresh) setRefreshing(true)
    else setLoading(true)
    setError('')
    try {
      const data = await (refresh
        ? refreshModels(accountId, undefined, { page: requestedPage, pageSize: requestedPageSize })
        : fetchModels(accountId, false, undefined, { page: requestedPage, pageSize: requestedPageSize }))
      const all = data.data || []
      const pagination = data.pagination
      const nextTotal = pagination?.total ?? all.length
      const nextPageCount = pagination?.total_pages ?? Math.max(1, Math.ceil(nextTotal / requestedPageSize))
      const nextPage = Math.min(Math.max(1, pagination?.page ?? requestedPage), nextPageCount)
      if (currentRequest !== requestID.current) return
      setModels(pagination ? all : all.slice((nextPage - 1) * requestedPageSize, nextPage * requestedPageSize))
      setTotal(nextTotal)
      setPageCount(nextPageCount)
      setPage(nextPage)
    } catch (err) {
      if (currentRequest !== requestID.current) return
      setModels([])
      setTotal(0)
      setPageCount(1)
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      if (currentRequest === requestID.current) {
        setLoading(false)
        setRefreshing(false)
      }
    }
  }

  useEffect(() => {
    if (!accountId) return
    setPage(1)
    void load(1, pageSize)
  }, [accountId])

  const shownFrom = total === 0 ? 0 : (page - 1) * pageSize + 1
  const shownTo = total === 0 ? 0 : Math.min(total, shownFrom + models.length - 1)

  return (
    <Modal.Root isOpen={Boolean(account)} onOpenChange={(next: boolean) => { if (!next) onClose() }}>
      <Modal.Backdrop variant="blur">
        <Modal.Container size="lg" scroll="inside">
          <Modal.Dialog>
            <Modal.Header className="items-start justify-between gap-4 px-5 pt-5">
              <div className="min-w-0">
                <Modal.Heading className="text-section font-semibold">{title}</Modal.Heading>
                <p className="mt-1 text-caption font-normal leading-5 text-muted">{t('accountModelsHint')}</p>
                {account ? (
                  <div className="mt-2 flex flex-wrap items-center gap-2 text-caption text-muted">
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
                <ul className="divide-y divide-separator overflow-hidden rounded-lg bg-surface-secondary">
                  {models.map((model) => {
                    const ownedBy = model.provider || model.owned_by || account?.provider || 'qoder'
                    const routed = routedModelName(model)
                    const credits = modelCreditsText(model)
                    const free = modelIsFree(model)
                    return (
                      <li key={model.id} className="flex items-start gap-3 px-3 py-2.5">
                        <span className="status-dot mt-1.5" data-state={model.stale ? undefined : 'ok'} />
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-2">
                            <span className="truncate text-body font-medium">{model.display_name || model.id}</span>
                            {free ? <Chip size="sm" variant="soft" color="success">{t('modelFree')}</Chip> : null}
                            {credits ? <span className="mono text-caption text-muted">{credits}</span> : null}
                            {model.stale ? <Chip size="sm" variant="soft" color="warning">{t('fallback')}</Chip> : null}
                          </div>
                          <div className="mono mt-0.5 truncate text-caption text-muted">{model.id}</div>
                          {routed ? <div className="mt-0.5 text-caption text-muted">{t('routedTo', { model: routed })}</div> : null}
                        </div>
                        <div className="flex shrink-0 items-center gap-1.5 pt-0.5 text-caption text-muted">
                          <ProviderMark provider={ownedBy} size={12} />
                          <span>{ownedBy}</span>
                        </div>
                      </li>
                    )
                  })}
                </ul>
              ) : (
                <EmptyPanel
                  className="min-h-48 rounded-3xl bg-surface-secondary"
                  icon={<Cube size={22} />}
                  title={t('accountModelsEmpty')}
                  hint={t('noModelsYet')}
                />
              )}
            </Modal.Body>
            <Modal.Footer className="flex-col items-stretch gap-3 px-5 pb-5">
              <div className="flex items-center justify-between gap-3">
                <span className="mono text-caption text-muted">
                  {loading ? t('refreshing') : t('shownTotal', { shown: total ? `${shownFrom}–${shownTo}` : 0, total })}
                </span>
                <div className="flex items-center gap-2">
                  <Button size="sm" variant="ghost" onPress={onClose}>{t('close')}</Button>
                  <Button size="sm" variant="secondary" isPending={refreshing} onPress={() => void load(1, pageSize, true)}>
                    <ArrowClockwise size={14} />
                    {refreshing ? t('refreshing') : t('refresh')}
                  </Button>
                </div>
              </div>
              {total > 0 ? (
                <ListPager
                  total={total}
                  page={page}
                  pageCount={pageCount}
                  pageSize={pageSize}
                  loading={loading || refreshing}
                  pageSizeLabel={t('logsPageSize')}
                  pageLabel={t('logsPage', { page, pages: pageCount })}
                  prevLabel={t('logsPrevPage')}
                  nextLabel={t('logsNextPage')}
                  onPage={(nextPage) => void load(nextPage, pageSize)}
                  onPageSize={(nextSize) => {
                    setPageSize(nextSize)
                    void load(1, nextSize)
                  }}
                />
              ) : null}
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal.Root>
  )
}
