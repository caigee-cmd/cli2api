import { useState } from 'react'
import { Outlet, useLocation } from 'react-router-dom'
import { AppHeader } from './AppHeader'
import { AppSidebar } from './AppSidebar'
import { useI18n } from '@/hooks/useI18n'

const routeKey: Record<string, string> = {
  '/': 'home',
  '/auth': 'auth',
  '/providers': 'providers',
  '/access': 'access',
  '/accounts': 'accounts',
}

export function AppLayout() {
  const [mobileOpen, setMobileOpen] = useState(false)
  const location = useLocation()
  const { page } = useI18n()
  const key = routeKey[location.pathname] || 'home'
  const copy = page(key)

  return (
    <div className="relative min-h-dvh bg-[#0b0f14] text-zinc-100">
      <div className="noise" aria-hidden />
      <div className="relative z-10 flex min-h-dvh">
        <AppSidebar mobileOpen={mobileOpen} onClose={() => setMobileOpen(false)} />
        <div className="flex min-w-0 flex-1 flex-col">
          <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-5 sm:px-6 lg:px-8">
            <AppHeader
              kicker={copy.kicker}
              title={copy.title}
              desc={copy.desc}
              onMenu={() => setMobileOpen(true)}
            />
            <Outlet />
          </main>
        </div>
      </div>
    </div>
  )
}
