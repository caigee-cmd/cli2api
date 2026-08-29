import { useLayoutEffect, useRef } from 'react'
import { Button, Chip, Input, TextArea, Tooltip } from '@heroui/react'
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
import { QuotaMeter } from '@/components/account/QuotaMeter'
import { RuntimeMeter } from '@/components/account/RuntimeMeter'
import { hoverLift } from '@/hooks/useGsapReveal'
import {
  accountState,
  cooldownLabel,
  type AccountRow,
} from '@/lib/account'
import { accountProviderLabel } from '@/lib/provider'

const ACCOUNT_BUTTON_CLASS = 'account-button'
const ACCOUNT_ICON_BUTTON_CLASS = 'account-button account-icon-button'
const ACCOUNT_CHIP_CLASS = 'account-chip'
const ACCOUNT_INPUT_CLASS = 'account-input'

export type AccountBusyKind = 'create' | 'import' | 'device' | 'pat' | 'callback' | 'rewarm' | 'toggle' | 'delete' | 'export' | 'settings'

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
  onRewarm: () => void
  onDelete: () => void
  onToggle: (selected: boolean) => void
  onToggleDropSystem: (selected: boolean) => void
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
  onRewarm,
  onDelete,
  onToggle,
  onToggleDropSystem,
  onEdit,
  onToggleAuthPanel,
  onViewModels,
}: Props) {
  const cardRef = useRef<HTMLElement>(null)
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

  return (
    <article
      ref={cardRef}
      data-gsap-reveal
      data-state={state}
      className="account-card app-panel-flat flex min-w-0 flex-col overflow-hidden rounded-lg"
      onMouseEnter={() => cardRef.current && hoverLift(cardRef.current, true, 1)}
      onMouseLeave={() => cardRef.current && hoverLift(cardRef.current, false, 1)}
    >
      <header className="flex items-start justify-between gap-3 px-3 pt-3 pb-2">
        <div className="flex min-w-0 items-center gap-2.5">
          <ProviderMark provider={account.provider} size={22} />
          <div className="min-w-0">
            <div className="truncate text-[13px] font-semibold leading-5 tracking-[-0.01em]">{account.name || account.id}</div>
            <div className="mono truncate text-[10px] leading-4 text-[var(--app-faint)]" title={`${account.id}${account.remote_uid ? ` · UID ${account.remote_uid}` : ''}`}>
              {meta}
            </div>
          </div>
        </div>
        <span ref={chipRef} className="shrink-0">
          <Chip className={ACCOUNT_CHIP_CLASS} size="sm" variant="soft" color={stateColor}>{stateCopy}</Chip>
        </span>
      </header>

      <div className="flex flex-1 flex-col gap-2.5 px-3 pb-2.5">
        <RuntimeMeter state={state} label={t('runtimeState')} stateCopy={stateCopy} />
        {account.quota ? (
          <QuotaMeter
            quota={account.quota}
            label={t('quota')}
            remainingLabel={t('quotaRemaining')}
            addOnLabel={t('quotaAddOn')}
            exceededLabel={t('quotaExceeded')}
          />
        ) : null}

        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-[var(--app-faint)]">
          <span>
            <span className="mono font-medium text-[var(--app-ink)]">{inFlight}/{account.max_inflight ?? 4}</span>
            {' '}{t('inFlight')}
          </span>
          <span>
            {t('priority')} <span className="mono font-medium text-[var(--app-ink)]">{account.priority ?? 50}</span>
          </span>
          <span>
            {t('restarts')} <span className="mono font-medium text-[var(--app-ink)]">{account.restarts ?? 0}</span>
          </span>
        </div>

        {account.provider === 'workbuddy' ? (
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
        ) : null}

        {lastError ? (
          <div className="flex gap-2 border-l-2 border-[var(--app-danger)] pl-2.5 text-xs leading-5 text-[var(--app-danger)]">
            <WarningCircle size={14} className="mt-0.5 shrink-0" />
            <div className="min-w-0">
              {errorKind ? <div className="mono mb-0.5 text-[10px] uppercase opacity-75">{errorKind}</div> : null}
              <p className="break-words">{lastError}</p>
            </div>
          </div>
        ) : null}
      </div>

      {authPanelOpen && account.enabled ? (
        <section ref={authRef} className="grid gap-3 border-t border-[var(--app-line)] bg-[var(--app-surface-muted)]/55 px-3 py-3">
          <div>
            <div className="text-[10px] font-semibold tracking-[0.1em] text-[var(--app-faint)] uppercase">{t('oauthDeviceFlow')}</div>
            <p className="mt-1.5 text-xs leading-5 text-[var(--app-muted)]">{t('qoderLoginHint')}</p>
            <div className="mt-2.5 flex flex-wrap gap-2">
              <Button className={ACCOUNT_BUTTON_CLASS} size="sm" isPending={busyKind === 'device'} onPress={onDeviceLogin}><ShieldCheck size={14} />{t('startBrowserLogin')}</Button>
              {authUrl ? <Button className={ACCOUNT_BUTTON_CLASS} size="sm" variant="ghost" onPress={() => window.open(authUrl, '_blank', 'noopener,noreferrer')}><ArrowSquareOut size={14} />{t('open')}</Button> : null}
            </div>
            {account.provider === 'trae' && onSubmitCallback && onCallbackChange ? (
              <div className="mt-3 space-y-2">
                <p className="text-[11px] leading-4 text-[var(--app-faint)]">{t('wizardCallbackLead')}</p>
                <TextArea
                  className="h-24 w-full resize-none font-mono text-xs leading-5"
                  value={callbackUrl || ''}
                  onChange={(event) => onCallbackChange(event.target.value)}
                  placeholder={t('wizardCallbackPh')}
                  aria-label={t('wizardCallbackPh')}
                />
                <Button className={ACCOUNT_BUTTON_CLASS} size="sm" variant="secondary" isPending={busyKind === 'callback'} onPress={onSubmitCallback}>
                  {t('wizardSubmitCallback')}
                </Button>
              </div>
            ) : null}
          </div>
          <div>
            <div className="text-[10px] font-semibold tracking-[0.1em] text-[var(--app-faint)] uppercase">{t('patFallback')}</div>
            <div className="mt-2.5 flex flex-col gap-2 sm:flex-row">
              <Input className={ACCOUNT_INPUT_CLASS} type="password" value={pat} onChange={(event) => onPatChange(event.target.value)} placeholder={t('pasteToken')} aria-label={t('pat')} />
              <Button className={ACCOUNT_BUTTON_CLASS} size="sm" variant="secondary" isPending={busyKind === 'pat'} onPress={onPatLogin}><Key size={14} />{t('usePat')}</Button>
            </div>
          </div>
          {authUrl || note ? (
            <div className="text-xs">
              {authUrl ? <code className="mono block break-all text-[var(--app-faint)]">{authUrl}</code> : null}
              {note ? <p className="mt-1 text-[var(--app-muted)]">{note}</p> : null}
            </div>
          ) : null}
        </section>
      ) : null}

      <footer className="flex flex-wrap items-center gap-1.5 border-t border-[var(--app-line)] px-3 py-2">
        <Button
          className={ACCOUNT_BUTTON_CLASS}
          size="sm"
          variant={state === 'login' ? 'primary' : 'secondary'}
          isDisabled={!account.enabled}
          onPress={onToggleAuthPanel}
        >
          <Key size={14} />{t('authentication')}
        </Button>
        <Button className={ACCOUNT_BUTTON_CLASS} size="sm" variant="secondary" onPress={onEdit}>
          <PencilSimple size={14} />{t('editAccount')}
        </Button>
        <Tooltip>
          <Tooltip.Trigger>
            <Button className={ACCOUNT_ICON_BUTTON_CLASS} isIconOnly size="sm" variant="secondary" onPress={onViewModels} aria-label={t('accountModels')}>
              <Cube size={14} />
            </Button>
          </Tooltip.Trigger>
          <Tooltip.Content>{t('accountModels')}</Tooltip.Content>
        </Tooltip>
        <Tooltip>
          <Tooltip.Trigger>
            <Button className={ACCOUNT_ICON_BUTTON_CLASS} isIconOnly size="sm" variant="secondary" isDisabled={!account.enabled} isPending={busyKind === 'rewarm'} onPress={onRewarm} aria-label={t('rewarm')}>
              <ArrowClockwise size={14} />
            </Button>
          </Tooltip.Trigger>
          <Tooltip.Content>{t('rewarm')}</Tooltip.Content>
        </Tooltip>
        {account.auth_type !== 'none' ? (
          <Tooltip>
            <Tooltip.Trigger>
              <Button className={ACCOUNT_ICON_BUTTON_CLASS} isIconOnly size="sm" variant="secondary" isPending={busyKind === 'export'} onPress={onExport} aria-label={t('export')}>
                <Copy size={14} />
              </Button>
            </Tooltip.Trigger>
            <Tooltip.Content>{t('export')}</Tooltip.Content>
          </Tooltip>
        ) : null}
        <Tooltip>
          <Tooltip.Trigger>
            <Button className={ACCOUNT_ICON_BUTTON_CLASS} isIconOnly size="sm" variant="danger-soft" isPending={busyKind === 'delete'} onPress={onDelete} aria-label={t('delete')}>
              <TrashSimple size={14} />
            </Button>
          </Tooltip.Trigger>
          <Tooltip.Content>{t('delete')}</Tooltip.Content>
        </Tooltip>
        <div className="ml-auto flex items-center gap-2">
          <span className="text-[11px] font-medium text-[var(--app-faint)]">{account.enabled ? t('enabledState') : t('disabled')}</span>
          <CompactSwitch
            isSelected={Boolean(account.enabled)}
            isDisabled={busyKind === 'toggle'}
            ariaLabel={account.enabled ? t('disable') : t('enable')}
            onChange={onToggle}
          />
        </div>
      </footer>
    </article>
  )
}
