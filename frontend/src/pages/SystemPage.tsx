import { copyText } from '@/lib/clipboard'
import { useEffect, useMemo, useState } from 'react'
import { Button, Card, Chip, Description, Input, Label, ListBox, Modal, Select, Tab, Tabs } from '@heroui/react'
import { useSearchParams } from 'react-router-dom'
import {
  ArrowClockwise,
  ArrowCircleUp,
  ArrowsSplit,
  CheckCircle,
  Copy,
  Database,
  GearSix,
  Key,
  ShieldCheck,
  SlidersHorizontal,
  X,
} from '@phosphor-icons/react'
import { fetchConsoleKey, rotateConsoleKey, type ConsoleKeyView } from '@/api/keys'
import { fetchSystemSettings, updateSystemSettings, type SystemSettings } from '@/api/system'
import { useApiKey } from '@/hooks/useApiKey'
import { PageAlert } from '@/components/ui/PageAlert'
import { ConfirmDialog } from '@/components/ui/ConfirmDialog'
import { FormRow } from '@/components/ui/FormRow'
import { SystemPageSkeleton } from '@/components/ui/PageSkeletons'
import { useI18n } from '@/hooks/useI18n'
import { CompactSwitch } from '@/components/ui/CompactSwitch'
import { CheckinDefaults } from '@/components/checkin/CheckinDefaults'
import { UpdateControls } from '@/components/system/UpdateControls'
import { useSystemUpdate } from '@/hooks/useSystemUpdate'
import { VersionHistory } from '@/components/VersionHistory'
import { buildVersionHistory } from '@/lib/versionHistory'


type SettingsTab = 'general' | 'routing' | 'security' | 'updates'
const SETTINGS_TABS: readonly SettingsTab[] = ['general', 'routing', 'security', 'updates']

export function SystemPage() {
  const { t } = useI18n()
  const [searchParams, setSearchParams] = useSearchParams()
  const tabParam = searchParams.get('tab') as SettingsTab | null
  const tab: SettingsTab = tabParam && SETTINGS_TABS.includes(tabParam) ? tabParam : 'general'
  const selectTab = (next: SettingsTab) => setSearchParams(next === 'general' ? {} : { tab: next }, { replace: true })
  const { setApiKey } = useApiKey()
  const [consoleKey, setConsoleKey] = useState<ConsoleKeyView | null>(null)
  const [settings, setSettings] = useState<SystemSettings | null>(null)
  const [proxyDraft, setProxyDraft] = useState('')
  const [settingsBusy, setSettingsBusy] = useState(false)
  const [consoleBusy, setConsoleBusy] = useState(false)
  const [rotateOpen, setRotateOpen] = useState(false)
  const [rotatedSecret, setRotatedSecret] = useState('')
  const [copied, setCopied] = useState(false)
  const [restoreTarget, setRestoreTarget] = useState('')
  const update = useSystemUpdate()
  const { info, loading, checking, submitting, error, setError, load, canRollback, rollbackTo } = update
  const historyReleases = useMemo(
    () => buildVersionHistory(info?.current_version || '', info?.next_version, info?.release, info?.recent_releases, info?.rollback_versions),
    [info?.current_version, info?.next_version, info?.release, info?.recent_releases, info?.rollback_versions],
  )
  const rollbackTags = useMemo(() => (info?.rollback_versions || []).map((release) => release.tag_name), [info?.rollback_versions])


  useEffect(() => {
    const timer = window.setTimeout(() => {
      void fetchConsoleKey().then(setConsoleKey).catch(() => undefined)
      void fetchSystemSettings().then((result) => {
        setSettings(result)
        setProxyDraft(result.proxy_url || '')
      }).catch((err) => setError(err instanceof Error ? err.message : String(err)))
    }, 0)
    return () => window.clearTimeout(timer)
  }, [setError])


  async function updateCrossProviderModelPool(enabled: boolean) {
    const previous = settings?.cross_provider_model_pool ?? true
    setSettings((current) => current ? { ...current, cross_provider_model_pool: enabled } : current)
    setSettingsBusy(true)
    setError('')
    try {
      setSettings(await updateSystemSettings({ cross_provider_model_pool: enabled }))
    } catch (err) {
      setSettings((current) => current ? { ...current, cross_provider_model_pool: previous } : current)
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSettingsBusy(false)
    }
  }

  async function updateCheckinDisabledAccounts(enabled: boolean) {
    const previous = settings?.checkin_disabled_accounts ?? false
    setSettings((current) => current ? { ...current, checkin_disabled_accounts: enabled } : current)
    setSettingsBusy(true)
    setError('')
    try {
      setSettings(await updateSystemSettings({ checkin_disabled_accounts: enabled }))
    } catch (err) {
      setSettings((current) => current ? { ...current, checkin_disabled_accounts: previous } : current)
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSettingsBusy(false)
    }
  }

  async function updateProxyURL(value: string) {
    const saved = settings?.proxy_url || ''
    // Nothing changed in the field: keep the draft as-is and skip the PATCH so
    // a no-op blur never reloads workers.
    if (value === saved) {
      setProxyDraft(saved)
      return
    }
    const previousDraft = proxyDraft
    setProxyDraft(value)
    setSettings((current) => current ? { ...current, proxy_url: value } : current)
    setSettingsBusy(true)
    setError('')
    try {
      const updated = await updateSystemSettings({ proxy_url: value })
      setSettings(updated)
      setProxyDraft(updated.proxy_url || '')
    } catch (err) {
      setProxyDraft(previousDraft)
      setSettings((current) => current ? { ...current, proxy_url: saved } : current)
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSettingsBusy(false)
    }
  }

  async function updateRoutingStrategy(strategy: SystemSettings['routing_strategy']) {
    const previous = settings?.routing_strategy || 'round-robin'
    setSettings((current) => current ? { ...current, routing_strategy: strategy } : current)
    setSettingsBusy(true)
    setError('')
    try {
      setSettings(await updateSystemSettings({ routing_strategy: strategy }))
    } catch (err) {
      setSettings((current) => current ? { ...current, routing_strategy: previous } : current)
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSettingsBusy(false)
    }
  }

  if (loading && !info) return <SystemPageSkeleton />

  return (
    <div className="space-y-6">
      <section className="flex flex-wrap items-end justify-between gap-4 pb-4">
        <div>
          <h2 data-gsap-reveal className="text-title font-semibold">{t('systemSettingsTitle')}</h2>
          <p className="mt-1 max-w-2xl text-body leading-6 text-muted">{t('systemSettingsLead')}</p>
        </div>
        <Button size="sm" variant="secondary" isPending={checking} onPress={() => void load(true)}>
          <ArrowClockwise size={15} />{t('checkUpdates')}
        </Button>
      </section>

      {error ? <PageAlert title={error} /> : null}

      <Tabs selectedKey={tab} onSelectionChange={(key) => selectTab(String(key) as SettingsTab)}>
        <Tabs.ListContainer className="w-fit max-w-full overflow-x-auto">
          <Tabs.List aria-label={t('systemSettingsTitle')}>
            <Tab id="general" className="gap-1.5 whitespace-nowrap">
              <Tabs.Indicator />
              <GearSix size={13} />{t('settingsTabGeneral')}
            </Tab>
            <Tab id="routing" className="gap-1.5 whitespace-nowrap">
              <Tabs.Indicator />
              <ArrowsSplit size={13} />{t('settingsTabRouting')}
            </Tab>
            <Tab id="security" className="gap-1.5 whitespace-nowrap">
              <Tabs.Indicator />
              <ShieldCheck size={13} />{t('settingsTabSecurity')}
            </Tab>
            <Tab id="updates" className="gap-1.5 whitespace-nowrap">
              <Tabs.Indicator />
              <ArrowCircleUp size={13} />{t('settingsTabUpdates')}
            </Tab>
          </Tabs.List>
        </Tabs.ListContainer>
        <Tabs.Panel id="general" className="pt-5">
          <div className="grid gap-5 xl:grid-cols-2 items-start">
          <Card data-gsap-reveal>
            <div className="flex items-start gap-3">
              <div className="grid size-8 shrink-0 place-items-center rounded-lg bg-surface-secondary text-foreground"><SlidersHorizontal size={15} /></div>
              <div>
                <h3 className="font-semibold">{t('proxySettingsTitle')}</h3>
                <p className="mt-1 text-caption leading-5 text-muted">{t('proxySettingsHint')}</p>
              </div>
            </div>
            <div className="mt-4 space-y-1.5">
              <Label className="text-body font-medium text-muted">{t('proxyUrl')}</Label>
              <Input
                value={proxyDraft}
                onChange={(event) => setProxyDraft(event.target.value)}
                onBlur={(event) => void updateProxyURL(event.target.value.trim())}
                placeholder={t('proxyUrlPlaceholder')}
                disabled={settingsBusy || !settings}
              />
              <Description className="text-caption leading-5 text-muted">{t('proxyUrlHint')}</Description>
            </div>
          </Card>
          <Card data-gsap-reveal>
            <div className="flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <div className="grid size-8 shrink-0 place-items-center rounded-lg bg-surface-secondary text-foreground"><CheckCircle size={15} /></div>
                <div>
                  <h3 className="font-semibold">{t('checkinDisabledAccountsTitle')}</h3>
                  <p className="mt-1 text-caption leading-5 text-muted">{t('checkinDisabledAccountsHint')}</p>
                </div>
              </div>
              <CompactSwitch
                isSelected={settings?.checkin_disabled_accounts ?? false}
                isDisabled={settingsBusy || !settings}
                ariaLabel={t('checkinDisabledAccountsAriaLabel')}
                onChange={(selected) => void updateCheckinDisabledAccounts(selected)}
              />
            </div>
            <div className="mt-4 flex items-center justify-between pt-3 text-caption text-muted">
              <span>{t('checkinDisabledAccountsStatus')}</span>
              <span className="font-medium text-foreground">{settings?.checkin_disabled_accounts ? t('enabled') : t('disabled')}</span>
            </div>
          </Card>
          <CheckinDefaults settings={settings} onSaved={setSettings} />
          </div>
        </Tabs.Panel>
        <Tabs.Panel id="routing" className="pt-5">
          <div className="grid gap-5 xl:grid-cols-2 items-start">
          <Card data-gsap-reveal>
            <div className="flex items-start gap-3">
              <div className="grid size-8 shrink-0 place-items-center rounded-lg bg-surface-secondary text-foreground"><SlidersHorizontal size={15} /></div>
              <div>
                <h3 className="font-semibold">{t('routingStrategyTitle')}</h3>
                <p className="mt-1 text-caption leading-5 text-muted">{t('routingStrategyHint')}</p>
              </div>
            </div>
            <div className="mt-4 pt-4">
              <FormRow label={t('routingStrategy')}>
                <Select
                  fullWidth
                  aria-label={t('routingStrategy')}
                  value={settings?.routing_strategy || 'round-robin'}
                  isDisabled={settingsBusy || !settings}
                  onChange={(value) => {
                    if (typeof value === 'string' && value) void updateRoutingStrategy(value as SystemSettings['routing_strategy'])
                  }}
                >
                  <Select.Trigger className="items-center">
                    <Select.Value className="min-w-0 truncate" />
                    <Select.Indicator />
                  </Select.Trigger>
                  <Select.Popover>
                    <ListBox>
                      <ListBox.Item id="round-robin" textValue="round-robin"><Label>{t('routingRoundRobin')}</Label><ListBox.ItemIndicator /></ListBox.Item>
                      <ListBox.Item id="weighted-round-robin" textValue="weighted-round-robin"><Label>{t('routingWeightedRoundRobin')}</Label><ListBox.ItemIndicator /></ListBox.Item>
                      <ListBox.Item id="fill-first" textValue="fill-first"><Label>{t('routingFillFirst')}</Label><ListBox.ItemIndicator /></ListBox.Item>
                      <ListBox.Item id="fefo" textValue="fefo"><Label>{t('routingFEFO')}</Label><ListBox.ItemIndicator /></ListBox.Item>
                    </ListBox>
                  </Select.Popover>
                </Select>
              </FormRow>
            </div>
            <div className="mt-4 grid grid-cols-2 gap-2 pt-3 text-caption text-muted sm:grid-cols-4">
              <div><span className="mono block text-body font-medium text-foreground">{settings?.session_affinity?.ttl_seconds ? `${Math.round(settings.session_affinity.ttl_seconds / 60)}m` : '—'}</span>{t('sessionAffinityTTL')}</div>
              <div><span className="mono block text-body font-medium text-foreground">{settings?.session_affinity?.hits ?? 0}</span>{t('sessionAffinityHits')}</div>
              <div><span className="mono block text-body font-medium text-foreground">{settings?.session_affinity?.misses ?? 0}</span>{t('sessionAffinityMisses')}</div>
              <div><span className="mono block text-body font-medium text-foreground">{settings?.session_affinity?.escapes ?? 0}</span>{t('sessionAffinityEscapes')}</div>
            </div>
            {settings?.session_affinity?.last_escape_reason ? <Description className="mt-3 text-caption">{t('lastSessionEscape')}: {settings.session_affinity.last_escape_reason}</Description> : null}
            {settings?.session_affinity?.last_miss_reason ? <Description className="mt-1 text-caption">{t('lastSessionMiss')}: {settings.session_affinity.last_miss_reason}</Description> : null}
          </Card>
          <Card data-gsap-reveal>
            <div className="flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <div className="grid size-8 shrink-0 place-items-center rounded-lg bg-surface-secondary text-foreground"><SlidersHorizontal size={15} /></div>
                <div>
                  <h3 className="font-semibold">{t('crossProviderModelPoolTitle')}</h3>
                  <p className="mt-1 text-caption leading-5 text-muted">{t('crossProviderModelPoolHint')}</p>
                </div>
              </div>
              <CompactSwitch
                isSelected={settings?.cross_provider_model_pool ?? true}
                isDisabled={settingsBusy || !settings}
                ariaLabel={t('crossProviderModelPoolAriaLabel')}
                onChange={(selected) => void updateCrossProviderModelPool(selected)}
              />
            </div>
            <div className="mt-4 flex items-center justify-between pt-3 text-caption text-muted">
              <span>{t('crossProviderModelPoolStatus')}</span>
              <span className="font-medium text-foreground">{settings?.cross_provider_model_pool ? t('enabled') : t('disabled')}</span>
            </div>
          </Card>
          </div>
        </Tabs.Panel>
        <Tabs.Panel id="security" className="pt-5">
          <div className="grid gap-5 xl:grid-cols-2 items-start">
          <Card data-gsap-reveal>
            <div className="flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <div className="grid size-8 shrink-0 place-items-center rounded-lg bg-surface-secondary text-foreground"><Key size={15} /></div>
                <div>
                  <h3 className="font-semibold">{t('consoleKeyTitle')}</h3>
                  <p className="mt-1 text-caption leading-5 text-muted">{t('consoleKeyHint')}</p>
                </div>
              </div>
            </div>
            <code className="mono mt-4 block rounded-lg bg-surface-secondary px-3 py-2 text-caption text-muted">{consoleKey?.prefix || '—'}</code>
            <p className="mt-3 text-caption leading-5 text-muted">{t('consoleKeyLead')}</p>
            <div className="mt-4 flex justify-end">
              <Button size="sm" variant="ghost" isPending={consoleBusy} onPress={() => setRotateOpen(true)}>{t('consoleKeyRotate')}</Button>
            </div>
          </Card>
          <Card data-gsap-reveal>
            <div className="flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <div className="grid size-8 shrink-0 place-items-center rounded-lg bg-surface-secondary text-foreground"><Database size={15} /></div>
                <div>
                  <h3 className="font-semibold">{t('sqliteProtection')}</h3>
                  <p className="mt-1 text-caption leading-5 text-muted">{t('sqliteProtectionHint')}</p>
                </div>
              </div>
              <ShieldCheck size={18} className="text-success" />
            </div>
            <div className="mono mt-4 rounded-lg bg-surface-secondary px-3 py-2 text-caption text-muted">/data</div>
            <div className="mt-3 grid gap-2 text-caption text-muted">
              <div className="flex items-center gap-2"><CheckCircle size={14} className="text-muted" />{t('sqliteBackupBeforeUpdate')}</div>
              <div className="flex items-center gap-2"><CheckCircle size={14} className="text-muted" />{t('sqliteKeepFive')}</div>
              <div className="flex items-center gap-2"><CheckCircle size={14} className="text-muted" />{t('sqliteRollbackTogether')}</div>
            </div>
          </Card>
          </div>
        </Tabs.Panel>
        <Tabs.Panel id="updates" className="pt-5">
          <div className="grid gap-5">
        <Card data-gsap-reveal className="overflow-hidden p-0">
          <div className="flex items-center justify-between gap-3 border-b border-separator px-5 py-4">
            <div>
              <h3 className="font-semibold">{t('versionUpdate')}</h3>
              <p className="mt-0.5 text-caption text-muted">{t('latestVersionHint')}</p>
            </div>
            <Chip size="sm" variant="soft" color={info?.has_update ? 'warning' : 'success'}>
              {info?.has_update ? t('updateAvailable') : t('upToDate')}
            </Chip>
          </div>

          <div className="px-5 py-5">
            <UpdateControls update={update} />
            <VersionHistory
              releases={historyReleases}
              currentVersion={info?.current_version || ''}
              nextVersion={info?.next_version}
              rollbackTags={rollbackTags}
              canRollback={canRollback}
              submitting={submitting}
              onRestore={setRestoreTarget}
            />
          </div>
        </Card>
          </div>
        </Tabs.Panel>
      </Tabs>

      <ConfirmDialog
        isOpen={Boolean(restoreTarget)}
        title={t('restoreVersionTitle', { version: restoreTarget })}
        description={t('restoreVersionHint')}
        confirmLabel={t('restoreThisVersion')}
        cancelLabel={t('cancel')}
        closeLabel={t('close')}
        isPending={submitting}
        status="warning"
        confirmVariant="primary"
        onClose={() => { if (!submitting) setRestoreTarget('') }}
        onConfirm={() => {
          const version = restoreTarget
          setRestoreTarget('')
          void rollbackTo(version)
        }}
      />

      <Modal.Root isOpen={rotateOpen} onOpenChange={(open: boolean) => { if (!open && !consoleBusy) setRotateOpen(false) }}>
        <Modal.Backdrop variant="blur" isDismissable={!consoleBusy}>
          <Modal.Container placement="center" size="sm">
            <Modal.Dialog>
              <Modal.Header className="items-start justify-between gap-4">
                <div>
                  <Modal.Heading className="text-section font-semibold">{t('consoleKeyRotate')}</Modal.Heading>
                  <p className="mt-1 text-caption leading-5 text-muted">{t('consoleKeyRotateHint')}</p>
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
                  <Modal.Heading className="text-section font-semibold">{t('consoleKeySecretTitle')}</Modal.Heading>
                  <p className="mt-1.5 text-body leading-6 text-muted">{t('consoleKeySecretHint')}</p>
                </div>
                <Modal.CloseTrigger aria-label={t('close')} className="grid size-9 place-items-center rounded-lg text-muted hover:bg-surface-secondary"><X size={18} /></Modal.CloseTrigger>
              </Modal.Header>
              <Modal.Body className="px-6 pb-2">
                <code className="mono block break-all rounded-lg bg-surface-secondary px-3 py-3 text-body">{rotatedSecret}</code>
              </Modal.Body>
              <Modal.Footer className="justify-end">
                <Button variant="ghost" onPress={() => setRotatedSecret('')}>{t('close')}</Button>
                <Button onPress={() => {
                  void copyText(rotatedSecret)
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

    </div>
  )
}
