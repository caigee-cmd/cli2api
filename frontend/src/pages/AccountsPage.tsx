import { toast } from '@/lib/toast'
import { copyText } from '@/lib/clipboard'
import { useCallback, useEffect, useMemo, useRef, useState, type ChangeEvent } from 'react'
import { Button, Label, ListBox, Select } from '@heroui/react'
import {
  ArrowsClockwise,
  CalendarCheck,
  DownloadSimple,
  MagnifyingGlass,
  Plus,
  SignIn,
  UploadSimple,
} from '@phosphor-icons/react'
import { AccountCard, type AccountBusyKind, type CheckinFeedback } from '@/components/account/AccountCard'
import { BatchReloginModal } from '@/components/account/BatchReloginModal'
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
  checkinAll,
  deleteAccount,
  exportAccount,
  exportAccountBundle,
  importAccountBundle,
  fetchAccounts,
  fetchProviders,
  completeLoginCallback,
  fetchLoginStatus,
  importAccount,
  loginWithPat,
  refreshAccount,
  refreshAllAccounts,
  startDeviceLogin,
  updateAccount,
  type ProviderDescriptor,
} from '@/api/overview'
import { fetchSystemSettings, type SystemSettings } from '@/api/system'
import { AccountsPageSkeleton } from '@/components/ui/PageSkeletons'
import {
  accountState,
  formatQuotaAmount,
  type AccountRow,
} from '@/lib/account'
import { useAccountEvents } from '@/hooks/useAccountEvents'
import type {
  AccountStateEvent,
  BulkEventData,
  CheckinStateEvent,
  QuotaStateEvent,
  ServerEvent,
} from '@/api/events'
import { accountProviderFamilyLabel } from '@/lib/provider'
import { ProviderMark } from '@/components/ProviderMark'
import { BatchImportConfirmModal } from '@/components/account/BatchImportConfirmModal'
import {
  detectImportBundle,
  kiroRegionOf,
  looksLikeKiro,
  parseImportBundleText,
  type ImportDetectResult,
  type ImportRegion,
} from '@/lib/accountImportDetect'
import {
  importWorkbuddyBundle,
  summarizeWorkbuddyImportResult,
  type WorkbuddyImportResult,
} from '@/lib/workbuddyImport'
import { downloadAccountBundle, parseAccountBundleText } from '@/lib/accountBundle'
import { AccountBundleImportModal } from '@/components/account/AccountBundleImportModal'
import type { AccountBundle } from '@/api/overview'

function kiroImportEntries(bundle: Record<string, unknown>): Record<string, unknown>[] {
  if (!Array.isArray(bundle.accounts)) return [bundle]
  const entries = bundle.accounts.filter((entry): entry is Record<string, unknown> => (
    Boolean(entry) && typeof entry === 'object' && !Array.isArray(entry) && looksLikeKiro(entry as Record<string, unknown>)
  ))
  return entries.length > 0 ? entries : [bundle]
}

function normalizeKiroImportBundle(bundle: Record<string, unknown>, region: ImportRegion) {
  const entries = kiroImportEntries(bundle)
  const named = entries.find((entry) => typeof entry.nickname === 'string' || typeof entry.email === 'string')
  const nickname = typeof named?.nickname === 'string' ? named.nickname.trim() : ''
  const email = typeof named?.email === 'string' ? named.email.trim() : ''
  return {
    format: 'kiro-oauth-v1',
    provider: 'kiro',
    region: kiroRegionOf(entries[0] || bundle) || region,
    enabled: true,
    name: entries.length === 1 ? (nickname || email || undefined) : undefined,
    accounts: entries.map((entry) => ({
      ...entry,
      region: kiroRegionOf(entry) || region,
    })),
  }
}

type AccountBusy = { id: string; kind: AccountBusyKind }
type AccountFilter = 'all' | 'available' | 'attention' | 'disabled'
const ALL_PROVIDERS = '__all__'

const EMPTY_ACCOUNTS: AccountRow[] = []

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
  const [bundleExporting, setBundleExporting] = useState(false)
  const [bundleImporting, setBundleImporting] = useState(false)
  const [bundleConfirmOpen, setBundleConfirmOpen] = useState(false)
  const [bundleImportData, setBundleImportData] = useState<AccountBundle | null>(null)
  const [bundleImportFileName, setBundleImportFileName] = useState('')
  const [batchConfirmOpen, setBatchConfirmOpen] = useState(false)
  const [batchDetection, setBatchDetection] = useState<ImportDetectResult | null>(null)
  const [batchBundle, setBatchBundle] = useState<Record<string, unknown> | null>(null)
  const [batchFileName, setBatchFileName] = useState('')
  const [busy, setBusy] = useState<AccountBusy | null>(null)
  const [patById, setPatById] = useState<Record<string, string>>({})
  const [callbackById, setCallbackById] = useState<Record<string, string>>({})
  const [noteById, setNoteById] = useState<Record<string, string>>({})
  const [checkinFeedbackById, setCheckinFeedbackById] = useState<Record<string, CheckinFeedback>>({})
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
  const [checkinAllRunning, setCheckinAllRunning] = useState(false)
  // Bulk re-login: selection mode on the account cards, then a sequential
  // authorization walkthrough (see BatchReloginModal).
  const [reloginMode, setReloginMode] = useState(false)
  const [reloginSelected, setReloginSelected] = useState<string[]>([])
  const [reloginOpen, setReloginOpen] = useState(false)
  /**
   * Live bulk-op progress streamed from GET /api/events (real per-account
   * frames). Null when no bulk op is running.
   */
  const [bulkProgress, setBulkProgress] = useState<{ kind: 'checkin' | 'refresh'; total: number; done: number } | null>(null)
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

  /** Single-account refresh: the response is the real post-probe row. */
  const refreshOne = useCallback(async (id: string, forceQuota = true) => {
    const refreshed = await refreshAccount(id, { quota: forceQuota })
    setAccounts((current) => current.map((account) => account.id === id ? { ...account, ...refreshed } : account))
  }, [])

  /**
   * One-shot refresh for every account. The backend fans out to all accounts
   * concurrently — no client batching, no queue — and every row returns the
   * real probe outcome with the verbatim upstream error.
   */
  const refreshEveryAccount = useCallback(async (forceQuota = true) => {
    const response = await refreshAllAccounts({ quota: forceQuota })
    const results = response.data || []
    const notes: Record<string, string> = {}
    const errors: string[] = []
    const quotas: Record<string, NonNullable<AccountRow['quota']>> = {}
    let failed = 0
    for (const result of results) {
      if (result.ok) {
        if (result.quota) quotas[result.account_id] = result.quota
        continue
      }
      failed += 1
      const message = result.message || result.code || result.kind || t('toastFailed')
      notes[result.account_id] = message
      errors.push(message)
    }
    if (Object.keys(quotas).length) {
      setAccounts((current) => current.map((account) => quotas[account.id]
        ? { ...account, quota: quotas[account.id] }
        : account))
    }
    if (failed) setNoteById((current) => ({ ...current, ...notes }))
    // Events already patch rows as they land; this pull keeps the list truthful
    // when the stream is offline.
    await reloadAccounts(false).catch(() => undefined)
    return { total: results.length, failed, errors }
  }, [reloadAccounts, t])

  /**
   * Events patch rows in place for instant feedback; a trailing debounced re-read
   * then reconciles every field with the server's truth (derived status, hot
   * flags, fields the event does not carry) without a manual reload.
   */
  const reconcileTimer = useRef<number | null>(null)
  const scheduleReconcile = useCallback(() => {
    if (reconcileTimer.current != null) return
    reconcileTimer.current = window.setTimeout(() => {
      reconcileTimer.current = null
      void reloadAccounts(false).catch(() => undefined)
    }, 400)
  }, [reloadAccounts])

  const onAccountEvent = useCallback((event: ServerEvent) => {
    const type = event.type
    if (type === 'account') {
      const data = (event.data || {}) as AccountStateEvent
      const id = data.account_id || event.account_id
      if (!id) return
      setAccounts((current) => current.map((account) => {
        if (account.id !== id) return account
        const next = { ...account }
        if (data.runtime_state) next.runtime_state = data.runtime_state
        if (typeof data.ready === 'boolean') {
          next.ready = data.ready
          if (data.ready) next.hot = account.hot && data.runtime_state !== 'ready'
        }
        if (data.cooldown_until) next.cooldown_until = data.cooldown_until
        else if (data.ready) next.cooldown_until = null
        if (data.last_error !== undefined) {
          next.last_error = data.last_error
          next.lastError = data.last_error || undefined
        }
        if (data.last_kind !== undefined) next.last_error_kind = data.last_kind || undefined
        return next
      }))
      scheduleReconcile()
      return
    }
    if (type === 'quota') {
      const data = (event.data || {}) as QuotaStateEvent
      const id = data.account_id || event.account_id
      if (!id || !data.quota) return
      setAccounts((current) => current.map((account) => account.id === id ? { ...account, quota: data.quota } : account))
      scheduleReconcile()
      return
    }
    if (type === 'checkin') {
      const data = (event.data || {}) as CheckinStateEvent
      const id = data.account_id || event.account_id
      if (!id) return
      const status = String(data.status || '').toLowerCase()
      const kind = status === 'already' || status === 'skipped' || status === 'error' || status === 'unknown'
        ? status
        : 'success'
      const reward = typeof data.reward_credits === 'number' && data.reward_credits > 0
        ? t('checkinRewardSuffix', { credits: formatQuotaAmount(data.reward_credits) })
        : ''
      const message = `${data.message || (kind === 'error' ? t('checkinRecordFailed') : kind === 'unknown' ? t('checkinStatusUnknown') : '')}${reward}`.trim()
      setCheckinFeedbackById((current) => ({
        ...current,
        [id]: { kind, message: message || undefined },
      }))
      setAccounts((current) => current.map((account) => account.id === id
        ? {
            ...account,
            last_checkin_status: data.status || account.last_checkin_status,
            last_checkin_msg: data.message || account.last_checkin_msg,
            last_checkin_at: data.at || account.last_checkin_at,
          }
        : account))
      return
    }
    if (type === 'accounts') {
      void reloadAccounts(false).catch(() => undefined)
      return
    }
    if (type === 'bulk') {
      const data = (event.data || {}) as BulkEventData
      const kind = data.kind === 'refresh' ? 'refresh' : data.kind === 'checkin' ? 'checkin' : null
      if (!kind) return
      if (data.phase === 'done') {
        setBulkProgress(null)
        return
      }
      if (!data.total) return
      setBulkProgress({ kind, total: data.total, done: data.done || 0 })
    }
  }, [reloadAccounts, scheduleReconcile, t])

  const { status: eventStatus } = useAccountEvents(onAccountEvent)

  useEffect(() => () => {
    if (reconcileTimer.current != null) window.clearTimeout(reconcileTimer.current)
  }, [])

  useEffect(() => {
    void reloadAccounts(false).catch(() => undefined)
  }, [reloadAccounts])
  // The page no longer re-probes every account on mount: the background quota
  // sweep already keeps snapshots fresh, and the event stream patches rows live.
  useEffect(() => {
    if (eventStatus === 'live') return
    // Stream unavailable (401/offline): fall back to a 5s list poll so the page
    // still converges on real state without manual refreshes.
    const timer = window.setInterval(() => {
      if (document.hidden) return
      void reloadAccounts(false).catch(() => undefined)
    }, 5000)
    return () => window.clearInterval(timer)
  }, [eventStatus, reloadAccounts])
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
  // On mount the page reads the real stored state once. It no longer re-probes
  // every account: the background quota sweep keeps snapshots fresh and the
  // event stream patches cards live, so mounting stays cheap and truthful.

  function checkinPolicyFor(account: AccountRow) {
    return providers.find((provider) => provider.id === account.provider)?.regions.find((region) => region.id === account.region)?.checkin
  }

  function checkinTimezoneFor(account: AccountRow) {
    const timezone = checkinPolicyFor(account)?.timezone
    // Check-in policies always run on the provider's server calendar. Never
    // fall back to the browser/PC zone, even if an old backend still says Local.
    const resolved = timezone === 'Local' ? 'Asia/Shanghai' : timezone || 'Asia/Shanghai'
    // Settings report the zone as "MST -07:00", which Intl rejects.
    try {
      Intl.DateTimeFormat('en-CA', { timeZone: resolved })
      return resolved
    } catch {
      return undefined
    }
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
  const reloginAccounts = useMemo(
    () => rows.filter((account) => reloginSelected.includes(account.id)),
    [rows, reloginSelected],
  )

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
      await refreshOne(id)
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
      await refreshOne(id)
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
      await refreshOne(id)
    })
  }

  async function onExport(id: string) {
    await run(id, 'export', async () => {
      const bundle = await exportAccount(id)
      await copyText(JSON.stringify(bundle, null, 2))
      setNoteById((current) => ({ ...current, [id]: t('credentialCopied') }))
    })
  }

  async function onExportAll() {
    if (bundleExporting) return
    setBundleExporting(true)
    const toastId = 'accounts-export-all'
    toast({ id: toastId, type: 'loading', title: t('toastAccountsExporting') })
    try {
      const bundle = await exportAccountBundle()
      downloadAccountBundle(bundle)
      toast({
        id: toastId,
        type: 'success',
        title: t('toastAccountsExported', { n: bundle.count ?? bundle.accounts.length }),
        description: t('bundleExportHint'),
      })
    } catch (error) {
      toast({
        id: toastId,
        type: 'error',
        title: t('toastFailed'),
        description: error instanceof Error ? error.message : String(error),
      })
    } finally {
      setBundleExporting(false)
    }
  }

  function closeBundleConfirm() {
    if (bundleImporting) return
    setBundleConfirmOpen(false)
    setBundleImportData(null)
    setBundleImportFileName('')
  }

  async function onBundleImportFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]
    event.target.value = ''
    if (!file) return
    try {
      const raw = await file.text()
      const bundle = parseAccountBundleText(raw)
      setBundleImportData(bundle)
      setBundleImportFileName(file.name)
      setBundleConfirmOpen(true)
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      toast({
        type: 'error',
        title: message === 'bad_json' ? t('wizardBadJson') : t('bundleImportUnrecognized'),
      })
    }
  }

  async function onBundleImportConfirm() {
    if (!bundleImportData || bundleImporting) return
    setBundleImporting(true)
    const toastId = 'bundle-import-accounts'
    toast({ id: toastId, type: 'loading', title: t('bundleImportLoading') })
    try {
      const result = await importAccountBundle(bundleImportData)
      const succeeded = result.succeeded ?? 0
      const failed = result.failed ?? 0
      const failureSummary = (result.errors || [])
        .slice(0, 3)
        .map((item) => item.message || `index ${item.index}`)
        .filter(Boolean)
        .join('; ')
      toast({
        id: toastId,
        type: failed > 0 && succeeded === 0 ? 'error' : failed > 0 ? 'info' : 'success',
        title: failed > 0
          ? t('accountImportPartial').replace('{ok}', String(succeeded)).replace('{fail}', String(failed))
          : t('accountImportBatch').replace('{n}', String(succeeded)),
        description: failureSummary || undefined,
      })
      setBundleConfirmOpen(false)
      setBundleImportData(null)
      setBundleImportFileName('')
      if (succeeded > 0) await reloadAccounts(false)
    } catch (error) {
      toast({
        id: toastId,
        type: 'error',
        title: t('toastFailed'),
        description: error instanceof Error ? error.message : String(error),
      })
    } finally {
      setBundleImporting(false)
    }
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
      await refreshOne(id)
      toast({ id: `refresh-${id}`, type: 'success', title: t('toastAccountRefreshed') })
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      setNoteById((current) => ({ ...current, [id]: message }))
      toast({ id: `refresh-${id}`, type: 'error', title: t('toastFailed'), description: message })
    } finally {
      setBusy(null)
    }
  }

  async function onRefreshCredits() {
    setQuotaRefreshing(true)
    toast({ id: 'refresh-credits', type: 'loading', title: t('toastRefreshingCredits') })
    try {
      const result = await refreshEveryAccount(true)
      if (result.failed > 0) {
        // Report a partial failure honestly: some accounts refreshed fine, so
        // name how many failed instead of implying the whole batch did.
        const partial = result.failed < result.total
        toast({
          id: 'refresh-credits',
          type: partial ? 'info' : 'error',
          title: partial
            ? t('toastCreditsPartiallyRefreshed', { failed: result.failed, total: result.total })
            : t('toastFailed'),
          description: result.errors[0] || t('toastFailed'),
        })
        return
      }
      toast({ id: 'refresh-credits', type: 'success', title: t('toastCreditsRefreshed') })
    } catch (error) {
      // A whole-request failure (network / console key) surfaces verbatim.
      toast({
        id: 'refresh-credits',
        type: 'error',
        title: t('toastFailed'),
        description: error instanceof Error ? error.message : String(error),
      })
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
      const result = batchDetection.provider === 'kiro'
        ? await importAccount(normalizeKiroImportBundle(batchBundle, region)) as WorkbuddyImportResult
        : await importWorkbuddyBundle(batchBundle, { region })
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

  async function onCheckinAll() {
    setCheckinAllRunning(true)
    toast({
      id: 'checkin-all',
      type: 'loading',
      title: t('toastCheckingInAll', { done: 0, total: rows.length }),
    })
    try {
      const response = await checkinAll()
      const results = response.data || []
      if (results.length === 0) {
        toast({ id: 'checkin-all', type: 'info', title: t('checkinAllEmpty') })
        return
      }
      const summary = { checked: 0, skipped: 0, failed: 0, unknown: 0, credits: 0 }
      const feedback: Record<string, CheckinFeedback> = {}
      const notes: Record<string, string> = {}
      let firstError = ''
      for (const result of results) {
        const raw = String(result.status || '').toLowerCase()
        const kind: CheckinFeedback['kind'] = raw === 'already' || raw === 'skipped' || raw === 'error' || raw === 'unknown'
          ? raw
          : 'success'
        if (kind === 'error') {
          summary.failed += 1
          if (!firstError) firstError = result.message || ''
        } else if (kind === 'skipped' || kind === 'already') summary.skipped += 1
        else if (kind === 'unknown') summary.unknown += 1
        else summary.checked += 1
        // Real upstream grant only — omitted when the provider reported none.
        if (typeof result.reward_credits === 'number' && result.reward_credits > 0) {
          summary.credits += result.reward_credits
        }
        const reward = result.reward_credits && result.reward_credits > 0
          ? t('checkinRewardSuffix', { credits: formatQuotaAmount(result.reward_credits) })
          : ''
        const message = `${result.message || (kind === 'error' ? t('checkinRecordFailed') : kind === 'unknown' ? t('checkinStatusUnknown') : '')}${reward}`.trim()
        feedback[result.account_id] = { kind, message: message || undefined }
        // Keep the verbatim upstream classification on the card so a failure
        // never collapses into a generic "failed".
        if (kind === 'error') {
          notes[result.account_id] = [
            result.message,
            result.code,
            result.http_status ? `HTTP ${result.http_status}` : '',
          ].filter(Boolean).join(' · ') || t('checkinRecordFailed')
        }
      }
      setCheckinFeedbackById((current) => ({ ...current, ...feedback }))
      if (Object.keys(notes).length) setNoteById((current) => ({ ...current, ...notes }))
      const values = { checked: summary.checked, skipped: summary.skipped, failed: summary.failed, total: results.length }
      const description = summary.credits > 0
        ? t('checkinRewardTotal', { credits: formatQuotaAmount(summary.credits) })
        : summary.failed && firstError ? firstError : undefined
      toast({
        id: 'checkin-all',
        type: summary.failed ? 'error' : summary.unknown ? 'info' : 'success',
        title: t(summary.failed ? 'toastCheckedInAllFailed' : 'toastCheckedInAll', values),
        description,
      })
    } catch (error) {
      toast({
        id: 'checkin-all',
        type: 'error',
        title: t('toastFailed'),
        description: error instanceof Error ? error.message : String(error),
      })
    } finally {
      setCheckinAllRunning(false)
      await reloadAccounts(false).catch(() => undefined)
    }
  }

  async function onCheckin(id: string) {
    await run(id, 'checkin', async () => {
      let result: { last_checkin_status?: string; last_checkin_msg?: string } | undefined
      try {
        result = await checkinAccount(id)
      } catch (error) {
        setCheckinFeedbackById((current) => ({
          ...current,
          [id]: { kind: 'error', message: error instanceof Error ? error.message : String(error) },
        }))
        await reloadAccounts(false).catch(() => undefined)
        await refreshOne(id).catch(() => undefined)
        return
      }
      const status = result?.last_checkin_status
      const message = result?.last_checkin_msg
      const kind = status === 'already' ? 'already' : status === 'skipped' ? 'skipped' : status === 'error' ? 'error' : status === 'unknown' ? 'unknown' : 'success'
      setCheckinFeedbackById((current) => ({
        ...current,
        [id]: { kind, message: kind === 'error' ? message || t('checkinRecordFailed') : message },
      }))
      await reloadAccounts(false).catch(() => undefined)
      // Pull the real upstream quota/credits once more so the card reflects
      // the check-in grant (do not rely on the check-in response alone).
      await refreshOne(id).catch(() => undefined)
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
    <div className="flex min-h-0 flex-1 flex-col gap-4">
      {checkinError ? <div className="shrink-0"><PageAlert title={checkinError} /></div> : null}
      <section data-gsap-reveal className="flex shrink-0 flex-wrap items-center gap-2">
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
          <Button size="sm" variant="secondary" isPending={bundleExporting} isDisabled={batchImporting || batchConfirmOpen || bundleImporting || bundleConfirmOpen} onPress={() => void onExportAll()}>
            <DownloadSimple size={14} />{t('exportAll')}
          </Button>
          {/*
            Separate file picker for full account migration bundles. Kept
            distinct from the credential "batch import" below on purpose.
          */}
          <label
            htmlFor="accounts-bundle-import-file"
            className={`button button--sm button--secondary inline-flex items-center gap-1.5 ${
              bundleImporting || bundleConfirmOpen || batchImporting || batchConfirmOpen || bundleExporting ? 'pointer-events-none opacity-60' : 'cursor-pointer'
            }`}
          >
            {bundleImporting ? <BrandMark size={14} loading /> : <DownloadSimple size={14} />}
            {t('importAll')}
          </label>
          <input
            id="accounts-bundle-import-file"
            type="file"
            accept=".json,application/json,text/json"
            disabled={bundleImporting || bundleConfirmOpen || batchImporting || batchConfirmOpen || bundleExporting}
            className="sr-only"
            onChange={(event) => void onBundleImportFile(event)}
          />
          <Button size="sm" variant="secondary" isPending={checkinAllRunning} isDisabled={quotaRefreshing} onPress={() => void onCheckinAll()}>
            <CalendarCheck size={14} />{t('checkinAll')}
          </Button>
          <Button size="sm" variant="secondary" isPending={quotaRefreshing} isDisabled={checkinAllRunning} onPress={() => void onRefreshCredits()}>
            <ArrowsClockwise size={14} />{t('refreshCredits')}
          </Button>
          <Button
            size="sm"
            variant={reloginMode ? 'primary' : 'secondary'}
            isDisabled={checkinAllRunning || quotaRefreshing}
            onPress={() => {
              setReloginMode((current) => !current)
              setReloginSelected([])
            }}
          >
            <SignIn size={14} />{t('reloginEntry')}
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
              batchImporting || batchConfirmOpen || bundleImporting || bundleConfirmOpen || bundleExporting ? 'pointer-events-none opacity-60' : 'cursor-pointer'
            }`}
          >
            {batchImporting ? <BrandMark size={14} loading /> : <UploadSimple size={14} />}
            {t('batchImport')}
          </label>
          <input
            id="accounts-batch-import-file"
            type="file"
            accept=".json,application/json,text/json"
            disabled={batchImporting || batchConfirmOpen || bundleImporting || bundleConfirmOpen || bundleExporting}
            className="sr-only"
            onChange={(event) => void onBatchImportFile(event)}
          />
          <Button size="sm" onPress={() => setAddOpen(true)}><Plus size={14} />{t('addAccount')}</Button>
        </div>
        {eventStatus === 'offline' ? (
          <span
            className="inline-flex items-center gap-1.5 rounded-full bg-surface-secondary px-2 py-0.5 text-caption text-muted"
            data-events-offline
          >
            <span className="size-1.5 rounded-full bg-warning" />
            {t('eventsOffline')}
          </span>
        ) : null}
      </section>

      {bulkProgress ? (
        <div
          data-bulk-progress
          className="flex shrink-0 items-center gap-3 rounded-lg bg-surface-secondary px-3 py-2 text-caption"
        >
          <BrandMark size={14} loading />
          <span className="shrink-0 font-medium text-foreground">
            {bulkProgress.kind === 'checkin' ? t('checkinAll') : t('refreshCredits')}
          </span>
          <span className="mono shrink-0 tabular-nums text-foreground/70">
            {bulkProgress.done}/{bulkProgress.total}
          </span>
          <div className="h-1.5 min-w-16 flex-1 overflow-hidden rounded-full bg-surface">
            <div
              className="h-full rounded-full bg-success transition-[width] duration-200"
              style={{ width: `${Math.min(100, Math.round((bulkProgress.done / Math.max(1, bulkProgress.total)) * 100))}%` }}
            />
          </div>
        </div>
      ) : null}

      <AddAccountModal isOpen={addOpen} onClose={() => setAddOpen(false)} onAdded={() => { toast({ type: 'success', title: t('toastAccountAdded') }); void reloadAccounts(false) }} />
      <AccountBundleImportModal
        isOpen={bundleConfirmOpen}
        bundle={bundleImportData}
        fileName={bundleImportFileName}
        isPending={bundleImporting}
        t={t}
        onClose={closeBundleConfirm}
        onConfirm={() => { void onBundleImportConfirm() }}
      />
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
      <BatchReloginModal
        isOpen={reloginOpen}
        accounts={reloginAccounts}
        t={t}
        onClose={() => { setReloginOpen(false); setReloginMode(false); setReloginSelected([]) }}
        onFinished={() => { void reloadAccounts(false) }}
      />
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
          }).then((ok) => {
            if (ok) toast({ type: 'success', title: t('toastAccountDeleted') })
          })
        }}
      />

      {reloginMode ? (
        <div
          data-relogin-bar
          className="flex shrink-0 flex-wrap items-center gap-2 rounded-lg border border-accent/40 bg-surface-secondary px-3 py-2 text-caption"
        >
          <span className="font-medium text-foreground">{t('reloginSelected', { count: reloginSelected.length })}</span>
          <Button size="sm" variant="ghost" onPress={() => setReloginSelected(pagedRows.map((account) => account.id))}>
            {t('reloginSelectPage')}
          </Button>
          <Button size="sm" variant="ghost" isDisabled={!reloginSelected.length} onPress={() => setReloginSelected([])}>
            {t('reloginClear')}
          </Button>
          <div className="ml-auto flex items-center gap-2">
            <Button size="sm" variant="secondary" onPress={() => { setReloginMode(false); setReloginSelected([]) }}>
              {t('cancel')}
            </Button>
            <Button size="sm" isDisabled={!reloginSelected.length} onPress={() => setReloginOpen(true)}>
              {t('reloginConfirm')}
            </Button>
          </div>
        </div>
      ) : null}

      {/*
        Only this region scrolls. The filter row above and the pager below stay
        put, so switching pages or filtering never moves the controls offscreen.
      */}
      <div
        data-account-scroll
        className="min-h-0 flex-1 overflow-x-hidden overflow-y-auto overscroll-contain [scrollbar-gutter:stable]"
      >
        <div className="space-y-4 pb-1">
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
                selectable={reloginMode}
                selected={reloginSelected.includes(account.id)}
                onSelectToggle={() => setReloginSelected((current) => (
                  current.includes(account.id) ? current.filter((id) => id !== account.id) : [...current, account.id]
                ))}
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
                checkinTimezone={checkinTimezoneFor(account)}
                onToggleAutoCheckin={checkinPolicyFor(account) ? (selected) => void onToggleAutoCheckin(account.id, selected) : undefined}
                onCheckin={checkinPolicyFor(account) ? () => void onCheckin(account.id) : undefined}
                checkinFeedback={checkinFeedbackById[account.id] ?? null}
                onViewCheckins={checkinPolicyFor(account) ? () => setCheckinHistoryId(account.id) : undefined}
                onEdit={() => setEditId(account.id)}
                onToggleAuthPanel={() => setAuthPanelId((current) => current === account.id ? null : account.id)}
                onViewModels={() => setModelsId(account.id)}
              />
            ))}
          </section>
        </div>
      </div>

      {filteredRows.length ? (
        <div className="shrink-0">
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
        </div>
      ) : null}
    </div>
  )
}
