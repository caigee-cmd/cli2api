import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Button, Card, Chip, Dropdown, Input, Label, TextArea, TextField, Tooltip } from '@heroui/react'
import {
  ArrowClockwise,
  ArrowSquareOut,
  CalendarCheck,
  Copy,
  Cube,
  DotsThreeVertical,
  Key,
  ListBullets,
  PencilSimple,
  ShieldCheck,
  TextAa,
  TrashSimple,
  WarningCircle,
} from '@phosphor-icons/react'
import gsap from 'gsap'
import { ProviderMark } from '@/components/ProviderMark'
import { CompactSwitch } from '@/components/ui/CompactSwitch'
import { QuotaMeter } from '@/components/account/QuotaMeter'
import { RuntimeMeter } from '@/components/account/RuntimeMeter'
import {
  accountState,
  cooldownLabel,
  modelCooldownEntries,
  type AccountRow,
} from '@/lib/account'
import { accountProviderLabel } from '@/lib/provider'

export type AccountBusyKind = 'create' | 'import' | 'device' | 'pat' | 'callback' | 'rewarm' | 'refresh' | 'toggle' | 'delete' | 'export' | 'settings' | 'checkin'

type Translate = (key: string, vars?: Record<string, string | number>) => string

type Props = {
  account: AccountRow
  busyKind: AccountBusyKind | ''
  authPanelOpen: boolean
  authUrl?: string
  note?: string
  pat: string
  t: Translate
  onPatChange: (value: string) => void
  onDeviceLogin: () => void
  onPatLogin: () => void
  callbackUrl?: string
  onCallbackChange?: (value: string) => void
  onSubmitCallback?: () => void
  onExport: () => void
  onRefresh?: () => void
  onDelete: () => void
  onToggle: (selected: boolean) => void
  onToggleDropSystem: (selected: boolean) => void
  checkinDefaultTime?: string
  onToggleAutoCheckin?: (selected: boolean) => void
  onCheckin?: () => void
  onViewCheckins?: () => void
  onEdit: () => void
  onToggleAuthPanel: () => void
  onViewModels: () => void
}

function stateCopyFor(state: ReturnType<typeof accountState>, cooldown: string, t: Translate) {
  if (state === 'hot') return t('signedIn')
  if (state === 'ready') return t('ready')
  if (state === 'cooling') return cooldown ? `${t('cooling')} ${cooldown}` : t('cooling')
  if (state === 'quota_exhausted') return t('quotaExceeded')
  if (state === 'loading') return t('quotaLoading')
  if (state === 'starting') return t('starting')
  if (state === 'unavailable') return t('quotaUnavailable')
  if (state === 'dead') return cooldown ? `${t('dead')} ${cooldown}` : t('dead')
  if (state === 'auth_failed') return t('authFailed')
  if (state === 'disabled') return t('disabled')
  return t('needQoderLogin')
}

export function AccountCard({
  account,
  busyKind,
  authPanelOpen,
  authUrl,
  note,
  pat,
  t,
  onPatChange,
  onDeviceLogin,
  onPatLogin,
  callbackUrl,
  onCallbackChange,
  onSubmitCallback,
  onExport,
  onRefresh,
  onDelete,
  onToggle,
  onToggleDropSystem,
  checkinDefaultTime,
  onToggleAutoCheckin,
  onCheckin,
  onViewCheckins,
  onEdit,
  onToggleAuthPanel,
  onViewModels,
}: Props) {
  const chipRef = useRef<HTMLSpanElement>(null)
  const authRef = useRef<HTMLElement>(null)
  const lastStateRef = useRef<string | null>(null)
  const [, refreshCooldowns] = useState(0)
  const state = accountState(account)
  const cooldown = cooldownLabel(account.down_until || account.cooldown_until)
  const restartIn = cooldownLabel(account.next_restart_at)
  const modelCooldowns = modelCooldownEntries(account)

  useEffect(() => {
    if (!cooldown && !restartIn && modelCooldowns.length === 0) return
    const timer = window.setInterval(() => refreshCooldowns((value) => value + 1), 1000)
    return () => window.clearInterval(timer)
  }, [cooldown, restartIn, modelCooldowns.length])
  const stateCopy = stateCopyFor(state, state === 'dead' ? restartIn : cooldown, t)
  const stateColor = state === 'hot' || state === 'ready'
    ? 'success'
    : state === 'cooling'
      ? 'warning'
      : state === 'quota_exhausted'
        ? 'danger'
      : state === 'login' || state === 'unavailable' || state === 'dead' || state === 'auth_failed'
        ? 'danger'
        : undefined
  const lastError = account.last_error || account.lastError
  const errorKind = account.last_error_kind || account.kind
  const provider = accountProviderLabel(account.provider, account.region, t)
  const checkinStatus = account.last_checkin_status
  const checkinLabel = checkinStatus === 'success' ? 'checkinRecordSuccess' : checkinStatus === 'already' ? 'checkinRecordAlready' : checkinStatus === 'skipped' ? 'checkinRecordSkipped' : checkinStatus === 'error' ? 'checkinRecordFailed' : 'lastCheckinNone'

  useLayoutEffect(() => {
    const chip = chipRef.current
    if (!chip) return
    const previous = lastStateRef.current
    lastStateRef.current = state
    if (!previous || previous === state) return

    const context = gsap.context(() => {
      const media = gsap.matchMedia()
      media.add('(prefers-reduced-motion: reduce)', () => {
        gsap.set(chip, { scale: 1, autoAlpha: 1 })
      })
      media.add('(prefers-reduced-motion: no-preference)', () => {
        gsap.fromTo(
          chip,
          { scale: 0.94, autoAlpha: 0.65 },
          { scale: 1, autoAlpha: 1, duration: 0.2, ease: 'power2.out', overwrite: true },
        )
      })
    }, chip)

    return () => context.revert()
  }, [state])

  useLayoutEffect(() => {
    const panel = authRef.current
    if (!panel || !authPanelOpen) return

    const context = gsap.context(() => {
      const media = gsap.matchMedia()
      media.add('(prefers-reduced-motion: reduce)', () => {
        gsap.set(panel, { autoAlpha: 1, y: 0 })
      })
      media.add('(prefers-reduced-motion: no-preference)', () => {
        gsap.fromTo(
          panel,
          { autoAlpha: 0, y: -6 },
          { autoAlpha: 1, y: 0, duration: 0.2, ease: 'power3.out', overwrite: true },
        )
      })
    }, panel)

    return () => context.revert()
  }, [authPanelOpen])

  // Refresh keeps the existing card mounted so the layout does not jump.
  // The refresh button spinner (busyKind === 'refresh') is the only visual
  // indicator; stale quota / status stay on screen until the new payload
  // arrives. Cards only mount a skeleton on the very first load.
  return (
    <Card
      data-gsap-reveal
      data-state={state}
      className="account-card overflow-hidden p-0"
    >
      <Card.Header className="flex-row items-start justify-between gap-2.5 px-3 pt-2.5 pb-1.5">
        <div className="flex min-w-0 items-center gap-2.5">
          <div className="flex size-8 shrink-0 items-center justify-center rounded-xl border border-border bg-surface-secondary">
            <ProviderMark provider={account.provider} size={20} />
          </div>
          <div className="min-w-0">
            <div className="flex min-w-0 items-center gap-2">
              <Card.Title className="truncate text-[13px] leading-5 tracking-[-0.01em]">{account.name || account.id}</Card.Title>
              <span className="shrink-0 rounded-md bg-surface-secondary px-1.5 py-0.5 text-[10px] text-foreground/65">{provider}</span>
            </div>
            <Card.Description className="mono truncate text-[10px] leading-4 text-foreground/60" title={`${account.id}${account.remote_uid ? ` · UID ${account.remote_uid}` : ''}`}>
              {account.remote_uid ? `UID ${account.remote_uid}` : account.id}
            </Card.Description>
          </div>
        </div>
        <div className="flex max-w-[48%] shrink-0 flex-wrap justify-end gap-1.5">
          <span ref={chipRef}>
            {modelCooldowns.length && state === 'cooling' ? (
              <Tooltip>
                <Tooltip.Trigger>
                  <span className="inline-flex cursor-help"><Chip size="sm" variant="soft" color={stateColor}>{stateCopy}</Chip></span>
                </Tooltip.Trigger>
                <Tooltip.Content>
                  <div className="space-y-1.5">
                    <div className="font-medium">{t('partialCooling')}</div>
                    {modelCooldowns.map(([model, until]) => (
                      <div key={model} className="flex items-center justify-between gap-4 text-xs">
                        <span>{model}</span>
                        <span className="mono text-foreground/65">{cooldownLabel(until)}</span>
                      </div>
                    ))}
                  </div>
                </Tooltip.Content>
              </Tooltip>
            ) : (
              <Chip size="sm" variant="soft" color={stateColor}>{stateCopy}</Chip>
            )}
          </span>
          {modelCooldowns.length && state !== 'cooling' ? (
            <Tooltip>
              <Tooltip.Trigger>
                <span className="inline-flex cursor-help"><Chip size="sm" variant="soft" color="warning">{t('partialCooling')}</Chip></span>
              </Tooltip.Trigger>
              <Tooltip.Content>
                <div className="space-y-1.5">
                  <div className="font-medium">{t('partialCooling')}</div>
                  {modelCooldowns.map(([model, until]) => (
                    <div key={model} className="flex items-center justify-between gap-4 text-xs">
                      <span>{model}</span>
                      <span className="mono text-foreground/65">{cooldownLabel(until)}</span>
                    </div>
                  ))}
                </div>
              </Tooltip.Content>
            </Tooltip>
          ) : null}
        </div>
      </Card.Header>

      <Card.Content className="gap-1.5 px-3 pb-2">
        <RuntimeMeter state={state} stateCopy={stateCopy} t={t} />

        {onCheckin ? (
          <Tooltip>
            <Tooltip.Trigger>
              <span className="flex w-fit cursor-help items-center gap-2 text-[11px] text-muted">
                <span className="font-medium">{t('lastCheckin')}</span>
                <span className={checkinStatus === 'error' ? 'text-danger' : ''}>{t(checkinLabel)}</span>
              </span>
            </Tooltip.Trigger>
            <Tooltip.Content>{account.last_checkin_at ? new Date(account.last_checkin_at).toLocaleString() : t('lastCheckinNone')}{account.last_checkin_msg ? ` · ${account.last_checkin_msg}` : ''}</Tooltip.Content>
          </Tooltip>
        ) : null}

        {account.quota ? (
          <QuotaMeter
            quota={account.quota}
            label={t('quota')}
            usedLabel={t('quotaUsed')}
            remainingLabel={t('quotaRemaining')}
            addOnLabel={t('quotaAddOn')}
            resourcePackageLabel={t('quotaResourcePackage')}
            exceededLabel={t('quotaExceeded')}
          />
        ) : <span className="text-[11px] text-foreground/65">{state === 'loading' ? t('quotaLoading') : t('quotaUnavailable')}</span>}

        {lastError ? (
          <Tooltip>
            <Tooltip.Trigger>
              <div className="flex w-full cursor-help gap-2 rounded-2xl border border-danger/25 bg-danger/5 p-2 text-left text-xs leading-5 text-danger">
                <WarningCircle size={14} className="mt-0.5 shrink-0" />
                <div className="min-w-0 flex-1">
                  {errorKind ? <div className="mono mb-0.5 truncate text-[10px] opacity-75">{errorKind}</div> : null}
                  <p className="break-words line-clamp-3">{lastError}</p>
                </div>
              </div>
            </Tooltip.Trigger>
            <Tooltip.Content>
              <div className="max-w-md whitespace-pre-wrap break-words">
                {errorKind ? <div className="mono mb-1 text-[10px] opacity-75">{errorKind}</div> : null}
                {lastError}
              </div>
            </Tooltip.Content>
          </Tooltip>
        ) : null}
      </Card.Content>

      {authPanelOpen && account.enabled ? (
        <section ref={authRef} className="grid gap-3 border-t border-separator bg-surface-secondary/55 px-3 py-3">
          <div>
            <div className="text-xs font-medium text-muted">{t('oauthDeviceFlow')}</div>
            <p className="mt-1.5 text-xs leading-5 text-muted">{t('qoderLoginHint')}</p>
            <div className="mt-2.5 flex flex-wrap gap-2">
              <Button size="sm" isPending={busyKind === 'device'} onPress={onDeviceLogin}><ShieldCheck size={14} />{t('startBrowserLogin')}</Button>
              {authUrl ? <Button size="sm" variant="ghost" onPress={() => window.open(authUrl, '_blank', 'noopener,noreferrer')}><ArrowSquareOut size={14} />{t('open')}</Button> : null}
            </div>
            {account.provider === 'trae' && onSubmitCallback && onCallbackChange ? (
              <div className="mt-3 space-y-2">
                <p className="text-[11px] leading-4 text-muted">{t('wizardCallbackLead')}</p>
                <TextArea
                  className="h-24 w-full resize-none font-mono text-xs leading-5"
                  value={callbackUrl || ''}
                  onChange={(event) => onCallbackChange(event.target.value)}
                  placeholder={t('wizardCallbackPh')}
                  aria-label={t('wizardCallbackPh')}
                />
                <Button size="sm" variant="secondary" isPending={busyKind === 'callback'} onPress={onSubmitCallback}>
                  {t('wizardSubmitCallback')}
                </Button>
              </div>
            ) : null}
          </div>
          <div>
            <div className="text-xs font-medium text-muted">{t('patFallback')}</div>
            <div className="mt-2.5 flex flex-col gap-2 sm:flex-row">
              <TextField className="flex-1" type="password" value={pat} onChange={onPatChange}>
                <Label className="sr-only">{t('pat')}</Label>
                <Input placeholder={t('pasteToken')} aria-label={t('pat')} />
              </TextField>
              <Button size="sm" variant="secondary" isPending={busyKind === 'pat'} onPress={onPatLogin}><Key size={14} />{t('usePat')}</Button>
            </div>
          </div>
          {authUrl || note ? (
            <div className="text-xs">
              {authUrl ? <code className="mono block break-all text-muted">{authUrl}</code> : null}
              {note ? <p className="mt-1 text-muted">{note}</p> : null}
            </div>
          ) : null}
        </section>
      ) : null}

      <Card.Footer className="flex flex-wrap items-center gap-1.5 border-t border-separator px-3 py-2">
        {account.provider === 'workbuddy' ? (
          <Tooltip>
            <Tooltip.Trigger>
              <button
                type="button"
                onClick={() => onToggleDropSystem?.(!account.drop_system_prompt)}
                className={`flex size-7 items-center justify-center rounded-lg border transition-colors ${account.drop_system_prompt ? 'border-accent/40 bg-accent/10 text-accent' : 'border-border text-foreground/50 hover:text-foreground'}`}
                aria-label={t('dropSystemPrompt')}
                aria-pressed={Boolean(account.drop_system_prompt)}
              >
                <TextAa size={14} />
              </button>
            </Tooltip.Trigger>
            <Tooltip.Content>{t('dropSystemPromptHint')}</Tooltip.Content>
          </Tooltip>
        ) : null}
        {onRefresh ? (
          <Tooltip>
            <Tooltip.Trigger>
              <Button isIconOnly size="sm" variant="secondary" isPending={busyKind === 'refresh'} onPress={onRefresh} aria-label={t('refreshAccount')}>
                <ArrowClockwise size={15} />
              </Button>
            </Tooltip.Trigger>
            <Tooltip.Content>{t('refreshAccount')}</Tooltip.Content>
          </Tooltip>
        ) : null}
        <Tooltip>
          <Tooltip.Trigger>
            <Button isIconOnly size="sm" variant={authPanelOpen ? 'secondary' : 'ghost'} isDisabled={!account.enabled} onPress={onToggleAuthPanel} aria-label={t('authentication')}>
              <Key size={15} />
            </Button>
          </Tooltip.Trigger>
          <Tooltip.Content>{t('authentication')}</Tooltip.Content>
        </Tooltip>
        <Tooltip>
          <Tooltip.Trigger>
            <Button isIconOnly size="sm" variant="ghost" onPress={onViewModels} aria-label={t('accountModels')}>
              <Cube size={15} />
            </Button>
          </Tooltip.Trigger>
          <Tooltip.Content>{t('accountModels')}</Tooltip.Content>
        </Tooltip>
        <Tooltip>
          <Tooltip.Trigger>
            <Button isIconOnly size="sm" variant="ghost" onPress={onEdit} aria-label={t('editAccount')}>
              <PencilSimple size={15} />
            </Button>
          </Tooltip.Trigger>
          <Tooltip.Content>{t('editAccount')}</Tooltip.Content>
        </Tooltip>
        {onCheckin ? (
          <Tooltip>
            <Tooltip.Trigger>
              <Button isIconOnly size="sm" variant="ghost" isDisabled={!account.enabled || Boolean(busyKind)} isPending={busyKind === 'checkin'} onPress={onCheckin} aria-label={t('checkinNow')}>
                <CalendarCheck size={15} />
              </Button>
            </Tooltip.Trigger>
            <Tooltip.Content>{t('checkinNow')}</Tooltip.Content>
          </Tooltip>
        ) : null}
        <Dropdown>
          <Dropdown.Trigger>
            <Button isIconOnly size="sm" variant="secondary" aria-label={t('more')}>
              <DotsThreeVertical size={16} />
            </Button>
          </Dropdown.Trigger>
          <Dropdown.Popover placement="bottom end">
            <Dropdown.Menu
              aria-label={t('more')}
              onAction={(key) => {
                if (key === 'export') onExport()
                if (key === 'checkin-records') onViewCheckins?.()
                if (key === 'delete') onDelete()
              }}
            >
              {account.auth_type !== 'none' ? <Dropdown.Item id="export" textValue={t('export')}><Copy size={15} />{t('export')}</Dropdown.Item> : null}
              {onViewCheckins ? <Dropdown.Item id="checkin-records" textValue={t('checkinRecords')}><ListBullets size={15} />{t('checkinRecords')}</Dropdown.Item> : null}
              <Dropdown.Item id="delete" textValue={t('delete')} className="text-danger"><TrashSimple size={15} />{t('delete')}</Dropdown.Item>
            </Dropdown.Menu>
          </Dropdown.Popover>
        </Dropdown>
        <div className="ml-auto flex shrink-0 items-center gap-3">
          {onCheckin ? (
            <Tooltip>
              <Tooltip.Trigger>
                <span className="flex cursor-help items-center gap-1.5 text-muted">
                  <span className="mono text-[10px]">{account.checkin_time || checkinDefaultTime}</span>
                  <CompactSwitch isSelected={Boolean(account.auto_checkin)} isDisabled={Boolean(busyKind)} ariaLabel={t('autoCheckin')} onChange={(selected) => onToggleAutoCheckin?.(selected)} />
                </span>
              </Tooltip.Trigger>
              <Tooltip.Content>{t('autoCheckin')}</Tooltip.Content>
            </Tooltip>
          ) : null}
          <CompactSwitch
            isSelected={Boolean(account.enabled)}
            isDisabled={busyKind === 'toggle'}
            ariaLabel={account.enabled ? t('disable') : t('enable')}
            label={account.enabled ? t('enabledState') : t('disabled')}
            onChange={onToggle}
          />
        </div>
      </Card.Footer>
    </Card>
  )
}
