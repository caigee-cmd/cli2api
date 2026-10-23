import { useEffect, useState } from 'react'
import { NavLink } from 'react-router-dom'
import { Drawer, Button, Chip, Separator, Tooltip } from '@heroui/react'
import { ChartBar, Cube, Gauge, GearSix, Key, Lightning, Scroll, SidebarSimple, UsersThree, X } from '@phosphor-icons/react'
import { SidebarUpdateItem } from '@/components/layout/SidebarUpdateItem'
import { BrandMark } from '@/components/BrandMark'
import { SkeletonBlock } from '@/components/ui/PageSkeletons'
import { pressScale } from '@/hooks/useGsapReveal'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import { fetchLive } from '@/api/live'

const nav = [
  { to: '/', key: 'navOverview', icon: Gauge, section: 'workspace', end: true },
  { to: '/usage', key: 'navUsage', icon: ChartBar, section: 'workspace' },
  { to: '/accounts', key: 'navAccounts', icon: UsersThree, section: 'workspace' },
  { to: '/providers', key: 'navProviders', icon: Cube, section: 'develop' },
  { to: '/access', key: 'navAccess', icon: Lightning, section: 'develop' },
  { to: '/logs', key: 'navLogs', icon: Scroll, section: 'system' },
  { to: '/keys', key: 'navKeys', icon: Key, section: 'system' },
  { to: '/system', key: 'navSystem', icon: GearSix, section: 'system' },
] as const

const COLLAPSE_KEY = 'cli2api:sidebar-collapsed'

type Props = {
  mobileOpen: boolean
  onClose: () => void
}

function NavList({ collapsed, onNavigate }: { collapsed: boolean; onNavigate?: () => void }) {
  const { t } = useI18n()
  const groups = [
    { key: 'workspace', label: t('workspace') },
    { key: 'develop', label: t('develop') },
    { key: 'system', label: t('system') },
  ] as const

  return (
    <nav className="flex flex-col gap-6">
      {groups.map((group) => (
        <div key={group.key} className="flex flex-col gap-1">
          {collapsed ? (
            <Separator className="mx-auto mb-1.5 w-6 opacity-60" />
          ) : (
            <span className="px-3 pb-1.5 text-caption font-medium text-muted">{group.label}</span>
          )}
          {nav.filter((item) => item.section === group.key).map((item) => {
            const Icon = item.icon
            return (
              <NavLink
                key={item.to}
                to={item.to}
                end={'end' in item ? item.end : false}
                onClick={onNavigate}
                aria-label={t(item.key)}
                title={collapsed ? t(item.key) : undefined}
                data-sidebar-item
                className={({ isActive }) => `group/navitem relative flex h-10 items-center gap-3 rounded-[10px] text-nav transition-colors will-change-transform ${collapsed ? 'mx-auto w-10 justify-center px-0' : 'px-3'} ${isActive ? 'sidebar-item-active font-medium text-foreground' : 'font-normal text-muted hover:text-foreground'}`}
                onMouseDown={(event) => pressScale(event.currentTarget)}
              >
                {({ isActive }) => (
                  <>
                    <span className="inline-flex shrink-0" data-nav-icon><Icon size={20} weight={isActive ? 'fill' : 'regular'} /></span>
                    {!collapsed && <span className="truncate" data-nav-label>{t(item.key)}</span>}
                    {collapsed && <span className="pointer-events-none absolute left-full z-50 ml-2 invisible whitespace-nowrap rounded-lg bg-foreground px-2 py-1 text-caption font-medium text-background opacity-0 shadow-overlay transition-opacity duration-150 group-hover/navitem:visible group-hover/navitem:opacity-100">{t(item.key)}</span>}
                  </>
                )}
              </NavLink>
            )
          })}
        </div>
      ))}
    </nav>
  )
}

function Brand({ compact = false }: { compact?: boolean }) {
  const { t } = useI18n()
  return (
    <div className={`flex min-w-0 items-center gap-3 ${compact ? 'justify-center' : 'px-1.5'}`}>
      <BrandMark size={36} />
      {!compact && (
        <div className="min-w-0 leading-tight">
          <div className="text-section font-semibold tracking-tight">CLI2API</div>
          <div className="mt-0.5 truncate text-caption text-muted">{t('controlPlane')}</div>
        </div>
      )}
    </div>
  )
}

function SidebarStatusSkeleton({ label }: { label: string }) {
  return (
    <div className="rounded-xl bg-surface shadow-surface p-3" aria-busy="true" aria-label={label}>
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <SkeletonBlock className="size-2" />
          <SkeletonBlock className="h-4 w-12" />
        </div>
        <SkeletonBlock className="h-5 w-10" />
      </div>
      <div className="mt-3 grid grid-cols-2 gap-3 pt-3">
        <div>
          <SkeletonBlock className="h-3 w-10" />
          <SkeletonBlock className="mt-1.5 h-4 w-12" />
        </div>
        <div>
          <SkeletonBlock className="h-3 w-8" />
          <SkeletonBlock className="mt-1.5 h-4 w-10" />
        </div>
      </div>
    </div>
  )
}

const IN_FLIGHT_POLL_MS = 2500

/**
 * Live in-flight request count from /api/live (same source as the overview
 * strip). Polls every 2.5s only while the tab is visible; the dot is muted
 * at 0 and pulses green while requests are running.
 */
function InFlightIndicator({ initial, collapsed }: { initial: number; collapsed: boolean }) {
  const { t } = useI18n()
  const [count, setCount] = useState<number | null>(null)
  const value = count ?? initial

  useEffect(() => {
    let cancelled = false
    let timer: number | undefined
    const poll = () => {
      void fetchLive()
        .then((data) => { if (!cancelled) setCount(data.in_flight ?? 0) })
        .catch(() => { /* keep last value */ })
    }
    const start = () => {
      if (timer != null || document.hidden) return
      poll()
      timer = window.setInterval(poll, IN_FLIGHT_POLL_MS)
    }
    const stop = () => {
      if (timer != null) window.clearInterval(timer)
      timer = undefined
    }
    const onVisibility = () => (document.hidden ? stop() : start())
    start()
    document.addEventListener('visibilitychange', onVisibility)
    return () => {
      cancelled = true
      stop()
      document.removeEventListener('visibilitychange', onVisibility)
    }
  }, [])

  const label = t('sidebarInFlight', { count: value })
  return (
    <Tooltip delay={200}>
      <Tooltip.Trigger>
        <span
          role="status"
          aria-label={label}
          className={`inline-flex h-8 items-center gap-2 rounded-lg text-caption text-muted ${collapsed ? 'w-10 justify-center' : 'px-3'}`}
        >
          <span className="status-dot" data-state={value > 0 ? 'ok' : undefined} />
          <span className="mono tabular text-foreground">{value}</span>
        </span>
      </Tooltip.Trigger>
      <Tooltip.Content>{label}</Tooltip.Content>
    </Tooltip>
  )
}

export function AppSidebar({ mobileOpen, onClose }: Props) {
  const { t } = useI18n()
  const { overview, loading } = useOverview()
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem(COLLAPSE_KEY) === '1')
  const proxyOk = Boolean(overview?.proxy?.ok)
  const workerOk = Boolean(overview?.worker?.ok)
  const healthy = proxyOk && workerOk
  const accountCount = overview?.worker?.account_count ?? 0
  const hotCount = overview?.worker?.hot_count ?? 0
  const showStatusSkeleton = loading

  function toggleCollapsed() {
    setCollapsed((current) => {
      const next = !current
      localStorage.setItem(COLLAPSE_KEY, next ? '1' : '0')
      return next
    })
  }

  const toggle = (
    <Button isIconOnly size="sm" variant="ghost" className="text-muted" onPress={toggleCollapsed} aria-label={t('toggleSidebar')}>
      <SidebarSimple size={18} className={collapsed ? 'rotate-180' : ''} />
    </Button>
  )

  const footer = (
    <div className="mt-auto shrink-0 space-y-2 pt-6">
      {!collapsed && (
        showStatusSkeleton ? (
          <SidebarStatusSkeleton label={t('refreshing')} />
        ) : (
          <div className="sidebar-status rounded-xl px-3.5 py-3">
            <div className="flex items-center justify-between gap-3">
              <div className="flex items-center gap-2 text-nav font-medium">
                <span className="status-dot" data-state={healthy ? 'ok' : 'danger'} />
                {healthy ? t('running') : t('degraded')}
              </div>
              <Chip size="sm" variant="soft" color={healthy ? 'success' : 'warning'}>{hotCount}/{accountCount}</Chip>
            </div>
            <div className="mt-2 flex items-center gap-1.5 text-caption text-muted">
              <span>Proxy</span>
              <span className="font-medium text-foreground">{proxyOk ? 'online' : 'down'}</span>
              <span aria-hidden>·</span>
              <span>{t('accountCount')}</span>
              <span className="mono font-medium text-foreground">{hotCount}/{accountCount}</span>
            </div>
          </div>
        )
      )}
      {collapsed ? (
        <div className="flex justify-center">
        <Tooltip>
          <Tooltip.Trigger>
            <Button isIconOnly size="sm" variant="ghost" aria-label={t('runtimeSnapshot')}>
              {showStatusSkeleton ? <SkeletonBlock className="size-2" /> : <span className="status-dot" data-state={healthy ? 'ok' : 'danger'} />}
            </Button>
          </Tooltip.Trigger>
          <Tooltip.Content>{showStatusSkeleton ? t('refreshing') : healthy ? t('running') : t('degraded')}</Tooltip.Content>
        </Tooltip>
        </div>
      ) : null}
      <SidebarUpdateItem collapsed={collapsed} />
      <div className={`flex items-center ${collapsed ? 'flex-col gap-1' : 'justify-between gap-2'}`}>
        <InFlightIndicator initial={overview?.worker?.in_flight ?? 0} collapsed={collapsed} />
        {!collapsed && <p className="min-w-0 truncate pr-3 text-caption text-muted" title={t('sidebarFoot')}>{t('sidebarFoot')}</p>}
      </div>
    </div>
  )

  return (
    <>
      <aside className={`hidden h-dvh shrink-0 flex-col bg-surface-secondary text-foreground transition-[width] duration-300 ease-out md:flex ${collapsed ? 'w-[76px] px-2.5 py-5' : 'w-[260px] px-4 py-5'}`}>
        <div className={`flex shrink-0 ${collapsed ? 'flex-col items-center gap-3' : 'h-12 items-center justify-between gap-2'}`}>
          <Brand compact={collapsed} />
          {toggle}
        </div>
        <div className={`${collapsed ? 'mt-5' : 'mt-7'} min-h-0 flex-1 overflow-y-auto overscroll-contain`}><NavList collapsed={collapsed} /></div>
        {footer}
      </aside>
      <Drawer isOpen={mobileOpen} onOpenChange={(open) => { if (!open) onClose() }}>
        <Drawer.Content placement="left">
          <Drawer.Dialog className="w-72 bg-surface-secondary px-4 py-5 text-foreground">
            <div className="flex h-12 items-center justify-between"><Brand /><Button isIconOnly size="sm" variant="ghost" onPress={onClose} aria-label={t('close')}><X size={16} /></Button></div>
            <Drawer.Body className="mt-5 px-0"><NavList collapsed={false} onNavigate={onClose} /></Drawer.Body>
          </Drawer.Dialog>
        </Drawer.Content>
      </Drawer>
    </>
  )
}
