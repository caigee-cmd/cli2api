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
  TrashSimple,
  WarningCircle,
} from '@phosphor-icons/react'
import gsap from 'gsap'
import { ProviderMark } from '@/components/ProviderMark'
import { CompactSwitch } from '@/components/ui/CompactSwitch'
import { AccountCardSkeleton, SkeletonBlock } from '@/components/ui/PageSkeletons'
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
  detailsLoading?: boolean
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
  onViewCheckins?: () => void
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

function checkinStatusFor(account: AccountRow, t: Translate) {
  if (account.last_checkin_status === 'error') return { label: t('checkinFailed'), state: 'warn' as const }
  if (!account.last_checkin_at) return { label: t('checkinPending'), state: undefined }
  const date = new Date(account.last_checkin_at)
  const now = new Date()
  if (!Number.isNaN(date.getTime()) && date.getFullYear() === now.getFullYear() && date.getMonth() === now.getMonth() && date.getDate() === now.getDate()) {
    return { label: t('checkinToday'), state: 'ok' as const }
  }
  return { label: t('checkinPending'), state: undefined }
}

export function AccountCard({
  account,
  busyKind,
  detailsLoading = false,
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
  const modelCooldowns = modelCooldownEntries(account)

  useEffect(() => {
    if (!cooldown && modelCooldowns.length === 0) return
    const timer = window.setInterval(() => refreshCooldowns((value) => value + 1), 1000)
    return () => window.clearInterval(timer)
  }, [cooldown, modelCooldowns.length])
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
  const checkin = checkinStatusFor(account, t)

  const detailsSkeleton = (
    <>
      <div className="rounded-2xl border border-border bg-surface-secondary/45 p-2.5">
        <SkeletonBlock className="h-3 w-20" />
        <SkeletonBlock className="mt-2.5 h-2 w-full" />
        <div className="mt-2.5 grid grid-cols-3 gap-2 border-t border-separator pt-2.5">
          <SkeletonBlock className="h-7 w-full" />
          <SkeletonBlock className="h-7 w-full" />
          <SkeletonBlock className="h-7 w-full" />
        </div>
      </div>
      <div className="min-h-[58px] rounded-2xl border border-border bg-surface-secondary/25 p-2.5">
        <SkeletonBlock className="h-3 w-20" />
        <SkeletonBlock className="mt-2 h-1.5 w-full rounded-[1px]" />
        <SkeletonBlock className="mt-2 h-3 w-36" />
      </div>
    </>
  )

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
      aria-busy={detailsLoading}
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
          {detailsLoading ? <SkeletonBlock className="h-5 w-16" /> : (
            <>
              <span ref={chipRef}>
                <Chip size="sm" variant="soft" color={stateColor}>{stateCopy}</Chip>
              </span>
              {modelCooldowns.length ? <Chip size="sm" variant="soft" color="warning">{t('partialCooling')}</Chip> : null}
            </>
          )}
        </div>
      </Card.Header>

      <Card.Content className="gap-2 px-3 pb-2">
        {detailsLoading ? detailsSkeleton : <>
        {modelCooldowns.length ? (
          <div className="flex items-start gap-2 rounded-2xl border border-warning/25 bg-warning/5 px-2.5 py-2 text-[11px] text-warning-foreground dark:text-warning">
            <span className="status-dot mt-0.5 shrink-0" data-state="warn" />
            <div className="min-w-0">
              <div className="font-medium">{t('partialCooling')}</div>
              <div className="mt-0.5 break-words text-warning-foreground/80 dark:text-warning/80">
                {modelCooldowns.slice(0, 3).map(([model, until]) => `${model} ${cooldownLabel(until)}`).join(' · ')}
                {modelCooldowns.length > 3 ? ` · +${modelCooldowns.length - 3}` : ''}
              </div>
            </div>
          </div>
        ) : null}
        <div className="rounded-2xl border border-border bg-surface-secondary/45 p-2">
          <RuntimeMeter state={state} label={t('runtimeState')} stateCopy={stateCopy} />
          <div className="mt-2 grid grid-cols-3 gap-2 border-t border-separator pt-2 text-[10px] text-foreground/65">
            <span><span className="mono block text-[12px] font-medium text-foreground">{inFlight}/{account.max_inflight ?? 4}</span>{t('inFlight')}</span>
            <span><span className="mono block text-[12px] font-medium text-foreground">{account.priority ?? 50}</span>{t('priority')}</span>
            <span><span className="mono block text-[12px] font-medium text-foreground">{account.restarts ?? 0}</span>{t('restarts')}</span>
          </div>
        </div>

        <div className="min-h-[52px] rounded-2xl border border-border bg-surface-secondary/25 p-2">
          {account.quota ? (
            <QuotaMeter
              quota={account.quota}
              label={t('quota')}
              usedLabel={t('quotaUsed')}
              remainingLabel={t('quotaRemaining')}
              addOnLabel={t('quotaAddOn')}
              exceededLabel={t('quotaExceeded')}
            />
          ) : <span className="text-[11px] text-foreground/65">{t('statsUnknown')}</span>}
        </div>

        {account.provider === 'workbuddy' ? (
          <div className="grid gap-1.5 rounded-2xl border border-border bg-surface-secondary/20 p-2">
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
            <div className="flex items-center gap-2 text-[11px]">
              <div className="flex min-w-0 items-center gap-2">
                <span className="status-dot shrink-0" data-state={checkin.state} />
                <span className="font-medium">{t('checkinStatus')}</span>
                <span className="truncate text-foreground/65">{checkin.label}</span>
              </div>
            </div>
          </div>
        ) : null}

        {lastError ? (
          <div className="flex gap-2 rounded-2xl border border-danger/25 bg-danger/5 p-2 text-xs leading-5 text-danger">
            <WarningCircle size={14} className="mt-0.5 shrink-0" />
            <div className="min-w-0">
              {errorKind ? <div className="mono mb-0.5 text-[10px] opacity-75">{errorKind}</div> : null}
              <p className="break-words">{lastError}</p>
            </div>
          </div>
        ) : null}
        </>}
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
        {onRefresh ? (
          <Tooltip>
            <Tooltip.Trigger>
              <Button isIconOnly size="sm" variant="secondary" onPress={onRefresh} aria-label={t('refreshAccount')}>
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
              <Button isIconOnly size="sm" variant="ghost" isDisabled={!account.enabled} isPending={busyKind === 'checkin'} onPress={onCheckin} aria-label={t('checkinNow')}>
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
        <div className="ml-auto flex shrink-0 items-center gap-2">
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
