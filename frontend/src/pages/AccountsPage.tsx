import { useState } from 'react'
import { Button, Chip, Input, Modal } from '@heroui/react'
import { Copy, ArrowSquareOut, Key, Plus, Power, ArrowClockwise, ShieldCheck, TrashSimple, UserCircle, WarningCircle, X } from '@phosphor-icons/react'
import { AddAccountModal } from '@/components/AddAccountModal'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import {
  deleteAccount,
  exportAccount,
  fetchLoginStatus,
  loginWithPat,
  rewarmWorker,
  startDeviceLogin,
  updateAccount,
} from '@/api/overview'
import type { Overview } from '@/api/types'
import { AccountsPageSkeleton } from '@/components/ui/PageSkeletons'

type AccountRow = NonNullable<Overview['accounts']>[number]
type BusyKind = 'create' | 'import' | 'device' | 'pat' | 'rewarm' | 'toggle' | 'delete' | 'export'
type AccountBusy = { id: string; kind: BusyKind }

function cooldownLabel(until?: string | null) {
  if (!until) return ''
  const milliseconds = Date.parse(until) - Date.now()
  if (!Number.isFinite(milliseconds) || milliseconds <= 0) return ''
  const seconds = Math.ceil(milliseconds / 1000)
  return seconds < 60 ? `${seconds}s` : `${Math.ceil(seconds / 60)}m`
}

function accountState(account: AccountRow) {
  if (!account.enabled) return 'disabled'
  if (cooldownLabel(account.down_until || account.cooldown_until)) return 'cooling'
  if (account.hot) return 'hot'
  if (account.ready) return 'ready'
  return 'login'
}

export function AccountsPage() {
  const { t } = useI18n()
  const { overview, loading, refresh } = useOverview()
  const rows = overview?.accounts || []
  const [addOpen, setAddOpen] = useState(false)
  const [busy, setBusy] = useState<AccountBusy | null>(null)
  const [patById, setPatById] = useState<Record<string, string>>({})
  const [noteById, setNoteById] = useState<Record<string, string>>({})
  const [urlById, setUrlById] = useState<Record<string, string>>({})
  const [confirmId, setConfirmId] = useState<string | null>(null)
  const signedCount = rows.filter((account) => account.hot).length
  const confirmAccount = rows.find((account) => account.id === confirmId)

  if (loading && !overview) return <AccountsPageSkeleton />

  async function run(id: string, kind: BusyKind, action: () => Promise<void>) {
    setBusy({ id, kind })
    setNoteById((current) => ({ ...current, [id]: '' }))
    try {
      await action()
    } catch (error) {
      setNoteById((current) => ({ ...current, [id]: error instanceof Error ? error.message : String(error) }))
    } finally {
      setBusy(null)
    }
  }



  async function onDeviceLogin(id: string) {
    await run(id, 'device', async () => {
      const output = await startDeviceLogin(id)
      if (output.authUrl) {
        setUrlById((current) => ({ ...current, [id]: output.authUrl || '' }))
        window.open(output.authUrl, '_blank', 'noopener,noreferrer')
      }
      for (let attempt = 0; attempt < 60; attempt++) {
        await new Promise((resolve) => window.setTimeout(resolve, 2000))
        const status = await fetchLoginStatus(id)
        const login = status.login || {}
        setNoteById((current) => ({ ...current, [id]: login.message || t('waitingQoderLogin') }))
        if (login.status === 'ok' || login.status === 'error') break
      }
      await refresh()
    })
  }

  async function onPat(id: string) {
    const pat = (patById[id] || '').trim()
    if (!pat) {
      setNoteById((current) => ({ ...current, [id]: t('pastePatFirst') }))
      return
    }
    await run(id, 'pat', async () => {
      await loginWithPat(pat, id)
      setPatById((current) => ({ ...current, [id]: '' }))
      await refresh()
    })
  }

  async function onExport(id: string) {
    await run(id, 'export', async () => {
      const bundle = await exportAccount(id)
      await navigator.clipboard.writeText(JSON.stringify(bundle, null, 2))
      setNoteById((current) => ({ ...current, [id]: t('credentialCopied') }))
    })
  }

  return (
    <div className="space-y-6">
      <section className="flex flex-wrap items-center justify-between gap-4 border-b border-[var(--app-line)] pb-4">
        <div className="flex items-center gap-5 text-sm">
          <div className="flex items-center gap-2"><span className="text-[var(--app-faint)]">{t('accountCount')}</span><span className="mono font-medium">{loading ? '…' : rows.length}</span></div>
          <div className="flex items-center gap-2 border-l border-[var(--app-line)] pl-5"><span className="text-[var(--app-faint)]">{t('signedIn')}</span><span className="mono font-medium">{loading ? '…' : signedCount}</span></div>
        </div>
        <Button onPress={() => setAddOpen(true)}><Plus size={15} />{t('addAccount')}</Button>
      </section>

      <AddAccountModal isOpen={addOpen} onClose={() => setAddOpen(false)} onAdded={refresh} />

      <Modal.Root isOpen={Boolean(confirmAccount)} onOpenChange={(next: boolean) => { if (!next) setConfirmId(null) }}>
        <Modal.Backdrop variant="blur">
          <Modal.Container placement="center" size="sm">
            <Modal.Dialog aria-label={t('delete')}>
              <Modal.Header className="items-start justify-between gap-4">
                <div className="flex items-center gap-2"><WarningCircle size={18} className="text-[var(--app-danger)]" /><Modal.Heading className="text-base font-semibold">{t('delete')}</Modal.Heading></div>
                <Modal.CloseTrigger aria-label={t('close')} className="grid size-8 place-items-center rounded-lg text-[var(--app-muted)] hover:bg-[var(--app-surface-muted)]"><X size={16} /></Modal.CloseTrigger>
              </Modal.Header>
              <Modal.Body className="pt-0">
                <p className="text-sm leading-6 text-[var(--app-muted)]">{t('deleteAccountConfirm', { name: confirmAccount?.name || confirmAccount?.id || '' })}</p>
              </Modal.Body>
              <Modal.Footer className="justify-end">
                <Button variant="ghost" onPress={() => setConfirmId(null)}>{t('cancel')}</Button>
                <Button variant="danger" isPending={busy?.id === confirmId && busy.kind === 'delete'} onPress={() => {
                  if (!confirmAccount) return
                  const id = confirmAccount.id
                  setConfirmId(null)
                  void run(id, 'delete', async () => { await deleteAccount(id); await refresh() })
                }}>{t('delete')}</Button>
              </Modal.Footer>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal.Root>


      {!loading && !rows.length ? (
        <div className="grid min-h-64 place-items-center rounded-lg border border-dashed border-[var(--app-line-strong)] text-center">
          <div>
            <UserCircle size={22} className="mx-auto text-[var(--app-faint)]" />
            <div className="mt-4 text-sm font-medium">{t('noAccounts')}</div>
            <div className="mt-1 text-xs text-[var(--app-faint)]">{t('accountEmptyHint')}</div>
          </div>
        </div>
      ) : null}

      <section className="overflow-hidden rounded-lg border border-[var(--app-line)] bg-[var(--app-surface)]">
        {rows.map((account, index) => {
          const thisBusy = busy?.id === account.id ? busy.kind : ''
          const authUrl = urlById[account.id]
          const lastError = account.last_error || account.lastError
          const cooldown = cooldownLabel(account.down_until || account.cooldown_until)
          const state = accountState(account)
          const stateCopy = state === 'hot' ? t('signedIn') : state === 'ready' ? t('ready') : state === 'cooling' ? `${t('cooling')} ${cooldown}` : state === 'disabled' ? t('disabled') : t('needQoderLogin')

          return (
            <article key={account.id} className={index ? 'border-t border-[var(--app-line)]' : ''}>
              <div className="grid gap-5 px-5 py-5 xl:grid-cols-[minmax(0,1fr)_auto] xl:items-start">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2.5">
                    <span className="status-dot" data-state={state === 'hot' || state === 'ready' ? 'ok' : state === 'login' ? 'danger' : undefined} />
                    <h3 className="text-lg font-semibold tracking-[-0.02em]">{account.name || account.id}</h3>
                    <Chip size="sm" variant="soft" color={state === 'hot' || state === 'ready' ? 'success' : state === 'cooling' ? 'warning' : undefined}>{stateCopy}</Chip>
                    <Chip size="sm" variant="soft" className="uppercase">{account.provider || 'qoder'}</Chip>
                    <Chip size="sm" variant="soft">{account.auth_type || 'none'}</Chip>
                  </div>
                  <div className="mono mt-2 flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-[var(--app-faint)]">
                    <span>{account.id}</span>
                    {account.remote_uid ? <span>UID {account.remote_uid}</span> : null}
                  </div>
                  <div className="mt-4 grid max-w-2xl grid-cols-2 gap-x-6 gap-y-3 text-xs sm:grid-cols-4">
                    {[
                      [t('inFlight'), account.in_flight ?? 0],
                      [t('restarts'), account.restarts ?? 0],
                      ['Priority', account.priority ?? 0],
                      ['Max', account.max_inflight ?? 0],
                    ].map(([label, value]) => (
                      <div key={String(label)}>
                        <div className="text-[var(--app-faint)]">{label}</div>
                        <div className="mono mt-1 font-medium">{value}</div>
                      </div>
                    ))}
                  </div>
                </div>

                <div className="flex flex-wrap gap-2 xl:justify-end">
                  <Button size="sm" variant="secondary" isPending={thisBusy === 'toggle'} onPress={() => void run(account.id, 'toggle', async () => { await updateAccount(account.id, { enabled: !account.enabled }); await refresh() })}>
                    <Power size={14} />
                    {account.enabled ? t('disable') : t('enable')}
                  </Button>
                  {account.auth_type !== 'none' ? (
                    <Button size="sm" variant="secondary" isPending={thisBusy === 'export'} onPress={() => void onExport(account.id)}><Copy size={14} />{t('export')}</Button>
                  ) : null}
                  <Button size="sm" variant="danger" isPending={thisBusy === 'delete'} onPress={() => {
                    setConfirmId(account.id)
                  }}><TrashSimple size={14} />{t('delete')}</Button>
                </div>
              </div>

              {account.enabled ? (
                <div className="grid gap-4 border-t border-[var(--app-line)] bg-[var(--app-surface-muted)]/55 px-5 py-4 xl:grid-cols-[auto_minmax(300px,1fr)] xl:items-end">
                  <div>
                    <div className="mb-2 text-[10px] font-semibold tracking-[0.1em] text-[var(--app-faint)] uppercase">{t('oauthDeviceFlow')}</div>
                    <div className="flex flex-wrap gap-2">
                      <Button size="sm" isPending={thisBusy === 'device'} onPress={() => void onDeviceLogin(account.id)}><ShieldCheck size={14} />{t('startBrowserLogin')}</Button>
                      <Button size="sm" variant="secondary" isPending={thisBusy === 'rewarm'} onPress={() => void run(account.id, 'rewarm', async () => { await rewarmWorker(account.id); await refresh() })}><ArrowClockwise size={14} />{t('rewarm')}</Button>
                      {authUrl ? <Button size="sm" variant="ghost" onPress={() => window.open(authUrl, '_blank', 'noopener,noreferrer')}><ArrowSquareOut size={14} />{t('open')}</Button> : null}
                    </div>
                  </div>
                  <div>
                    <div className="mb-2 text-[10px] font-semibold tracking-[0.1em] text-[var(--app-faint)] uppercase">{t('patFallback')}</div>
                    <div className="flex flex-col gap-2 sm:flex-row">
                      <Input type="password" value={patById[account.id] || ''} onChange={(event) => setPatById((current) => ({ ...current, [account.id]: event.target.value }))} placeholder={t('pasteToken')} aria-label={t('pat')} />
                      <Button size="sm" variant="secondary" isPending={thisBusy === 'pat'} onPress={() => void onPat(account.id)}><Key size={14} />{t('usePat')}</Button>
                    </div>
                  </div>
                </div>
              ) : null}

              {authUrl || noteById[account.id] || lastError ? (
                <div className="border-t border-[var(--app-line)] px-5 py-3 text-xs">
                  {authUrl ? <code className="mono block break-all text-[var(--app-muted)]">{authUrl}</code> : null}
                  {noteById[account.id] || lastError ? <p className={`mt-1 ${lastError ? 'text-[var(--app-danger)]' : 'text-[var(--app-muted)]'}`}>{noteById[account.id] || lastError}</p> : null}
                </div>
              ) : null}
            </article>
          )
        })}
      </section>
    </div>
  )
}
