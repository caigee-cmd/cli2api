import { useEffect, useState } from 'react'
import { NavLink } from 'react-router-dom'
import { ChartBar, Cube, Gauge, GearSix, Key, Lightning, Scroll, SidebarSimple, UsersThree, X } from '@phosphor-icons/react'
import { SidebarUpdateItem } from '@/components/layout/SidebarUpdateItem'
import { BrandMark } from '@/components/BrandMark'
import { SkeletonBlock } from '@/components/ui/SkeletonBlock'
import { ShellChip, ShellIconButton, ShellSeparator, ShellTooltip } from '@/components/ui/ShellControls'
import { pressScale } from '@/hooks/useGsapReveal'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'

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
            <ShellSeparator className="mx-auto mb-1.5 w-6 opacity-60" />
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

/**
 * In-flight count shown in the sidebar. The number comes from the overview
 * summary, which loads with the shell. Polling /api/live here would hit it on
 * every page, including ones that never render the live strip.
 */
function InFlightIndicator({ initial, collapsed }: { initial: number; collapsed: boolean }) {
  const { t } = useI18n()
  const value = initial

  const label = t('sidebarInFlight', { count: value })
  return (
    <ShellTooltip label={label} side="top">
      <span
        role="status"
        aria-label={label}
        className={`inline-flex h-8 items-center gap-2 rounded-lg text-caption text-muted ${collapsed ? 'w-10 justify-center' : 'px-3'}`}
      >
        <span className="status-dot" data-state={value > 0 ? 'ok' : undefined} />
        <span className="mono tabular text-foreground">{value}</span>
      </span>
    </ShellTooltip>
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

  useEffect(() => {
    if (!mobileOpen) return
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.body.style.overflow = previousOverflow
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [mobileOpen, onClose])

  useEffect(() => {
    if (!mobileOpen) return
    const media = window.matchMedia('(min-width: 1024px)')
    const onChange = () => {
      if (media.matches) onClose()
    }
    media.addEventListener('change', onChange)
    return () => media.removeEventListener('change', onChange)
  }, [mobileOpen, onClose])

  const toggle = (
    <ShellIconButton label={t('toggleSidebar')} onClick={toggleCollapsed}>
      <SidebarSimple size={18} className={collapsed ? 'rotate-180' : ''} />
    </ShellIconButton>
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
              <ShellChip tone={healthy ? 'success' : 'warning'}>{hotCount}/{accountCount}</ShellChip>
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
        <ShellTooltip label={showStatusSkeleton ? t('refreshing') : healthy ? t('running') : t('degraded')} side="top">
          <ShellIconButton label={showStatusSkeleton ? t('refreshing') : healthy ? t('running') : t('degraded')}>
            {showStatusSkeleton ? <SkeletonBlock className="size-2" /> : <span className="status-dot" data-state={healthy ? 'ok' : 'danger'} />}
          </ShellIconButton>
        </ShellTooltip>
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
      {mobileOpen ? (
        <div className="fixed inset-0 z-50 lg:hidden">
          <div className="absolute inset-0 bg-black/35 backdrop-blur-[2px]" onClick={onClose} aria-hidden="true" />
          <aside
            role="dialog"
            aria-modal="true"
            aria-label={t('menu')}
            className="absolute left-0 top-0 flex h-dvh w-72 flex-col bg-surface-secondary px-4 py-5 text-foreground shadow-overlay"
          >
            <div className="flex h-12 items-center justify-between">
              <Brand />
              <ShellIconButton label={t('close')} onClick={onClose}>
                <X size={16} />
              </ShellIconButton>
            </div>
            <div className="mt-5 min-h-0 flex-1 overflow-y-auto overscroll-contain">
              <NavList collapsed={false} onNavigate={onClose} />
            </div>
          </aside>
        </div>
      ) : null}
    </>
  )
}
