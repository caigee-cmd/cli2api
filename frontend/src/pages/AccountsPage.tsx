import { useState } from 'react'
import { Button, Chip, Input } from '@heroui/react'
import { Copy, ExternalLink, KeyRound, Plus, Power, RefreshCw, ShieldCheck, Trash2, UserRound } from 'lucide-react'
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
  const signedCount = rows.filter((account) => account.hot).length
  const enabledCount = rows.filter((account) => account.enabled).length

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
      <section className="grid gap-6 border-b border-[var(--app-line)] pb-6 xl:grid-cols-[minmax(0,1fr)_480px] xl:items-end">
        <div>
          <p className="mb-2 text-xs font-semibold tracking-[0.14em] text-[var(--accent)] uppercase">{t('accountPool')}</p>
          <h2 className="text-3xl font-semibold tracking-[-0.04em] sm:text-4xl">{t('accountsTitle')}</h2>
          <p className="mt-2 max-w-2xl text-sm leading-6 text-[var(--app-muted)]">{t('qoderLoginHint')}</p>
        </div>
        <div className="grid grid-cols-3 overflow-hidden rounded-xl border border-[var(--app-line)] bg-[var(--app-surface)]">
          {[
            [t('accountsTitle'), rows.length],
            [t('signedIn'), signedCount],
            [t('enable'), enabledCount],
          ].map(([label, value], index) => (
            <div key={String(label)} className={`px-4 py-3 ${index ? 'border-l border-[var(--app-line)]' : ''}`}>
              <div className="text-[10px] text-[var(--app-faint)]">{label}</div>
              <div className="mono mt-1 text-xl font-semibold">{loading ? '…' : value}</div>
            </div>
          ))}
        </div>
      </section>

      <div className="flex items-center justify-between gap-4">
        <div className="flex items-baseline gap-2">
          <span className="text-[10px] font-semibold tracking-[0.12em] text-[var(--app-faint)] uppercase">Accounts</span>
          <span className="text-xs text-[var(--app-faint)]">{loading ? '…' : t('accountsSigned', { n: signedCount, total: enabledCount })}</span>
        </div>
        <Button onPress={() => setAddOpen(true)}><Plus size={15} />{t('addAccount')}</Button>
      </div>

      <AddAccountModal isOpen={addOpen} onClose={() => setAddOpen(false)} onAdded={refresh} />


      {!loading && !rows.length ? (
        <div className="grid min-h-64 place-items-center rounded-xl border border-dashed border-[var(--app-line-strong)] text-center">
          <div>
            <UserRound size={22} className="mx-auto text-[var(--app-faint)]" />
            <div className="mt-4 text-sm font-medium">{t('noAccounts')}</div>
            <div className="mt-1 text-xs text-[var(--app-faint)]">{t('accountEmptyHint')}</div>
          </div>
        </div>
      ) : null}

      <section className="overflow-hidden rounded-xl border border-[var(--app-line)] bg-[var(--app-surface)]">
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
                    if (window.confirm(`${t('delete')} ${account.name || account.id}?`)) void run(account.id, 'delete', async () => { await deleteAccount(account.id); await refresh() })
                  }}><Trash2 size={14} />{t('delete')}</Button>
                </div>
              </div>

              {account.enabled ? (
                <div className="grid gap-4 border-t border-[var(--app-line)] bg-[var(--app-surface-muted)]/55 px-5 py-4 xl:grid-cols-[auto_minmax(300px,1fr)] xl:items-end">
                  <div>
                    <div className="mb-2 text-[10px] font-semibold tracking-[0.1em] text-[var(--app-faint)] uppercase">{t('oauthDeviceFlow')}</div>
                    <div className="flex flex-wrap gap-2">
                      <Button size="sm" isPending={thisBusy === 'device'} onPress={() => void onDeviceLogin(account.id)}><ShieldCheck size={14} />{t('startBrowserLogin')}</Button>
                      <Button size="sm" variant="secondary" isPending={thisBusy === 'rewarm'} onPress={() => void run(account.id, 'rewarm', async () => { await rewarmWorker(account.id); await refresh() })}><RefreshCw size={14} />{t('rewarm')}</Button>
                      {authUrl ? <Button size="sm" variant="ghost" onPress={() => window.open(authUrl, '_blank', 'noopener,noreferrer')}><ExternalLink size={14} />{t('open')}</Button> : null}
                    </div>
                  </div>
                  <div>
                    <div className="mb-2 text-[10px] font-semibold tracking-[0.1em] text-[var(--app-faint)] uppercase">{t('patFallback')}</div>
                    <div className="flex flex-col gap-2 sm:flex-row">
                      <Input type="password" value={patById[account.id] || ''} onChange={(event) => setPatById((current) => ({ ...current, [account.id]: event.target.value }))} placeholder={t('pasteToken')} aria-label={t('pat')} />
                      <Button size="sm" variant="secondary" isPending={thisBusy === 'pat'} onPress={() => void onPat(account.id)}><KeyRound size={14} />{t('usePat')}</Button>
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
