import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import gsap from 'gsap'
import { Button, Input, Modal, NumberField, Skeleton, TextArea } from '@heroui/react'
import { ArrowSquareOut, CaretLeft, CaretRight, CheckCircle, FileCode, Key, ShieldCheck, X } from '@phosphor-icons/react'
import { BrandMark } from '@/components/BrandMark'
import { ProviderMark } from '@/components/ProviderMark'
import { CompactSwitch } from '@/components/ui/CompactSwitch'
import { FilterToggle } from '@/components/ui/FilterToggle'
import { FormRow } from '@/components/ui/FormRow'
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
  'devin-global': 'accountTypeDevinGlobal',
  'command-global': 'accountTypeCommandGlobal',
  'codex-global': 'accountTypeCodexGlobal',
  'zhipu-cn': 'accountTypeZhipuCN',
}

const hintKeys: Record<string, string> = {
  'qoder-global': 'accountTypeQoderGlobalHint',
  'qoder-cn': 'accountTypeQoderCNHint',
  'workbuddy-cn': 'accountTypeWorkBuddyCNHint',
  'workbuddy-global': 'accountTypeWorkBuddyGlobalHint',
  'trae-cn': 'accountTypeTraeCNHint',
  'devin-global': 'accountTypeDevinGlobalHint',
  'command-global': 'accountTypeCommandGlobalHint',
  'codex-global': 'accountTypeCodexGlobalHint',
  'zhipu-cn': 'accountTypeZhipuCNHint',
}

function AccountTypeSkeleton({ ariaLabel }: { ariaLabel: string }) {
  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2" aria-busy="true" aria-label={ariaLabel}>
      {Array.from({ length: 6 }, (_, index) => (
        <div key={index} className="flex items-center gap-2.5 rounded-xl border border-separator px-3 py-2.5">
          <Skeleton className="size-7 shrink-0 rounded-lg" />
          <Skeleton className="h-4 w-20 rounded-lg" />
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

export function AddAccountModal({ isOpen, onClose, onAdded }: Props) {
  const { t } = useI18n()
  const [step, setStep] = useState<Step>('method')
  const [tab, setTab] = useState<TabKey>('browser')
  const [accountType, setAccountType] = useState('')
  const [providerOptions, setProviderOptions] = useState<ProviderOption[]>([])
  const [typesLoading, setTypesLoading] = useState(false)
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [name, setName] = useState('')
  const [maxInFlight, setMaxInFlight] = useState(4)
  const [priority, setPriority] = useState(50)
  const [dropSystemPrompt, setDropSystemPrompt] = useState(true)
  const [proxyUrl, setProxyUrl] = useState('')
  const [pat, setPat] = useState('')
  const [json, setJson] = useState('')
  const [zhipuMode, setZhipuMode] = useState<'payg' | 'coding'>('payg')
  const [zhipuKey, setZhipuKey] = useState('')
  const [zhipuOrganization, setZhipuOrganization] = useState('')
  const [zhipuProject, setZhipuProject] = useState('')
  const [phase, setPhase] = useState<Phase>('idle')
  const [message, setMessage] = useState('')
  const [authUrl, setAuthUrl] = useState('')
  const [callbackUrl, setCallbackUrl] = useState('')
  const stepBody = useRef<HTMLDivElement>(null)
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
  // Command Code has no browser login (a pasted user_… key is the only auth),
  // so the wizard opens on the PAT tab instead of the browser tab.
  const hasBrowserLogin = activeOption?.descriptor.capabilities?.browser_login !== false
  const showDropSystem = activeOption?.provider === 'workbuddy'
  const showCallbackPaste = activeOption?.provider === 'trae' || activeOption?.provider === 'devin' || activeOption?.provider === 'codex'
  const busy = phase === 'busy' || phase === 'polling'
  const settingsLocked = Boolean(createdId.current) || busy
  const isDone = phase === 'done'
  const hint = optionHint(activeOption, t)

  useEffect(() => {
    if (tab === 'pat' && !showPatTab) setTab('browser')
    if (tab === 'import' && !showImportTab) setTab('browser')
  }, [showImportTab, showPatTab, tab])

  useEffect(() => {
    // Providers without a browser login (e.g. Command Code) must not land on an
    // empty browser tab; send the operator to the paste-key tab.
    if (hasBrowserLogin) return
    if (showPatTab) setTab('pat')
    else if (showImportTab) setTab('import')
  }, [hasBrowserLogin, showImportTab, showPatTab])

  function parsedMaxInFlight() {
    if (!Number.isInteger(maxInFlight) || maxInFlight < 1 || maxInFlight > 32) return 4
    return maxInFlight
  }

  function parsedPriority() {
    if (!Number.isInteger(priority) || priority < 1 || priority > 100) return 50
    return priority
  }

  function accountOptions() {
    return {
      max_inflight: parsedMaxInFlight(),
      priority: parsedPriority(),
      drop_system_prompt: showDropSystem ? dropSystemPrompt : true,
      proxy_url: proxyUrl.trim(),
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
    setMaxInFlight(4)
    setPriority(50)
    setDropSystemPrompt(true)
    setProxyUrl('')
    setPat('')
    setJson('')
    setZhipuMode('payg')
    setZhipuKey('')
    setZhipuOrganization('')
    setZhipuProject('')
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

  useLayoutEffect(() => {
    const body = stepBody.current
    if (!body || !isOpen) return
    const context = gsap.context(() => {
      const media = gsap.matchMedia()
      media.add('(prefers-reduced-motion: reduce)', () => {
        gsap.set(body, { autoAlpha: 1, y: 0 })
      })
      media.add('(prefers-reduced-motion: no-preference)', () => {
        gsap.fromTo(
          body,
          { autoAlpha: 0, y: 12 },
          { autoAlpha: 1, y: 0, duration: 0.34, ease: 'power3.out', overwrite: true },
        )
      })
    }, body)
    return () => context.revert()
  }, [isOpen, step])

  function chooseProvider(next: string) {
    if (busy || !typesReady) return
    const option = providerOptions.find((item) => item.id === next)
    setAccountType(next)
    setTab(option?.provider === 'zhipu' ? 'pat' : 'browser')
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

  async function runZhipu() {
    const key = zhipuKey.trim()
    if (!key) { setMessage(t('zhipuKeyRequired')); return }
    if (!activeOption) { setMessage(t('accountTypeHint')); return }
    setMessage('')
    try {
      setPhase('busy')
      const options = accountOptions()
      await importAccount({
        format: 'zhipu-key-v1',
        api_key: key,
        mode: zhipuMode,
        organization: zhipuMode === 'coding' ? zhipuOrganization.trim() : '',
        project: zhipuMode === 'coding' ? zhipuProject.trim() : '',
        name: name.trim() || t('accountTypeZhipuCN'),
        enabled: true,
        provider: activeOption.provider,
        region: activeOption.region,
        max_inflight: options.max_inflight,
        priority: options.priority,
        drop_system_prompt: options.drop_system_prompt,
        proxy_url: options.proxy_url,
      })
      setPhase('done')
      setMessage(t('wizardAccountReady'))
      onAdded()
      window.setTimeout(finishAndClose, 700)
    } catch (error) {
      setPhase('idle')
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
      else if (activeOption.descriptor.id === 'devin') bundle.format = 'devin-session-v1'
      else if (activeOption.descriptor.id === 'command') bundle.format = 'command-key-v1'
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
  const typeOptions = providerOptions.map((option) => ({
    value: option.id,
    label: optionLabel(option, t),
    hint: optionHint(option, t),
    icon: <ProviderMark provider={option.provider} size={18} />,
    disabled: settingsLocked,
  }))

  const isZhipu = activeOption?.provider === 'zhipu'
  const methodOptions = [
    activeOption?.descriptor.capabilities?.browser_login !== false
      ? { value: 'browser' as const, label: t('tabBrowser'), icon: <ShieldCheck size={16} /> }
      : null,
    showPatTab ? { value: 'pat' as const, label: t('tabPat'), icon: <Key size={16} /> } : null,
    showImportTab ? { value: 'import' as const, label: t('tabImport'), icon: <FileCode size={16} /> } : null,
  ].filter(Boolean) as Array<{ value: TabKey; label: string; icon: React.ReactNode }>

  const tabLead = tab === 'browser'
    ? t('wizardBrowserLead')
    : tab === 'pat'
      ? t(activeOption?.provider === 'command'
          ? 'wizardPatLeadCommand'
          : activeOption?.region === 'cn' && activeOption?.provider === 'qoder'
            ? 'wizardPatLeadCN'
            : 'wizardPatLead')
      : t('wizardImportLead')

  return (
    <Modal.Root isOpen={isOpen} onOpenChange={(next: boolean) => { if (!next) close() }}>
      <Modal.Backdrop variant="blur" isDismissable={!busy}>
        <Modal.Container size="lg" scroll="inside" className="sm:max-w-4xl">
          <Modal.Dialog>
            <Modal.Header className="relative items-center justify-center px-12 pt-5 text-center">
              <div className="min-w-0">
                <Modal.Heading className="text-lg font-semibold tracking-[-0.01em]">{t('addAccountTitle')}</Modal.Heading>
                <p className="mt-1 text-xs font-normal leading-5 text-muted">
                  {step === 'method' ? t('addAccountDesc') : (hint || t('addAccountDesc'))}
                </p>
              </div>
              <Modal.CloseTrigger aria-label={t('close')} className="absolute right-4 top-4 grid size-8 shrink-0 place-items-center rounded-lg text-muted transition-colors hover:bg-surface-secondary hover:text-foreground"><X size={16} /></Modal.CloseTrigger>
            </Modal.Header>
            <Modal.Body className="px-6 pb-2">
              <div ref={stepBody}>
              {step === 'method' ? (
                <>
                  <section className="space-y-2.5">
                    {typesLoading ? (
                      <AccountTypeSkeleton ariaLabel={t('accountType')} />
                    ) : !typesReady ? (
                      <p className="rounded-lg border border-separator bg-surface-secondary/45 px-3.5 py-3 text-xs leading-5 text-muted">{t('accountTypeHint')}</p>
                    ) : (
                      <OptionTiles
                        ariaLabel={t('accountType')}
                        columns={2}
                        value=""
                        onChange={chooseProvider}
                        options={typeOptions}
                      />
                    )}
                  </section>
                </>
              ) : (
                <>
                  <div className="flex items-center gap-3 rounded-lg border border-separator bg-surface-secondary/45 px-3 py-2.5">
                    <span className="shrink-0"><ProviderMark provider={activeOption?.provider} size={18} /></span>
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-sm font-medium text-foreground">{activeOption ? optionLabel(activeOption, t) : t('accountType')}</div>
                      <div className="truncate text-[11px] text-muted">{hint}</div>
                    </div>
                    <Button size="sm" variant="ghost" onPress={() => setStep('method')} isDisabled={busy || Boolean(createdId.current)}>
                      <CaretLeft size={12} />{t('wizardBack')}
                    </Button>
                  </div>
                  <div className="mt-4 space-y-3">
                    <FormRow label={t('accountName')}>
                      <Input
                        value={name}
                        onChange={(event) => setName(event.target.value)}
                        placeholder={t('wizardNamePh')}
                        aria-label={t('accountName')}
                        disabled={settingsLocked}
                        autoFocus
                      />
                    </FormRow>
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
                      <div className="space-y-3 rounded-lg border border-separator px-3.5 py-3.5">
                        <FormRow label={t('maxInflight')} hint={t('maxInflightHint')}>
                          <NumberField
                            value={maxInFlight}
                            onChange={(value) => setMaxInFlight(value ?? 4)}
                            minValue={1}
                            maxValue={32}
                            isDisabled={settingsLocked}
                            isRequired
                          >
                            <NumberField.Group>
                              <NumberField.DecrementButton />
                              <NumberField.Input aria-label={t('maxInflight')} />
                              <NumberField.IncrementButton />
                            </NumberField.Group>
                          </NumberField>
                        </FormRow>
                        <FormRow label={t('priority')} hint={t('priorityHint')}>
                          <NumberField
                            value={priority}
                            onChange={(value) => setPriority(value ?? 50)}
                            minValue={1}
                            maxValue={100}
                            isDisabled={settingsLocked}
                            isRequired
                          >
                            <NumberField.Group>
                              <NumberField.DecrementButton />
                              <NumberField.Input aria-label={t('priority')} />
                              <NumberField.IncrementButton />
                            </NumberField.Group>
                          </NumberField>
                        </FormRow>
                        <FormRow label={t('proxyUrl')} hint={t('proxyUrlHint')}>
                          <Input
                            value={proxyUrl}
                            onChange={(event) => setProxyUrl(event.target.value)}
                            placeholder={t('proxyUrlPlaceholder')}
                            aria-label={t('proxyUrl')}
                            disabled={settingsLocked}
                          />
                        </FormRow>
                        {showDropSystem ? (
                          <div className="flex items-center justify-between gap-3">
                            <div className="min-w-0">
                              <div className="text-sm font-medium text-muted">{t('dropSystemPrompt')}</div>
                              <p className="mt-0.5 text-xs leading-5 text-muted">{t('dropSystemPromptCreateHint')}</p>
                            </div>
                            <CompactSwitch
                              isSelected={dropSystemPrompt}
                              isDisabled={settingsLocked}
                              ariaLabel={t('dropSystemPrompt')}
                              onChange={setDropSystemPrompt}
                            />
                          </div>
                        ) : null}
                      </div>
                    ) : null}
                  </div>

                  {isZhipu ? (
                    <div className="mt-4 flex flex-col gap-4">
                      <div className="space-y-2">
                        <span className="text-sm font-medium text-muted">{t('zhipuAccountMode')}</span>
                        <OptionTiles
                          ariaLabel={t('zhipuAccountMode')}
                          columns={2}
                          value={zhipuMode}
                          onChange={(next) => { if (!busy) setZhipuMode(next) }}
                          options={[
                            { value: 'payg', label: t('zhipuModePayg'), hint: t('zhipuModePaygHint'), disabled: busy },
                            { value: 'coding', label: t('zhipuModeCoding'), hint: t('zhipuModeCodingHint'), disabled: busy },
                          ]}
                        />
                      </div>
                      <FormRow label={t('zhipuApiKey')}>
                        <Input type="password" value={zhipuKey} onChange={(event) => setZhipuKey(event.target.value)} placeholder={t('zhipuApiKeyPh')} aria-label={t('zhipuApiKey')} disabled={busy} />
                      </FormRow>
                      {zhipuMode === 'coding' ? (
                        <>
                          <FormRow label={t('zhipuOrganization')}>
                            <Input value={zhipuOrganization} onChange={(event) => setZhipuOrganization(event.target.value)} placeholder={t('zhipuOrganizationPh')} aria-label={t('zhipuOrganization')} disabled={busy} />
                          </FormRow>
                          <FormRow label={t('zhipuProject')}>
                            <Input value={zhipuProject} onChange={(event) => setZhipuProject(event.target.value)} placeholder={t('zhipuProjectPh')} aria-label={t('zhipuProject')} disabled={busy} />
                          </FormRow>
                        </>
                      ) : null}
                      {message ? (
                        <p className="flex items-center gap-2 rounded-lg border border-separator bg-surface-secondary px-3 py-2 text-xs">{isDone ? <CheckCircle size={14} className="shrink-0 text-success" /> : null}<span className={isDone ? 'font-medium text-foreground' : 'text-muted'}>{message}</span></p>
                      ) : null}
                      <Button className="w-full" isPending={phase === 'busy'} onPress={() => void runZhipu()} isDisabled={isDone}>
                        {isDone ? <><CheckCircle size={15} />{t('wizardAccountReady')}</> : <><Key size={15} />{t('zhipuCreate')}</>}
                      </Button>
                    </div>
                  ) : null}

                  {isZhipu ? null : (
                    <>
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
                          <p className="flex items-center gap-2 rounded-lg border border-separator bg-surface-secondary px-3 py-2 text-xs">{isDone ? <CheckCircle size={14} className="shrink-0 text-success" /> : null}<span className={isDone ? 'font-medium text-foreground' : 'text-muted'}>{message}</span></p>
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
                        <FormRow label={t('tabPat')}>
                          <Input type="password" value={pat} onChange={(event) => setPat(event.target.value)} placeholder={t('wizardPatPh')} aria-label={t('wizardPatPh')} disabled={busy} />
                        </FormRow>
                        {message ? (
                          <p className="flex items-center gap-2 rounded-lg border border-separator bg-surface-secondary px-3 py-2 text-xs">{isDone ? <CheckCircle size={14} className="shrink-0 text-success" /> : null}<span className={isDone ? 'font-medium text-foreground' : 'text-muted'}>{message}</span></p>
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
                          <p className="flex items-center gap-2 rounded-lg border border-separator bg-surface-secondary px-3 py-2 text-xs">{isDone ? <CheckCircle size={14} className="shrink-0 text-success" /> : null}<span className={isDone ? 'font-medium text-foreground' : 'text-muted'}>{message}</span></p>
                        ) : null}
                        <Button className="w-full" isPending={tabPending('import')} onPress={() => void runImport()}>
                          {isDone ? <><CheckCircle size={15} />{t('accountImported')}</> : <><FileCode size={15} />{t('importCredential')}</>}
                        </Button>
                      </>
                    )}
                  </div>
                    </>
                  )}
                </>
              )}
              </div>
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
