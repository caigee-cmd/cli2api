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
  const key = routeKey[location.pathname] || 'home'
  const copy = page(key)
  // The accounts page keeps its filter bar and pager fixed and scrolls only
  // the card grid. Every other page still scrolls as one document.
  const fillsViewport = location.pathname === '/accounts'

  return (
    <div className="relative flex h-dvh overflow-hidden bg-background text-foreground">
      <div className="relative z-10 flex min-h-0 min-w-0 flex-1">
        <AppSidebar mobileOpen={mobileOpen} onClose={closeMobile} />
        <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
          <AppHeader
            title={copy.title}
            desc={copy.desc}
            onMenu={() => setMobileOpen(true)}
          />
          <main className={fillsViewport
            ? 'min-h-0 flex-1 overflow-hidden'
            : 'min-h-0 flex-1 overflow-y-auto'}
          >
            <div className={fillsViewport
              ? 'mx-auto flex h-full min-h-0 w-full max-w-[1480px] flex-col px-4 pb-3 pt-5 sm:px-6 lg:px-8 lg:pt-6'
              : 'mx-auto w-full max-w-[1480px] px-4 pb-12 pt-5 sm:px-6 lg:px-8 lg:pb-16 lg:pt-6'}
            >
              <Suspense fallback={<RouteFallback />}>
                <div key={location.pathname} className={fillsViewport ? 'flex min-h-0 flex-1 flex-col' : undefined}>
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
