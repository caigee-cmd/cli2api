import { Navigate, Outlet, useLocation } from 'react-router-dom'
import { useOverview } from '@/hooks/useOverview'
import { useApiKey } from '@/hooks/useApiKey'
import { isUnauthorized } from '@/api/client'
import { Spinner } from '@heroui/react'
import { useI18n } from '@/hooks/useI18n'

export function RequireAuth() {
  const { overview, loading, error } = useOverview()
  const { apiKey } = useApiKey()
  const { t } = useI18n()
  const location = useLocation()

  if (!apiKey) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />
  }

  if (loading && !overview) {
    return (
      <div className="grid min-h-dvh place-items-center bg-[var(--app-bg)] text-sm text-[var(--app-muted)]">
        <div className="flex items-center gap-3">
          <Spinner size="sm" />
          {t('checkingSession')}
        </div>
      </div>
    )
  }

  if (!overview && isUnauthorized(error)) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />
  }

  return <Outlet />
}
