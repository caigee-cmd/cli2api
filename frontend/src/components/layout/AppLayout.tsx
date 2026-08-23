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
    <div className="relative min-h-dvh bg-[var(--app-bg)] text-[var(--app-ink)]">
      <div className="noise" aria-hidden />
      <div className="relative z-10 grid min-h-dvh lg:grid-cols-[248px_minmax(0,1fr)]">
        <AppSidebar mobileOpen={mobileOpen} onClose={() => setMobileOpen(false)} />
        <div className="min-w-0">
          <AppHeader
            title={copy.title}
            desc={copy.desc}
            onMenu={() => setMobileOpen(true)}
          />
          <main className="mx-auto w-full max-w-[1480px] px-4 pb-12 pt-5 sm:px-6 lg:px-8 lg:pb-16 lg:pt-7">
            <div className="page-enter" key={location.pathname}>
              <Outlet />
            </div>
          </main>
        </div>
      </div>
    </div>
  )
}
