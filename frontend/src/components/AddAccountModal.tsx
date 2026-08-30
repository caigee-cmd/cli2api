import { useEffect, useRef, useState } from 'react'
import { Button, Description, Input, Label, ListBox, Modal, Select, Skeleton, TextArea } from '@heroui/react'
import { ArrowSquareOut, CaretLeft, CaretRight, CheckCircle, FileCode, Key, ShieldCheck, X } from '@phosphor-icons/react'
import { BrandMark } from '@/components/BrandMark'
import { ProviderMark } from '@/components/ProviderMark'
import { CompactSwitch } from '@/components/ui/CompactSwitch'
import { FilterToggle } from '@/components/ui/FilterToggle'
import { OptionTiles } from '@/components/ui/OptionTiles'
import { useI18n } from '@/hooks/useI18n'
import {
  createAccount,
  fetchLoginStatus,
  fetchProviders,
  importAccount,
  loginWithPat,
  completeLoginCallback,
  startDeviceLogin,
  type ProviderDescriptor,
} from '@/api/overview'

type Props = {
  isOpen: boolean
  onClose: () => void
  onAdded: () => void
}

type TabKey = 'browser' | 'pat' | 'import'
type Step = 'method' | 'login'

type ProviderOption = {
  id: string
  provider: string
  region: string
  descriptor: ProviderDescriptor
}

// Region suffix for descriptors without a dedicated i18n label.
const regionLabels: Record<string, string> = {
  global: 'Global',
  cn: 'CN',
}

// Dedicated i18n labels/hints for known families; unknown provider×region
// combinations fall back to the descriptor, so backend registration alone
// puts a new type on this list.
const labelKeys: Record<string, string> = {
  'qoder-global': 'accountTypeQoderGlobal',
  'qoder-cn': 'accountTypeQoderCN',
  'workbuddy-cn': 'accountTypeWorkBuddyCN',
  'workbuddy-global': 'accountTypeWorkBuddyGlobal',
  'trae-cn': 'accountTypeTraeCN',
}

const hintKeys: Record<string, string> = {
  'qoder-global': 'accountTypeQoderGlobalHint',
  'qoder-cn': 'accountTypeQoderCNHint',
  'workbuddy-cn': 'accountTypeWorkBuddyCNHint',
  'workbuddy-global': 'accountTypeWorkBuddyGlobalHint',
  'trae-cn': 'accountTypeTraeCNHint',
}

function AccountTypeSkeleton({ ariaLabel }: { ariaLabel: string }) {
  return (
    <div className="grid grid-cols-1 gap-2 sm:grid-cols-2" aria-busy="true" aria-label={ariaLabel}>
      {Array.from({ length: 4 }, (_, index) => (
        <div key={index} className="flex items-start gap-2.5 rounded-xl border border-separator px-3.5 py-3">
          <Skeleton className="mt-0.5 size-[18px] shrink-0 rounded-lg" />
          <div className="min-w-0 flex-1 space-y-1.5">
            <Skeleton className="h-4 w-28 rounded-lg" />
            <Skeleton className="h-3 w-full rounded-lg" />
          </div>
        </div>
      ))}
    </div>
  )
}

async function loadProviderOptions(): Promise<ProviderOption[]> {
  const output = await fetchProviders().catch(() => null)
  const descriptors = output?.data || []
  const options: ProviderOption[] = []
  for (const descriptor of descriptors) {
    for (const region of descriptor.regions) {
      options.push({ id: `${descriptor.id}-${region.id}`, provider: descriptor.id, region: region.id, descriptor })
    }
  }
  return options
}

function optionLabel(option: ProviderOption, t: (key: string) => string) {
  const key = labelKeys[option.id]
  if (key) {
    const localized = t(key)
    if (localized !== key) return localized
  }
  const regionLabel = option.descriptor.regions.find((r) => r.id === option.region)?.label || regionLabels[option.region] || ''
  const suffix = option.descriptor.regions.length > 1 && regionLabel ? ` ${regionLabel}` : ''
  return `${option.descriptor.label}${suffix}`.trim()
}

function optionHint(option: ProviderOption | undefined, t: (key: string) => string) {
  if (!option) return ''
  const key = hintKeys[option.id]
  if (!key) return ''
  const localized = t(key)
  return localized === key ? '' : localized
}

function StatusIcon({ phase, busy, tab, forTab }: { phase: Phase; busy: boolean; tab: TabKey; forTab: TabKey }) {
  if (phase === 'done') return <CheckCircle size={16} className="text-success" />
  if (busy && tab === forTab) return <BrandMark size={16} loading />
  return null
}

type Phase = 'idle' | 'busy' | 'polling' | 'done'

const POLL_ATTEMPTS = 90
const POLL_INTERVAL = 2000
const TILE_LIMIT = 6

export function AddAccountModal({ isOpen, onClose, onAdded }: Props) {
  const { t } = useI18n()
  const [step, setStep] = useState<Step>('method')
  const [tab, setTab] = useState<TabKey>('browser')
  const [accountType, setAccountType] = useState('')
  const [providerOptions, setProviderOptions] = useState<ProviderOption[]>([])
  const [typesLoading, setTypesLoading] = useState(false)
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [name, setName] = useState('')
  const [maxInFlight, setMaxInFlight] = useState('4')
  const [priority, setPriority] = useState('50')
  const [dropSystemPrompt, setDropSystemPrompt] = useState(true)
  const [autoCheckin, setAutoCheckin] = useState(false)
  const [pat, setPat] = useState('')
  const [json, setJson] = useState('')
  const [phase, setPhase] = useState<Phase>('idle')
  const [message, setMessage] = useState('')
  const [authUrl, setAuthUrl] = useState('')
  const [callbackUrl, setCallbackUrl] = useState('')
  const createdId = useRef<string>('')
  const pollTimer = useRef<number | null>(null)
  const fileInput = useRef<HTMLInputElement>(null)

  function stopPolling() {
    if (pollTimer.current !== null) {
      window.clearTimeout(pollTimer.current)
      pollTimer.current = null
    }
  }

  useEffect(() => {
    return () => stopPolling()
  }, [])

  useEffect(() => {
    if (!isOpen) return
    let cancelled = false
    setTypesLoading(true)
    setProviderOptions([])
    setAccountType('')
    void loadProviderOptions()
      .then((options) => {
        if (cancelled) return
        setProviderOptions(options)
        setAccountType(options[0]?.id || '')
        setTab('browser')
      })
      .finally(() => {
        if (!cancelled) setTypesLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [isOpen])

  const activeOption = providerOptions.find((option) => option.id === accountType)
  const typesReady = Boolean(activeOption) && !typesLoading
  const showPatTab = activeOption?.descriptor.capabilities?.pat_login !== false
  const showImportTab = activeOption?.descriptor.capabilities?.import_export !== false
  const showDropSystem = activeOption?.provider === 'workbuddy'
  const showAutoCheckin = activeOption?.provider === 'workbuddy'
  const showCallbackPaste = activeOption?.provider === 'trae'
  const busy = phase === 'busy' || phase === 'polling'
  const settingsLocked = Boolean(createdId.current) || busy
  const isDone = phase === 'done'
  const hint = optionHint(activeOption, t)

  useEffect(() => {
    if (tab === 'pat' && !showPatTab) setTab('browser')
    if (tab === 'import' && !showImportTab) setTab('browser')
  }, [showImportTab, showPatTab, tab])

  function parsedMaxInFlight() {
    const value = Number(maxInFlight)
    if (!Number.isInteger(value) || value < 1 || value > 32) return 4
    return value
  }

  function parsedPriority() {
    const value = Number(priority)
    if (!Number.isInteger(value) || value < 1 || value > 100) return 50
    return value
  }

  function accountOptions() {
    return {
      max_inflight: parsedMaxInFlight(),
      priority: parsedPriority(),
      drop_system_prompt: showDropSystem ? dropSystemPrompt : true,
      workbuddy_auto_checkin: showAutoCheckin ? autoCheckin : false,
    }
  }

  function reset() {
    stopPolling()
    setStep('method')
    setTab('browser')
    setAccountType('')
    setProviderOptions([])
    setTypesLoading(false)
    setName('')
    setMaxInFlight('4')
    setPriority('50')
    setDropSystemPrompt(true)
    setAutoCheckin(false)
    setPat('')
    setJson('')
    setAdvancedOpen(false)
    setPhase('idle')
    setMessage('')
    setAuthUrl('')
    setCallbackUrl('')
    createdId.current = ''
  }

  function close() {
    if (busy) return
    reset()
    onClose()
  }

  function finishAndClose() {
    reset()
    onClose()
  }

  function goLogin(next: TabKey) {
    if (busy || !typesReady) return
    setTab(next)
    setMessage('')
    setStep('login')
  }

  async function ensureAccount(): Promise<string> {
    if (createdId.current) return createdId.current
    if (!activeOption) throw new Error(t('accountTypeHint'))
    const options = accountOptions()
    const account = await createAccount(name.trim() || t('account'), activeOption.provider, activeOption.region, options)
    const id = account?.id || account?.data?.id
    if (!id) throw new Error('create account returned no id')
    createdId.current = id
    return id
  }

  async function runBrowser() {
    setMessage('')
    setAuthUrl('')
    try {
      setPhase('busy')
      const id = await ensureAccount()
      setMessage(t('wizardStartingSession'))
      setPhase('polling')
      const output = await startDeviceLogin(id)
      if (output.authUrl) {
        setAuthUrl(output.authUrl)
        window.open(output.authUrl, '_blank', 'noopener,noreferrer')
      }
      setMessage(t('wizardWaitingBrowser'))
      for (let attempt = 0; attempt < POLL_ATTEMPTS; attempt++) {
        await new Promise((resolve) => { pollTimer.current = window.setTimeout(resolve, POLL_INTERVAL) })
        const status = await fetchLoginStatus(id)
        const login = status.login || {}
        if (login.message) setMessage(login.message)
        if (login.status === 'ok') break
        if (login.status === 'error') throw new Error(login.message || 'login failed')
        if (attempt === POLL_ATTEMPTS - 1) throw new Error(t('wizardLoginTimeout'))
      }
      setPhase('done')
      setMessage(t('wizardAccountReady'))
      onAdded()
      window.setTimeout(finishAndClose, 900)
    } catch (error) {
      setPhase('idle')
      setMessage(error instanceof Error ? error.message : String(error))
    }
  }

  async function runCallback() {
    const pasted = callbackUrl.trim()
    if (!pasted) {
      setMessage(t('wizardCallbackPh'))
      return
    }
    try {
      setPhase('busy')
      const id = await ensureAccount()
      stopPolling()
      await completeLoginCallback(id, pasted)
      setPhase('done')
      setMessage(t('wizardAccountReady'))
      onAdded()
      window.setTimeout(finishAndClose, 900)
    } catch (error) {
      setPhase('polling')
      setMessage(error instanceof Error ? error.message : String(error))
    }
  }

  async function runPat() {
    const token = pat.trim()
    if (!token) { setMessage(t('pastePatFirst')); return }
    setMessage('')
    try {
      setPhase('busy')
      const id = await ensureAccount()
      setMessage(t('wizardStartingSession'))
      await loginWithPat(token, id)
      setPhase('done')
      setMessage(t('patDone'))
      onAdded()
      window.setTimeout(finishAndClose, 700)
    } catch (error) {
      setPhase('idle')
      setMessage(error instanceof Error ? error.message : String(error))
    }
  }

  async function runImport() {
    setMessage('')
    let bundle: any
    try {
      bundle = JSON.parse(json)
    } catch {
      setMessage(t('wizardBadJson'))
      return
    }
    if (!bundle || typeof bundle !== 'object') {
      setMessage(t('wizardBadJson'))
      return
    }
    if (!activeOption) { setMessage(t('accountTypeHint')); return }
    if (activeOption.provider === 'qoder' && (typeof bundle.user_blob !== 'string' || typeof bundle.machine_id !== 'string')) {
      setMessage(t('wizardBadJson'))
      return
    }
    if (!bundle.format) {
      if (activeOption.descriptor.id === 'workbuddy') bundle.format = 'workbuddy-oauth-v1'
      else if (activeOption.descriptor.id === 'trae') bundle.format = 'trae-oauth-v1'
      else bundle.format = 'qoder-native-v1'
    }
    try {
      setPhase('busy')
      const options = accountOptions()
      await importAccount({
        ...bundle,
        name: name.trim() || bundle.name,
        enabled: true,
        provider: activeOption.provider,
        region: activeOption.region,
        max_inflight: options.max_inflight,
        priority: options.priority,
        drop_system_prompt: options.drop_system_prompt,
        workbuddy_auto_checkin: options.workbuddy_auto_checkin,
      })
      setPhase('done')
      setMessage(t('accountImported'))
      onAdded()
      window.setTimeout(finishAndClose, 700)
    } catch (error) {
      setPhase('idle')
      setMessage(error instanceof Error ? error.message : String(error))
    }
  }

  function onPickFile() {
    fileInput.current?.click()
  }

  function onFileChange(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]
    if (!file) return
    const reader = new FileReader()
    reader.onload = () => setJson(String(reader.result || ''))
    reader.readAsText(file)
    event.target.value = ''
  }

  const tabPending = (key: TabKey) => busy && tab === key
  const useTypeSelect = providerOptions.length > TILE_LIMIT
  const typeOptions = providerOptions.map((option) => ({
    value: option.id,
    label: optionLabel(option, t),
    hint: optionHint(option, t),
    icon: <ProviderMark provider={option.provider} size={18} />,
    disabled: settingsLocked,
  }))

  const methodOptions = [
    { value: 'browser' as const, label: t('tabBrowser'), icon: <ShieldCheck size={16} /> },
    showPatTab ? { value: 'pat' as const, label: t('tabPat'), icon: <Key size={16} /> } : null,
    showImportTab ? { value: 'import' as const, label: t('tabImport'), icon: <FileCode size={16} /> } : null,
  ].filter(Boolean) as Array<{ value: TabKey; label: string; icon: React.ReactNode }>

  const tabLead = tab === 'browser'
    ? t('wizardBrowserLead')
    : tab === 'pat'
      ? t(activeOption?.region === 'cn' && activeOption?.provider === 'qoder' ? 'wizardPatLeadCN' : 'wizardPatLead')
      : t('wizardImportLead')

  return (
    <Modal.Root isOpen={isOpen} onOpenChange={(next: boolean) => { if (!next) close() }}>
      <Modal.Backdrop variant="blur" isDismissable={!busy}>
        <Modal.Container size="lg" scroll="inside">
          <Modal.Dialog>
            <Modal.Header className="items-start justify-between gap-4 px-5 pt-5">
              <div className="min-w-0">
                <Modal.Heading className="text-lg font-semibold tracking-[-0.01em]">{t('addAccountTitle')}</Modal.Heading>
                <p className="mt-1 text-xs font-normal leading-5 text-muted">
                  {step === 'method' ? t('addAccountDesc') : (hint || t('addAccountDesc'))}
                </p>
              </div>
              <Modal.CloseTrigger aria-label={t('close')} className="grid size-8 shrink-0 place-items-center rounded-lg text-muted transition-colors hover:bg-surface-secondary hover:text-foreground"><X size={16} /></Modal.CloseTrigger>
            </Modal.Header>
            <Modal.Body className="px-5 pb-2">
              {step === 'method' ? (
                <>
                  <section className="space-y-2.5">
                    {typesLoading ? (
                      <>
                        <span className="text-sm font-medium text-muted">{t('accountType')}</span>
                        <AccountTypeSkeleton ariaLabel={t('accountType')} />
                      </>
                    ) : !typesReady ? (
                      <>
                        <span className="text-sm font-medium text-muted">{t('accountType')}</span>
                        <p className="rounded-lg border border-separator bg-surface-secondary/45 px-3.5 py-3 text-xs leading-5 text-muted">{t('accountTypeHint')}</p>
                      </>
                    ) : useTypeSelect ? (
                      <Select
                        fullWidth
                        aria-label={t('accountType')}
                        value={accountType}
                        isDisabled={settingsLocked}
                        onChange={(next) => {
                          if (typeof next === 'string' && next && !settingsLocked) setAccountType(next)
                        }}
                      >
                        <Label className="text-sm font-medium text-muted">{t('accountType')}</Label>
                        <Select.Trigger className="items-center">
                          <Select.Value className="min-w-0 truncate">
                            {({ defaultChildren, isPlaceholder }) => {
                              const selected = typeOptions.find((option) => option.value === accountType)
                              if (isPlaceholder || !selected) return defaultChildren
                              return (
                                <span className="flex min-w-0 items-center gap-2">
                                  <span className="grid size-5 shrink-0 place-items-center">{selected.icon}</span>
                                  <span className="truncate">{selected.label}</span>
                                </span>
                              )
                            }}
                          </Select.Value>
                          <Select.Indicator />
                        </Select.Trigger>
                        <Select.Popover className="max-h-72">
                          <ListBox>
                            {typeOptions.map((option) => (
                              <ListBox.Item key={option.value} id={option.value} textValue={option.label}>
                                <span className="grid size-5 shrink-0 place-items-center">{option.icon}</span>
                                <div className="min-w-0 flex-1">
                                  <Label className="block truncate">{option.label}</Label>
                                  {option.hint ? <Description className="truncate">{option.hint}</Description> : null}
                                </div>
                                <ListBox.ItemIndicator />
                              </ListBox.Item>
                            ))}
                          </ListBox>
                        </Select.Popover>
                      </Select>
                    ) : (
                      <>
                        <span className="text-sm font-medium text-muted">{t('accountType')}</span>
                        <OptionTiles
                          ariaLabel={t('accountType')}
                          columns={2}
                          value={accountType}
                          onChange={(next) => { if (!settingsLocked) setAccountType(next) }}
                          options={typeOptions}
                        />
                      </>
                    )}
                    {typesReady && hint ? <p className="min-h-5 text-xs leading-5 text-muted">{hint}</p> : null}
                  </section>

                  <section className="mt-5 space-y-2.5">
                    <Input
                      value={name}
                      onChange={(event) => setName(event.target.value)}
                      placeholder={t('wizardNamePh')}
                      aria-label={t('accountName')}
                      disabled={settingsLocked}
                    />
                    <button
                      type="button"
                      onClick={() => setAdvancedOpen((open) => !open)}
                      aria-expanded={advancedOpen}
                      className="inline-flex items-center gap-1 text-xs font-medium text-muted transition-colors hover:text-foreground"
                    >
                      <CaretRight size={12} className={`transition-transform duration-200 ${advancedOpen ? 'rotate-90' : ''}`} />
                      {t('wizardAdvanced')}
                    </button>
                    {advancedOpen ? (
                      <div className="space-y-3 rounded-lg border border-separator px-3.5 py-3">
                        <div className="grid gap-3 sm:grid-cols-2">
                          <label className="block space-y-1.5">
                            <span className="text-xs font-medium text-muted">{t('maxInflight')}</span>
                            <Input
                              type="number"
                              min={1}
                              max={32}
                              value={maxInFlight}
                              onChange={(event) => setMaxInFlight(event.target.value)}
                              aria-label={t('maxInflight')}
                              disabled={settingsLocked}
                            />
                            <p className="text-[11px] leading-4 text-muted">{t('maxInflightHint')}</p>
                          </label>
                          <label className="block space-y-1.5">
                            <span className="text-xs font-medium text-muted">{t('priority')}</span>
                            <Input
                              type="number"
                              min={1}
                              max={100}
                              value={priority}
                              onChange={(event) => setPriority(event.target.value)}
                              aria-label={t('priority')}
                              disabled={settingsLocked}
                            />
                            <p className="text-[11px] leading-4 text-muted">{t('priorityHint')}</p>
                          </label>
                        </div>
                        {showDropSystem ? (
                          <div className="flex items-center justify-between gap-3">
                            <div className="min-w-0">
                              <div className="text-xs font-medium text-muted">{t('dropSystemPrompt')}</div>
                              <p className="mt-0.5 text-[11px] leading-4 text-muted">{t('dropSystemPromptCreateHint')}</p>
                            </div>
                            <CompactSwitch
                              isSelected={dropSystemPrompt}
                              isDisabled={settingsLocked}
                              ariaLabel={t('dropSystemPrompt')}
                              onChange={setDropSystemPrompt}
                            />
                          </div>
                        ) : null}
                        {showAutoCheckin ? (
                          <div className="flex items-center justify-between gap-3">
                            <div className="min-w-0">
                              <div className="text-xs font-medium text-muted">{t('autoCheckin')}</div>
                              <p className="mt-0.5 text-[11px] leading-4 text-muted">{t('autoCheckinCreateHint')}</p>
                            </div>
                            <CompactSwitch
                              isSelected={autoCheckin}
                              isDisabled={settingsLocked}
                              ariaLabel={t('autoCheckin')}
                              onChange={setAutoCheckin}
                            />
                          </div>
                        ) : null}
                      </div>
                    ) : null}
                  </section>

                  <div className="mt-5">
                    <Button className="w-full" isDisabled={!typesReady} onPress={() => goLogin(tab)}>
                      {t('wizardContinue')}
                    </Button>
                  </div>
                </>
              ) : (
                <>
                  <div className="flex items-center gap-3 rounded-lg border border-separator bg-surface-secondary/45 px-3 py-2.5">
                    <span className="shrink-0"><ProviderMark provider={activeOption?.provider} size={18} /></span>
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-sm font-medium text-foreground">{activeOption ? optionLabel(activeOption, t) : t('accountType')}</div>
                      <div className="truncate text-[11px] text-muted">{name.trim() || t('account')}</div>
                    </div>
                    <Button size="sm" variant="ghost" onPress={() => setStep('method')} isDisabled={busy || Boolean(createdId.current)}>
                      <CaretLeft size={12} />{t('wizardBack')}
                    </Button>
                  </div>

                  <FilterToggle
                    className="mt-3"
                    value={tab}
                    onChange={(next) => {
                      if (!busy && next !== tab) {
                        setTab(next as TabKey)
                        setMessage('')
                        setAuthUrl('')
                      }
                    }}
                    ariaLabel={t('authentication')}
                    options={methodOptions.map((option) => ({
                      id: option.value,
                      label: option.label,
                      icon: option.icon,
                    }))}
                  />

                  <p className="mt-2 min-h-5 text-xs leading-5 text-muted">{tabLead}</p>

                  <div className="mt-3 flex flex-col gap-4">
                    {tab === 'browser' ? (
                      <>
                        {authUrl ? (
                          <div className="rounded-lg border border-separator bg-surface-secondary px-3 py-2.5">
                            <div className="flex items-center gap-2 text-xs">
                              <StatusIcon phase={phase} busy={busy} tab={tab} forTab="browser" />
                              <span className="text-muted">{message || t('loginOpenMsg')}</span>
                            </div>
                            <button onClick={() => window.open(authUrl, '_blank', 'noopener,noreferrer')} className="mt-2 inline-flex items-center gap-1.5 text-xs font-medium text-foreground hover:underline">
                              <ArrowSquareOut size={12} />{t('wizardOpenBrowser')}
                            </button>
                          </div>
                        ) : null}
                        {message && !authUrl ? (
                          <p className={`rounded-lg border px-3 py-2 text-xs ${isDone ? 'border-success bg-success-soft text-success' : 'border-separator bg-surface-secondary text-muted'}`}>{message}</p>
                        ) : null}
                        {showCallbackPaste ? (
                          <div className="space-y-2">
                            <p className="text-[11px] leading-4 text-muted">{t('wizardCallbackLead')}</p>
                            <TextArea
                              className="h-28 w-full resize-none font-mono text-xs leading-5"
                              value={callbackUrl}
                              onChange={(event) => setCallbackUrl(event.target.value)}
                              placeholder={t('wizardCallbackPh')}
                              aria-label={t('wizardCallbackPh')}
                              disabled={isDone}
                            />
                            <Button className="w-full" variant="secondary" isPending={phase === 'busy' && Boolean(callbackUrl.trim())} onPress={() => void runCallback()} isDisabled={isDone}>
                              {t('wizardSubmitCallback')}
                            </Button>
                          </div>
                        ) : null}
                        <Button className="w-full" isPending={tabPending('browser')} onPress={() => void runBrowser()}>
                          {isDone ? <><CheckCircle size={15} />{t('wizardAccountReady')}</> : <><ShieldCheck size={15} />{t('wizardStartBrowser')}</>}
                        </Button>
                      </>
                    ) : tab === 'pat' ? (
                      <>
                        <Input type="password" value={pat} onChange={(event) => setPat(event.target.value)} placeholder={t('wizardPatPh')} aria-label={t('wizardPatPh')} disabled={busy} />
                        {message ? (
                          <p className={`rounded-lg border px-3 py-2 text-xs ${isDone ? 'border-success bg-success-soft text-success' : 'border-separator bg-surface-secondary text-muted'}`}>{message}</p>
                        ) : null}
                        <Button className="w-full" isPending={tabPending('pat')} onPress={() => void runPat()}>
                          {isDone ? <><CheckCircle size={15} />{t('patDone')}</> : <><Key size={15} />{t('wizardCreateAndLogin')}</>}
                        </Button>
                      </>
                    ) : (
                      <>
                        <div className="flex items-center justify-between gap-3">
                          <span className="text-xs font-medium text-muted">JSON</span>
                          <Button size="sm" variant="secondary" onPress={onPickFile} isDisabled={busy}><FileCode size={13} />{t('wizardChooseFile')}</Button>
                          <input ref={fileInput} type="file" accept="application/json,.json" className="hidden" onChange={onFileChange} />
                        </div>
                        <TextArea className="min-h-32 font-mono text-xs" value={json} onChange={(event) => setJson(event.target.value)} placeholder={t('wizardImportPh')} aria-label={t('tabImport')} disabled={busy} />
                        {message ? (
                          <p className={`rounded-lg border px-3 py-2 text-xs ${isDone ? 'border-success bg-success-soft text-success' : 'border-separator bg-surface-secondary text-muted'}`}>{message}</p>
                        ) : null}
                        <Button className="w-full" isPending={tabPending('import')} onPress={() => void runImport()}>
                          {isDone ? <><CheckCircle size={15} />{t('accountImported')}</> : <><FileCode size={15} />{t('importCredential')}</>}
                        </Button>
                      </>
                    )}
                  </div>
                </>
              )}
            </Modal.Body>
            <Modal.Footer className="justify-end px-5 pb-5">
              <Button variant="ghost" onPress={close} isDisabled={busy}>{t('cancel')}</Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal.Root>
  )
}
