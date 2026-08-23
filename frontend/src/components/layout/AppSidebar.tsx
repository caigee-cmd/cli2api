import { NavLink } from 'react-router-dom'
import { Boxes, ChevronRight, Gauge, PlugZap, Users, X } from 'lucide-react'
import { Button, Chip } from '@heroui/react'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'

const nav = [
  { to: '/', key: 'navOverview', icon: Gauge, end: true },
  { to: '/accounts', key: 'navAccounts', icon: Users },
  { to: '/providers', key: 'navProviders', icon: Boxes },
  { to: '/access', key: 'navAccess', icon: PlugZap },
] as const

export function AppSidebar({ mobileOpen, onClose }: { mobileOpen: boolean; onClose: () => void }) {
  const { t } = useI18n()
  const { overview } = useOverview()
  const proxyOk = Boolean(overview?.proxy?.ok)
  const workerOk = Boolean(overview?.worker?.ok)
  const healthy = proxyOk && workerOk
  const accountCount = overview?.accounts?.length ?? 0
  const hotCount = overview?.accounts?.filter((account) => account.hot).length ?? 0

  return (
    <>
      <button
        type="button"
        aria-label={t('menu')}
        className={`fixed inset-0 z-30 bg-black/45 backdrop-blur-[2px] lg:hidden ${mobileOpen ? 'block' : 'hidden'}`}
        onClick={onClose}
      />
      <aside
        className={`fixed inset-y-0 left-0 z-40 flex w-[248px] flex-col border-r border-[var(--app-line)] bg-[var(--app-sidebar)] px-3 py-4 transition-transform lg:sticky lg:top-0 lg:h-dvh lg:translate-x-0 ${mobileOpen ? 'translate-x-0' : '-translate-x-full'}`}
      >
        <div className="flex items-center justify-between gap-3 px-2 pb-6 pt-1">
          <div className="flex items-center gap-3">
            <div className="grid size-9 place-items-center rounded-[10px] border border-[var(--accent-line)] bg-[var(--accent-soft)] text-[var(--accent)]">
              <PlugZap size={17} strokeWidth={2.2} />
            </div>
            <div>
              <div className="text-[15px] font-semibold tracking-[-0.02em]">CLI2API</div>
              <div className="text-[11px] text-[var(--app-faint)]">{t('controlPlane')}</div>
            </div>
          </div>
          <Button isIconOnly size="sm" variant="ghost" className="lg:hidden" onPress={onClose} aria-label="Close">
            <X size={16} />
          </Button>
        </div>

        <div className="mb-3 px-3 text-[10px] font-semibold tracking-[0.14em] text-[var(--app-faint)] uppercase">{t('workspace')}</div>
        <nav className="flex flex-col gap-1">
          {nav.map((item) => {
            const Icon = item.icon
            return (
              <NavLink
                key={item.to}
                to={item.to}
                end={'end' in item ? item.end : false}
                onClick={onClose}
                className={({ isActive }) => `group flex items-center gap-3 rounded-[10px] px-3 py-2.5 text-sm font-medium ${isActive ? 'bg-[var(--app-surface-solid)] text-[var(--app-ink)] shadow-sm ring-1 ring-[var(--app-line)]' : 'text-[var(--app-muted)] hover:bg-[var(--app-surface)] hover:text-[var(--app-ink)]'}`}
              >
                {({ isActive }) => (
                  <>
                    <Icon size={16} className={isActive ? 'text-[var(--accent)]' : ''} />
                    <span className="flex-1">{t(item.key)}</span>
                    <ChevronRight size={14} className={`opacity-0 transition-opacity group-hover:opacity-50 ${isActive ? 'opacity-40' : ''}`} />
                  </>
                )}
              </NavLink>
            )
          })}
        </nav>

        <div className="mt-auto px-2 pt-6">
          <div className="app-panel-flat rounded-xl p-3.5">
            <div className="flex items-center justify-between gap-3">
              <div className="flex items-center gap-2 text-sm font-medium">
                <span className="status-dot" data-state={healthy ? 'ok' : 'danger'} />
                {healthy ? t('running') : t('degraded')}
              </div>
              <Chip size="sm" variant="soft" color={healthy ? 'success' : 'warning'}>{hotCount}/{accountCount}</Chip>
            </div>
            <div className="mt-3 grid grid-cols-2 gap-3 border-t border-[var(--app-line)] pt-3 text-xs">
              <div>
                <div className="text-[var(--app-faint)]">Proxy</div>
                <div className="mt-1 font-medium">{proxyOk ? 'online' : 'down'}</div>
              </div>
              <div>
                <div className="text-[var(--app-faint)]">{t('accountCount')}</div>
                <div className="mono mt-1 font-medium">{hotCount}/{accountCount}</div>
              </div>
            </div>
          </div>
          <p className="px-1 pb-1 pt-4 text-[11px] leading-5 text-[var(--app-faint)]">{t('sidebarFoot')}</p>
        </div>
      </aside>
    </>
  )
}
