import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { AppLayout } from '@/components/layout/AppLayout'
import { RequireAuth } from '@/components/RequireAuth'
import { ApiKeyProvider } from '@/hooks/useApiKey'
import { I18nProvider } from '@/hooks/useI18n'
import { OverviewProvider } from '@/hooks/useOverview'
import { ThemeProvider } from '@/hooks/useTheme'
import { OverviewPage } from '@/pages/OverviewPage'
import { ProvidersPage } from '@/pages/ProvidersPage'
import { AccessPage } from '@/pages/AccessPage'
import { AccountsPage } from '@/pages/AccountsPage'
import { LoginPage } from '@/pages/LoginPage'
import { SystemPage } from '@/pages/SystemPage'
import { LogsPage } from '@/pages/LogsPage'

export default function App() {
  return (
    <ThemeProvider>
      <I18nProvider>
        <ApiKeyProvider>
          <OverviewProvider>
            <BrowserRouter>
              <Routes>
                <Route path="/login" element={<LoginPage />} />
                <Route element={<RequireAuth />}>
                  <Route element={<AppLayout />}>
                    <Route path="/" element={<OverviewPage />} />
                    <Route path="/auth" element={<Navigate to="/accounts" replace />} />
                    <Route path="/providers" element={<ProvidersPage />} />
                    <Route path="/access" element={<AccessPage />} />
                    <Route path="/accounts" element={<AccountsPage />} />
                    <Route path="/logs" element={<LogsPage />} />
                    <Route path="/system" element={<SystemPage />} />
                    <Route path="*" element={<Navigate to="/" replace />} />
                  </Route>
                </Route>
              </Routes>
            </BrowserRouter>
          </OverviewProvider>
        </ApiKeyProvider>
      </I18nProvider>
    </ThemeProvider>
  )
}
