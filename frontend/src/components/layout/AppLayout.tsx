import { Suspense, useCallback, useState } from 'react'
import { Outlet, useLocation } from 'react-router-dom'
import { AppHeader } from './AppHeader'
import { AppSidebar } from './AppSidebar'
import { RouteFallback } from '@/components/ui/RouteFallback'
import { useI18n } from '@/hooks/useI18n'
import { Toaster } from '@/components/motion/Toaster'

const routeKey: Record<string, string> = {
  '/': 'home',
  '/auth': 'auth',
  '/providers': 'providers',
  '/model-pool': 'modelPool',
  '/access': 'access',
  '/accounts': 'accounts',
  '/usage': 'usage',
  '/logs': 'logs',
  '/keys': 'keys',
  '/system': 'system',
}

export function AppLayout() {
  const [mobileOpen, setMobileOpen] = useState(false)
  const closeMobile = useCallback(() => setMobileOpen(false), [])
  const location = useLocation()
  const { page } = useI18n()
  const key = location.pathname === '/model-pool' || location.pathname.startsWith('/model-pool/')
    ? 'modelPool'
    : routeKey[location.pathname] || 'home'
  const copy = page(key)
  const wide = location.pathname.startsWith('/model-pool/')

  return (
    <div className="relative flex h-dvh overflow-hidden bg-background text-foreground">
      <div className="relative z-10 flex min-h-0 min-w-0 flex-1">
        <AppSidebar mobileOpen={mobileOpen} onClose={closeMobile} />
        <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
          <AppHeader
            title={copy.title}
            desc={copy.desc}
            wide={wide}
            onMenu={() => setMobileOpen(true)}
          />
          <main className="min-h-0 flex-1 overflow-y-auto">
            <div className={wide
              ? 'w-full px-3 pb-6 pt-3 sm:px-4 lg:px-5 lg:pb-8 lg:pt-4'
              : 'mx-auto w-full max-w-[1480px] px-4 pb-12 pt-5 sm:px-6 lg:px-8 lg:pb-16 lg:pt-6'}>
              <Suspense fallback={<RouteFallback />}>
                <div key={location.pathname}>
                  <Outlet />
                </div>
              </Suspense>
            </div>
          </main>
        </div>
      </div>
      <Toaster />
    </div>
  )
}
