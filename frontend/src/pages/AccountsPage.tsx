import { useMemo, useState } from 'react'
import { Button, ButtonGroup, Chip, Input, InputGroup, Modal, Tooltip } from '@heroui/react'
import {
  ArrowClockwise,
  ArrowSquareOut,
  Copy,
  Key,
  MagnifyingGlass,
  Plus,
  ShieldCheck,
  TrashSimple,
  WarningCircle,
  X,
} from '@phosphor-icons/react'
import { AddAccountModal } from '@/components/AddAccountModal'
import { BrandMark } from '@/components/BrandMark'
import { ProviderMark } from '@/components/ProviderMark'
import { accountProviderLabel } from '@/lib/provider'
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
import { CompactSwitch } from '@/components/ui/CompactSwitch'
import { hoverLift } from '@/hooks/useGsapReveal'

type AccountRow = NonNullable<Overview['accounts']>[number]
type BusyKind = 'create' | 'import' | 'device' | 'pat' | 'rewarm' | 'toggle' | 'delete' | 'export'
type AccountBusy = { id: string; kind: BusyKind }
type AccountState = 'disabled' | 'cooling' | 'hot' | 'ready' | 'login'
type AccountFilter = 'all' | 'available' | 'attention' | 'disabled'

const EMPTY_ACCOUNTS: AccountRow[] = []
const ACCOUNT_BUTTON_CLASS = 'account-button'
const ACCOUNT_ICON_BUTTON_CLASS = 'account-button account-icon-button'
const ACCOUNT_CHIP_CLASS = 'account-chip'
const ACCOUNT_INPUT_CLASS = 'account-input'

function cooldownLabel(until?: string | null) {
  if (!until) return ''
  const milliseconds = Date.parse(until) - Date.now()
  if (!Number.isFinite(milliseconds) || milliseconds <= 0) return ''
  const seconds = Math.ceil(milliseconds / 1000)
  return seconds < 60 ? `${seconds}s` : `${Math.ceil(seconds / 60)}m`
}

function accountState(account: AccountRow): AccountState {
  if (!account.enabled) return 'disabled'
  if (cooldownLabel(account.down_until || account.cooldown_until)) return 'cooling'
  if (account.hot) return 'hot'
  if (account.ready) return 'ready'
  return 'login'
}

function isAvailable(account: AccountRow) {
  const state = accountState(account)
  return state === 'hot' || state === 'ready'
}

function runtimeSegments(state: AccountState) {
  if (state === 'hot') return 12
  if (state === 'ready') return 9
  if (state === 'cooling') return 5
  if (state === 'login') return 3
  return 1
}

function formatQuotaAmount(value: number | undefined) {
  if (value == null || !Number.isFinite(value)) return '—'
  if (Math.abs(value) >= 1000) return `${(value / 1000).toFixed(value % 1000 === 0 ? 0 : 1)}k`
  return String(Math.round(value * 100) / 100)
}

function formatUpdatedAt(value: string | undefined, lang: 'en' | 'zh') {
  if (!value) return ''
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return ''
  return new Intl.DateTimeFormat(lang === 'zh' ? 'zh-CN' : 'en-US', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

function providerLabel(provider: string | undefined, region: string | undefined, t: (key: string) => string) {
  return accountProviderLabel(provider, region, t)
}

export function AccountsPage() {
  const { lang, t } = useI18n()
  const { overview, loading, refresh } = useOverview()
  const rows = overview?.accounts ?? EMPTY_ACCOUNTS
  const [addOpen, setAddOpen] = useState(false)
  const [busy, setBusy] = useState<AccountBusy | null>(null)
  const [patById, setPatById] = useState<Record<string, string>>({})
  const [noteById, setNoteById] = useState<Record<string, string>>({})
  const [urlById, setUrlById] = useState<Record<string, string>>({})
  const [confirmId, setConfirmId] = useState<string | null>(null)
  const [authPanelId, setAuthPanelId] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<AccountFilter>('all')
  const [enabledById, setEnabledById] = useState<Record<string, boolean>>({})
  const [dropSystemById, setDropSystemById] = useState<Record<string, boolean>>({})
  const displayRows = useMemo(() => rows.map((account) => {
    const enabled = enabledById[account.id]
    const dropSystem = dropSystemById[account.id]
    let next = account
    if (enabled !== undefined) next = { ...next, enabled }
    if (dropSystem !== undefined) next = { ...next, drop_system_prompt: dropSystem }
    return next
  }), [enabledById, dropSystemById, rows])

  const availableCount = displayRows.filter(isAvailable).length
  const attentionCount = displayRows.filter((account) => account.enabled && !isAvailable(account)).length
  const inFlightCount = displayRows.reduce((total, account) => total + (account.in_flight ?? account.inFlight ?? 0), 0)
  const confirmAccount = displayRows.find((account) => account.id === confirmId)
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

  if (loading && !overview) return <AccountsPageSkeleton />

  async function run(id: string, kind: BusyKind, action: () => Promise<void>) {
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
      setNoteById((current) => ({ ...current, [id]: t('wizardStartingSession') }))
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

  async function onToggle(id: string, selected: boolean) {
    setEnabledById((current) => ({ ...current, [id]: selected }))
    await run(id, 'toggle', async () => {
      await updateAccount(id, { enabled: selected })
      await refresh()
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
      await refresh()
    })
    setDropSystemById((current) => {
      const next = { ...current }
      delete next[id]
      return next
    })
  }

  return (
    <div className="space-y-5">
      <section data-gsap-reveal className="grid gap-4 border-b border-[var(--app-line)] pb-4 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end">
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
                <Button className={ACCOUNT_BUTTON_CLASS} size="sm" variant="ghost" onPress={() => setConfirmId(null)}>{t('cancel')}</Button>
                <Button className={ACCOUNT_BUTTON_CLASS} size="sm" variant="danger" isPending={busy?.id === confirmId && busy.kind === 'delete'} onPress={() => {
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

      {!rows.length ? (
        <div className="grid min-h-72 place-items-center rounded-lg border border-dashed border-[var(--app-line-strong)] text-center">
          <div className="max-w-sm px-6">
            <BrandMark size={28} className="mx-auto" />
            <div className="mt-4 text-sm font-medium">{t('noAccounts')}</div>
            <div className="mt-1 text-xs leading-5 text-[var(--app-faint)]">{t('accountEmptyHint')}</div>
            <Button className={`${ACCOUNT_BUTTON_CLASS} mt-5`} size="sm" onPress={() => setAddOpen(true)}><Plus size={14} />{t('addAccount')}</Button>
          </div>
        </div>
      ) : null}

      {rows.length && !filteredRows.length ? (
        <div className="grid min-h-56 place-items-center rounded-lg border border-dashed border-[var(--app-line-strong)] text-center">
          <div>
            <MagnifyingGlass size={22} className="mx-auto text-[var(--app-faint)]" />
            <div className="mt-3 text-sm font-medium">{t('noAccountsMatch')}</div>
            <Button className={`${ACCOUNT_BUTTON_CLASS} mt-4`} size="sm" variant="ghost" onPress={() => { setQuery(''); setFilter('all') }}>{t('clearFilters')}</Button>
          </div>
        </div>
      ) : null}

      <section className="grid gap-3 xl:grid-cols-2">
        {filteredRows.map((account) => {
          const thisBusy = busy?.id === account.id ? busy.kind : ''
          const authUrl = urlById[account.id]
          const lastError = account.last_error || account.lastError
          const errorKind = account.last_error_kind || account.kind
          const cooldown = cooldownLabel(account.down_until || account.cooldown_until)
          const state = accountState(account)
          const stateCopy = state === 'hot'
            ? t('signedIn')
            : state === 'ready'
              ? t('ready')
              : state === 'cooling'
                ? `${t('cooling')} ${cooldown}`
                : state === 'disabled'
                  ? t('disabled')
                  : t('needQoderLogin')
          const stateColor = state === 'hot' || state === 'ready' ? 'success' : state === 'cooling' ? 'warning' : state === 'login' ? 'danger' : undefined
          const segmentCount = runtimeSegments(state)
          const inFlight = account.in_flight ?? account.inFlight ?? 0
          const updatedAt = formatUpdatedAt(account.updated_at, lang)
          const authPanelOpen = authPanelId === account.id

          return (
            <article key={account.id} data-gsap-reveal className="account-card app-panel-flat flex flex-col overflow-hidden rounded-lg" onMouseEnter={(event) => hoverLift(event.currentTarget, true)} onMouseLeave={(event) => hoverLift(event.currentTarget, false)}>
              <header className="p-4">
                <div className="flex items-start justify-between gap-4">
                  <div className="flex min-w-0 items-start gap-3">
                    <ProviderMark provider={account.provider} size={32} />
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-1.5">
                        <Chip className={ACCOUNT_CHIP_CLASS} size="sm" variant="soft">
                          <span className="inline-flex items-center gap-1">
                            <ProviderMark provider={account.provider} size={12} />
                            {providerLabel(account.provider, account.region, t)}
                          </span>
                        </Chip>
                        <Chip className={ACCOUNT_CHIP_CLASS} size="sm" variant="soft">{account.auth_type || 'none'}</Chip>
                      </div>
                      <h2 className="mt-1.5 truncate text-sm font-semibold tracking-[-0.01em]">{account.name || account.id}</h2>
                    </div>
                  </div>
                  <Chip className={ACCOUNT_CHIP_CLASS} size="sm" variant="soft" color={stateColor}>{stateCopy}</Chip>
                </div>

                <div className="mono mt-3 space-y-0.5 text-[10px] text-[var(--app-faint)]">
                  <div className="truncate">{account.id}</div>
                  {account.remote_uid ? <div className="truncate">UID {account.remote_uid}</div> : null}
                </div>
              </header>

              <div className="flex-1 border-t border-[var(--app-line)] px-4 py-3">
                <div className="flex items-center justify-between gap-3 text-xs">
                  <div className="flex items-center gap-2 font-medium">
                    <span className="status-dot" data-state={state === 'hot' || state === 'ready' ? 'ok' : state === 'login' ? 'danger' : undefined} />
                    {t('runtimeState')}
                  </div>
                  <span className="text-[var(--app-faint)]">{stateCopy}</span>
                </div>
                <div className="mt-2.5 grid grid-cols-12 gap-1" aria-hidden>
                  {Array.from({ length: 12 }, (_, index) => (
                    <span
                      key={index}
                      className={`h-1.5 rounded-[2px] ${index < segmentCount
                        ? state === 'hot' || state === 'ready'
                          ? 'bg-[var(--app-ok)]'
                          : state === 'login'
                            ? 'bg-[var(--app-danger)]'
                            : 'bg-[var(--app-faint)]'
                        : 'bg-[var(--app-line)]'}`}
                    />
                  ))}
                </div>

                <div className="mt-4 grid grid-cols-2 gap-x-4 gap-y-3 border-y border-[var(--app-line)] py-3 text-[11px] sm:grid-cols-4">
                  {[
                    [t('inFlight'), inFlight],
                    [t('maxInflight'), account.max_inflight ?? 0],
                    [t('priority'), account.priority ?? 0],
                    [t('restarts'), account.restarts ?? 0],
                  ].map(([label, value]) => (
                    <div key={String(label)}>
                      <div className="text-[var(--app-faint)]">{label}</div>
                      <div className="mono mt-1 font-medium">{value}</div>
                    </div>
                  ))}
                </div>

                {account.quota ? (
                  <div className="mt-3">
                    <div className="flex items-center justify-between gap-3 text-[11px]">
                      <div className="flex items-center gap-2 font-medium">
                        {t('quota')}
                        {account.quota.exceeded ? (
                          <span className="text-[var(--app-danger)]">{t('quotaExceeded')}</span>
                        ) : null}
                      </div>
                      <span className="mono font-medium">{Math.round(account.quota.percentage ?? 0)}%</span>
                    </div>
                    <div className="mt-2 h-1.5 overflow-hidden rounded-[2px] bg-[var(--app-line)]" role="progressbar" aria-label={t('quota')} aria-valuenow={account.quota.percentage} aria-valuemin={0} aria-valuemax={100}>
                      <div
                        className={`h-full rounded-[2px] transition-[width] ${
                          account.quota.exceeded || (account.quota.percentage ?? 0) >= 100
                            ? 'bg-[var(--app-danger)]'
                            : (account.quota.percentage ?? 0) >= 80
                              ? 'bg-[var(--warning)]'
                              : 'bg-[var(--app-ok)]'
                        }`}
                        style={{ width: `${Math.min(100, Math.max(0, account.quota.percentage ?? 0))}%` }}
                      />
                    </div>
                    <div className="mono mt-1.5 text-[10px] text-[var(--app-faint)]">
                      {formatQuotaAmount(account.quota.remaining)} / {formatQuotaAmount(account.quota.total)} {account.quota.unit || 'credits'}
                      {account.quota.has_add_on ? (
                        <span>
                          {' · '}{t('quotaAddOn')} {formatQuotaAmount(account.quota.add_on_used)} / {formatQuotaAmount(account.quota.add_on_total)} {account.quota.add_on_unit || 'credits'}
                        </span>
                      ) : null}
                    </div>
                  </div>
                ) : null}

                {account.provider === 'workbuddy' ? (
                  <div className="mt-3 flex items-center justify-between gap-3 text-[11px]">
                    <Tooltip>
                      <Tooltip.Trigger>
                        <span className="font-medium">{t('dropSystemPrompt')}</span>
                      </Tooltip.Trigger>
                      <Tooltip.Content>{t('dropSystemPromptHint')}</Tooltip.Content>
                    </Tooltip>
                    <CompactSwitch
                      isSelected={Boolean(account.drop_system_prompt)}
                      isDisabled={thisBusy === 'toggle'}
                      ariaLabel={t('dropSystemPrompt')}
                      onChange={(selected) => void onToggleDropSystem(account.id, selected)}
                    />
                  </div>
                ) : null}

                {updatedAt ? <div className="mono mt-3 text-[10px] text-[var(--app-faint)]">{t('updatedAt')} {updatedAt}</div> : null}

                {lastError ? (
                  <div className="mt-3 flex gap-2 border-l-2 border-[var(--app-danger)] pl-3 text-xs leading-5 text-[var(--app-danger)]">
                    <WarningCircle size={15} className="mt-0.5 shrink-0" />
                    <div className="min-w-0">
                      {errorKind ? <div className="mono mb-0.5 text-[10px] uppercase opacity-75">{errorKind}</div> : null}
                      <p className="break-words">{lastError}</p>
                    </div>
                  </div>
                ) : null}
              </div>

              {authPanelOpen && account.enabled ? (
                <section className="grid gap-3 border-t border-[var(--app-line)] bg-[var(--app-surface-muted)]/55 px-4 py-3 md:grid-cols-2">
                  <div>
                    <div className="text-[10px] font-semibold tracking-[0.1em] text-[var(--app-faint)] uppercase">{t('oauthDeviceFlow')}</div>
                    <p className="mt-2 text-xs leading-5 text-[var(--app-muted)]">{t('qoderLoginHint')}</p>
                    <div className="mt-3 flex flex-wrap gap-2">
                      <Button className={ACCOUNT_BUTTON_CLASS} size="sm" isPending={thisBusy === 'device'} onPress={() => void onDeviceLogin(account.id)}><ShieldCheck size={14} />{t('startBrowserLogin')}</Button>
                      {authUrl ? <Button className={ACCOUNT_BUTTON_CLASS} size="sm" variant="ghost" onPress={() => window.open(authUrl, '_blank', 'noopener,noreferrer')}><ArrowSquareOut size={14} />{t('open')}</Button> : null}
                    </div>
                  </div>
                  <div>
                    <div className="text-[10px] font-semibold tracking-[0.1em] text-[var(--app-faint)] uppercase">{t('patFallback')}</div>
                    <div className="mt-3 flex flex-col gap-2 sm:flex-row">
                      <Input className={ACCOUNT_INPUT_CLASS} type="password" value={patById[account.id] || ''} onChange={(event) => setPatById((current) => ({ ...current, [account.id]: event.target.value }))} placeholder={t('pasteToken')} aria-label={t('pat')} />
                      <Button className={ACCOUNT_BUTTON_CLASS} size="sm" variant="secondary" isPending={thisBusy === 'pat'} onPress={() => void onPat(account.id)}><Key size={14} />{t('usePat')}</Button>
                    </div>
                  </div>
                  {authUrl || noteById[account.id] ? (
                    <div className="md:col-span-2 text-xs">
                      {authUrl ? <code className="mono block break-all text-[var(--app-faint)]">{authUrl}</code> : null}
                      {noteById[account.id] ? <p className="mt-1 text-[var(--app-muted)]">{noteById[account.id]}</p> : null}
                    </div>
                  ) : null}
                </section>
              ) : null}

              <footer className="flex flex-wrap items-center gap-1.5 border-t border-[var(--app-line)] px-4 py-2.5">
                <Button
                  className={ACCOUNT_BUTTON_CLASS}
                  size="sm"
                  variant={state === 'login' ? 'primary' : 'secondary'}
                  isDisabled={!account.enabled}
                  onPress={() => setAuthPanelId((current) => current === account.id ? null : account.id)}
                >
                  <Key size={14} />{t('authentication')}
                </Button>
                <Tooltip>
                  <Tooltip.Trigger><Button className={ACCOUNT_ICON_BUTTON_CLASS} isIconOnly size="sm" variant="secondary" isDisabled={!account.enabled} isPending={thisBusy === 'rewarm'} onPress={() => void run(account.id, 'rewarm', async () => { await rewarmWorker(account.id); await refresh() })} aria-label={t('rewarm')}><ArrowClockwise size={14} /></Button></Tooltip.Trigger>
                  <Tooltip.Content>{t('rewarm')}</Tooltip.Content>
                </Tooltip>
                {account.auth_type !== 'none' ? (
                  <Tooltip>
                    <Tooltip.Trigger><Button className={ACCOUNT_ICON_BUTTON_CLASS} isIconOnly size="sm" variant="secondary" isPending={thisBusy === 'export'} onPress={() => void onExport(account.id)} aria-label={t('export')}><Copy size={14} /></Button></Tooltip.Trigger>
                    <Tooltip.Content>{t('export')}</Tooltip.Content>
                  </Tooltip>
                ) : null}
                <Tooltip>
                  <Tooltip.Trigger><Button className={ACCOUNT_ICON_BUTTON_CLASS} isIconOnly size="sm" variant="danger-soft" isPending={thisBusy === 'delete'} onPress={() => setConfirmId(account.id)} aria-label={t('delete')}><TrashSimple size={14} /></Button></Tooltip.Trigger>
                  <Tooltip.Content>{t('delete')}</Tooltip.Content>
                </Tooltip>
                <div className="ml-auto flex items-center gap-2">
                  <span className="text-[11px] font-medium text-[var(--app-faint)]">{account.enabled ? t('enabledState') : t('disabled')}</span>
                  <CompactSwitch
                    isSelected={Boolean(account.enabled)}
                    isDisabled={thisBusy === 'toggle'}
                    ariaLabel={account.enabled ? t('disable') : t('enable')}
                    onChange={(selected) => void onToggle(account.id, selected)}
                  />
                </div>
              </footer>
            </article>
          )
        })}
      </section>
    </div>
  )
}
