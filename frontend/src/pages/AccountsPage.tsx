import { useCallback, useEffect, useMemo, useState } from 'react'
import { Button } from '@heroui/react'
import {
  MagnifyingGlass,
  Plus,
} from '@phosphor-icons/react'
import { AccountCard, type AccountBusyKind } from '@/components/account/AccountCard'
import { AccountCheckinRecordsModal } from '@/components/account/AccountCheckinRecordsModal'
import { AccountModelsModal } from '@/components/account/AccountModelsModal'
import { EditAccountModal } from '@/components/account/EditAccountModal'
import { AddAccountModal } from '@/components/AddAccountModal'
import { BrandMark } from '@/components/BrandMark'
import { ConfirmDialog } from '@/components/ui/ConfirmDialog'
import { EmptyPanel } from '@/components/ui/EmptyPanel'
import { FilterToggle } from '@/components/ui/FilterToggle'
import { ACCOUNT_PAGE_SIZES, ListPager, type PageSize } from '@/components/ui/ListPager'
import { SearchBar } from '@/components/ui/SearchBar'
import { useI18n } from '@/hooks/useI18n'
import {
  checkinAccount,
  deleteAccount,
  exportAccount,
  fetchAccounts,
  completeLoginCallback,
  fetchLoginStatus,
  loginWithPat,
  refreshAccount,
  startDeviceLogin,
  updateAccount,
} from '@/api/overview'
import { AccountsPageSkeleton } from '@/components/ui/PageSkeletons'
import {
  accountState,
  isAvailable,
  type AccountRow,
} from '@/lib/account'
import { accountProviderFamilyLabel } from '@/lib/provider'
import { ProviderMark } from '@/components/ProviderMark'

type AccountBusy = { id: string; kind: AccountBusyKind }
type AccountFilter = 'all' | 'available' | 'attention' | 'disabled'

const EMPTY_ACCOUNTS: AccountRow[] = []
const ACCOUNT_REFRESH_BATCH_SIZE = 2

export function AccountsPage() {
  const { t } = useI18n()
  const [accounts, setAccounts] = useState<AccountRow[]>([])
  const [accountsLoading, setAccountsLoading] = useState(true)
  const [accountsRefreshing, setAccountsRefreshing] = useState(false)
  const [refreshingById, setRefreshingById] = useState<Record<string, boolean>>({})
  const rows = accounts
  const hasAccounts = rows.length > 0
  const refreshing = accountsRefreshing && hasAccounts
  const [addOpen, setAddOpen] = useState(false)
  const [busy, setBusy] = useState<AccountBusy | null>(null)
  const [patById, setPatById] = useState<Record<string, string>>({})
  const [callbackById, setCallbackById] = useState<Record<string, string>>({})
  const [noteById, setNoteById] = useState<Record<string, string>>({})
  const [urlById, setUrlById] = useState<Record<string, string>>({})
  const [confirmId, setConfirmId] = useState<string | null>(null)
  const [modelsId, setModelsId] = useState<string | null>(null)
  const [checkinHistoryId, setCheckinHistoryId] = useState<string | null>(null)
  const [editId, setEditId] = useState<string | null>(null)
  const [authPanelId, setAuthPanelId] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<AccountFilter>('all')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState<PageSize>(5)
  const [enabledById, setEnabledById] = useState<Record<string, boolean>>({})
  const [dropSystemById, setDropSystemById] = useState<Record<string, boolean>>({})
  const [autoCheckinById, setAutoCheckinById] = useState<Record<string, boolean>>({})
  const [nameById, setNameById] = useState<Record<string, string>>({})
  const [inflightById, setInflightById] = useState<Record<string, number>>({})
  const [priorityById, setPriorityById] = useState<Record<string, number>>({})
  const [quotaRefreshing, setQuotaRefreshing] = useState(false)
  const reloadAccounts = useCallback(async (refresh = false) => {
    if (refresh) setAccountsRefreshing(true)
    try {
      const response = await fetchAccounts(refresh)
      const next = response.data || EMPTY_ACCOUNTS
      setAccounts(next)
      return next
    } finally {
      setAccountsLoading(false)
      if (refresh) setAccountsRefreshing(false)
    }
  }, [])

  const refreshAccountsInBatches = useCallback(async (ids: string[], forceQuota = true) => {
    const accountIds = [...new Set(ids)]
    if (!accountIds.length) return
    setAccountsRefreshing(true)
    try {
      for (let start = 0; start < accountIds.length; start += ACCOUNT_REFRESH_BATCH_SIZE) {
        const batch = accountIds.slice(start, start + ACCOUNT_REFRESH_BATCH_SIZE)
        await Promise.all(batch.map(async (id) => {
          setRefreshingById((current) => ({ ...current, [id]: true }))
          try {
            const refreshed = await refreshAccount(id, { quota: forceQuota })
            setAccounts((current) => current.map((account) => account.id === id ? refreshed : account))
          } catch (error) {
            setNoteById((current) => ({ ...current, [id]: error instanceof Error ? error.message : String(error) }))
          } finally {
            setRefreshingById((current) => {
              const next = { ...current }
              delete next[id]
              return next
            })
          }
        }))
      }
    } finally {
      setAccountsRefreshing(false)
    }
  }, [])

  useEffect(() => {
    let active = true
    void (async () => {
      try {
        const initialAccounts = await reloadAccounts(false)
        if (active) void refreshAccountsInBatches(initialAccounts.map((account) => account.id)).catch(() => undefined)
      } catch {
      }
    })()
    return () => { active = false }
  }, [refreshAccountsInBatches, reloadAccounts])
  const displayRows = useMemo(() => rows.map((account) => {
    const enabled = enabledById[account.id]
    const dropSystem = dropSystemById[account.id]
    const autoCheckin = autoCheckinById[account.id]
    const name = nameById[account.id]
    const inflight = inflightById[account.id]
    const priority = priorityById[account.id]
    let next = account
    if (enabled !== undefined) next = { ...next, enabled }
    if (dropSystem !== undefined) next = { ...next, drop_system_prompt: dropSystem }
    if (autoCheckin !== undefined) next = { ...next, workbuddy_auto_checkin: autoCheckin }
    if (name !== undefined) next = { ...next, name }
    if (inflight !== undefined) next = { ...next, max_inflight: inflight }
    if (priority !== undefined) next = { ...next, priority }
    return next
  }), [autoCheckinById, enabledById, dropSystemById, inflightById, nameById, priorityById, rows])

  const availableCount = displayRows.filter(isAvailable).length
  const attentionCount = displayRows.filter((account) => account.enabled && !isAvailable(account)).length
  const inFlightCount = displayRows.reduce((total, account) => total + (account.in_flight ?? account.inFlight ?? 0), 0)
  const providerCounts = useMemo(() => {
    const counts = new Map<string, number>()
    for (const account of displayRows) {
      const key = String(account.provider || 'qoder').toLowerCase()
      counts.set(key, (counts.get(key) || 0) + 1)
    }
    return [...counts.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
  }, [displayRows])
  const confirmAccount = displayRows.find((account) => account.id === confirmId)
  const modelsAccount = displayRows.find((account) => account.id === modelsId) || null
  const checkinHistoryAccount = displayRows.find((account) => account.id === checkinHistoryId) || null
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
  const filterKey = [query, filter, pageSize].join('\0')
  const [appliedFilterKey, setAppliedFilterKey] = useState(filterKey)
  if (appliedFilterKey !== filterKey) {
    setAppliedFilterKey(filterKey)
    setPage(1)
  }
  const pageCount = Math.max(1, Math.ceil(filteredRows.length / pageSize))
  const currentPage = Math.min(appliedFilterKey !== filterKey ? 1 : Math.max(1, page), pageCount)
  if (page !== currentPage) {
    setPage(currentPage)
  }
  const pagedRows = useMemo(() => {
    const start = (currentPage - 1) * pageSize
    return filteredRows.slice(start, start + pageSize)
  }, [currentPage, filteredRows, pageSize])
  const shownFrom = filteredRows.length === 0 ? 0 : (currentPage - 1) * pageSize + 1
  const shownTo = Math.min(filteredRows.length, currentPage * pageSize)

  if (accountsLoading && !hasAccounts) return <AccountsPageSkeleton />

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
      await reloadAccounts(false)
      await refreshAccountsInBatches([id])
    })
  }

  async function onCallback(id: string) {
    const pasted = (callbackById[id] || '').trim()
    if (!pasted) {
      setNoteById((current) => ({ ...current, [id]: t('wizardCallbackPh') }))
      return
    }
    const ok = await run(id, 'callback', async () => {
      setNoteById((current) => ({ ...current, [id]: t('wizardStartingSession') }))
      await completeLoginCallback(id, pasted)
      setCallbackById((current) => ({ ...current, [id]: '' }))
      await reloadAccounts(false)
      await refreshAccountsInBatches([id])
    })
    if (ok) {
      setAuthPanelId((current) => (current === id ? null : current))
      setNoteById((current) => ({ ...current, [id]: '' }))
    }
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
      await reloadAccounts(false)
      await refreshAccountsInBatches([id])
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
      await reloadAccounts(false)
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
      await reloadAccounts(false)
    })
    setDropSystemById((current) => {
      const next = { ...current }
      delete next[id]
      return next
    })
  }

  async function onToggleAutoCheckin(id: string, selected: boolean) {
    setAutoCheckinById((current) => ({ ...current, [id]: selected }))
    await run(id, 'toggle', async () => {
      await updateAccount(id, { workbuddy_auto_checkin: selected })
      await reloadAccounts(false)
    })
    setAutoCheckinById((current) => {
      const next = { ...current }
      delete next[id]
      return next
    })
  }

  async function onCheckin(id: string) {
    await run(id, 'checkin', async () => {
      try {
        await checkinAccount(id)
      } finally {
        await reloadAccounts(false)
      }
    })
  }

  async function onRefreshAccount(id: string) {
    setBusy({ id, kind: 'refresh' })
    setNoteById((current) => {
      const next = { ...current }
      delete next[id]
      return next
    })
    try {
      await refreshAccountsInBatches([id])
    } finally {
      setBusy(null)
    }
  }

  async function onRefreshCredits() {
    setQuotaRefreshing(true)
    try {
      await refreshAccountsInBatches(accounts.map((account) => account.id))
    } finally {
      setQuotaRefreshing(false)
    }
  }

  async function onSaveSettings(id: string, input: { name: string; max_inflight: number; priority: number }) {
    if (!id) throw new Error(t('accountNameRequired'))
    setNameById((current) => ({ ...current, [id]: input.name }))
    setInflightById((current) => ({ ...current, [id]: input.max_inflight }))
    setPriorityById((current) => ({ ...current, [id]: input.priority }))
    setBusy({ id, kind: 'settings' })
    try {
      await updateAccount(id, input)
      await reloadAccounts(false)
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
      <section data-gsap-reveal className="grid gap-3 border-b border-separator pb-3 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end">
        <div className="flex flex-wrap items-center gap-x-5 gap-y-2 text-sm">
          <div className="flex items-center gap-3">
            <span className="mono font-semibold">{rows.length}</span>
            <span className="text-muted">{t('accountCount')}</span>
            <span className="text-border">·</span>
            <span className="mono font-medium text-success">{availableCount}</span>
            <span className="text-success">{t('availableAccounts')}</span>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-muted">{t('needsAttention')}</span>
            <span className={`mono font-medium ${attentionCount ? 'text-danger' : ''}`}>{attentionCount}</span>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-muted">{t('inFlight')}</span>
            <span className="mono font-medium">{inFlightCount}</span>
          </div>
          {providerCounts.map(([provider, count]) => (
            <div key={provider} className="flex items-center gap-1.5">
              <ProviderMark provider={provider} size={12} />
              <span className="text-muted">{accountProviderFamilyLabel(provider, t)}</span>
              <span className="mono font-medium">{count}</span>
            </div>
          ))}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {accountsRefreshing ? <span className="text-xs text-muted" role="status">{t('refreshing')}</span> : null}
          <Button size="sm" variant="secondary" isPending={quotaRefreshing} onPress={() => void onRefreshCredits()}>
            {t('refreshCredits')}
          </Button>
          <Button size="sm" onPress={() => setAddOpen(true)}><Plus size={14} />{t('addAccount')}</Button>
        </div>
      </section>

      <AddAccountModal isOpen={addOpen} onClose={() => setAddOpen(false)} onAdded={() => void reloadAccounts(false)} />
      <AccountModelsModal key={modelsId ?? 'closed'} account={modelsAccount} t={t} onClose={() => setModelsId(null)} />
      <AccountCheckinRecordsModal key={checkinHistoryId ?? 'closed'} account={checkinHistoryAccount} t={t} onClose={() => setCheckinHistoryId(null)} />
      <EditAccountModal
        key={editId ?? 'closed'}
        account={editAccount}
        busy={Boolean(busy && busy.id === editId && busy.kind === 'settings')}
        t={t}
        onClose={() => setEditId(null)}
        onSave={(input) => onSaveSettings(editId || '', input)}
      />

      <ConfirmDialog
        isOpen={Boolean(confirmAccount)}
        title={t('delete')}
        description={t('deleteAccountConfirm', { name: confirmAccount?.name || confirmAccount?.id || '' })}
        confirmLabel={t('delete')}
        cancelLabel={t('cancel')}
        closeLabel={t('close')}
        isPending={busy?.id === confirmId && busy.kind === 'delete'}
        onClose={() => setConfirmId(null)}
        onConfirm={() => {
          if (!confirmAccount) return
          const id = confirmAccount.id
          setConfirmId(null)
          void run(id, 'delete', async () => {
            await deleteAccount(id)
            await reloadAccounts(false)
          })
        }}
      />

      {rows.length ? (
        <section data-gsap-reveal className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
          <div className="w-full lg:max-w-md">
            <SearchBar
              className="w-full"
              value={query}
              onChange={setQuery}
              placeholder={t('searchAccounts')}
              ariaLabel={t('searchAccounts')}
            />
          </div>
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
            <FilterToggle
              value={filter}
              onChange={(next) => setFilter(next as AccountFilter)}
              ariaLabel={t('filterAll')}
              options={[
                { id: 'all', label: t('filterAll') },
                { id: 'available', label: t('availableAccounts') },
                { id: 'attention', label: t('needsAttention') },
                { id: 'disabled', label: t('disabled') },
              ]}
            />
            <span className="mono text-xs text-muted">
              {filteredRows.length
                ? t('logsShownTotal', { shown: `${shownFrom}–${shownTo}`, total: filteredRows.length })
                : t('shownAccounts', { shown: 0, total: rows.length })}
            </span>
          </div>
        </section>
      ) : null}

      {!refreshing && !hasAccounts ? (
        <EmptyPanel
          className="rounded-3xl border border-dashed border-border"
          icon={<BrandMark size={28} />}
          title={t('noAccounts')}
          hint={t('accountEmptyHint')}
          action={<Button size="sm" onPress={() => setAddOpen(true)}><Plus size={14} />{t('addAccount')}</Button>}
        />
      ) : null}

      {hasAccounts && !filteredRows.length ? (
        <EmptyPanel
          className="rounded-3xl border border-dashed border-border"
          icon={<MagnifyingGlass size={22} />}
          title={t('noAccountsMatch')}
          action={<Button size="sm" variant="ghost" onPress={() => { setQuery(''); setFilter('all') }}>{t('clearFilters')}</Button>}
        />
      ) : null}

      <section className="grid gap-2.5 lg:grid-cols-2 xl:grid-cols-3" aria-busy={refreshing}>
        {pagedRows.map((account) => (
          <AccountCard
            key={account.id}
            account={account}
            busyKind={busy?.id === account.id ? busy.kind : ''}
            detailsLoading={Boolean(refreshingById[account.id])}
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
            onRefresh={() => void onRefreshAccount(account.id)}
            onDelete={() => setConfirmId(account.id)}
            onToggle={(selected) => void onToggle(account.id, selected)}
            onToggleDropSystem={(selected) => void onToggleDropSystem(account.id, selected)}
            onToggleAutoCheckin={(selected) => void onToggleAutoCheckin(account.id, selected)}
            onCheckin={account.provider === 'workbuddy' ? () => void onCheckin(account.id) : undefined}
            onViewCheckins={account.provider === 'workbuddy' ? () => setCheckinHistoryId(account.id) : undefined}
            onEdit={() => setEditId(account.id)}
            onToggleAuthPanel={() => setAuthPanelId((current) => current === account.id ? null : account.id)}
            onViewModels={() => setModelsId(account.id)}
          />
        ))}
      </section>

      {filteredRows.length ? (
        <ListPager
          total={filteredRows.length}
          page={currentPage}
          pageCount={pageCount}
          pageSize={pageSize}
          pageSizes={ACCOUNT_PAGE_SIZES}
          loading={false}
          pageSizeLabel={t('logsPageSize')}
          pageLabel={t('logsPage', { page: currentPage, pages: pageCount })}
          prevLabel={t('logsPrevPage')}
          nextLabel={t('logsNextPage')}
          onPage={setPage}
          onPageSize={setPageSize}
        />
      ) : null}
    </div>
  )
}
