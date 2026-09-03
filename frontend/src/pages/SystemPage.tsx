import { useCallback, useEffect, useMemo, useState } from 'react'
import { Button, Card, Chip, Modal } from '@heroui/react'
import {
  ArrowClockwise,
  ArrowCircleUp,
  CheckCircle,
  Copy,
  Database,
  Key,
  ShieldCheck,
  SlidersHorizontal,
  X,
} from '@phosphor-icons/react'
import { fetchConsoleKey, rotateConsoleKey, type ConsoleKeyView } from '@/api/keys'
import { fetchSystemSettings, fetchSystemUpdate, startSystemUpdate, updateSystemSettings, type StartUpdateResult, type SystemSettings, type SystemUpdateInfo } from '@/api/system'
import { useApiKey } from '@/hooks/useApiKey'
import { ConfirmDialog } from '@/components/ui/ConfirmDialog'
import { PageAlert } from '@/components/ui/PageAlert'
import { SystemBodySkeleton, SystemPageSkeleton } from '@/components/ui/PageSkeletons'
import { ReleaseNotes } from '@/components/ReleaseNotes'
import { useI18n } from '@/hooks/useI18n'
import { extractReleaseNotes } from '@/lib/releaseNotes'
import { CompactSwitch } from '@/components/ui/CompactSwitch'

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
  const [settings, setSettings] = useState<SystemSettings | null>(null)
  const [settingsBusy, setSettingsBusy] = useState(false)
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
  const [updateStartedAt, setUpdateStartedAt] = useState<number | null>(null)
  const [updateElapsed, setUpdateElapsed] = useState(0)
  const [reloadIn, setReloadIn] = useState<number | null>(null)

  const load = useCallback(async (force = false, quiet = false) => {
    if (force && !quiet) setChecking(true)
    try {
      const result = await fetchSystemUpdate(force)
      setInfo(result)
      setError('')
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
      void fetchSystemSettings().then(setSettings).catch((err) => setError(err instanceof Error ? err.message : String(err)))
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
    if (!submitting || !started || !info || info.agent.job_id !== started.job_id) return
    const state = info.agent.state
    if (state === 'succeeded') {
      setSubmitting(false)
      setUpdateStartedAt(null)
      setReloadIn((current) => current ?? 3)
    } else if (state === 'failed' || state === 'rolled_back') {
      setSubmitting(false)
      setUpdateStartedAt(null)
    }
  }, [info, started, submitting])

  useEffect(() => {
    if (updateStartedAt == null || reloadIn != null) return
    const update = () => setUpdateElapsed(Math.floor((Date.now() - updateStartedAt) / 1000))
    update()
    const timer = window.setInterval(update, 1000)
    return () => window.clearInterval(timer)
  }, [updateStartedAt, reloadIn])

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
  const statusMatchesStarted = Boolean(started && info?.agent?.job_id === started.job_id)
  const visibleState = reloadIn != null
    ? 'succeeded'
    : submitting && (!statusMatchesStarted || !activeStates.has(agentState))
      ? 'preparing'
      : agentState
  const stateLabel = t(`updateState_${visibleState}`)
  const releaseBody = useMemo(() => extractReleaseNotes(info?.release?.body, lang), [info, lang])

  async function applyUpdate() {
    setSubmitting(true)
    setStarted(null)
    setUpdateStartedAt(Date.now())
    setUpdateElapsed(0)
    setConfirmOpen(false)
    setError('')
    try {
      const result = await startSystemUpdate()
      setStarted(result)
      await load(false, true)
    } catch (err) {
      setSubmitting(false)
      setUpdateStartedAt(null)
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  async function updateCrossProviderModelPool(enabled: boolean) {
    const previous = settings?.cross_provider_model_pool ?? true
    setSettings((current) => current ? { ...current, cross_provider_model_pool: enabled } : current)
    setSettingsBusy(true)
    setError('')
    try {
      setSettings(await updateSystemSettings(enabled))
    } catch (err) {
      setSettings((current) => current ? { ...current, cross_provider_model_pool: previous } : current)
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSettingsBusy(false)
    }
  }

  if (loading && !info) return <SystemPageSkeleton />

  return (
    <div className="space-y-6">
      <section className="flex flex-wrap items-end justify-between gap-4 border-b border-separator pb-4">
        <div>
          <h2 data-gsap-reveal className="text-2xl font-semibold tracking-[-0.035em]">{t('systemSettingsTitle')}</h2>
          <p className="mt-1 max-w-2xl text-sm leading-6 text-muted">{t('systemSettingsLead')}</p>
        </div>
        <Button size="sm" variant="secondary" isPending={checking} onPress={() => void load(true)}>
          <ArrowClockwise size={15} />{t('checkUpdates')}
        </Button>
      </section>

      {error ? <PageAlert title={error} /> : null}

      {checking ? <SystemBodySkeleton /> : (
      <div className="grid gap-5 xl:grid-cols-[minmax(0,1.18fr)_minmax(360px,.82fr)]">
        <Card data-gsap-reveal className="overflow-hidden p-0">
          <div className="flex items-center justify-between gap-3 border-b border-separator px-5 py-4">
            <div>
              <h3 className="font-semibold tracking-[-0.015em]">{t('versionUpdate')}</h3>
              <p className="mt-0.5 text-xs text-muted">{t('latestVersionHint')}</p>
            </div>
            <Chip size="sm" variant="soft" color={info?.has_update ? 'warning' : 'success'}>
              {info?.has_update ? t('updateAvailable') : t('upToDate')}
            </Chip>
          </div>

          <div className="px-5 py-5">
            <div className="grid overflow-hidden rounded-lg border border-separator sm:grid-cols-[1fr_auto_1fr]">
              <div className="p-4">
                <div className="text-xs font-medium text-muted">{t('currentVersion')}</div>
                <div className="mono mt-2 text-lg font-semibold">{info?.current_version || '—'}</div>
              </div>
              <div className="hidden items-center border-x border-separator px-4 text-muted sm:flex">
                <ArrowCircleUp size={18} />
              </div>
              <div className="border-t border-separator p-4 sm:border-t-0">
                <div className="text-xs font-medium text-muted">{t('latestVersion')}</div>
                <div className="mono mt-2 text-lg font-semibold">{info?.next_version || '—'}</div>
              </div>
            </div>

            {info?.skipped_versions?.length ? (
              <div className="mt-4 rounded-lg border border-separator bg-surface-secondary px-4 py-3">
                <div className="text-xs font-medium text-muted">{t('skippedBetweenLabel')}</div>
                <div className="mt-2 flex flex-wrap items-center gap-1.5">
                  <span className="mono text-xs text-muted">{info.current_version}</span>
                  {info.skipped_versions.map((version) => (
                    <span key={version} className="mono flex items-center gap-1.5 text-xs text-muted">
                      <span aria-hidden>→</span>
                      <span className="rounded border border-separator bg-surface px-1.5 py-0.5">{version}</span>
                    </span>
                  ))}
                  <span className="mono flex items-center gap-1.5 text-xs font-semibold text-foreground">
                    <span aria-hidden>→</span>
                    <span className="rounded border border-separator bg-surface px-1.5 py-0.5">{info.next_version}</span>
                  </span>
                </div>
              </div>
            ) : null}

            <div className="release-notes-panel mt-5">
              <div className="text-xs font-medium text-muted">{t('releaseNotes')}</div>
              <div className="release-notes-box">
                <ReleaseNotes markdown={releaseBody} emptyLabel={t('updateNoNotes')} />
              </div>
            </div>

            <div className="mt-5 flex flex-wrap items-center justify-between gap-3">
              <div className="flex items-center gap-2 text-xs text-muted">
                <span className="status-dot" data-state={info?.agent?.available ? 'ok' : 'danger'} />
                {info?.agent?.available ? t('updaterReady') : t('updaterUnavailable')}
              </div>
              <Button isDisabled={!canUpdate && reloadIn == null} isPending={submitting || active} onPress={() => setConfirmOpen(true)}>
                <ArrowCircleUp size={16} />
                {reloadIn != null ? t('updateReloadingIn', { seconds: reloadIn }) : active ? stateLabel : t('updateNow')}
              </Button>
            </div>
            {active ? (
              <div className="mt-4 rounded-lg border border-warning/25 bg-warning/5 px-3 py-3" role="status" aria-live="polite">
                <div className="flex items-center justify-between gap-3 text-xs">
                  <div className="flex min-w-0 items-center gap-2 font-medium text-warning-foreground dark:text-warning">
                    <span className="status-dot shrink-0" data-state="warn" />
                    <span className="truncate">{stateLabel}</span>
                  </div>
                  {updateStartedAt != null ? <span className="mono shrink-0 text-[10px] text-muted">{t('updateElapsed', { seconds: updateElapsed })}</span> : null}
                </div>
                <p className="mt-1.5 text-xs leading-5 text-muted">
                  {reloadIn != null ? t('updateReloadingHint') : started ? t('updateRunningHint') : t('updatePreparingHint')}
                </p>
              </div>
            ) : null}
          </div>
        </Card>

        <div className="space-y-5">
          <Card data-gsap-reveal>
            <div className="flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <div className="grid size-8 shrink-0 place-items-center rounded-lg bg-surface-secondary text-foreground"><SlidersHorizontal size={15} /></div>
                <div>
                  <h3 className="font-semibold">{t('crossProviderModelPoolTitle')}</h3>
                  <p className="mt-1 text-xs leading-5 text-muted">{t('crossProviderModelPoolHint')}</p>
                </div>
              </div>
              <CompactSwitch
                isSelected={settings?.cross_provider_model_pool ?? true}
                isDisabled={settingsBusy || !settings}
                ariaLabel={t('crossProviderModelPoolAriaLabel')}
                onChange={(selected) => void updateCrossProviderModelPool(selected)}
              />
            </div>
            <div className="mt-4 flex items-center justify-between border-t border-separator pt-3 text-xs text-muted">
              <span>{t('crossProviderModelPoolStatus')}</span>
              <span className="font-medium text-foreground">{settings?.cross_provider_model_pool ? t('enabled') : t('disabled')}</span>
            </div>
          </Card>

          <Card data-gsap-reveal>
            <div className="flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <div className="grid size-8 shrink-0 place-items-center rounded-lg bg-surface-secondary text-foreground"><Database size={15} /></div>
                <div>
                  <h3 className="font-semibold">{t('sqliteProtection')}</h3>
                  <p className="mt-1 text-xs leading-5 text-muted">{t('sqliteProtectionHint')}</p>
                </div>
              </div>
              <ShieldCheck size={18} className="text-success" />
            </div>
            <div className="mono mt-4 rounded-lg bg-surface-secondary px-3 py-2 text-xs text-muted">/data</div>
            <div className="mt-3 grid gap-2 text-xs text-muted">
              <div className="flex items-center gap-2"><CheckCircle size={14} className="text-success" />{t('sqliteBackupBeforeUpdate')}</div>
              <div className="flex items-center gap-2"><CheckCircle size={14} className="text-success" />{t('sqliteKeepFive')}</div>
              <div className="flex items-center gap-2"><CheckCircle size={14} className="text-success" />{t('sqliteRollbackTogether')}</div>
            </div>
            {started?.backup?.name ? <div className="mono mt-4 break-all border-t border-separator pt-3 text-[10px] text-muted">{started.backup.name}</div> : null}
          </Card>

          <Card data-gsap-reveal>
            <div className="flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <div className="grid size-8 shrink-0 place-items-center rounded-lg bg-surface-secondary text-foreground"><Key size={15} /></div>
                <div>
                  <h3 className="font-semibold">{t('consoleKeyTitle')}</h3>
                  <p className="mt-1 text-xs leading-5 text-muted">{t('consoleKeyHint')}</p>
                </div>
              </div>
            </div>
            <code className="mono mt-4 block rounded-lg bg-surface-secondary px-3 py-2 text-xs text-muted">{consoleKey?.prefix || '—'}</code>
            <p className="mt-3 text-xs leading-5 text-muted">{t('consoleKeyLead')}</p>
            <div className="mt-4 flex justify-end">
              <Button size="sm" variant="ghost" isPending={consoleBusy} onPress={() => setRotateOpen(true)}>{t('consoleKeyRotate')}</Button>
            </div>
          </Card>

          <Card data-gsap-reveal>
            <div className="flex items-center justify-between gap-3">
              <div>
                <h3 className="font-semibold">{t('updateStatus')}</h3>
                <p className="mt-1 text-xs text-muted">{info?.agent?.job_id || (info?.agent?.available ? t('noUpdateJob') : t('updaterNeedsHost'))}</p>
              </div>
              <Chip size="sm" variant="soft" color={statusColor(visibleState)}>{stateLabel}</Chip>
            </div>
            {info?.agent?.available ? null : (
              <div className="mt-4 space-y-2 text-xs leading-5 text-muted">
                <p>{t('updaterUnavailableHint')}</p>
                <ul className="list-disc space-y-1 pl-4 text-muted">
                  <li>{t('updaterNeedInstall')}</li>
                  <li>{t('updaterNeedCompose')}</li>
                  <li>{t('updaterNeedRelease')}</li>
                </ul>
              </div>
            )}
          </Card>
        </div>
      </div>
      )}

      <Modal.Root isOpen={rotateOpen} onOpenChange={(open: boolean) => { if (!open && !consoleBusy) setRotateOpen(false) }}>
        <Modal.Backdrop variant="blur" isDismissable={!consoleBusy}>
          <Modal.Container placement="center" size="sm">
            <Modal.Dialog>
              <Modal.Header className="items-start justify-between gap-4">
                <div>
                  <Modal.Heading className="text-base font-semibold">{t('consoleKeyRotate')}</Modal.Heading>
                  <p className="mt-1 text-xs leading-5 text-muted">{t('consoleKeyRotateHint')}</p>
                </div>
                <Modal.CloseTrigger isDisabled={consoleBusy} aria-label={t('close')} className="grid size-8 place-items-center rounded-lg text-muted hover:bg-surface-secondary"><X size={16} /></Modal.CloseTrigger>
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
                  <p className="mt-1.5 text-sm leading-6 text-muted">{t('consoleKeySecretHint')}</p>
                </div>
                <Modal.CloseTrigger aria-label={t('close')} className="grid size-9 place-items-center rounded-lg text-muted hover:bg-surface-secondary"><X size={18} /></Modal.CloseTrigger>
              </Modal.Header>
              <Modal.Body className="px-6 pb-2">
                <code className="mono block break-all rounded-lg border border-separator bg-surface-secondary px-3 py-3 text-sm">{rotatedSecret}</code>
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

      <ConfirmDialog
        isOpen={confirmOpen}
        title={t('confirmUpdate')}
        description={`${t('confirmUpdateHint')} ${info?.current_version || ''} → ${info?.next_version || ''}`}
        confirmLabel={reloadIn != null ? t('updateReloadingIn', { seconds: reloadIn }) : submitting ? t('updateState_preparing') : t('updateNow')}
        cancelLabel={t('cancel')}
        closeLabel={t('close')}
        status="warning"
        confirmVariant="primary"
        isPending={submitting}
        onClose={() => { if (!submitting && reloadIn == null) setConfirmOpen(false) }}
        onConfirm={() => void applyUpdate()}
      />
    </div>
  )
}
