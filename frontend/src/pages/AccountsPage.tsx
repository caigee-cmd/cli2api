import { useMemo, useState } from 'react'
import { Button, ButtonGroup, InputGroup, Modal } from '@heroui/react'
import {
  MagnifyingGlass,
  Plus,
  WarningCircle,
  X,
} from '@phosphor-icons/react'
import { AccountCard, type AccountBusyKind } from '@/components/account/AccountCard'
import { AccountModelsModal } from '@/components/account/AccountModelsModal'
import { EditAccountModal } from '@/components/account/EditAccountModal'
import { AddAccountModal } from '@/components/AddAccountModal'
import { BrandMark } from '@/components/BrandMark'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import {
  deleteAccount,
  exportAccount,
  completeLoginCallback,
  fetchLoginStatus,
  loginWithPat,
  rewarmWorker,
  startDeviceLogin,
  updateAccount,
} from '@/api/overview'
import { AccountsPageSkeleton } from '@/components/ui/PageSkeletons'
import {
  accountState,
  isAvailable,
  type AccountRow,
} from '@/lib/account'

type AccountBusy = { id: string; kind: AccountBusyKind }
type AccountFilter = 'all' | 'available' | 'attention' | 'disabled'

const EMPTY_ACCOUNTS: AccountRow[] = []
const ACCOUNT_BUTTON_CLASS = 'account-button'
const ACCOUNT_INPUT_CLASS = 'account-input'

export function AccountsPage() {
  const { t } = useI18n()
  const { overview, loading, refresh } = useOverview()
  const rows = overview?.accounts ?? EMPTY_ACCOUNTS
  const hasAccounts = rows.length > 0
  const refreshing = loading && hasAccounts
  const [addOpen, setAddOpen] = useState(false)
  const [busy, setBusy] = useState<AccountBusy | null>(null)
  const [patById, setPatById] = useState<Record<string, string>>({})
  const [callbackById, setCallbackById] = useState<Record<string, string>>({})
  const [noteById, setNoteById] = useState<Record<string, string>>({})
  const [urlById, setUrlById] = useState<Record<string, string>>({})
  const [confirmId, setConfirmId] = useState<string | null>(null)
  const [modelsId, setModelsId] = useState<string | null>(null)
  const [editId, setEditId] = useState<string | null>(null)
  const [authPanelId, setAuthPanelId] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<AccountFilter>('all')
  const [enabledById, setEnabledById] = useState<Record<string, boolean>>({})
  const [dropSystemById, setDropSystemById] = useState<Record<string, boolean>>({})
  const [nameById, setNameById] = useState<Record<string, string>>({})
  const [inflightById, setInflightById] = useState<Record<string, number>>({})
  const [priorityById, setPriorityById] = useState<Record<string, number>>({})
  const displayRows = useMemo(() => rows.map((account) => {
    const enabled = enabledById[account.id]
    const dropSystem = dropSystemById[account.id]
    const name = nameById[account.id]
    const inflight = inflightById[account.id]
    const priority = priorityById[account.id]
    let next = account
    if (enabled !== undefined) next = { ...next, enabled }
    if (dropSystem !== undefined) next = { ...next, drop_system_prompt: dropSystem }
    if (name !== undefined) next = { ...next, name }
    if (inflight !== undefined) next = { ...next, max_inflight: inflight }
    if (priority !== undefined) next = { ...next, priority }
    return next
  }), [enabledById, dropSystemById, inflightById, nameById, priorityById, rows])

  const availableCount = displayRows.filter(isAvailable).length
  const attentionCount = displayRows.filter((account) => account.enabled && !isAvailable(account)).length
  const inFlightCount = displayRows.reduce((total, account) => total + (account.in_flight ?? account.inFlight ?? 0), 0)
  const confirmAccount = displayRows.find((account) => account.id === confirmId)
  const modelsAccount = displayRows.find((account) => account.id === modelsId) || null
  const editAccount = displayRows.find((account) => account.id === editId) || null
  const filteredRows = useMemo(() => {
    const normalized = query.trim().toLowerCase()
    return displayRows.filter((account) => {
      const state = accountState(account)
      const matchesFilter = filter === 'all'
        || (filter === 'available' && (state === 'hot' || state === 'ready'))
        || (filter === 'attention' && account.enabled && state !== 'hot' && state !== 'ready')
        || (filter === 'disabled' && state === 'disabled')
      if (!matchesFilter) return false
      if (!normalized) return true
      return [account.name, account.id, account.remote_uid, account.provider, account.auth_type]
        .some((value) => String(value || '').toLowerCase().includes(normalized))
    })
  }, [displayRows, filter, query])

  if (loading && !hasAccounts) return <AccountsPageSkeleton />

  async function run(id: string, kind: AccountBusyKind, action: () => Promise<void>) {
    setBusy({ id, kind })
    setNoteById((current) => ({ ...current, [id]: '' }))
    try {
      await action()
      return true
    } catch (error) {
      setNoteById((current) => ({ ...current, [id]: error instanceof Error ? error.message : String(error) }))
      return false
    } finally {
      setBusy(null)
    }
  }

  async function onDeviceLogin(id: string) {
    setAuthPanelId(id)
    await run(id, 'device', async () => {
      setNoteById((current) => ({ ...current, [id]: t('wizardStartingSession') }))
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
      await refresh(undefined, { silent: true })
    })
  }

  async function onCallback(id: string) {
    const pasted = (callbackById[id] || '').trim()
    if (!pasted) {
      setNoteById((current) => ({ ...current, [id]: t('wizardCallbackPh') }))
      return
    }
    await run(id, 'callback', async () => {
      await completeLoginCallback(id, pasted)
      setCallbackById((current) => ({ ...current, [id]: '' }))
      await refresh(undefined, { silent: true })
    })
  }

  async function onPat(id: string) {
    const pat = (patById[id] || '').trim()
    if (!pat) {
      setNoteById((current) => ({ ...current, [id]: t('pastePatFirst') }))
      return
    }
    await run(id, 'pat', async () => {
      setNoteById((current) => ({ ...current, [id]: t('wizardStartingSession') }))
      await loginWithPat(pat, id)
      setPatById((current) => ({ ...current, [id]: '' }))
      await refresh(undefined, { silent: true })
    })
  }

  async function onExport(id: string) {
    await run(id, 'export', async () => {
      const bundle = await exportAccount(id)
      await navigator.clipboard.writeText(JSON.stringify(bundle, null, 2))
      setNoteById((current) => ({ ...current, [id]: t('credentialCopied') }))
    })
  }

  async function onToggle(id: string, selected: boolean) {
    setEnabledById((current) => ({ ...current, [id]: selected }))
    await run(id, 'toggle', async () => {
      await updateAccount(id, { enabled: selected })
      await refresh(undefined, { silent: true })
    })
    setEnabledById((current) => {
      const next = { ...current }
      delete next[id]
      return next
    })
  }

  async function onToggleDropSystem(id: string, selected: boolean) {
    setDropSystemById((current) => ({ ...current, [id]: selected }))
    await run(id, 'toggle', async () => {
      await updateAccount(id, { drop_system_prompt: selected })
      await refresh(undefined, { silent: true })
    })
    setDropSystemById((current) => {
      const next = { ...current }
      delete next[id]
      return next
    })
  }

  async function onSaveSettings(id: string, input: { name: string; max_inflight: number; priority: number }) {
    if (!id) throw new Error(t('accountNameRequired'))
    setNameById((current) => ({ ...current, [id]: input.name }))
    setInflightById((current) => ({ ...current, [id]: input.max_inflight }))
    setPriorityById((current) => ({ ...current, [id]: input.priority }))
    setBusy({ id, kind: 'settings' })
    try {
      await updateAccount(id, input)
      await refresh(undefined, { silent: true })
    } catch (error) {
      throw error instanceof Error ? error : new Error(String(error))
    } finally {
      setBusy(null)
      setNameById((current) => {
        const next = { ...current }
        delete next[id]
        return next
      })
      setInflightById((current) => {
        const next = { ...current }
        delete next[id]
        return next
      })
      setPriorityById((current) => {
        const next = { ...current }
        delete next[id]
        return next
      })
    }
  }

  return (
    <div className="space-y-4">
      <section data-gsap-reveal className="grid gap-3 border-b border-[var(--app-line)] pb-3 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end">
        <div className="flex flex-wrap items-center gap-x-5 gap-y-2 text-sm">
          <div className="flex items-center gap-3 border-l-2 border-[var(--app-ok)] pl-3">
            <span className="mono font-semibold">{rows.length}</span>
            <span className="text-[var(--app-faint)]">{t('accountCount')}</span>
            <span className="text-[var(--app-line-strong)]">·</span>
            <span className="mono font-medium text-[var(--app-ok)]">{availableCount}</span>
            <span className="text-[var(--app-ok)]">{t('availableAccounts')}</span>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-[var(--app-faint)]">{t('needsAttention')}</span>
            <span className={`mono font-medium ${attentionCount ? 'text-[var(--app-danger)]' : ''}`}>{attentionCount}</span>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-[var(--app-faint)]">{t('inFlight')}</span>
            <span className="mono font-medium">{inFlightCount}</span>
          </div>
        </div>
        <Button className={ACCOUNT_BUTTON_CLASS} size="sm" onPress={() => setAddOpen(true)}><Plus size={14} />{t('addAccount')}</Button>
      </section>

      <AddAccountModal isOpen={addOpen} onClose={() => setAddOpen(false)} onAdded={() => void refresh(undefined, { silent: true })} />
      <AccountModelsModal key={modelsId ?? 'closed'} account={modelsAccount} t={t} onClose={() => setModelsId(null)} />
      <EditAccountModal
        key={editId ?? 'closed'}
        account={editAccount}
        busy={Boolean(busy && busy.id === editId && busy.kind === 'settings')}
        t={t}
        onClose={() => setEditId(null)}
        onSave={(input) => onSaveSettings(editId || '', input)}
      />

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
                <Button className={ACCOUNT_BUTTON_CLASS} size="sm" variant="ghost" onPress={() => setConfirmId(null)}>{t('cancel')}</Button>
                <Button className={ACCOUNT_BUTTON_CLASS} size="sm" variant="danger" isPending={busy?.id === confirmId && busy.kind === 'delete'} onPress={() => {
                  if (!confirmAccount) return
                  const id = confirmAccount.id
                  setConfirmId(null)
                  void run(id, 'delete', async () => { await deleteAccount(id); await refresh(undefined, { silent: true }) })
                }}>{t('delete')}</Button>
              </Modal.Footer>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal.Root>

      {rows.length ? (
        <section data-gsap-reveal className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
          <div className="w-full lg:max-w-md">
            <InputGroup className={ACCOUNT_INPUT_CLASS} fullWidth>
              <InputGroup.Prefix>
                <MagnifyingGlass size={14} className="text-[var(--app-faint)]" />
              </InputGroup.Prefix>
              <InputGroup.Input
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder={t('searchAccounts')}
                aria-label={t('searchAccounts')}
              />
            </InputGroup>
          </div>
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
            <ButtonGroup className="toolbar-group">
              {([
                ['all', t('filterAll')],
                ['available', t('availableAccounts')],
                ['attention', t('needsAttention')],
                ['disabled', t('disabled')],
              ] as Array<[AccountFilter, string]>).map(([value, label]) => (
                <Button key={value} className={ACCOUNT_BUTTON_CLASS} size="sm" variant={filter === value ? 'secondary' : 'ghost'} onPress={() => setFilter(value)}>{label}</Button>
              ))}
            </ButtonGroup>
            <span className="mono text-xs text-[var(--app-faint)]">{t('shownAccounts', { shown: filteredRows.length, total: rows.length })}</span>
          </div>
        </section>
      ) : null}

      {!hasAccounts ? (
        <div className="grid min-h-72 place-items-center rounded-lg border border-dashed border-[var(--app-line-strong)] text-center">
          <div className="max-w-sm px-6">
            <BrandMark size={28} className="mx-auto" />
            <div className="mt-4 text-sm font-medium">{t('noAccounts')}</div>
            <div className="mt-1 text-xs leading-5 text-[var(--app-faint)]">{t('accountEmptyHint')}</div>
            <Button className={`${ACCOUNT_BUTTON_CLASS} mt-5`} size="sm" onPress={() => setAddOpen(true)}><Plus size={14} />{t('addAccount')}</Button>
          </div>
        </div>
      ) : null}

      {hasAccounts && !filteredRows.length ? (
        <div className="grid min-h-56 place-items-center rounded-lg border border-dashed border-[var(--app-line-strong)] text-center">
          <div>
            <MagnifyingGlass size={22} className="mx-auto text-[var(--app-faint)]" />
            <div className="mt-3 text-sm font-medium">{t('noAccountsMatch')}</div>
            <Button className={`${ACCOUNT_BUTTON_CLASS} mt-4`} size="sm" variant="ghost" onPress={() => { setQuery(''); setFilter('all') }}>{t('clearFilters')}</Button>
          </div>
        </div>
      ) : null}

      <section className="grid gap-2.5 lg:grid-cols-2 xl:grid-cols-3" aria-busy={refreshing}>
        {filteredRows.map((account) => (
          <AccountCard
            key={account.id}
            account={account}
            busyKind={busy?.id === account.id ? busy.kind : ''}
            authPanelOpen={authPanelId === account.id}
            authUrl={urlById[account.id]}
            note={noteById[account.id]}
            pat={patById[account.id] || ''}
            t={t}
            onPatChange={(value) => setPatById((current) => ({ ...current, [account.id]: value }))}
            callbackUrl={callbackById[account.id] || ''}
            onCallbackChange={(value) => setCallbackById((current) => ({ ...current, [account.id]: value }))}
            onSubmitCallback={() => void onCallback(account.id)}
            onDeviceLogin={() => void onDeviceLogin(account.id)}
            onPatLogin={() => void onPat(account.id)}
            onExport={() => void onExport(account.id)}
            onRewarm={() => void run(account.id, 'rewarm', async () => { await rewarmWorker(account.id); await refresh(undefined, { silent: true }) })}
            onDelete={() => setConfirmId(account.id)}
            onToggle={(selected) => void onToggle(account.id, selected)}
            onToggleDropSystem={(selected) => void onToggleDropSystem(account.id, selected)}
            onEdit={() => setEditId(account.id)}
            onToggleAuthPanel={() => setAuthPanelId((current) => current === account.id ? null : account.id)}
            onViewModels={() => setModelsId(account.id)}
          />
        ))}
      </section>
    </div>
  )
}
