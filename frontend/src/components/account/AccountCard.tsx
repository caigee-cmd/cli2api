import { useLayoutEffect, useRef } from 'react'
import { Button, Card, Chip, Input, Label, TextArea, TextField, Tooltip } from '@heroui/react'
import {
  ArrowClockwise,
  ArrowSquareOut,
  Copy,
  Cube,
  Key,
  PencilSimple,
  ShieldCheck,
  TrashSimple,
  WarningCircle,
} from '@phosphor-icons/react'
import gsap from 'gsap'
import { ProviderMark } from '@/components/ProviderMark'
import { CompactSwitch } from '@/components/ui/CompactSwitch'
import { AccountCardSkeleton } from '@/components/ui/PageSkeletons'
import { QuotaMeter } from '@/components/account/QuotaMeter'
import { RuntimeMeter } from '@/components/account/RuntimeMeter'
import {
  accountState,
  cooldownLabel,
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
  onToggleAutoCheckin?: (selected: boolean) => void
  onCheckin?: () => void
  onEdit: () => void
  onToggleAuthPanel: () => void
  onViewModels: () => void
}

function stateCopyFor(state: ReturnType<typeof accountState>, cooldown: string, t: Translate) {
  if (state === 'hot') return t('signedIn')
  if (state === 'ready') return t('ready')
  if (state === 'cooling') return cooldown ? `${t('cooling')} ${cooldown}` : t('cooling')
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
  onToggleAutoCheckin,
  onCheckin,
  onEdit,
  onToggleAuthPanel,
  onViewModels,
}: Props) {
  const chipRef = useRef<HTMLSpanElement>(null)
  const authRef = useRef<HTMLElement>(null)
  const lastStateRef = useRef<string | null>(null)
  const state = accountState(account)
  const cooldown = cooldownLabel(account.down_until || account.cooldown_until)
  const stateCopy = stateCopyFor(state, cooldown, t)
  const stateColor = state === 'hot' || state === 'ready'
    ? 'success'
    : state === 'cooling'
      ? 'warning'
      : state === 'login'
        ? 'danger'
        : undefined
  const inFlight = account.in_flight ?? account.inFlight ?? 0
  const lastError = account.last_error || account.lastError
  const errorKind = account.last_error_kind || account.kind
  const provider = accountProviderLabel(account.provider, account.region, t)
  const meta = [
    provider,
    account.remote_uid ? `UID ${account.remote_uid}` : account.id,
  ].join(' · ')

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

  // Refreshing replaces the whole card with a skeleton so stale quota and
  // status are never left on screen while the account is re-probed.
  if (busyKind === 'refresh') {
    return (
      <div data-gsap-reveal aria-busy="true" aria-label={t('refreshingAccount')}>
        <AccountCardSkeleton />
      </div>
    )
  }

  return (
    <Card
      data-gsap-reveal
      data-state={state}
      className="account-card overflow-hidden p-0"
    >
      <Card.Header className="flex-row items-start justify-between gap-3 px-3 pt-3 pb-2">
        <div className="flex min-w-0 items-center gap-2.5">
          <ProviderMark provider={account.provider} size={22} />
          <div className="min-w-0">
            <Card.Title className="truncate text-[13px] leading-5 tracking-[-0.01em]">{account.name || account.id}</Card.Title>
            <Card.Description className="mono truncate text-[10px] leading-4" title={`${account.id}${account.remote_uid ? ` · UID ${account.remote_uid}` : ''}`}>
              {meta}
            </Card.Description>
          </div>
        </div>
        <span ref={chipRef} className="shrink-0">
          <Chip size="sm" variant="soft" color={stateColor}>{stateCopy}</Chip>
        </span>
      </Card.Header>

      <Card.Content className="gap-2.5 px-3 pb-2.5">
        <RuntimeMeter state={state} label={t('runtimeState')} stateCopy={stateCopy} />
        {account.quota ? (
          <QuotaMeter
            quota={account.quota}
            label={t('quota')}
            usedLabel={t('quotaUsed')}
            remainingLabel={t('quotaRemaining')}
            addOnLabel={t('quotaAddOn')}
            exceededLabel={t('quotaExceeded')}
          />
        ) : null}

        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-muted">
          <span>
            <span className="mono font-medium text-foreground">{inFlight}/{account.max_inflight ?? 4}</span>
            {' '}{t('inFlight')}
          </span>
          <span>
            {t('priority')} <span className="mono font-medium text-foreground">{account.priority ?? 50}</span>
          </span>
          <span>
            {t('restarts')} <span className="mono font-medium text-foreground">{account.restarts ?? 0}</span>
          </span>
        </div>

        {account.provider === 'workbuddy' ? (
          <div className="grid gap-2">
            <div className="flex items-center justify-between gap-3 text-[11px]">
              <Tooltip>
                <Tooltip.Trigger>
                  <span className="font-medium">{t('dropSystemPrompt')}</span>
                </Tooltip.Trigger>
                <Tooltip.Content>{t('dropSystemPromptHint')}</Tooltip.Content>
              </Tooltip>
              <CompactSwitch
                isSelected={Boolean(account.drop_system_prompt)}
                isDisabled={busyKind === 'toggle'}
                ariaLabel={t('dropSystemPrompt')}
                onChange={onToggleDropSystem}
              />
            </div>
            <div className="flex items-center justify-between gap-3 text-[11px]">
              <Tooltip>
                <Tooltip.Trigger>
                  <span className="font-medium">{t('autoCheckin')}</span>
                </Tooltip.Trigger>
                <Tooltip.Content>{t('autoCheckinHint')}</Tooltip.Content>
              </Tooltip>
              <CompactSwitch
                isSelected={Boolean(account.workbuddy_auto_checkin)}
                isDisabled={busyKind === 'toggle' || !onToggleAutoCheckin}
                ariaLabel={t('autoCheckin')}
                onChange={(selected) => onToggleAutoCheckin?.(selected)}
              />
            </div>
            <div className="flex flex-wrap items-center justify-between gap-2 text-[11px] text-muted">
              <span className="min-w-0 break-words">
                {account.last_checkin_at
                  ? `${t('lastCheckin')}: ${account.last_checkin_msg || '—'} · ${account.last_checkin_at}`
                  : t('lastCheckinNone')}
              </span>
              {onCheckin ? (
                <Button
                  size="sm"
                  variant="secondary"
                  isPending={busyKind === 'checkin'}
                  isDisabled={!account.enabled}
                  onPress={onCheckin}
                >
                  {t('checkinNow')}
                </Button>
              ) : null}
            </div>
          </div>
        ) : null}

        {lastError ? (
          <div className="flex gap-2 text-xs leading-5 text-danger">
            <WarningCircle size={14} className="mt-0.5 shrink-0" />
            <div className="min-w-0">
              {errorKind ? <div className="mono mb-0.5 text-[10px] opacity-75">{errorKind}</div> : null}
              <p className="break-words">{lastError}</p>
            </div>
          </div>
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

      <Card.Footer className="flex-wrap gap-1.5 border-t border-separator px-3 py-2">
        <Button
          size="sm"
          variant={state === 'login' ? 'primary' : 'secondary'}
          isDisabled={!account.enabled}
          onPress={onToggleAuthPanel}
        >
          <Key size={14} />{t('authentication')}
        </Button>
        <Button size="sm" variant="secondary" onPress={onEdit}>
          <PencilSimple size={14} />{t('editAccount')}
        </Button>
        <Tooltip>
          <Tooltip.Trigger>
            <Button isIconOnly size="sm" variant="secondary" onPress={onViewModels} aria-label={t('accountModels')}>
              <Cube size={14} />
            </Button>
          </Tooltip.Trigger>
          <Tooltip.Content>{t('accountModels')}</Tooltip.Content>
        </Tooltip>
        {onRefresh ? (
          <Tooltip>
            <Tooltip.Trigger>
              <Button isIconOnly size="sm" variant="secondary" onPress={onRefresh} aria-label={t('refreshAccount')}>
                <ArrowClockwise size={14} />
              </Button>
            </Tooltip.Trigger>
            <Tooltip.Content>{t('refreshAccount')}</Tooltip.Content>
          </Tooltip>
        ) : null}
        {account.auth_type !== 'none' ? (
          <Tooltip>
            <Tooltip.Trigger>
              <Button isIconOnly size="sm" variant="secondary" isPending={busyKind === 'export'} onPress={onExport} aria-label={t('export')}>
                <Copy size={14} />
              </Button>
            </Tooltip.Trigger>
            <Tooltip.Content>{t('export')}</Tooltip.Content>
          </Tooltip>
        ) : null}
        <Tooltip>
          <Tooltip.Trigger>
            <Button isIconOnly size="sm" variant="danger-soft" isPending={busyKind === 'delete'} onPress={onDelete} aria-label={t('delete')}>
              <TrashSimple size={14} />
            </Button>
          </Tooltip.Trigger>
          <Tooltip.Content>{t('delete')}</Tooltip.Content>
        </Tooltip>
        <div className="ml-auto flex items-center gap-2">
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
