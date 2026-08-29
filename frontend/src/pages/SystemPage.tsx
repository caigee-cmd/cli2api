import { useCallback, useEffect, useMemo, useState } from 'react'
import { Button, Card, Chip, Modal, Skeleton } from '@heroui/react'
import {
  ArrowClockwise,
  ArrowCircleUp,
  CheckCircle,
  Copy,
  Database,
  Key,
  ShieldCheck,
  WarningCircle,
  X,
} from '@phosphor-icons/react'
import { fetchConsoleKey, rotateConsoleKey, type ConsoleKeyView } from '@/api/keys'
import { fetchSystemUpdate, startSystemUpdate, type StartUpdateResult, type SystemUpdateInfo } from '@/api/system'
import { useApiKey } from '@/hooks/useApiKey'
import { ReleaseNotes } from '@/components/ReleaseNotes'
import { useI18n } from '@/hooks/useI18n'
import { extractReleaseNotes } from '@/lib/releaseNotes'

const activeStates = new Set(['queued', 'preparing', 'pulling', 'recreating', 'checking', 'rolling_back'])

function statusColor(state?: string): 'success' | 'warning' | 'danger' | 'default' {
  if (state === 'succeeded') return 'success'
  if (state === 'failed' || state === 'rolled_back' || state === 'unavailable') return 'danger'
  if (state && activeStates.has(state)) return 'warning'
  return 'default'
}

export function SystemPage() {
  const { lang, t } = useI18n()
  const { setApiKey } = useApiKey()
  const [info, setInfo] = useState<SystemUpdateInfo | null>(null)
  const [consoleKey, setConsoleKey] = useState<ConsoleKeyView | null>(null)
  const [consoleBusy, setConsoleBusy] = useState(false)
  const [rotateOpen, setRotateOpen] = useState(false)
  const [rotatedSecret, setRotatedSecret] = useState('')
  const [copied, setCopied] = useState(false)
  const [loading, setLoading] = useState(true)
  const [checking, setChecking] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [error, setError] = useState('')
  const [started, setStarted] = useState<StartUpdateResult | null>(null)
  const [reloadIn, setReloadIn] = useState<number | null>(null)

  const load = useCallback(async (force = false, quiet = false) => {
    if (force && !quiet) setChecking(true)
    try {
      const result = await fetchSystemUpdate(force)
      setInfo(result)
      setError('')
      if (result.agent.state === 'succeeded' || result.agent.state === 'rolled_back' || result.agent.state === 'failed') {
        setSubmitting(false)
      }
    } catch (err) {
      if (!quiet) setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
      if (force && !quiet) setChecking(false)
    }
  }, [])

  useEffect(() => {
    const timer = window.setTimeout(() => {
      void load(false)
      void fetchConsoleKey().then(setConsoleKey).catch(() => undefined)
    }, 0)
    return () => window.clearTimeout(timer)
  }, [load])

  const active = Boolean(info?.agent?.state && activeStates.has(info.agent.state)) || submitting || reloadIn != null
  useEffect(() => {
    if (!active || reloadIn != null) return
    const timer = window.setInterval(() => void load(false, true), 2000)
    return () => window.clearInterval(timer)
  }, [active, load, reloadIn])

  useEffect(() => {
    if (reloadIn == null) return
    if (reloadIn <= 0) {
      window.location.reload()
      return
    }
    const timer = window.setTimeout(() => setReloadIn((current) => (current == null ? current : current - 1)), 1000)
    return () => window.clearTimeout(timer)
  }, [reloadIn])

  const canUpdate = Boolean(info?.managed && info?.has_update && info?.next_version && info?.agent?.available && !active)
  const agentState = info?.agent?.state || 'unavailable'
  const stateLabel = t(`updateState_${agentState}`)
  const releaseBody = useMemo(() => extractReleaseNotes(info?.release?.body, lang), [info, lang])

  async function applyUpdate() {
    setSubmitting(true)
    setError('')
    try {
      const result = await startSystemUpdate()
      setStarted(result)
      setConfirmOpen(false)
      setReloadIn(10)
      await load(false, true)
    } catch (err) {
      setSubmitting(false)
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  if (loading && !info) {
    return <div className="space-y-5"><Skeleton className="h-24 rounded-lg" /><Skeleton className="h-72 rounded-lg" /></div>
  }

  return (
    <div className="space-y-6">
      <section className="flex flex-wrap items-end justify-between gap-4 border-b border-[var(--app-line)] pb-4">
        <div>
          <h2 data-gsap-reveal className="text-2xl font-semibold tracking-[-0.035em]">{t('systemUpdateTitle')}</h2>
          <p className="mt-1 max-w-2xl text-sm leading-6 text-[var(--app-muted)]">{t('systemUpdateLead')}</p>
        </div>
        <Button size="sm" variant="secondary" isPending={checking} onPress={() => void load(true)}>
          <ArrowClockwise size={15} />{t('checkUpdates')}
        </Button>
      </section>

      {error ? (
        <div className="flex items-start gap-2 rounded-lg border border-[color-mix(in_srgb,var(--app-danger)_28%,var(--app-line))] bg-[color-mix(in_srgb,var(--app-danger)_7%,var(--app-surface))] px-4 py-3 text-sm text-[var(--app-danger)]">
          <WarningCircle className="mt-0.5 shrink-0" size={17} />{error}
        </div>
      ) : null}

      <div className="grid gap-5 xl:grid-cols-[minmax(0,1.18fr)_minmax(360px,.82fr)]">
        <Card data-gsap-reveal className="app-panel-flat overflow-hidden rounded-lg p-0">
          <div className="flex items-center justify-between gap-3 border-b border-[var(--app-line)] px-5 py-4">
            <div>
              <h3 className="font-semibold tracking-[-0.015em]">{t('versionUpdate')}</h3>
              <p className="mt-0.5 text-xs text-[var(--app-faint)]">{t('latestVersionHint')}</p>
            </div>
            <Chip size="sm" variant="soft" color={info?.has_update ? 'warning' : 'success'}>
              {info?.has_update ? t('updateAvailable') : t('upToDate')}
            </Chip>
          </div>

          <div className="px-5 py-5">
            <div className="grid overflow-hidden rounded-lg border border-[var(--app-line)] sm:grid-cols-[1fr_auto_1fr]">
              <div className="p-4">
                <div className="text-xs font-medium text-[var(--app-faint)]">{t('currentVersion')}</div>
                <div className="mono mt-2 text-lg font-semibold">{info?.current_version || '—'}</div>
              </div>
              <div className="hidden items-center border-x border-[var(--app-line)] px-4 text-[var(--app-faint)] sm:flex">
                <ArrowCircleUp size={18} />
              </div>
              <div className="border-t border-[var(--app-line)] p-4 sm:border-t-0">
                <div className="text-xs font-medium text-[var(--app-faint)]">{t('latestVersion')}</div>
                <div className="mono mt-2 text-lg font-semibold">{info?.next_version || '—'}</div>
              </div>
            </div>

            {info?.skipped_versions?.length ? (
              <div className="mt-4 rounded-lg border border-[var(--app-line)] bg-[var(--app-surface-muted)] px-4 py-3">
                <div className="text-xs font-medium text-[var(--app-muted)]">{t('skippedBetweenLabel')}</div>
                <div className="mt-2 flex flex-wrap items-center gap-1.5">
                  <span className="mono text-xs text-[var(--app-faint)]">{info.current_version}</span>
                  {info.skipped_versions.map((version) => (
                    <span key={version} className="mono flex items-center gap-1.5 text-xs text-[var(--app-faint)]">
                      <span aria-hidden>→</span>
                      <span className="rounded border border-[var(--app-line)] bg-[var(--app-surface)] px-1.5 py-0.5">{version}</span>
                    </span>
                  ))}
                  <span className="mono flex items-center gap-1.5 text-xs font-semibold text-[var(--app-ink)]">
                    <span aria-hidden>→</span>
                    <span className="rounded border border-[var(--app-line)] bg-[var(--app-surface)] px-1.5 py-0.5">{info.next_version}</span>
                  </span>
                </div>
              </div>
            ) : null}

            <div className="release-notes-panel mt-5">
              <div className="text-xs font-medium text-[var(--app-muted)]">{t('releaseNotes')}</div>
              <div className="release-notes-box">
                <ReleaseNotes markdown={releaseBody} emptyLabel={t('updateNoNotes')} />
              </div>
            </div>

            <div className="mt-5 flex flex-wrap items-center justify-between gap-3">
              <div className="flex items-center gap-2 text-xs text-[var(--app-faint)]">
                <span className="status-dot" data-state={info?.agent?.available ? 'ok' : 'danger'} />
                {info?.agent?.available ? t('updaterReady') : t('updaterUnavailable')}
              </div>
              <Button isDisabled={!canUpdate && reloadIn == null} isPending={submitting || active} onPress={() => setConfirmOpen(true)}>
                <ArrowCircleUp size={16} />
                {reloadIn != null ? t('updateReloadingIn', { seconds: reloadIn }) : active ? stateLabel : t('updateNow')}
              </Button>
            </div>
          </div>
        </Card>

        <div className="space-y-5">
          <Card data-gsap-reveal className="app-panel-flat rounded-lg p-5">
            <div className="flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <div className="grid size-8 shrink-0 place-items-center rounded-lg bg-[var(--app-ink)] text-[var(--app-bg)]"><Database size={15} /></div>
                <div>
                  <h3 className="font-semibold">{t('sqliteProtection')}</h3>
                  <p className="mt-1 text-xs leading-5 text-[var(--app-faint)]">{t('sqliteProtectionHint')}</p>
                </div>
              </div>
              <ShieldCheck size={18} className="text-[var(--app-ok)]" />
            </div>
            <div className="mono mt-4 rounded-lg bg-[var(--app-surface-muted)] px-3 py-2 text-xs text-[var(--app-muted)]">/data</div>
            <div className="mt-3 grid gap-2 text-xs text-[var(--app-faint)]">
              <div className="flex items-center gap-2"><CheckCircle size={14} className="text-[var(--app-ok)]" />{t('sqliteBackupBeforeUpdate')}</div>
              <div className="flex items-center gap-2"><CheckCircle size={14} className="text-[var(--app-ok)]" />{t('sqliteKeepFive')}</div>
              <div className="flex items-center gap-2"><CheckCircle size={14} className="text-[var(--app-ok)]" />{t('sqliteRollbackTogether')}</div>
            </div>
            {started?.backup?.name ? <div className="mono mt-4 break-all border-t border-[var(--app-line)] pt-3 text-[10px] text-[var(--app-faint)]">{started.backup.name}</div> : null}
          </Card>

          <Card data-gsap-reveal className="app-panel-flat rounded-lg p-5">
            <div className="flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <div className="grid size-8 shrink-0 place-items-center rounded-lg bg-[var(--app-ink)] text-[var(--app-bg)]"><Key size={15} /></div>
                <div>
                  <h3 className="font-semibold">{t('consoleKeyTitle')}</h3>
                  <p className="mt-1 text-xs leading-5 text-[var(--app-faint)]">{t('consoleKeyHint')}</p>
                </div>
              </div>
            </div>
            <code className="mono mt-4 block rounded-lg bg-[var(--app-surface-muted)] px-3 py-2 text-xs text-[var(--app-muted)]">{consoleKey?.prefix || '—'}</code>
            <p className="mt-3 text-xs leading-5 text-[var(--app-faint)]">{t('consoleKeyLead')}</p>
            <div className="mt-4 flex justify-end">
              <Button size="sm" variant="ghost" isPending={consoleBusy} onPress={() => setRotateOpen(true)}>{t('consoleKeyRotate')}</Button>
            </div>
          </Card>

          <Card data-gsap-reveal className="app-panel-flat rounded-lg p-5">
            <div className="flex items-center justify-between gap-3">
              <div>
                <h3 className="font-semibold">{t('updateStatus')}</h3>
                <p className="mt-1 text-xs text-[var(--app-faint)]">{info?.agent?.job_id || (info?.agent?.available ? t('noUpdateJob') : t('updaterNeedsHost'))}</p>
              </div>
              <Chip size="sm" variant="soft" color={statusColor(agentState)}>{stateLabel}</Chip>
            </div>
            {info?.agent?.available ? null : (
              <div className="mt-4 space-y-2 text-xs leading-5 text-[var(--app-muted)]">
                <p>{t('updaterUnavailableHint')}</p>
                <ul className="list-disc space-y-1 pl-4 text-[var(--app-faint)]">
                  <li>{t('updaterNeedInstall')}</li>
                  <li>{t('updaterNeedCompose')}</li>
                  <li>{t('updaterNeedRelease')}</li>
                </ul>
              </div>
            )}
          </Card>
        </div>
      </div>

      <Modal.Root isOpen={rotateOpen} onOpenChange={(open: boolean) => { if (!open && !consoleBusy) setRotateOpen(false) }}>
        <Modal.Backdrop variant="blur" isDismissable={!consoleBusy}>
          <Modal.Container placement="center" size="sm">
            <Modal.Dialog>
              <Modal.Header className="items-start justify-between gap-4">
                <div>
                  <Modal.Heading className="text-base font-semibold">{t('consoleKeyRotate')}</Modal.Heading>
                  <p className="mt-1 text-xs leading-5 text-[var(--app-faint)]">{t('consoleKeyRotateHint')}</p>
                </div>
                <Modal.CloseTrigger isDisabled={consoleBusy} aria-label={t('close')} className="grid size-8 place-items-center rounded-lg text-[var(--app-muted)] hover:bg-[var(--app-surface-muted)]"><X size={16} /></Modal.CloseTrigger>
              </Modal.Header>
              <Modal.Footer className="justify-end">
                <Button variant="ghost" isDisabled={consoleBusy} onPress={() => setRotateOpen(false)}>{t('cancel')}</Button>
                <Button variant="danger" isPending={consoleBusy} onPress={() => {
                  setConsoleBusy(true)
                  void rotateConsoleKey().then((result) => {
                    setConsoleKey(result)
                    setRotatedSecret(result.secret || '')
                    if (result.secret) setApiKey(result.secret)
                    setRotateOpen(false)
                  }).catch((err) => {
                    setError(err instanceof Error ? err.message : String(err))
                  }).finally(() => setConsoleBusy(false))
                }}>{t('consoleKeyRotateNow')}</Button>
              </Modal.Footer>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal.Root>

      <Modal.Root isOpen={Boolean(rotatedSecret)} onOpenChange={(open: boolean) => { if (!open) setRotatedSecret('') }}>
        <Modal.Backdrop variant="blur">
          <Modal.Container placement="center" size="lg">
            <Modal.Dialog>
              <Modal.Header className="items-start justify-between gap-4 px-6 pt-6">
                <div>
                  <Modal.Heading className="text-lg font-semibold">{t('consoleKeySecretTitle')}</Modal.Heading>
                  <p className="mt-1.5 text-sm leading-6 text-[var(--app-muted)]">{t('consoleKeySecretHint')}</p>
                </div>
                <Modal.CloseTrigger aria-label={t('close')} className="grid size-9 place-items-center rounded-lg text-[var(--app-muted)] hover:bg-[var(--app-surface-muted)]"><X size={18} /></Modal.CloseTrigger>
              </Modal.Header>
              <Modal.Body className="px-6 pb-2">
                <code className="mono block break-all rounded-lg border border-[var(--app-line)] bg-[var(--app-surface-muted)] px-3 py-3 text-sm">{rotatedSecret}</code>
              </Modal.Body>
              <Modal.Footer className="justify-end">
                <Button variant="ghost" onPress={() => setRotatedSecret('')}>{t('close')}</Button>
                <Button onPress={() => {
                  void navigator.clipboard.writeText(rotatedSecret)
                  setCopied(true)
                  window.setTimeout(() => setCopied(false), 1200)
                }}>
                  <Copy size={14} />{copied ? t('copied') : t('copy')}
                </Button>
              </Modal.Footer>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal.Root>

      <Modal.Root isOpen={confirmOpen} onOpenChange={(open: boolean) => { if (!open && !submitting && reloadIn == null) setConfirmOpen(false) }}>
        <Modal.Backdrop variant="blur" isDismissable={!submitting && reloadIn == null}>
          <Modal.Container placement="center" size="sm">
            <Modal.Dialog aria-label={t('confirmUpdate')}>
              <Modal.Header className="items-start justify-between gap-4">
                <div><Modal.Heading className="text-base font-semibold">{t('confirmUpdate')}</Modal.Heading><p className="mt-1 text-xs text-[var(--app-faint)]">{t('confirmUpdateHint')}</p></div>
                <Modal.CloseTrigger aria-label={t('close')} className="grid size-8 place-items-center rounded-lg text-[var(--app-muted)] hover:bg-[var(--app-surface-muted)]"><X size={16} /></Modal.CloseTrigger>
              </Modal.Header>
              <Modal.Body className="pt-0">
                <div className="mono rounded-lg border border-[var(--app-line)] bg-[var(--app-surface-muted)] px-3 py-3 text-sm">{info?.current_version} → {info?.next_version}</div>
              </Modal.Body>
              <Modal.Footer className="justify-end">
                <Button variant="ghost" isDisabled={submitting || reloadIn != null} onPress={() => setConfirmOpen(false)}>{t('cancel')}</Button>
                <Button isPending={submitting} onPress={() => void applyUpdate()}>
                  <ArrowCircleUp size={15} />
                  {reloadIn != null ? t('updateReloadingIn', { seconds: reloadIn }) : t('updateNow')}
                </Button>
              </Modal.Footer>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal.Root>
    </div>
  )
}
