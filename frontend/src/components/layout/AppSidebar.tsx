import { NavLink } from 'react-router-dom'
import { Boxes, Home, PlugZap, Users, X } from 'lucide-react'
import { Button, Chip } from '@heroui/react'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'

const nav = [
  { to: '/', key: 'navOverview', icon: Home, end: true },
  { to: '/accounts', key: 'navAccounts', icon: Users },
  { to: '/providers', key: 'navProviders', icon: Boxes },
  { to: '/access', key: 'navAccess', icon: PlugZap },
] as const

export function AppSidebar({
  mobileOpen,
  onClose,
}: {
  mobileOpen: boolean
  onClose: () => void
}) {
  const { t } = useI18n()
  const { overview } = useOverview()
  const proxyOk = !!overview?.proxy?.ok
  const workerOk = !!overview?.worker?.ok
  const healthy = proxyOk && workerOk

  return (
    <>
      <div
        className={`fixed inset-0 z-30 bg-black/50 lg:hidden ${mobileOpen ? 'block' : 'hidden'}`}
        onClick={onClose}
      />
      <aside
        className={`fixed inset-y-0 left-0 z-40 flex w-72 flex-col border-r border-white/10 bg-[#12171d]/95 px-4 py-5 backdrop-blur-md transition-transform lg:static lg:translate-x-0 ${
          mobileOpen ? 'translate-x-0' : '-translate-x-full'
        }`}
      >
        <div className="mb-6 flex items-center justify-between gap-3 px-2">
          <div className="flex items-center gap-3">
            <div className="grid size-10 place-items-center rounded-xl bg-emerald-500/15 text-emerald-400 ring-1 ring-emerald-500/30">
              <PlugZap size={18} />
            </div>
            <div>
              <div className="text-base font-semibold tracking-tight">CLI2API</div>
              <div className="text-xs text-zinc-400">{t('brandSub')}</div>
            </div>
          </div>
          <Button isIconOnly size="sm" variant="ghost" className="lg:hidden" onPress={onClose}>
            <X size={16} />
          </Button>
        </div>

        <nav className="flex flex-1 flex-col gap-1">
          {nav.map((item) => {
            const Icon = item.icon
            return (
              <NavLink
                key={item.to}
                to={item.to}
                end={'end' in item ? item.end : false}
                onClick={onClose}
                className={({ isActive }) =>
                  `flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm transition ${
                    isActive
                      ? 'bg-emerald-500/15 text-white ring-1 ring-emerald-500/25'
                      : 'text-zinc-400 hover:bg-white/5 hover:text-white'
                  }`
                }
              >
                <Icon size={16} />
                <span>{t(item.key)}</span>
              </NavLink>
            )
          })}
        </nav>

        <div className="mt-auto space-y-3 border-t border-white/10 pt-4">
          <Chip
            size="sm"
            variant="soft"
            color={healthy ? 'success' : 'warning'}
            className="w-full justify-start"
          >
            {healthy ? t('running') : t('degraded')}
          </Chip>
          <div className="px-1 text-xs text-zinc-500">{t('sidebarFoot')}</div>
        </div>
      </aside>
    </>
  )
}
