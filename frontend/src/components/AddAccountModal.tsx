import { useEffect, useRef, useState } from 'react'
import { Button, Input, Modal, TextArea } from '@heroui/react'
import { CheckCircle, ArrowSquareOut, FileCode, Globe, Key, MapPin, ShieldCheck, SpinnerGap, X } from '@phosphor-icons/react'
import { QoderMark } from '@/components/QoderMark'
import { OptionTiles } from '@/components/ui/OptionTiles'
import { useI18n } from '@/hooks/useI18n'
import {
  createAccount,
  fetchLoginStatus,
  fetchProviders,
  importAccount,
  loginWithPat,
  startDeviceLogin,
  type ProviderDescriptor,
} from '@/api/overview'

type Props = {
  isOpen: boolean
  onClose: () => void
  onAdded: () => void
}

type TabKey = 'browser' | 'pat' | 'import'

type ProviderOption = {
  id: string
  provider: string
  region: string
  labelKey: string
  hintKey: string
  descriptor: ProviderDescriptor
}

const labelKeys: Record<string, { label: string; hint: string }> = {
  'qoder-global': { label: 'accountTypeQoderGlobal', hint: 'accountTypeQoderGlobalHint' },
  'workbuddy-cn': { label: 'accountTypeWorkBuddyCN', hint: 'accountTypeWorkBuddyCNHint' },
  'workbuddy-global': { label: 'accountTypeWorkBuddyGlobal', hint: 'accountTypeWorkBuddyGlobalHint' },
}

const fallbackProviderOptions: ProviderOption[] = [
  {
    id: 'qoder-global', provider: 'qoder', region: 'global',
    labelKey: 'accountTypeQoderGlobal', hintKey: 'accountTypeQoderGlobalHint',
    descriptor: {
      id: 'qoder', label: 'Qoder', runtime: 'child_process', default_region: 'global',
      capabilities: { browser_login: true, pat_login: true, import_export: true },
      regions: [{ id: 'global', label: 'Global' }],
    },
  },
]

async function loadProviderOptions(): Promise<ProviderOption[]> {
  const output = await fetchProviders().catch(() => null)
  const descriptors = output?.data || []
  const options: ProviderOption[] = []
  for (const descriptor of descriptors) {
    for (const region of descriptor.regions) {
      const id = `${descriptor.id}-${region.id}`
      const keys = labelKeys[id]
      if (!keys) continue
      options.push({
        id, provider: descriptor.id, region: region.id,
        labelKey: keys.label, hintKey: keys.hint, descriptor,
      })
    }
  }
  return options.length ? options : fallbackProviderOptions
}

function providerBadge(option: ProviderOption) {
  if (option.provider === 'qoder') return <QoderMark size={18} />
  return (
    <span className="grid size-[18px] place-items-center rounded-[4px] bg-[var(--app-surface)] text-[10px] font-semibold text-[var(--app-muted)] ring-1 ring-[var(--app-line)]">
      W
    </span>
  )
}

function StatusIcon({ phase, busy, tab, forTab }: { phase: Phase; busy: boolean; tab: TabKey; forTab: TabKey }) {
  if (phase === 'done') return <CheckCircle size={16} className="text-[var(--app-ok)]" />
  if (busy && tab === forTab) return <SpinnerGap size={16} className="animate-spin" />
  return null
}

type Phase = 'idle' | 'busy' | 'polling' | 'starting' | 'done'

const POLL_ATTEMPTS = 90
const POLL_INTERVAL = 2000

// Fixed step height keeps the dialog from jumping between login methods.
const STEP_HEIGHT = 328

export function AddAccountModal({ isOpen, onClose, onAdded }: Props) {
  const { t } = useI18n()
  const [tab, setTab] = useState<TabKey>('browser')
  const [accountType, setAccountType] = useState<string>('qoder-global')
  const [providerOptions, setProviderOptions] = useState<ProviderOption[]>(fallbackProviderOptions)
  const [name, setName] = useState('')
  const [pat, setPat] = useState('')
  const [json, setJson] = useState('')
  const [phase, setPhase] = useState<Phase>('idle')
  const [message, setMessage] = useState('')
  const [authUrl, setAuthUrl] = useState('')
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
    void loadProviderOptions().then((options) => {
      setProviderOptions(options)
      if (!options.some((option) => option.id === accountType)) {
        setAccountType(options[0]?.id || 'qoder-global')
        setTab('browser')
      }
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isOpen])

  const activeOption = providerOptions.find((option) => option.id === accountType) || fallbackProviderOptions[0]
  const showPatTab = activeOption.descriptor.capabilities?.pat_login !== false

  function reset() {
    stopPolling()
    setTab('browser')
    setAccountType('qoder-global')
    setName('')
    setPat('')
    setJson('')
    setPhase('idle')
    setMessage('')
    setAuthUrl('')
    createdId.current = ''
  }

  function close() {
    if (phase === 'busy' || phase === 'polling' || phase === 'starting') return
    reset()
    onClose()
  }

  function switchTab(next: TabKey) {
    if (busy || next === 'pat' && !showPatTab) return
    setTab(next)
  }

  async function ensureAccount(): Promise<string> {
    if (createdId.current) return createdId.current
    const account = await createAccount(name.trim() || t('account'), activeOption.provider, activeOption.region)
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
      setPhase('starting')
      setMessage(t('wizardStartingWorker'))
      await new Promise((resolve) => { pollTimer.current = window.setTimeout(resolve, 1500) })
      setPhase('done')
      setMessage(t('wizardAccountReady'))
      onAdded()
      window.setTimeout(close, 900)
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
      await loginWithPat(token, id)
      setPhase('done')
      setMessage(t('patDone'))
      onAdded()
      window.setTimeout(close, 700)
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
    if (activeOption.provider === 'qoder' && (typeof bundle.user_blob !== 'string' || typeof bundle.machine_id !== 'string')) {
      setMessage(t('wizardBadJson'))
      return
    }
    if (!bundle.format) bundle.format = activeOption.descriptor.id === 'workbuddy' ? 'workbuddy-oauth-v1' : 'qoder-native-v1'
    try {
      setPhase('busy')
      await importAccount({ ...bundle, name: name.trim() || bundle.name, enabled: true, provider: activeOption.provider, region: activeOption.region })
      setPhase('done')
      setMessage(t('accountImported'))
      onAdded()
      window.setTimeout(close, 700)
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

  const busy = phase === 'busy' || phase === 'polling' || phase === 'starting'
  const tabPending = (key: TabKey) => busy && tab === key

  const methodOptions = [
    { value: 'browser' as const, label: t('tabBrowser'), icon: <ShieldCheck size={16} className={tab === 'browser' ? 'text-[var(--app-ink)]' : 'text-[var(--app-muted)]'} />, disabled: busy },
    { value: 'pat' as const, label: t('tabPat'), icon: <Key size={16} className={tab === 'pat' ? 'text-[var(--app-ink)]' : 'text-[var(--app-muted)]'} />, disabled: busy || !showPatTab },
    { value: 'import' as const, label: t('tabImport'), icon: <FileCode size={16} className={tab === 'import' ? 'text-[var(--app-ink)]' : 'text-[var(--app-muted)]'} />, disabled: busy },
  ]

  return (
    <Modal.Root isOpen={isOpen} onOpenChange={(next: boolean) => { if (!next) close() }}>
      <Modal.Backdrop variant="blur" isDismissable={!busy}>
        <Modal.Container size="lg" scroll="inside">
          <Modal.Dialog>
            <Modal.Header className="items-start justify-between gap-4 px-5 pt-5">
              <div className="min-w-0">
                <Modal.Heading className="text-lg font-semibold tracking-[-0.01em]">{t('addAccountTitle')}</Modal.Heading>
                <p className="mt-1 text-xs font-normal leading-5 text-[var(--app-faint)]">{t('addAccountDesc')}</p>
              </div>
              <Modal.CloseTrigger aria-label={t('close')} className="grid size-8 shrink-0 place-items-center rounded-lg text-[var(--app-muted)] transition-colors hover:bg-[var(--app-surface-muted)] hover:text-[var(--app-ink)]"><X size={16} /></Modal.CloseTrigger>
            </Modal.Header>
            <Modal.Body className="px-5 pb-2">
              <section className="space-y-2.5">
                <div className="flex items-baseline justify-between gap-3">
                  <span className="text-sm font-medium text-[var(--app-muted)]">{t('accountType')}</span>
                  <span className="inline-flex items-center gap-1 text-[11px] text-[var(--app-faint)]">
                    {activeOption.provider === 'qoder' ? <Globe size={11} /> : <MapPin size={11} />}
                    {activeOption.provider === 'qoder' ? 'WASM runtime' : 'HTTP runtime'}
                  </span>
                </div>
                <OptionTiles
                  ariaLabel={t('accountType')}
                  columns={3}
                  value={accountType}
                  onChange={setAccountType}
                  options={providerOptions.map((option) => ({
                    value: option.id,
                    label: t(option.labelKey),
                    hint: t(option.hintKey),
                    icon: providerBadge(option),
                  }))}
                />
              </section>

              <section className="mt-5 space-y-2.5">
                <span className="block text-sm font-medium text-[var(--app-muted)]">{t('loginMethod')}</span>
                <OptionTiles
                  ariaLabel={t('loginMethod')}
                  columns={3}
                  value={tab}
                  onChange={(next) => switchTab(next)}
                  options={methodOptions}
                />
              </section>

              <div className="relative mt-3 overflow-hidden rounded-lg" style={{ height: STEP_HEIGHT }}>
                <div
                  key={tab}
                  className="absolute inset-0 flex flex-col gap-4 pt-4 animate-[step-in_260ms_cubic-bezier(0.16,1,0.3,1)]"
                >
                  {tab === 'browser' ? (
                    <>
                      <p className="text-xs leading-5 text-[var(--app-faint)]">{t('wizardBrowserLead')}</p>
                      <Input value={name} onChange={(event) => setName(event.target.value)} placeholder={t('wizardNamePh')} aria-label={t('wizardNamePh')} disabled={busy} />
                      {authUrl ? (
                        <div className="rounded-lg border border-[var(--app-line)] bg-[var(--app-surface-muted)] px-3 py-2.5">
                          <div className="flex items-center gap-2 text-xs">
                            <StatusIcon phase={phase} busy={busy} tab={tab} forTab="browser" />
                            <span className="text-[var(--app-muted)]">{message || t('loginOpenMsg')}</span>
                          </div>
                          <button onClick={() => window.open(authUrl, '_blank', 'noopener,noreferrer')} className="mt-2 inline-flex items-center gap-1.5 text-xs font-medium text-[var(--app-ink)] hover:underline">
                            <ArrowSquareOut size={12} />{t('wizardOpenBrowser')}
                          </button>
                        </div>
                      ) : null}
                      <div className="mt-auto">
                        <Button className="w-full" isPending={tabPending('browser')} onPress={() => void runBrowser()}>
                          {phase === 'done' ? <><CheckCircle size={15} />{t('wizardAccountReady')}</> : <><ShieldCheck size={15} />{t('wizardStartBrowser')}</>}
                        </Button>
                      </div>
                    </>
                  ) : tab === 'pat' ? (
                    <>
                      <p className="text-xs leading-5 text-[var(--app-faint)]">{t('wizardPatLead')}</p>
                      <Input value={name} onChange={(event) => setName(event.target.value)} placeholder={t('wizardNamePh')} aria-label={t('wizardNamePh')} disabled={busy} />
                      <Input type="password" value={pat} onChange={(event) => setPat(event.target.value)} placeholder={t('wizardPatPh')} aria-label={t('wizardPatPh')} disabled={busy} />
                      <div className="mt-auto">
                        <Button className="w-full" isPending={tabPending('pat')} onPress={() => void runPat()}>
                          {phase === 'done' ? <><CheckCircle size={15} />{t('patDone')}</> : <><Key size={15} />{t('wizardCreateAndLogin')}</>}
                        </Button>
                      </div>
                    </>
                  ) : (
                    <>
                      <p className="text-xs leading-5 text-[var(--app-faint)]">{t('wizardImportLead')}</p>
                      <Input value={name} onChange={(event) => setName(event.target.value)} placeholder={t('wizardNamePh')} aria-label={t('wizardNamePh')} disabled={busy} />
                      <div className="flex items-center justify-between gap-3">
                        <span className="text-[10px] font-semibold tracking-[0.1em] text-[var(--app-faint)] uppercase">JSON</span>
                        <Button size="sm" variant="secondary" onPress={onPickFile} isDisabled={busy}><FileCode size={13} />{t('wizardChooseFile')}</Button>
                        <input ref={fileInput} type="file" accept="application/json,.json" className="hidden" onChange={onFileChange} />
                      </div>
                      <TextArea className="min-h-32 flex-1 font-mono text-xs" value={json} onChange={(event) => setJson(event.target.value)} placeholder={t('wizardImportPh')} aria-label={t('tabImport')} disabled={busy} />
                      <div>
                        <Button className="w-full" isPending={tabPending('import')} onPress={() => void runImport()}>
                          {phase === 'done' ? <><CheckCircle size={15} />{t('accountImported')}</> : <><FileCode size={15} />{t('importCredential')}</>}
                        </Button>
                      </div>
                    </>
                  )}
                </div>
              </div>

              {message && !authUrl ? (
                <p className={`rounded-lg border px-3 py-2 text-xs ${phase === 'done' ? 'border-[var(--app-ok-line)] bg-[var(--app-ok-soft)] text-[var(--app-ok-strong)]' : 'border-[var(--app-line)] bg-[var(--app-surface-muted)] text-[var(--app-muted)]'}`}>{message}</p>
              ) : null}
            </Modal.Body>
            <Modal.Footer className="justify-end px-5 pb-5">
              <Button variant="ghost" onPress={close} isDisabled={busy}>{t('cancel')}</Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
      <style>{`@keyframes step-in{from{opacity:0;transform:translateY(6px)}to{opacity:1;transform:translateY(0)}}`}</style>
    </Modal.Root>
  )
}
