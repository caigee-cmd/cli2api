import { useEffect, useRef, useState } from 'react'
import {
  Button,
  Input,
  Label,
  ListBox,
  Modal,
  Tab,
  Tabs,
  Select,
  TextArea,
} from '@heroui/react'
import { CheckCircle, ArrowSquareOut, FileCode, Key, SpinnerGap, ShieldCheck, X } from '@phosphor-icons/react'
import { QoderMark } from '@/components/QoderMark'
import { useI18n } from '@/hooks/useI18n'
import {
  createAccount,
  fetchLoginStatus,
  importAccount,
  loginWithPat,
  startDeviceLogin,
} from '@/api/overview'

type Props = {
  isOpen: boolean
  onClose: () => void
  onAdded: () => void
}

type TabKey = 'browser' | 'pat' | 'import'
type AccountType = 'qoder-global'

const accountTypes: Array<{ id: AccountType; labelKey: string; hintKey: string }> = [
  { id: 'qoder-global', labelKey: 'accountTypeQoderGlobal', hintKey: 'accountTypeQoderGlobalHint' },
]
function StatusIcon({ phase, busy, tab, forTab }: { phase: Phase; busy: boolean; tab: TabKey; forTab: TabKey }) {
  if (phase === 'done') return <CheckCircle size={16} className="text-[var(--app-ok)]" />
  if (busy && tab === forTab) return <SpinnerGap size={16} className="animate-spin" />
  return null
}

type Phase = 'idle' | 'busy' | 'polling' | 'starting' | 'done'

const POLL_ATTEMPTS = 90
const POLL_INTERVAL = 2000

export function AddAccountModal({ isOpen, onClose, onAdded }: Props) {
  const { t } = useI18n()
  const [tab, setTab] = useState<TabKey>('browser')
  const [accountType, setAccountType] = useState<AccountType>('qoder-global')
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

  async function ensureAccount(): Promise<string> {
    if (createdId.current) return createdId.current
    const account = await createAccount(name.trim() || t('account'), accountType)
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
    if (!bundle || typeof bundle !== 'object' || typeof bundle.user_blob !== 'string' || typeof bundle.machine_id !== 'string') {
      setMessage(t('wizardBadJson'))
      return
    }
    if (!bundle.format) bundle.format = 'qoder-native-v1'
    try {
      setPhase('busy')
      await importAccount({ ...bundle, name: name.trim() || bundle.name, enabled: true, provider: accountType })
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
              <div className="grid gap-3 rounded-lg border border-[var(--app-line)] bg-[var(--app-surface-muted)]/55 p-3 sm:grid-cols-[minmax(0,1fr)_230px] sm:items-center">
                <div>
                  <div className="text-sm font-medium">{t('accountType')}</div>
                  <p className="mt-1 text-xs leading-5 text-[var(--app-faint)]">{t('accountTypeHint')}</p>
                </div>
                <Select selectedKey={accountType} onSelectionChange={(key) => setAccountType(String(key) as AccountType)} isDisabled={busy} aria-label={t('accountType')}>
                  <Select.Trigger>
                    <Select.Value>
                      {({ defaultChildren, isPlaceholder }) => (
                        <span className="inline-flex min-w-0 items-center gap-2">
                          {!isPlaceholder ? <QoderMark size={16} /> : null}
                          <span className="truncate">{defaultChildren}</span>
                        </span>
                      )}
                    </Select.Value>
                    <Select.Indicator />
                  </Select.Trigger>
                  <Select.Popover>
                    <ListBox>
                      {accountTypes.map((item) => (
                        <ListBox.Item key={item.id} id={item.id} textValue={t(item.labelKey)}>
                          <div className="flex min-w-0 items-start gap-2.5">
                            <QoderMark size={18} className="mt-0.5" />
                            <div className="min-w-0">
                              <Label>{t(item.labelKey)}</Label>
                              <div className="mt-0.5 text-xs text-[var(--app-faint)]">{t(item.hintKey)}</div>
                            </div>
                          </div>
                        </ListBox.Item>
                      ))}
                    </ListBox>
                  </Select.Popover>
                </Select>
              </div>
              <Tabs.Root selectedKey={tab} onSelectionChange={(key) => { if (!busy) setTab(String(key) as TabKey) }} disabledKeys={busy ? ['browser', 'pat', 'import'] : []}>
                <Tabs.List className="grid grid-cols-3 gap-1 rounded-lg bg-[var(--app-surface-muted)] p-1">
                  <Tab id="browser" className="flex items-center justify-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium data-[selected=true]:bg-[var(--app-surface)] data-[selected=true]:shadow-sm data-[selected=true]:text-[var(--app-ink)] data-[hovered=true]:text-[var(--app-fg)] text-[var(--app-faint)]">
                    <ShieldCheck size={13} />{t('tabBrowser')}
                  </Tab>
                  <Tab id="pat" className="flex items-center justify-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium data-[selected=true]:bg-[var(--app-surface)] data-[selected=true]:shadow-sm data-[selected=true]:text-[var(--app-ink)] data-[hovered=true]:text-[var(--app-fg)] text-[var(--app-faint)]">
                    <Key size={13} />{t('tabPat')}
                  </Tab>
                  <Tab id="import" className="flex items-center justify-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium data-[selected=true]:bg-[var(--app-surface)] data-[selected=true]:shadow-sm data-[selected=true]:text-[var(--app-ink)] data-[hovered=true]:text-[var(--app-fg)] text-[var(--app-faint)]">
                    <FileCode size={13} />{t('tabImport')}
                  </Tab>
                </Tabs.List>

                <Tabs.Panel id="browser" className="space-y-4 pb-2 pt-5">
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
                  <Button className="w-full" isPending={tabPending('browser')} onPress={() => void runBrowser()}>
                    {phase === 'done' ? <><CheckCircle size={15} />{t('wizardAccountReady')}</> : <><ShieldCheck size={15} />{t('wizardStartBrowser')}</>}
                  </Button>
                </Tabs.Panel>

                <Tabs.Panel id="pat" className="space-y-4 pb-2 pt-5">
                  <p className="text-xs leading-5 text-[var(--app-faint)]">{t('wizardPatLead')}</p>
                  <Input value={name} onChange={(event) => setName(event.target.value)} placeholder={t('wizardNamePh')} aria-label={t('wizardNamePh')} disabled={busy} />
                  <Input type="password" value={pat} onChange={(event) => setPat(event.target.value)} placeholder={t('wizardPatPh')} aria-label={t('wizardPatPh')} disabled={busy} />
                  <Button className="w-full" isPending={tabPending('pat')} onPress={() => void runPat()}>
                    {phase === 'done' ? <><CheckCircle size={15} />{t('patDone')}</> : <><Key size={15} />{t('wizardCreateAndLogin')}</>}
                  </Button>
                </Tabs.Panel>

                <Tabs.Panel id="import" className="space-y-4 pb-2 pt-5">
                  <p className="text-xs leading-5 text-[var(--app-faint)]">{t('wizardImportLead')}</p>
                  <Input value={name} onChange={(event) => setName(event.target.value)} placeholder={t('wizardNamePh')} aria-label={t('wizardNamePh')} disabled={busy} />
                  <div className="flex items-center justify-between gap-3">
                    <span className="text-[10px] font-semibold tracking-[0.1em] text-[var(--app-faint)] uppercase">JSON</span>
                    <Button size="sm" variant="secondary" onPress={onPickFile} isDisabled={busy}><FileCode size={13} />{t('wizardChooseFile')}</Button>
                    <input ref={fileInput} type="file" accept="application/json,.json" className="hidden" onChange={onFileChange} />
                  </div>
                  <TextArea className="min-h-32 font-mono text-xs" value={json} onChange={(event) => setJson(event.target.value)} placeholder={t('wizardImportPh')} aria-label={t('tabImport')} disabled={busy} />
                  <Button className="w-full" isPending={tabPending('import')} onPress={() => void runImport()}>
                    {phase === 'done' ? <><CheckCircle size={15} />{t('accountImported')}</> : <><FileCode size={15} />{t('importCredential')}</>}
                  </Button>
                </Tabs.Panel>
              </Tabs.Root>

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
    </Modal.Root>
  )
}
