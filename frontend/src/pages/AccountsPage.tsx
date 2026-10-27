import { toast } from '@/lib/toast'
import { copyText } from '@/lib/clipboard'
import { useCallback, useEffect, useMemo, useState, type ChangeEvent } from 'react'
import { Button, Label, ListBox, Select } from '@heroui/react'
import {
  ArrowsClockwise,
  MagnifyingGlass,
  Plus,
  UploadSimple,
} from '@phosphor-icons/react'
import { AccountCard, type AccountBusyKind } from '@/components/account/AccountCard'
import { AccountCheckinRecordsModal } from '@/components/account/AccountCheckinRecordsModal'
import { AccountModelsModal } from '@/components/account/AccountModelsModal'
import { EditAccountModal } from '@/components/account/EditAccountModal'
import { AddAccountModal } from '@/components/AddAccountModal'
import { BrandMark } from '@/components/BrandMark'
import { ConfirmDialog } from '@/components/ui/ConfirmDialog'
import { EmptyPanel } from '@/components/ui/EmptyPanel'
import { CaiaiSoftLink } from '@/components/CaiaiSoftLink'
import { PageAlert } from '@/components/ui/PageAlert'
import { FilterToggle } from '@/components/ui/FilterToggle'
import { ACCOUNT_PAGE_SIZES, ListPager, type PageSize } from '@/components/ui/ListPager'
import { SearchBar } from '@/components/ui/SearchBar'
import { useI18n } from '@/hooks/useI18n'
import {
  checkinAccount,
  deleteAccount,
  exportAccount,
  fetchAccounts,
  fetchProviders,
  completeLoginCallback,
  fetchLoginStatus,
  loginWithPat,
  refreshAccount,
  startDeviceLogin,
  updateAccount,
  type ProviderDescriptor,
} from '@/api/overview'
import { fetchSystemSettings, type SystemSettings } from '@/api/system'
import { AccountsPageSkeleton } from '@/components/ui/PageSkeletons'
import {
  accountState,
  type AccountRow,
} from '@/lib/account'
import { accountProviderFamilyLabel } from '@/lib/provider'
import { ProviderMark } from '@/components/ProviderMark'
import { BatchImportConfirmModal } from '@/components/account/BatchImportConfirmModal'
import {
  detectImportBundle,
  parseImportBundleText,
  type ImportDetectResult,
  type ImportRegion,
} from '@/lib/accountImportDetect'
import {
  importWorkbuddyBundle,
  summarizeWorkbuddyImportResult,
} from '@/lib/workbuddyImport'

type AccountBusy = { id: string; kind: AccountBusyKind }
type AccountFilter = 'all' | 'available' | 'attention' | 'disabled'
const ALL_PROVIDERS = '__all__'

const EMPTY_ACCOUNTS: AccountRow[] = []
const ACCOUNT_REFRESH_BATCH_SIZE = 2

export function AccountsPage() {
  const { t } = useI18n()
  const [providers, setProviders] = useState<ProviderDescriptor[]>([])
  const [settings, setSettings] = useState<SystemSettings | null>(null)
  const [checkinError, setCheckinError] = useState('')
  const [accounts, setAccounts] = useState<AccountRow[]>([])
  const [accountsLoading, setAccountsLoading] = useState(true)
  const rows = accounts
  const hasAccounts = rows.length > 0
  const [addOpen, setAddOpen] = useState(false)
  const [batchImporting, setBatchImporting] = useState(false)
  const [batchConfirmOpen, setBatchConfirmOpen] = useState(false)
  const [batchDetection, setBatchDetection] = useState<ImportDetectResult | null>(null)
  const [batchBundle, setBatchBundle] = useState<Record<string, unknown> | null>(null)
  const [batchFileName, setBatchFileName] = useState('')
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
  const [providerFilter, setProviderFilter] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState<PageSize>(20)
  const [enabledById, setEnabledById] = useState<Record<string, boolean>>({})
  const [nameById, setNameById] = useState<Record<string, string>>({})
  const [inflightById, setInflightById] = useState<Record<string, number>>({})
  const [priorityById, setPriorityById] = useState<Record<string, number>>({})
  const [quotaRefreshing, setQuotaRefreshing] = useState(false)
  const reloadAccounts = useCallback(async (refresh = false) => {
    try {
      const response = await fetchAccounts(refresh)
      const next = response.data || EMPTY_ACCOUNTS
      setAccounts(next)
      return next
    } finally {
      setAccountsLoading(false)
    }
  }, [])

  const refreshAccountsInBatches = useCallback(async (ids: string[], forceQuota = true) => {
    const accountIds = [...new Set(ids)]
    if (!accountIds.length) return
    for (let start = 0; start < accountIds.length; start += ACCOUNT_REFRESH_BATCH_SIZE) {
      const batch = accountIds.slice(start, start + ACCOUNT_REFRESH_BATCH_SIZE)
      await Promise.all(batch.map(async (id) => {
        try {
          const refreshed = await refreshAccount(id, { quota: forceQuota })
          setAccounts((current) => current.map((account) => account.id === id ? refreshed : account))
        } catch (error) {
          setNoteById((current) => ({ ...current, [id]: error instanceof Error ? error.message : String(error) }))
        }
      }))
    }
  }, [])

  useEffect(() => {
    void reloadAccounts(false).catch(() => undefined)
  }, [reloadAccounts])
  useEffect(() => {
    let active = true
    void Promise.all([fetchProviders(), fetchSystemSettings()]).then(([providerData, systemData]) => {
      if (!active) return
      setProviders(providerData.data || [])
      setSettings(systemData)
    }).catch((error) => {
      if (active) setCheckinError(error instanceof Error ? error.message : String(error))
    })
    return () => { active = false }
  }, [])

  function checkinPolicyFor(account: AccountRow) {
    return providers.find((provider) => provider.id === account.provider)?.regions.find((region) => region.id === account.region)?.checkin
  }

  function checkinTimezoneFor(account: AccountRow) {
    const timezone = checkinPolicyFor(account)?.timezone
    return timezone === 'Local' ? settings?.timezone : timezone
  }
  const displayRows = useMemo(() => rows.map((account) => {
    const enabled = enabledById[account.id]
    const name = nameById[account.id]
    const inflight = inflightById[account.id]
    const priority = priorityById[account.id]
    let next = account
    if (enabled !== undefined) next = { ...next, enabled }
    if (name !== undefined) next = { ...next, name }
    if (inflight !== undefined) next = { ...next, max_inflight: inflight }
    if (priority !== undefined) next = { ...next, priority }
    return next
  }), [enabledById, inflightById, nameById, priorityById, rows])

  const statusCounts = useMemo(() => {
    const counts = { all: displayRows.length, available: 0, attention: 0, disabled: 0 }
    for (const account of displayRows) {
      const state = accountState(account)
      if (state === 'hot' || state === 'ready') counts.available += 1
      else if (state === 'disabled') counts.disabled += 1
      if (account.enabled && state !== 'hot' && state !== 'ready') counts.attention += 1
    }
    return counts
  }, [displayRows])
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
      if (providerFilter && String(account.provider || 'qoder').toLowerCase() !== providerFilter) return false
      if (!normalized) return true
      return [account.name, account.id, account.remote_uid, account.provider, account.auth_type]
        .some((value) => String(value || '').toLowerCase().includes(normalized))
    })
  }, [displayRows, filter, providerFilter, query])
  const filterKey = [query, filter, providerFilter, pageSize].join('\0')
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

  async function onDeviceLogin(id: string, loginMethod?: string) {
    setAuthPanelId(id)
    await run(id, 'device', async () => {
      setNoteById((current) => ({ ...current, [id]: t('wizardStartingSession') }))
      const output = await startDeviceLogin(id, loginMethod)
      if (output.authUrl) {
        setUrlById((current) => ({ ...current, [id]: output.authUrl || '' }))
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
      await copyText(JSON.stringify(bundle, null, 2))
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

  async function onRefreshAccount(id: string) {
    toast({ id: `refresh-${id}`, type: 'loading', title: t('toastRefreshingAccount') })
    setBusy({ id, kind: 'refresh' })
    setNoteById((current) => {
      const next = { ...current }
      delete next[id]
      return next
    })
    try {
      await refreshAccountsInBatches([id])
      toast({ id: `refresh-${id}`, type: 'success', title: t('toastAccountRefreshed') })
    } catch (error) {
      toast({ id: `refresh-${id}`, type: 'error', title: t('toastFailed'), description: error instanceof Error ? error.message : String(error) })
      throw error
    } finally {
      setBusy(null)
    }
  }

  async function onRefreshCredits() {
    setQuotaRefreshing(true)
    toast({ id: 'refresh-credits', type: 'loading', title: t('toastRefreshingCredits') })
    try {
      await refreshAccountsInBatches(accounts.map((account) => account.id))
      toast({ id: 'refresh-credits', type: 'success', title: t('toastCreditsRefreshed') })
    } catch (error) {
      toast({ id: 'refresh-credits', type: 'error', title: t('toastFailed'), description: error instanceof Error ? error.message : String(error) })
      throw error
    } finally {
      setQuotaRefreshing(false)
    }
  }

  function closeBatchConfirm() {
    if (batchImporting) return
    setBatchConfirmOpen(false)
    setBatchDetection(null)
    setBatchBundle(null)
    setBatchFileName('')
  }

  async function onBatchImportFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]
    event.target.value = ''
    if (!file) return
    try {
      const raw = await file.text()
      const record = parseImportBundleText(raw)
      const detection = detectImportBundle(record)
      if (!detection.provider) {
        toast({ type: 'error', title: t('batchImportUnrecognized') })
        return
      }
      setBatchBundle(record)
      setBatchDetection(detection)
      setBatchFileName(file.name)
      setBatchConfirmOpen(true)
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      toast({
        type: 'error',
        title: message === 'bad_json' ? t('wizardBadJson') : t('batchImportUnrecognized'),
        description: message === 'bad_json' ? undefined : message,
      })
    }
  }

  async function onBatchImportConfirm(region: ImportRegion) {
    if (!batchBundle || !batchDetection?.batchImportSupported || batchImporting) return
    setBatchImporting(true)
    const toastId = 'batch-import-accounts'
    toast({ id: toastId, type: 'loading', title: t('batchImportLoading') })
    try {
      const result = await importWorkbuddyBundle(batchBundle, { region })
      const { succeeded, failed, message } = summarizeWorkbuddyImportResult(result, t)
      toast({
        id: toastId,
        type: failed > 0 && succeeded === 0 ? 'error' : failed > 0 ? 'info' : 'success',
        title: message,
      })
      setBatchConfirmOpen(false)
      setBatchDetection(null)
      setBatchBundle(null)
      setBatchFileName('')
      if (succeeded > 0) await reloadAccounts(false)
    } catch (error) {
      toast({
        id: toastId,
        type: 'error',
        title: t('toastFailed'),
        description: error instanceof Error ? error.message : String(error),
      })
    } finally {
      setBatchImporting(false)
    }
  }

  async function onToggleAutoCheckin(id: string, selected: boolean) {
    await run(id, 'toggle', async () => {
      await updateAccount(id, { auto_checkin: selected })
      await reloadAccounts(false)
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

  async function onSaveSettings(id: string, input: { name: string; max_inflight: number; priority: number; proxy_url?: string; checkin_time?: string; drop_system_prompt?: boolean; model_aliases?: Record<string, string>; enabled_models?: string[]; set_model_routing?: boolean }) {
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
      {checkinError ? <PageAlert title={checkinError} /> : null}
      <section data-gsap-reveal className="flex flex-wrap items-center gap-2">
        {rows.length ? (
          <>
            <FilterToggle
              value={filter}
              onChange={(next) => setFilter(next as AccountFilter)}
              ariaLabel={t('accountStatusFilter')}
              options={[
                { id: 'all', label: <>{t('filterAll')}<span className="mono text-muted">{statusCounts.all}</span></> },
                { id: 'available', label: <>{t('availableAccounts')}<span className="mono text-muted">{statusCounts.available}</span></> },
                { id: 'attention', label: <>{t('needsAttention')}<span className={`mono ${statusCounts.attention ? 'text-danger' : 'text-muted'}`}>{statusCounts.attention}</span></> },
                { id: 'disabled', label: <>{t('disabled')}<span className="mono text-muted">{statusCounts.disabled}</span></> },
              ]}
            />
            <Select
              className="w-48"
              aria-label={t('providerFilterAll')}
              value={providerFilter || ALL_PROVIDERS}
              onChange={(next) => {
                if (typeof next === 'string') setProviderFilter(next === ALL_PROVIDERS ? '' : next)
              }}
            >
              <Select.Trigger className="items-center">
                <Select.Value className="min-w-0 truncate" />
                <Select.Indicator />
              </Select.Trigger>
              <Select.Popover>
                <ListBox>
                  <ListBox.Item id={ALL_PROVIDERS} textValue={t('providerFilterAll')}>
                    <Label className="flex items-center gap-2">{t('providerFilterAll')}<span className="mono text-muted">{rows.length}</span></Label>
                    <ListBox.ItemIndicator />
                  </ListBox.Item>
                  {providerCounts.map(([provider, count]) => (
                    <ListBox.Item key={provider} id={provider} textValue={accountProviderFamilyLabel(provider, t)}>
                      <Label className="flex items-center gap-2"><ProviderMark provider={provider} size={13} />{accountProviderFamilyLabel(provider, t)}<span className="mono text-muted">{count}</span></Label>
                      <ListBox.ItemIndicator />
                    </ListBox.Item>
                  ))}
                </ListBox>
              </Select.Popover>
            </Select>
            <SearchBar
              className="w-full sm:w-60"
              value={query}
              onChange={setQuery}
              placeholder={t('searchAccounts')}
              ariaLabel={t('searchAccounts')}
            />
          </>
        ) : null}
        <div className="flex w-full flex-wrap items-center justify-end gap-2 sm:ml-auto sm:w-auto">
          <Button size="sm" variant="secondary" isPending={quotaRefreshing} onPress={() => void onRefreshCredits()}>
            <ArrowsClockwise size={14} />{t('refreshCredits')}
          </Button>
          {/*
            Mobile Safari ignores programmatic input.click() from React Aria
            onPress when the file input is display:none. Associate a real
            label with an sr-only input so the picker opens from a direct
            user gesture (same detect → confirm → WorkBuddy path).
          */}
          <label
            htmlFor="accounts-batch-import-file"
            className={`button button--sm button--secondary inline-flex items-center gap-1.5 ${
              batchImporting || batchConfirmOpen ? 'pointer-events-none opacity-60' : 'cursor-pointer'
            }`}
          >
            {batchImporting ? <BrandMark size={14} loading /> : <UploadSimple size={14} />}
            {t('batchImport')}
          </label>
          <input
            id="accounts-batch-import-file"
            type="file"
            accept=".json,application/json,text/json"
            disabled={batchImporting || batchConfirmOpen}
            className="sr-only"
            onChange={(event) => void onBatchImportFile(event)}
          />
          <Button size="sm" onPress={() => setAddOpen(true)}><Plus size={14} />{t('addAccount')}</Button>
        </div>
      </section>

      <AddAccountModal isOpen={addOpen} onClose={() => setAddOpen(false)} onAdded={() => { toast({ type: 'success', title: t('toastAccountAdded') }); void reloadAccounts(false) }} />
      <BatchImportConfirmModal
        isOpen={batchConfirmOpen}
        detection={batchDetection}
        fileName={batchFileName}
        isPending={batchImporting}
        t={t}
        onClose={closeBatchConfirm}
        onConfirm={(region) => { void onBatchImportConfirm(region) }}
      />
      <AccountModelsModal key={modelsId ?? 'closed'} account={modelsAccount} t={t} onClose={() => setModelsId(null)} />
      <AccountCheckinRecordsModal key={checkinHistoryId ?? 'closed'} account={checkinHistoryAccount} t={t} onClose={() => setCheckinHistoryId(null)} />
      <EditAccountModal
        key={editId ?? 'closed'}
        account={editAccount}
        checkinDefaultTime={editAccount && checkinPolicyFor(editAccount) ? settings?.checkin_times[editAccount.provider || ''] : undefined}
        checkinTimezone={editAccount ? checkinTimezoneFor(editAccount) : undefined}
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
          }).then((ok) => {
            if (ok) toast({ type: 'success', title: t('toastAccountDeleted') })
          })
        }}
      />


      {!hasAccounts ? (
        <div className="space-y-3">
          <EmptyPanel
            className="rounded-3xl bg-surface-secondary"
            icon={<BrandMark size={28} />}
            title={t('noAccounts')}
            hint={t('accountEmptyHint')}
            action={<Button size="sm" onPress={() => setAddOpen(true)}><Plus size={14} />{t('addAccount')}</Button>}
          />
          <div className="text-center">
            <CaiaiSoftLink />
          </div>
        </div>
      ) : null}

      {hasAccounts && !filteredRows.length ? (
        <EmptyPanel
          className="rounded-3xl bg-surface-secondary"
          icon={<MagnifyingGlass size={22} />}
          title={t('noAccountsMatch')}
          action={<Button size="sm" variant="ghost" onPress={() => { setQuery(''); setFilter('all'); setProviderFilter('') }}>{t('clearFilters')}</Button>}
        />
      ) : null}

      <section className="grid auto-rows-[minmax(280px,auto)] gap-2.5 lg:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
        {pagedRows.map((account) => (
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
            onDeviceLogin={(loginMethod) => void onDeviceLogin(account.id, loginMethod)}
            onPatLogin={() => void onPat(account.id)}
            onExport={() => void onExport(account.id)}
            onRefresh={() => void onRefreshAccount(account.id)}
            onDelete={() => setConfirmId(account.id)}
            onToggle={(selected) => void onToggle(account.id, selected)}
            checkinDefaultTime={settings?.checkin_times[account.provider || '']}
            onToggleAutoCheckin={checkinPolicyFor(account) ? (selected) => void onToggleAutoCheckin(account.id, selected) : undefined}
            onCheckin={checkinPolicyFor(account) ? () => void onCheckin(account.id) : undefined}
            onViewCheckins={checkinPolicyFor(account) ? () => setCheckinHistoryId(account.id) : undefined}
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
