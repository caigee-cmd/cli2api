import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { AppLayout } from '@/components/layout/AppLayout'
import { I18nProvider } from '@/hooks/useI18n'
import { OverviewProvider } from '@/hooks/useOverview'
import { OverviewPage } from '@/pages/OverviewPage'
import { AuthPage } from '@/pages/AuthPage'
import { ProvidersPage } from '@/pages/ProvidersPage'
import { AccessPage } from '@/pages/AccessPage'

export default function App() {
  return (
    <I18nProvider>
      <OverviewProvider>
        <BrowserRouter>
          <Routes>
            <Route element={<AppLayout />}>
              <Route path="/" element={<OverviewPage />} />
              <Route path="/auth" element={<AuthPage />} />
              <Route path="/providers" element={<ProvidersPage />} />
              <Route path="/access" element={<AccessPage />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Route>
          </Routes>
        </BrowserRouter>
      </OverviewProvider>
    </I18nProvider>
  )
}
