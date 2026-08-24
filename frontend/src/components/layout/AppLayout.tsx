import { useRef, useState } from 'react'
import { Outlet, useLocation } from 'react-router-dom'
import { AppHeader } from './AppHeader'
import { AppSidebar } from './AppSidebar'
import { useGsapReveal } from '@/hooks/useGsapReveal'
import { useI18n } from '@/hooks/useI18n'

const routeKey: Record<string, string> = {
  '/': 'home',
  '/auth': 'auth',
  '/providers': 'providers',
  '/access': 'access',
  '/accounts': 'accounts',
  '/system': 'system',
}

export function AppLayout() {
  const [mobileOpen, setMobileOpen] = useState(false)
  const location = useLocation()
  const { page } = useI18n()
  const key = routeKey[location.pathname] || 'home'
  const copy = page(key)
  const pageRef = useRef<HTMLDivElement>(null)
  useGsapReveal(pageRef, location.pathname)

  return (
    <div className="relative flex h-dvh overflow-hidden bg-[var(--app-bg)] text-[var(--app-ink)]">
      <div className="noise" aria-hidden />
      <div className="relative z-10 flex min-h-0 min-w-0 flex-1">
        <AppSidebar mobileOpen={mobileOpen} onClose={() => setMobileOpen(false)} />
        <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
          <AppHeader
            title={copy.title}
            desc={copy.desc}
            onMenu={() => setMobileOpen(true)}
          />
          <main className="min-h-0 flex-1 overflow-y-auto">
            <div className="mx-auto w-full max-w-[1480px] px-4 pb-12 pt-5 sm:px-6 lg:px-8 lg:pb-16 lg:pt-6">
              <div ref={pageRef} key={location.pathname}>
                <Outlet />
              </div>
            </div>
          </main>
        </div>
      </div>
    </div>
  )
}
