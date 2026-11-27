import { Suspense, lazy } from 'react'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { AppLayout } from '@/components/layout/AppLayout'
import { AppErrorBoundary } from '@/components/ui/AppErrorBoundary'
import { RequireAuth } from '@/components/RequireAuth'
import { RouteFallback } from '@/components/ui/RouteFallback'
import { ApiKeyProvider } from '@/hooks/useApiKey'
import { I18nProvider } from '@/hooks/useI18n'
import { OverviewProvider } from '@/hooks/useOverview'
import { ThemeProvider } from '@/hooks/useTheme'


const LoginPage = lazy(() => import('@/pages/LoginPage').then((module) => ({ default: module.LoginPage })))

const OverviewPage = lazy(() => import('@/pages/OverviewPage').then((module) => ({ default: module.OverviewPage })))
const ProvidersPage = lazy(() => import('@/pages/ProvidersPage').then((module) => ({ default: module.ProvidersPage })))
const ModelPoolPage = lazy(() => import('@/pages/ModelPoolPage').then((module) => ({ default: module.ModelPoolPage })))
const AccessPage = lazy(() => import('@/pages/AccessPage').then((module) => ({ default: module.AccessPage })))
const AccountsPage = lazy(() => import('@/pages/AccountsPage').then((module) => ({ default: module.AccountsPage })))
const SystemPage = lazy(() => import('@/pages/SystemPage').then((module) => ({ default: module.SystemPage })))
const LogsPage = lazy(() => import('@/pages/LogsPage').then((module) => ({ default: module.LogsPage })))
const UsagePage = lazy(() => import('@/pages/UsagePage').then((module) => ({ default: module.UsagePage })))
const KeysPage = lazy(() => import('@/pages/KeysPage').then((module) => ({ default: module.KeysPage })))

export default function App() {
  return (
    <ThemeProvider>
      <I18nProvider>
        <ApiKeyProvider>
          <OverviewProvider>
            <AppErrorBoundary>
              <BrowserRouter>
                <Suspense fallback={<RouteFallback />}>
                  <Routes>
                    <Route path="/login" element={<LoginPage />} />
                    <Route element={<RequireAuth />}>
                      <Route element={<AppLayout />}>
                        <Route path="/" element={<OverviewPage />} />
                        <Route path="/auth" element={<Navigate to="/accounts" replace />} />
                        <Route path="/providers" element={<ProvidersPage />} />
                        <Route path="/model-pool" element={<ModelPoolPage />} />
                        <Route path="/access" element={<AccessPage />} />
                        <Route path="/accounts" element={<AccountsPage />} />
                        <Route path="/checkins" element={<Navigate to="/accounts" replace />} />
                        <Route path="/usage" element={<UsagePage />} />
                        <Route path="/logs" element={<LogsPage />} />
                        <Route path="/keys" element={<KeysPage />} />
                        <Route path="/system" element={<SystemPage />} />
                        <Route path="*" element={<Navigate to="/" replace />} />
                      </Route>
                    </Route>
                  </Routes>
                </Suspense>
              </BrowserRouter>
            </AppErrorBoundary>
          </OverviewProvider>
        </ApiKeyProvider>
      </I18nProvider>
    </ThemeProvider>
  )
}
