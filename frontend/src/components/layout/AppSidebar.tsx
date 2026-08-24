import { useEffect, useRef, useState } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import { Drawer, Button, Chip, Tooltip } from '@heroui/react'
import { Cube, Gauge, Lightning, SidebarSimple, UsersThree, X } from '@phosphor-icons/react'
import { gsap } from 'gsap'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'

const nav = [
  { to: '/', key: 'navOverview', icon: Gauge, section: 'workspace', end: true },
  { to: '/accounts', key: 'navAccounts', icon: UsersThree, section: 'workspace' },
  { to: '/providers', key: 'navProviders', icon: Cube, section: 'develop' },
  { to: '/access', key: 'navAccess', icon: Lightning, section: 'develop' },
] as const

const COLLAPSE_KEY = 'cli2api:sidebar-collapsed'

type Props = {
  mobileOpen: boolean
  onClose: () => void
}

function NavList({ collapsed, onNavigate }: { collapsed: boolean; onNavigate?: () => void }) {
  const { t } = useI18n()
  const location = useLocation()
  const navRef = useRef<HTMLElement>(null)
  const indicatorRef = useRef<HTMLSpanElement>(null)
  const groups = [
    { key: 'workspace', label: t('workspace') },
    { key: 'develop', label: t('develop') },
  ] as const

  useEffect(() => {
    const navElement = navRef.current
    const indicator = indicatorRef.current
    if (!navElement || !indicator) return
    const active = navElement.querySelector<HTMLElement>('[aria-current="page"]')
    if (!active) {
      gsap.set(indicator, { autoAlpha: 0 })
      return
    }
    const navRect = navElement.getBoundingClientRect()
    const activeRect = active.getBoundingClientRect()
    gsap.to(indicator, {
      x: activeRect.left - navRect.left,
      y: activeRect.top - navRect.top + activeRect.height / 2 - 8,
      autoAlpha: 1,
      scaleY: 1,
      duration: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 0 : 0.3,
      ease: 'power3.out',
      overwrite: true,
    })
    return () => gsap.killTweensOf(indicator)
  }, [collapsed, location.pathname])

  return (
    <nav ref={navRef} className="relative flex flex-col gap-5">
      <span ref={indicatorRef} aria-hidden className="pointer-events-none absolute left-0 top-0 h-4 w-0.5 origin-center rounded-full bg-[var(--accent)] opacity-0" />
      {groups.map((group) => (
        <div key={group.key} className="flex flex-col gap-0.5">
          {collapsed ? (
            <span className="mx-auto mb-1 h-px w-6 bg-[var(--app-line)]" aria-hidden />
          ) : (
            <span className="px-3 pb-1.5 text-[10px] font-semibold tracking-[0.08em] text-[var(--app-faint)] uppercase">{group.label}</span>
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
                className={({ isActive }) => `group/navitem relative flex items-center gap-3 rounded-lg py-2.5 text-[15px] transition-colors will-change-transform ${collapsed ? 'mx-auto w-10 justify-center px-0' : 'px-3'} ${isActive ? 'bg-[var(--accent-soft)] font-semibold text-[var(--app-ink)]' : 'font-medium text-[var(--app-muted)] hover:bg-[var(--app-surface-muted)] hover:text-[var(--app-ink)]'}`}
                onMouseDown={(event) => {
                  if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return
                  gsap.fromTo(event.currentTarget, { scale: 0.975 }, { scale: 1, duration: 0.18, ease: 'power2.out', overwrite: true })
                }}
              >
                {({ isActive }) => (
                  <>
                    <span className="inline-flex shrink-0" data-nav-icon><Icon size={19} weight={isActive ? 'fill' : 'regular'} /></span>
                    {!collapsed && <span className="truncate" data-nav-label>{t(item.key)}</span>}
                    {collapsed && <span className="pointer-events-none absolute left-full z-50 ml-2 invisible whitespace-nowrap rounded-md bg-[var(--app-ink)] px-2 py-1 text-xs font-medium text-[var(--app-bg)] opacity-0 shadow-lg transition-opacity duration-150 group-hover/navitem:visible group-hover/navitem:opacity-100">{t(item.key)}</span>}
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
    <div className={`flex items-center gap-3 ${compact ? 'justify-center' : 'px-2'}`}>
      <div className="grid size-9 shrink-0 place-items-center rounded-lg border border-[var(--accent-line)] bg-[var(--accent-soft)] text-[var(--accent)]">
        <Lightning size={17} weight="bold" />
      </div>
      {!compact && <div><div className="text-[15px] font-semibold tracking-[-0.02em]">CLI2API</div><div className="text-[11px] text-[var(--app-faint)]">{t('controlPlane')}</div></div>}
    </div>
  )
}

export function AppSidebar({ mobileOpen, onClose }: Props) {
  const { t } = useI18n()
  const { overview } = useOverview()
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem(COLLAPSE_KEY) === '1')
  const proxyOk = Boolean(overview?.proxy?.ok)
  const workerOk = Boolean(overview?.worker?.ok)
  const healthy = proxyOk && workerOk
  const accountCount = overview?.accounts?.length ?? 0
  const hotCount = overview?.accounts?.filter((account) => account.hot).length ?? 0

  function toggleCollapsed() {
    setCollapsed((current) => {
      const next = !current
      localStorage.setItem(COLLAPSE_KEY, next ? '1' : '0')
      return next
    })
  }

  const footer = (
    <div className="mt-auto shrink-0 space-y-3 border-t border-[var(--app-line)] pt-4">
      {!collapsed && <div className="app-panel-flat rounded-lg p-3"><div className="flex items-center justify-between gap-3"><div className="flex items-center gap-2 text-sm font-medium"><span className="status-dot" data-state={healthy ? 'ok' : 'danger'} />{healthy ? t('running') : t('degraded')}</div><Chip size="sm" variant="soft" color={healthy ? 'success' : 'warning'}>{hotCount}/{accountCount}</Chip></div><div className="mt-3 grid grid-cols-2 gap-3 border-t border-[var(--app-line)] pt-3 text-xs"><div><div className="text-[var(--app-faint)]">Proxy</div><div className="mt-1 font-medium">{proxyOk ? 'online' : 'down'}</div></div><div><div className="text-[var(--app-faint)]">{t('accountCount')}</div><div className="mono mt-1 font-medium">{hotCount}/{accountCount}</div></div></div></div>}
      <div className={`flex items-center gap-2 ${collapsed ? 'flex-col' : ''}`}>
        <Tooltip>
          <Tooltip.Trigger><Button isIconOnly size="sm" variant="ghost" className={collapsed ? '' : 'hidden'} aria-label={t('runtimeSnapshot')}><span className="status-dot" data-state={healthy ? 'ok' : 'danger'} /></Button></Tooltip.Trigger>
          <Tooltip.Content>{healthy ? t('running') : t('degraded')}</Tooltip.Content>
        </Tooltip>
        <Button isIconOnly size="sm" variant="ghost" className={collapsed ? '' : 'ml-auto'} onPress={toggleCollapsed} aria-label={t('toggleSidebar')}><SidebarSimple size={16} className={collapsed ? 'rotate-180' : ''} /></Button>
      </div>
      {!collapsed && <p className="px-1 text-[11px] leading-5 text-[var(--app-faint)]">{t('sidebarFoot')}</p>}
    </div>
  )

  return (
    <>
      <aside className={`hidden h-dvh shrink-0 flex-col border-r border-[var(--app-line)] bg-[var(--app-sidebar)] text-[var(--app-ink)] transition-[width] duration-300 ease-out md:flex ${collapsed ? 'w-[76px] px-2.5 py-4' : 'w-[272px] px-4 py-4'}`}>
        <div className={`flex h-9 shrink-0 items-center ${collapsed ? 'justify-center' : 'justify-start'}`}><Brand compact={collapsed} /></div>
        <div className="mt-5 min-h-0 flex-1 overflow-y-auto overscroll-contain"><NavList collapsed={collapsed} /></div>
        {footer}
      </aside>
      <Drawer isOpen={mobileOpen} onOpenChange={(open) => { if (!open) onClose() }}>
        <Drawer.Content placement="left">
          <Drawer.Dialog className="w-72 bg-[var(--app-sidebar)] p-3 text-[var(--app-ink)]">
            <div className="flex items-center justify-between px-2 py-3"><Brand /><Button isIconOnly size="sm" variant="ghost" onPress={onClose} aria-label={t('close')}><X size={16} /></Button></div>
            <Drawer.Body className="px-0"><NavList collapsed={false} onNavigate={onClose} /></Drawer.Body>
          </Drawer.Dialog>
        </Drawer.Content>
      </Drawer>
    </>
  )
}
